// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	capiv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	bootstrapv1beta1 "github.com/siderolabs/cluster-api-bootstrap-provider-talos/api/v1beta1"
	// +kubebuilder:scaffold:imports
)

// While a TalosConfig carries the in-place update-in-progress annotation the bootstrap provider
// must not regenerate the bootstrap data (bootstrap-config.md:364-365).
func TestTalosConfigReconcileSkipsRegenerationDuringInPlaceUpdate(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name        string
		annotations map[string]string
		// bootstrapDataSecret is the secret the controller is expected to write with the
		// regenerated bootstrap data; "" means no bootstrap data secret at all.
		bootstrapDataSecret string
	}{
		{
			name:                "without the in-place update annotation the bootstrap data is generated",
			annotations:         map[string]string{},
			bootstrapDataSecret: "test-machine-bootstrap-data",
		},
		{
			name: "with the in-place update annotation the bootstrap data is not regenerated",
			annotations: map[string]string{
				capiv1.UpdateInProgressAnnotation: "",
			},
			bootstrapDataSecret: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cluster := &capiv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "default"},
				Spec: capiv1.ClusterSpec{
					ControlPlaneEndpoint: capiv1.APIEndpoint{Host: "test-cluster.example.com", Port: 6443},
				},
				Status: capiv1.ClusterStatus{
					Initialization: capiv1.ClusterInitializationStatus{
						InfrastructureProvisioned: ptr.To(true),
					},
				},
			}

			machine := &capiv1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-machine",
					Namespace: "default",
					Labels:    map[string]string{capiv1.ClusterNameLabel: "test-cluster"},
				},
				Spec: capiv1.MachineSpec{
					ClusterName: "test-cluster",
					Bootstrap: capiv1.Bootstrap{
						ConfigRef: capiv1.ContractVersionedObjectReference{
							APIGroup: bootstrapv1beta1.GroupVersion.Group,
							Kind:     "TalosConfig",
							Name:     "test-machine",
						},
					},
					InfrastructureRef: capiv1.ContractVersionedObjectReference{
						APIGroup: "infrastructure.cluster.x-k8s.io",
						Kind:     "TestInfra",
						Name:     "test-machine",
					},
					Version: "v1.31.0",
				},
			}

			talosConfig := &bootstrapv1beta1.TalosConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-machine",
					Namespace:   "default",
					Annotations: tc.annotations,
					OwnerReferences: []metav1.OwnerReference{
						*metav1.NewControllerRef(machine, capiv1.GroupVersion.WithKind("Machine")),
					},
				},
				Spec: bootstrapv1beta1.TalosConfigSpec{
					GenerateType: "worker",
					TalosVersion: "v1.14",
				},
			}

			c := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(cluster, machine, talosConfig).
				WithStatusSubresource(&bootstrapv1beta1.TalosConfig{}).
				Build()

			r := &TalosConfigReconciler{
				Client: c,
				Log:    ctrl.LoggerFrom(ctx),
				Scheme: scheme.Scheme,
			}

			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKey{Namespace: "default", Name: talosConfig.Name}})
			assert.NoError(t, err)

			updated := &bootstrapv1beta1.TalosConfig{}
			assert.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: "default", Name: talosConfig.Name}, updated))

			secret := &corev1.Secret{}
			err = c.Get(ctx, client.ObjectKey{Namespace: "default", Name: "test-machine-bootstrap-data"}, secret)

			if tc.bootstrapDataSecret == "" {
				assert.True(t, apierrors.IsNotFound(err), "expected no bootstrap data secret while the in-place update is in progress")
				assert.Equal(t, "", updated.Status.DataSecretName, "expected no bootstrap data secret name while the in-place update is in progress")
				assert.False(t, ptr.Deref(updated.Status.Initialization.DataSecretCreated, false), "expected the bootstrap data secret not to be created while the in-place update is in progress")
			} else {
				assert.NoError(t, err, "expected the bootstrap data secret to be created")
				assert.Equal(t, tc.bootstrapDataSecret, updated.Status.DataSecretName)
				assert.True(t, ptr.Deref(updated.Status.Initialization.DataSecretCreated, false))
			}
		})
	}
}
