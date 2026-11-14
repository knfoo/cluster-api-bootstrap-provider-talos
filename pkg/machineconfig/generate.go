// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package machineconfig

import (
	"fmt"

	"github.com/siderolabs/talos/pkg/machinery/config"
	configconfig "github.com/siderolabs/talos/pkg/machinery/config/config"
	"github.com/siderolabs/talos/pkg/machinery/config/container"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/stdpatches"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/config/types/k8s"
	"github.com/siderolabs/talos/pkg/machinery/config/types/meta"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"gopkg.in/yaml.v3"
)

// GenerateInput carries everything the machine configuration generation needs.
// It is a plain value with no client: the secrets bundle, the Kubernetes
// version and the hostname are resolved by the caller (Metal3/CAPM3 and every
// other infrastructure provider resolve them from their own objects).
type GenerateInput struct {
	// ClusterName is the name of the cluster the machine belongs to.
	ClusterName string

	// ControlPlaneEndpoint is the Kubernetes API endpoint, including the
	// scheme, e.g. "https://control-plane.example.com:6443".
	ControlPlaneEndpoint string

	// KubernetesVersion is the Kubernetes version without the leading "v".
	// Empty means the Talos machinery default.
	KubernetesVersion string

	// TalosVersion is the Talos version of the machine configuration, i.e.
	// "v1.14". Empty means the latest supported version.
	TalosVersion string

	// SecretsBundle is the cluster's Talos secrets bundle.
	SecretsBundle *secrets.Bundle

	// GenerateType is the Talos machine type: init, controlplane or worker.
	GenerateType string

	// Hostname sets a static hostname in the generated configuration when it
	// is not empty.
	Hostname string

	// DNSDomain is the cluster service domain. Empty means the default.
	DNSDomain string

	// PodSubnets and ServiceSubnets are the cluster network CIDRs. Empty means
	// the Talos defaults.
	PodSubnets     []string
	ServiceSubnets []string
}

// Generate builds the Talos machine configuration for in and returns it
// together with the standard patches the generator produces (pod/service
// subnets and the static hostname).
//
// This is the machine-config generation formerly living in
// controllers/talosconfig_controller.go:806-945 (genConfigs) and
// controllers/patches.go:17-64 (patchPodServiceSubnets), moved here so the
// bootstrap controller and the in-place update extension share one
// implementation.
func Generate(in GenerateInput) (string, []string, error) {
	// Determine what type of node this is.
	machineType, err := machine.ParseType(in.GenerateType)
	if err != nil {
		machineType = machine.TypeWorker
	}

	patches := []string{}

	k8sVersion := constants.DefaultKubernetesVersion
	if in.KubernetesVersion != "" {
		k8sVersion = in.KubernetesVersion
	}

	clusterDNS := constants.DefaultDNSDomain
	if in.DNSDomain != "" {
		clusterDNS = in.DNSDomain
	}

	genOptions := []generate.Option{generate.WithDNSDomain(clusterDNS)}

	versionContract, err := VersionContract(in.TalosVersion)
	if err != nil {
		return "", patches, err
	}

	genOptions = append(genOptions, generate.WithVersionContract(versionContract))

	if in.SecretsBundle == nil {
		return "", patches, fmt.Errorf("no secrets bundle provided for generation")
	}

	genOptions = append(genOptions, generate.WithSecretsBundle(in.SecretsBundle))

	// Talos dropped support for version contracts <= 0.14, but we still need to
	// support old secret bundles.
	if versionContract != nil && versionContract.Major < 1 && versionContract.Minor < 14 {
		genOptions = append(genOptions, generate.WithClusterDiscovery(false))
	}

	input, err := generate.NewInput(
		in.ClusterName,
		in.ControlPlaneEndpoint,
		k8sVersion,
		genOptions...,
	)
	if err != nil {
		return "", patches, err
	}

	data, err := input.Config(machineType)
	if err != nil {
		return "", patches, err
	}

	podSubnets := in.PodSubnets
	if len(podSubnets) == 0 {
		podSubnets = []string{constants.DefaultIPv4PodCIDR}
	}

	serviceSubnets := in.ServiceSubnets
	if len(serviceSubnets) == 0 {
		serviceSubnets = []string{constants.DefaultIPv4ServiceCIDR}
	}

	podSubnetPatch, err := patchPodServiceSubnets(versionContract, podSubnets, serviceSubnets)
	if err != nil {
		return "", patches, err
	}

	patches = append(patches, string(podSubnetPatch))

	if in.Hostname != "" {
		hostnamePatch, err := stdpatches.WithStaticHostname(versionContract, in.Hostname)
		if err != nil {
			return "", patches, err
		}

		patches = append(patches, string(hostnamePatch))
	}

	dataOut, err := data.EncodeString(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		return "", patches, err
	}

	return dataOut, patches, nil
}

// VersionContract returns the Talos version contract for a Talos version
// string, defaulting to the current contract. Ported from the
// version-contract handling in controllers/talosconfig_controller.go:846-854.
func VersionContract(talosVersion string) (*config.VersionContract, error) {
	if talosVersion == "" {
		return config.TalosVersionCurrent, nil
	}

	contract, err := config.ParseContractFromVersion(talosVersion)
	if err != nil {
		return nil, fmt.Errorf("invalid talos-version: %w", err)
	}

	return contract, nil
}

func patchPodServiceSubnets(versionContract *config.VersionContract, podBlocks, serviceBlocks []string) ([]byte, error) {
	if versionContract.MultidocKubernetesConfigSupported() {
		podSubnets := make([]meta.Prefix, len(podBlocks))

		for i := range podSubnets {
			if err := podSubnets[i].UnmarshalText([]byte(podBlocks[i])); err != nil {
				return nil, err
			}
		}

		serviceSubnets := make([]meta.Prefix, len(serviceBlocks))

		for i := range serviceSubnets {
			if err := serviceSubnets[i].UnmarshalText([]byte(serviceBlocks[i])); err != nil {
				return nil, err
			}
		}

		networkConfig := k8s.NewKubeNetworkConfigV1Alpha1()
		networkConfig.NetworkPodSubnets = podSubnets
		networkConfig.NetworkServiceSubnets = serviceSubnets

		return patchFromDocument(networkConfig)
	}

	return patchFromV1Alpha1(map[string]any{
		"cluster": map[string]any{
			"network": map[string]any{
				"podSubnets":     podBlocks,
				"serviceSubnets": serviceBlocks,
			},
		},
	})
}

func patchFromDocument(doc configconfig.Document) ([]byte, error) {
	ctr, err := container.New(doc)
	if err != nil {
		return nil, err
	}

	return ctr.EncodeBytes(encoder.WithComments(encoder.CommentsDisabled))
}

func patchFromV1Alpha1(doc any) ([]byte, error) {
	return yaml.Marshal(doc)
}
