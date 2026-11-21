// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package machineconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"

	bootstrapv1beta1 "github.com/siderolabs/cluster-api-bootstrap-provider-talos/api/v1beta1"
)

func TestMetadataSecret(t *testing.T) {
	cases := []struct {
		name     string
		from     *bootstrapv1beta1.MetadataFrom
		infraRef string
		wantName string
		wantKey  string
		wantErr  bool
	}{
		{
			name: "no metadataFrom resolves nothing",
			from: nil,
		},
		{
			name:     "secretRef without a key uses the default key",
			from:     &bootstrapv1beta1.MetadataFrom{SecretRef: &bootstrapv1beta1.MetadataSecretRef{Name: "worker-abc-metadata"}},
			wantName: "worker-abc-metadata",
			wantKey:  MetadataDataKey,
		},
		{
			name:     "secretRef with an explicit key keeps it",
			from:     &bootstrapv1beta1.MetadataFrom{SecretRef: &bootstrapv1beta1.MetadataSecretRef{Name: "metal3-data-0", Key: "userData"}},
			wantName: "metal3-data-0",
			wantKey:  "userData",
		},
		{
			name:    "secretRef without a name is invalid",
			from:    &bootstrapv1beta1.MetadataFrom{SecretRef: &bootstrapv1beta1.MetadataSecretRef{}},
			wantErr: true,
		},
		{
			name:     "infrastructureMachine without suffix or key uses the defaults",
			from:     &bootstrapv1beta1.MetadataFrom{InfrastructureMachine: &bootstrapv1beta1.InfrastructureMachineMetadata{}},
			infraRef: "worker-abc-7df9",
			wantName: "worker-abc-7df9" + MetadataSecretSuffix,
			wantKey:  MetadataDataKey,
		},
		{
			name:     "infrastructureMachine with suffix and key uses them",
			from:     &bootstrapv1beta1.MetadataFrom{InfrastructureMachine: &bootstrapv1beta1.InfrastructureMachineMetadata{SecretNameSuffix: "-data", DataKey: "metadata"}},
			infraRef: "worker-abc-7df9",
			wantName: "worker-abc-7df9-data",
			wantKey:  "metadata",
		},
		{
			name:    "infrastructureMachine without an infrastructure reference is invalid",
			from:    &bootstrapv1beta1.MetadataFrom{InfrastructureMachine: &bootstrapv1beta1.InfrastructureMachineMetadata{}},
			wantErr: true,
		},
		{
			name:     "both secretRef and infrastructureMachine is invalid",
			from:     &bootstrapv1beta1.MetadataFrom{SecretRef: &bootstrapv1beta1.MetadataSecretRef{Name: "metal3-data-0"}, InfrastructureMachine: &bootstrapv1beta1.InfrastructureMachineMetadata{}},
			infraRef: "worker-abc-7df9",
			wantErr:  true,
		},
		{
			name:    "metadataFrom with neither source is invalid",
			from:    &bootstrapv1beta1.MetadataFrom{},
			wantErr: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spec := bootstrapv1beta1.TalosConfigSpec{MetadataFrom: c.from}

			name, key, err := MetadataSecret(&spec, c.infraRef)

			if c.wantErr {
				assert.Error(t, err)

				return
			}

			assert.NoError(t, err)
			assert.Equal(t, c.wantName, name)
			assert.Equal(t, c.wantKey, key)
		})
	}
}

func TestMetadataSecretDefaultsTheSpec(t *testing.T) {
	spec := bootstrapv1beta1.TalosConfigSpec{
		MetadataFrom: &bootstrapv1beta1.MetadataFrom{
			SecretRef: &bootstrapv1beta1.MetadataSecretRef{Name: "metal3-data-0"},
		},
	}

	_, _, err := MetadataSecret(&spec, "worker-abc-7df9")
	assert.NoError(t, err)

	assert.Equal(t, MetadataDataKey, spec.MetadataFrom.SecretRef.Key)

	spec = bootstrapv1beta1.TalosConfigSpec{
		MetadataFrom: &bootstrapv1beta1.MetadataFrom{
			InfrastructureMachine: &bootstrapv1beta1.InfrastructureMachineMetadata{},
		},
	}

	_, _, err = MetadataSecret(&spec, "worker-abc-7df9")
	assert.NoError(t, err)

	assert.Equal(t, MetadataSecretSuffix, spec.MetadataFrom.InfrastructureMachine.SecretNameSuffix)
	assert.Equal(t, MetadataDataKey, spec.MetadataFrom.InfrastructureMachine.DataKey)
}
