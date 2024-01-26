package spaceuserconfig_test

import (
	"context"
	"os"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/host-operator/controllers/spaceuserconfig"
	space "github.com/codeready-toolchain/host-operator/controllers/spaceuserconfig"
	"github.com/codeready-toolchain/host-operator/pkg/apis"
	tiertest "github.com/codeready-toolchain/host-operator/test/nstemplatetier"
	spacebindingtest "github.com/codeready-toolchain/host-operator/test/spacebinding"
	"github.com/codeready-toolchain/toolchain-common/pkg/test"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestVisibility(t *testing.T) {
	logf.SetLogger(zap.New(zap.UseDevMode(true)))
	err := apis.AddToScheme(scheme.Scheme)
	require.NoError(t, err)
	base1nsTier := tiertest.Base1nsTier(t, tiertest.CurrentBase1nsTemplates)

	t.Run("community SpaceBinding for user public-viewer is created for community space", func(t *testing.T) {
		// given
		cfg := &toolchainv1alpha1.SpaceUserConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "oddity",
				Namespace: test.HostOperatorNs,
			},
			Spec: toolchainv1alpha1.SpaceUserConfigSpec{
				Visibility: toolchainv1alpha1.SpaceVisibilityCommunity,
			},
		}

		hostClient := test.NewFakeClient(t, cfg, base1nsTier)
		ctrl := newReconciler(hostClient)

		// when
		_, err := ctrl.Reconcile(context.TODO(), requestFor(cfg))

		// then
		require.NoError(t, err)
		spacebindingtest.AssertThatSpaceBinding(t, cfg.Namespace, "public-viewer", cfg.Name, hostClient).Exists()

		t.Run("SpaceBinding for user public-viewer is deleted when community space is set as private", func(t *testing.T) {
			// given
			cfg = cfg.DeepCopy()
			cfg.Spec.Visibility = toolchainv1alpha1.SpaceVisibilityPrivate

			hostClient := test.NewFakeClient(t, cfg, base1nsTier)
			ctrl := newReconciler(hostClient)

			// when
			_, err := ctrl.Reconcile(context.TODO(), requestFor(cfg))

			// then
			require.NoError(t, err)
			spacebindingtest.AssertThatSpaceBinding(t, cfg.Namespace, "public-viewer", cfg.Name, hostClient).DoesNotExist()
		})
	})
}

func newReconciler(hostCl runtimeclient.Client) *space.Reconciler {
	os.Setenv("WATCH_NAMESPACE", test.HostOperatorNs)
	return &spaceuserconfig.Reconciler{
		Client:    hostCl,
		Namespace: test.HostOperatorNs,
	}
}

func requestFor(s *toolchainv1alpha1.SpaceUserConfig) reconcile.Request {
	return reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: s.Namespace,
			Name:      s.Name,
		},
	}
}
