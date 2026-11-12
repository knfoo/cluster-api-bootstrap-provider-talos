// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	capiv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/conditions"
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

// newTestCluster returns a provisioned cluster that TalosConfig reconciliation can run against.
func newTestCluster(name string) *capiv1.Cluster {
	return &capiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: capiv1.ClusterSpec{
			ControlPlaneEndpoint: capiv1.APIEndpoint{Host: name + ".example.com", Port: 6443},
		},
		Status: capiv1.ClusterStatus{
			Initialization: capiv1.ClusterInitializationStatus{
				InfrastructureProvisioned: ptr.To(true),
			},
		},
	}
}

// newTestMachine returns a Machine owned by cluster, with an infrastructure reference
// named infraName (the name CAPM3 uses for its Metal3Data metadata secret).
func newTestMachine(name, clusterName, infraName string) *capiv1.Machine {
	return &capiv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    map[string]string{capiv1.ClusterNameLabel: clusterName},
		},
		Spec: capiv1.MachineSpec{
			ClusterName: clusterName,
			Bootstrap: capiv1.Bootstrap{
				ConfigRef: capiv1.ContractVersionedObjectReference{
					APIGroup: bootstrapv1beta1.GroupVersion.Group,
					Kind:     "TalosConfig",
					Name:     name,
				},
			},
			InfrastructureRef: capiv1.ContractVersionedObjectReference{
				APIGroup: "infrastructure.cluster.x-k8s.io",
				Kind:     "Metal3Machine",
				Name:     infraName,
			},
			Version: "v1.31.0",
		},
	}
}

// newTestTalosConfig returns a TalosConfig owned by owner.
func newTestTalosConfig(name string, owner *capiv1.Machine, spec bootstrapv1beta1.TalosConfigSpec) *bootstrapv1beta1.TalosConfig {
	return &bootstrapv1beta1.TalosConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(owner, capiv1.GroupVersion.WithKind("Machine")),
			},
		},
		Spec: spec,
	}
}

func reconcileTalosConfig(ctx context.Context, c client.Client, name string) (ctrl.Result, *bootstrapv1beta1.TalosConfig, error) {
	r := &TalosConfigReconciler{
		Client: c,
		Log:    ctrl.LoggerFrom(ctx),
		Scheme: scheme.Scheme,
	}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKey{Namespace: "default", Name: name}})

	updated := &bootstrapv1beta1.TalosConfig{}
	if getErr := c.Get(ctx, client.ObjectKey{Namespace: "default", Name: name}, updated); getErr != nil {
		return result, updated, getErr
	}

	return result, updated, err
}

func getSecret(ctx context.Context, c client.Client, name string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}

	return secret, c.Get(ctx, client.ObjectKey{Namespace: "default", Name: name}, secret)
}

// A strategic patch that is a Go template plus templateVariables is rendered before
// it is handed to the Talos config patcher.
func TestTalosConfigReconcileRendersStrategicPatchTemplatesWithVariables(t *testing.T) {
	ctx := context.Background()

	cluster := newTestCluster("test-cluster")
	machine := newTestMachine("test-machine", "test-cluster", "test-infra")
	config := newTestTalosConfig("test-machine", machine, bootstrapv1beta1.TalosConfigSpec{
		GenerateType: "worker",
		TalosVersion: "v1.14",
		StrategicPatches: []string{
			"machine:\n  install:\n    image: {{ get \"installerImage\" }}",
		},
		TemplateVariables: map[string]string{
			"installerImage": "ghcr.io/example/installer:rendered",
		},
	})

	c := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(cluster, machine, config).
		WithStatusSubresource(&bootstrapv1beta1.TalosConfig{}).
		Build()

	result, updated, err := reconcileTalosConfig(ctx, c, config.Name)
	assert.NoError(t, err)
	assert.Equal(t, time.Duration(0), result.RequeueAfter, "no metadata is pending, so no requeue is needed")
	assert.False(t, ptr.Deref(updated.Status.TemplatePatchesPending, false))

	// No metadataFrom: the secret keeps the owner-based name and the "value" key.
	secret, err := getSecret(ctx, c, "test-machine-bootstrap-data")
	assert.NoError(t, err)
	assert.Equal(t, "test-machine-bootstrap-data", updated.Status.DataSecretName)
	assert.NotEmpty(t, secret.Data["value"])
	assert.Contains(t, string(secret.Data["value"]), "image: ghcr.io/example/installer:rendered")
	assert.NotContains(t, string(secret.Data["value"]), "{{")
}

