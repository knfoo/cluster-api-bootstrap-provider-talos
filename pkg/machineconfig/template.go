// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package machineconfig

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	capiv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// TemplateData builds the data a strategicPatches template is rendered with:
//
//   - the metadata secret's data, decoded as YAML into nested maps and lists,
//     with integers kept as integers;
//   - the Cluster under .cluster and the Machine under .machine;
//   - the inline templateVariables as top-level keys, overriding metadata keys of
//     the same name (cluster and machine are context, not metadata, so variables
//     cannot replace them).
//
// metadata is the raw bytes of the key metadataFrom names in its secret (the
// metaData key of a CAPM3 Metal3DataTemplate secret by default); cluster may be
// nil, and machineName and infraRefName are empty for a MachinePool, which has no
// single machine identity. Templates read a missing key as an error, so callers
// keep passing a nil data map when there is no metadata context at all.
func TemplateData(metadata []byte, cluster *capiv1.Cluster, machineName, infraRefName string, variables map[string]string) (map[string]any, error) {
	data, err := decodeMetadata(metadata)
	if err != nil {
		return nil, err
	}

	for key, value := range variables {
		if key == "" {
			continue
		}

		data[key] = value
	}

	if cluster != nil {
		data["cluster"] = clusterContext(cluster)
	}

	if machine := machineContext(machineName, infraRefName); len(machine) > 0 {
		data["machine"] = machine
	}

	return data, nil
}

// decodeMetadata decodes metadata secret data as YAML, keeping the nesting and
// the number types. Flat "key: value" lines decode to a map the same way.
func decodeMetadata(raw []byte) (map[string]any, error) {
	if strings.TrimSpace(string(raw)) == "" {
		return map[string]any{}, nil
	}

	var data map[string]any
	if err := yaml.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("metadata must be a YAML object: %w", err)
	}

	if data == nil {
		return map[string]any{}, nil
	}

	return data, nil
}

// clusterContext nests the cluster values a template can read: .cluster.name,
// .cluster.namespace, .cluster.controlPlaneEndpoint.host and .port,
// .cluster.clusterNetwork.pods.cidrBlocks and .services.cidrBlocks,
// .cluster.clusterNetwork.serviceDomain, .cluster.infrastructureRef.kind and .name.
func clusterContext(cluster *capiv1.Cluster) map[string]any {
	data := map[string]any{
		"name":      cluster.Name,
		"namespace": cluster.Namespace,
	}

	if cluster.Spec.ControlPlaneEndpoint.Host != "" {
		data["controlPlaneEndpoint"] = map[string]any{
			"host": cluster.Spec.ControlPlaneEndpoint.Host,
			"port": int(cluster.Spec.ControlPlaneEndpoint.Port),
		}
	}

	network := map[string]any{}
	if len(cluster.Spec.ClusterNetwork.Pods.CIDRBlocks) > 0 {
		network["pods"] = map[string]any{"cidrBlocks": cluster.Spec.ClusterNetwork.Pods.CIDRBlocks}
	}

	if len(cluster.Spec.ClusterNetwork.Services.CIDRBlocks) > 0 {
		network["services"] = map[string]any{"cidrBlocks": cluster.Spec.ClusterNetwork.Services.CIDRBlocks}
	}

	if cluster.Spec.ClusterNetwork.ServiceDomain != "" {
		network["serviceDomain"] = cluster.Spec.ClusterNetwork.ServiceDomain
	}

	if len(network) > 0 {
		data["clusterNetwork"] = network
	}

	if cluster.Spec.InfrastructureRef.Kind != "" {
		data["infrastructureRef"] = map[string]any{
			"kind": cluster.Spec.InfrastructureRef.Kind,
			"name": cluster.Spec.InfrastructureRef.Name,
		}
	}

	return data
}

// machineContext nests the machine values a template can read: .machine.name and
// .machine.infrastructureRef.name. Both are empty for a MachinePool.
func machineContext(machineName, infraRefName string) map[string]any {
	data := map[string]any{}

	if machineName != "" {
		data["name"] = machineName
	}

	if infraRefName != "" {
		data["infrastructureRef"] = map[string]any{"name": infraRefName}
	}

	return data
}
