package spaceuserconfig

import (
	"context"
	"fmt"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/host-operator/pkg/cluster"

	errs "github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
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
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager, memberClusters map[string]cluster.Cluster) error {
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

	// check if community label is correctly set on space,
	// if not, update the label and requeue space reconciliation
	if err := r.ensureCommunitySpaceBindingExistsIfNeeded(ctx, cfg); err != nil {
		return ctrl.Result{}, err
	}
	logger.Info("SpaceUserConfig reconciled")
	return ctrl.Result{}, nil
}

// Ensures the community label is set on the space if an SBR for special user public-viewer is found.
// If any changes is made to the space, this function returns `true` as first value, otherwise `false`.
func (r *Reconciler) ensureCommunitySpaceBindingExistsIfNeeded(ctx context.Context, cfg *toolchainv1alpha1.SpaceUserConfig) error {
	n := fmt.Sprintf("public-viewer:%s", cfg.Name)
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
