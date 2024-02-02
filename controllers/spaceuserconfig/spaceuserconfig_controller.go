package spaceuserconfig

import (
	"context"
	"fmt"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"

	errs "github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Reconciler reconciles a Space object
type Reconciler struct {
	Client    runtimeclient.Client
	Namespace string
}

// SetupWithManager sets up the controller reconciler with the Manager and the given member clusters.
// Watches the Space resources in the current (host) cluster as its primary resources.
// Watches NSTemplateSets on the member clusters as its secondary resources.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr).
		// watch Spaces in the host cluster
		For(&toolchainv1alpha1.SpaceUserConfig{}, builder.WithPredicates(predicate.GenerationChangedPredicate{}))
	return b.Complete(r)
}

//+kubebuilder:rbac:groups=toolchain.dev.openshift.com,resources=spaceuserconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=toolchain.dev.openshift.com,resources=spaceuserconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=toolchain.dev.openshift.com,resources=spaceuserconfigs/finalizers,verbs=update

// Reconcile ensures that there is an NSTemplateSet resource defined in the target member cluster
func (r *Reconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("spaceuserconfig", request.Name)
	logger.Info("reconciling SpaceUserConfig")

	// Fetch the Space
	cfg := &toolchainv1alpha1.SpaceUserConfig{}
	err := r.Client.Get(ctx, types.NamespacedName{
		Namespace: r.Namespace,
		Name:      request.Name,
	}, cfg)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("SpaceUserConfig not found")
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, errs.Wrap(err, "unable to get the current SpaceUserConfig")
	}

	if err := r.updateStatus(ctx, cfg, r.reconcile(ctx, cfg)); err != nil {
		logger.Info("error reconciling SpaceUserConfig", "error", err)
		return ctrl.Result{}, err
	}

	logger.Info("SpaceUserConfig reconciled")
	return ctrl.Result{}, nil
}

func (r *Reconciler) updateStatus(ctx context.Context, cfg *toolchainv1alpha1.SpaceUserConfig, err error) error {
	c := toolchainv1alpha1.Condition{
		Type:   toolchainv1alpha1.ConditionReady,
		Status: corev1.ConditionTrue,
		Reason: "reconciled successfully",
	}
	if err != nil {
		c.Status = corev1.ConditionFalse
		c.Reason = err.Error()
	}
	cfg.Status.Conditions = append(cfg.Status.Conditions, c)
	if err := r.Client.Status().Update(ctx, cfg); err != nil {
		return err
	}

	return err
}

func (r *Reconciler) reconcile(ctx context.Context, cfg *toolchainv1alpha1.SpaceUserConfig) error {
	// create/update the role and rolebinding
	// needed for giving the space owner the right
	// to update the space user config
	if err := r.ensureOwnerIsProvidedEditAccess(ctx, cfg); err != nil {
		return err
	}

	// check if community label is correctly set on space,
	// if not, update the label and requeue space reconciliation
	if err := r.ensureCommunitySpaceBindingExistsIfNeeded(ctx, cfg); err != nil {
		return err
	}

	return nil
}

func (r *Reconciler) ensureOwnerIsProvidedEditAccess(ctx context.Context, cfg *toolchainv1alpha1.SpaceUserConfig) error {
	s := toolchainv1alpha1.Space{}
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(cfg), &s); err != nil {
		return err
	}

	if s.Labels == nil {
		return fmt.Errorf("owner label not found on space %s", s.Name)
	}

	c, ok := s.Labels[toolchainv1alpha1.SpaceCreatorLabelKey]
	if !ok {
		return fmt.Errorf("owner label not found on space %s", s.Name)
	}

	// create or update the role
	ro := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s:owner", cfg.Name),
			Namespace: cfg.Namespace,
		},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, ro, func() error {
		ro.Rules = []rbacv1.PolicyRule{
			{
				APIGroups:     []string{"toolchain.dev.openshift.com"},
				Resources:     []string{"spaceuserconfigs"},
				ResourceNames: []string{s.Name},
				Verbs:         []string{"get", "list", "watch", "update"},
			},
		}

		return controllerutil.SetOwnerReference(cfg, ro, r.Client.Scheme())
	}); err != nil {
		return fmt.Errorf("error creating or updating role %v: %v", client.ObjectKeyFromObject(ro), err)
	}

	// create or update role binding
	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s:owner", cfg.Name),
			Namespace: cfg.Namespace,
		},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, rb, func() error {
		rb.Subjects = []rbacv1.Subject{
			{
				Kind:     "User",
				Name:     c,
				APIGroup: "",
			},
		}
		rb.RoleRef = rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     ro.Name,
		}

		return controllerutil.SetOwnerReference(cfg, rb, r.Client.Scheme())
	}); err != nil {
		return fmt.Errorf("error creating or updating rolebinding %v: %v", client.ObjectKeyFromObject(rb), err)
	}
	return nil
}

// Ensures the community label is set on the space if an SBR for special user public-viewer is found.
// If any changes is made to the space, this function returns `true` as first value, otherwise `false`.
func (r *Reconciler) ensureCommunitySpaceBindingExistsIfNeeded(ctx context.Context, cfg *toolchainv1alpha1.SpaceUserConfig) error {
	n := fmt.Sprintf("public-viewer-%s", cfg.Name)
	sb := toolchainv1alpha1.SpaceBinding{}
	t := types.NamespacedName{Namespace: cfg.Namespace, Name: n}
	err := r.Client.Get(ctx, t, &sb)

	switch {
	// SpaceBinding not found
	case err != nil && errors.IsNotFound(err):
		if cfg.Spec.Visibility == toolchainv1alpha1.SpaceVisibilityPrivate {
			return nil
		}

		csb := &toolchainv1alpha1.SpaceBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      n,
				Namespace: cfg.Namespace,
				Labels: map[string]string{
					toolchainv1alpha1.SpaceBindingMasterUserRecordLabelKey: "public-viewer",
					toolchainv1alpha1.SpaceBindingSpaceLabelKey:            cfg.Name,
				},
			},
			Spec: toolchainv1alpha1.SpaceBindingSpec{
				MasterUserRecord: "public-viewer",
				Space:            cfg.Name,
				SpaceRole:        "viewer",
			},
		}
		if err := r.Client.Create(ctx, csb); err != nil && !errors.IsAlreadyExists(err) {
			return err
		}
		return nil

	case err != nil && !errors.IsNotFound(err):
		return err

		// Space Binding found
	default:
		if cfg.Spec.Visibility == toolchainv1alpha1.SpaceVisibilityCommunity {
			return nil
		}

		if err := r.Client.Delete(ctx, &toolchainv1alpha1.SpaceBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      n,
				Namespace: cfg.Namespace,
			},
		}); err != nil && !errors.IsNotFound(err) {
			return err
		}
		return nil
	}
}