// Phase 1 creates the bootstrap secret (named after the TalosConfig) without data,
// phase 2 renders the metadata into it under dataSecretKey when the metadata secret
// appears. This mirrors CAPM3, which creates the Metal3DataClaim only after the
// bootstrap config is ready.
func TestTalosConfigReconcileTwoPhaseMetadataBootstrap(t *testing.T) {
	ctx := context.Background()

	cluster := newTestCluster("test-cluster")
	machine := newTestMachine("test-machine", "test-cluster", "test-infra")
	config := newTestTalosConfig("test-machine", machine, bootstrapv1beta1.TalosConfigSpec{
		GenerateType:  "worker",
		TalosVersion:  "v1.14",
		MetadataFrom:  &bootstrapv1beta1.MetadataFrom{InfrastructureMachine: &bootstrapv1beta1.InfrastructureMachineMetadata{}},
		DataSecretKey: "userData",
		StrategicPatches: []string{
			"machine:\n  install:\n    image: {{ get \"installerImage\" }}",
			"machine:\n  network:\n    hostname: {{ get \"machine.name\" }}-{{ get \"cluster.name\" }}",
		},
	})

	c := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(cluster, machine, config).
		WithStatusSubresource(&bootstrapv1beta1.TalosConfig{}).
		Build()

	// Phase 1: the metadata secret does not exist yet.
	result, updated, err := reconcileTalosConfig(ctx, c, config.Name)
	assert.NoError(t, err)
	assert.Equal(t, 30*time.Second, result.RequeueAfter)
	assert.True(t, ptr.Deref(updated.Status.TemplatePatchesPending, false), "template patches are pending while metadata is missing")
	assert.True(t, ptr.Deref(updated.Status.Initialization.DataSecretCreated, false))
	assert.Equal(t, "test-machine", updated.Status.DataSecretName, "the secret is named after the TalosConfig when metadataFrom is set")

	secret, err := getSecret(ctx, c, "test-machine")
	assert.NoError(t, err)
	assert.Empty(t, secret.Data, "phase 1 creates a bootstrap secret with no data keys")

	// Metadata appears: reconcile again, without waiting for the requeue.
	metadataSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "test-infra-metadata", Namespace: "default"},
		Type:       "infrastructure.cluster.x-k8s.io/secret",
		Data: map[string][]byte{
			"metaData": []byte("installerImage: ghcr.io/example/installer:rendered\nnicNames:\n  - ens2f1np1\n"),
		},
	}
	assert.NoError(t, c.Create(ctx, metadataSecret))

	result, updated, err = reconcileTalosConfig(ctx, c, config.Name)
	assert.NoError(t, err)
	assert.Equal(t, time.Duration(0), result.RequeueAfter)
	assert.False(t, ptr.Deref(updated.Status.TemplatePatchesPending, false))

	secret, err = getSecret(ctx, c, "test-machine")
	assert.NoError(t, err)
	assert.NotEmpty(t, secret.Data["userData"], "phase 2 writes the rendered config under dataSecretKey")
	assert.NotContains(t, secret.Data, "value", "the rendered config is stored under the configured key only")
	rendered := string(secret.Data["userData"])
	assert.Contains(t, rendered, "image: ghcr.io/example/installer:rendered")
	assert.Contains(t, rendered, "hostname: test-machine-test-cluster")
	assert.NotContains(t, rendered, "{{")
}

// A template that cannot be parsed fails reconciliation with the patch snippet in the
// condition message, and the bootstrap secret is left untouched.
func TestTalosConfigReconcileTemplateParseFailureReportsDataSecretUnavailable(t *testing.T) {
	ctx := context.Background()

	cluster := newTestCluster("test-cluster")
	machine := newTestMachine("test-machine", "test-cluster", "test-infra")
	config := newTestTalosConfig("test-machine", machine, bootstrapv1beta1.TalosConfigSpec{
		GenerateType:      "worker",
		TalosVersion:      "v1.14",
		TemplateVariables: map[string]string{"disk": "/dev/nvme0n1"},
		StrategicPatches: []string{
			"machine: {{ get \"disk\"",
		},
	})

	c := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(cluster, machine, config).
		WithStatusSubresource(&bootstrapv1beta1.TalosConfig{}).
		Build()

	_, updated, err := reconcileTalosConfig(ctx, c, config.Name)
	assert.Error(t, err)
	assert.False(t, ptr.Deref(updated.Status.Initialization.DataSecretCreated, false))

	dataSecretAvailable := conditions.Get(updated, bootstrapv1beta1.DataSecretAvailableCondition)
	assert.NotNil(t, dataSecretAvailable)
	assert.Equal(t, metav1.ConditionFalse, dataSecretAvailable.Status)
	assert.Contains(t, dataSecretAvailable.Message, "machine: {{ get \"disk\"")

	_, err = getSecret(ctx, c, "test-machine-bootstrap-data")
	assert.True(t, apierrors.IsNotFound(err), "no bootstrap secret is written when the template fails to render")
}

