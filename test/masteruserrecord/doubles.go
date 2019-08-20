package murtest

import (
	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/host-operator/test"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type MurModifier func(mur *toolchainv1alpha1.MasterUserRecord)
type UaInMurModifier func(targetCluster string, mur *toolchainv1alpha1.MasterUserRecord)

func NewMasterUserRecord(userName string, modifiers ...MurModifier) *toolchainv1alpha1.MasterUserRecord {
	mur := &toolchainv1alpha1.MasterUserRecord{
		ObjectMeta: metav1.ObjectMeta{
			Name:      userName,
			Namespace: test.HostOperatorNs,
		},
		Spec: toolchainv1alpha1.MasterUserRecordSpec{
			UserID:       "12345abcdef",
			UserAccounts: []toolchainv1alpha1.UserAccountEmbedded{newEmbeddedUa(test.MemberClusterName)},
		},
	}
	ModifyMur(mur, modifiers...)
	return mur
}

func newEmbeddedUa(targetCluster string) toolchainv1alpha1.UserAccountEmbedded {
	return toolchainv1alpha1.UserAccountEmbedded{
		TargetCluster: targetCluster,
		SyncIndex:     "123abc",
		Spec: toolchainv1alpha1.UserAccountSpec{
			UserID:  "12345abcdef",
			NSLimit: "basic",
			NSTemplateSet: toolchainv1alpha1.NSTemplateSetSpec{
				TierName: "basic",
				Namespaces: []toolchainv1alpha1.Namespace{
					{
						Type:     "ide",
						Revision: "123abc",
						Template: "",
					},
					{
						Type:     "ci/cd",
						Revision: "123abc",
						Template: "",
					},
					{
						Type:     "staging",
						Revision: "123abc",
						Template: "",
					},
				},
			},
		},
	}
}

func ModifyMur(mur *toolchainv1alpha1.MasterUserRecord, modifiers ...MurModifier) {
	for _, modify := range modifiers {
		modify(mur)
	}
}

func Namespace(namespace string) MurModifier {
	return func(mur *toolchainv1alpha1.MasterUserRecord) {
		mur.Namespace = namespace
	}
}

func TargetCluster(targetCluster string) MurModifier {
	return func(mur *toolchainv1alpha1.MasterUserRecord) {
		for i := range mur.Spec.UserAccounts {
			mur.Spec.UserAccounts[i].TargetCluster = targetCluster
		}
	}
}

func AdditionalAccounts(clusters ...string) MurModifier {
	return func(mur *toolchainv1alpha1.MasterUserRecord) {
		for _, cluster := range clusters {
			mur.Spec.UserAccounts = append(mur.Spec.UserAccounts, newEmbeddedUa(cluster))
		}
	}
}