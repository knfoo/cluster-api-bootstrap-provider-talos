// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package machineconfig

import (
	"errors"
	"fmt"

	bootstrapv1beta1 "github.com/siderolabs/cluster-api-bootstrap-provider-talos/api/v1beta1"
)

// MetadataDataKey is the default key of a metadata secret and MetadataSecretSuffix
// the default suffix appended to the name of the infrastructure machine. These are
// the metadataFrom defaults of the TalosConfig CRD, written here and nowhere else
// in Go.
const (
	MetadataDataKey      = "metaData"
	MetadataSecretSuffix = "-metadata"
)

// MetadataSecret applies the metadataFrom defaults to spec and returns the name and
// key of the metadata secret metadataFrom points at.
//
// metadataFrom either names a secret directly (secretRef) or derives the secret name
// from the machine's infrastructure reference (infrastructureMachine): the metadata
// secret of a CAPM3 Metal3DataTemplate is <infrastructureRef><secretNameSuffix> with
// the metadata under dataKey. Exactly one of the two must be set, as the TalosConfig
// webhook validates.
//
// The defaults are applied to the spec the caller passes, so a spec read from a
// TalosControlPlane, whose CRD does not define them, compares equal to the stored
// TalosConfig. It returns no name and no key when metadataFrom is not set.
func MetadataSecret(spec *bootstrapv1beta1.TalosConfigSpec, infraRefName string) (string, string, error) {
	if spec.MetadataFrom == nil {
		return "", "", nil
	}

	metadataFrom := spec.MetadataFrom

	if metadataFrom.SecretRef != nil && metadataFrom.InfrastructureMachine != nil {
		return "", "", errors.New("metadataFrom must specify either secretRef or infrastructureMachine, not both")
	}

	if metadataFrom.SecretRef != nil {
		if metadataFrom.SecretRef.Name == "" {
			return "", "", errors.New("metadataFrom.secretRef.name must not be empty")
		}

		if metadataFrom.SecretRef.Key == "" {
			metadataFrom.SecretRef.Key = MetadataDataKey
		}

		return metadataFrom.SecretRef.Name, metadataFrom.SecretRef.Key, nil
	}

	if metadataFrom.InfrastructureMachine == nil {
		return "", "", errors.New("metadataFrom must specify either secretRef or infrastructureMachine")
	}

	if infraRefName == "" {
		return "", "", fmt.Errorf("metadataFrom.infrastructureMachine needs the infrastructure reference name of the machine")
	}

	if metadataFrom.InfrastructureMachine.SecretNameSuffix == "" {
		metadataFrom.InfrastructureMachine.SecretNameSuffix = MetadataSecretSuffix
	}

	if metadataFrom.InfrastructureMachine.DataKey == "" {
		metadataFrom.InfrastructureMachine.DataKey = MetadataDataKey
	}

	return infraRefName + metadataFrom.InfrastructureMachine.SecretNameSuffix, metadataFrom.InfrastructureMachine.DataKey, nil
}