// Without metadataFrom or templateVariables, strategicPatches are applied exactly as
// before this change.
func TestTalosConfigReconcilePlainPatchesAreByteIdentical(t *testing.T) {
	ctx := context.Background()

	cluster := newTestCluster("test-cluster")
	plainMachine := newTestMachine("plain-machine", "test-cluster", "plain-infra")
	plainConfig := newTestTalosConfig("plain-machine", plainMachine, bootstrapv1beta1.TalosConfigSpec{
		GenerateType:     "worker",
		TalosVersion:     "v1.14",
		StrategicPatches: []string{"machine:\n  install:\n    image: ghcr.io/example/installer:plain"},
	})

	templatedMachine := newTestMachine("templated-machine", "test-cluster", "templated-infra")
	templatedConfig := newTestTalosConfig("templated-machine", templatedMachine, bootstrapv1beta1.TalosConfigSpec{
		GenerateType:      "worker",
		TalosVersion:      "v1.14",
		MetadataFrom:      &bootstrapv1beta1.MetadataFrom{SecretRef: &bootstrapv1beta1.MetadataSecretRef{Name: "templated-infra-metadata"}},
		TemplateVariables: map[string]string{"unused": "value"},
		StrategicPatches:  []string{"machine:\n  install:\n    image: ghcr.io/example/installer:plain"},
	})

	metadataSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "templated-infra-metadata", Namespace: "default"},
		Data:       map[string][]byte{"metaData": []byte("hostname: worker-01\n")},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(cluster, plainMachine, plainConfig, templatedMachine, templatedConfig, metadataSecret).
		WithStatusSubresource(&bootstrapv1beta1.TalosConfig{}).
		Build()

	_, _, err := reconcileTalosConfig(ctx, c, plainConfig.Name)
	assert.NoError(t, err)

	_, updated, err := reconcileTalosConfig(ctx, c, templatedConfig.Name)
	assert.NoError(t, err)
	assert.False(t, ptr.Deref(updated.Status.TemplatePatchesPending, false))

	plainSecret, err := getSecret(ctx, c, "plain-machine-bootstrap-data")
	assert.NoError(t, err)

	templatedSecret, err := getSecret(ctx, c, "templated-machine")
	assert.NoError(t, err)

	assert.Equal(t, plainSecret.Data["value"], templatedSecret.Data["value"], "patches without template syntax render byte-identically")
}

// The Secret watch maps an infrastructure metadata secret to the TalosConfigs waiting
// for it, so phase 2 does not depend on the requeue timer.
func TestMetadataSecretToTalosConfigsEnqueuesPendingConfigs(t *testing.T) {
	ctx := context.Background()

	pending := newTestTalosConfig("pending-config", newTestMachine("pending-machine", "test-cluster", "pending-infra"), bootstrapv1beta1.TalosConfigSpec{
		GenerateType: "worker",
		MetadataFrom: &bootstrapv1beta1.MetadataFrom{InfrastructureMachine: &bootstrapv1beta1.InfrastructureMachineMetadata{}},
	})
	pending.Status.TemplatePatchesPending = ptr.To(true)

	done := newTestTalosConfig("done-config", newTestMachine("done-machine", "test-cluster", "done-infra"), bootstrapv1beta1.TalosConfigSpec{
		GenerateType: "worker",
	})

	r := &TalosConfigReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithObjects(pending, done).
			WithStatusSubresource(&bootstrapv1beta1.TalosConfig{}).
			Build(),
		Log: ctrl.LoggerFrom(ctx),
	}

	metadataSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "pending-infra-metadata", Namespace: "default"},
		Type:       "infrastructure.cluster.x-k8s.io/secret",
	}

	requests := r.MetadataSecretToTalosConfigs(ctx, metadataSecret)
	assert.Len(t, requests, 1)
	assert.Equal(t, "pending-config", requests[0].Name)
	assert.Equal(t, "default", requests[0].Namespace)

	unrelated := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "some-other-secret", Namespace: "default"}}
	assert.Empty(t, r.MetadataSecretToTalosConfigs(ctx, unrelated))
}
