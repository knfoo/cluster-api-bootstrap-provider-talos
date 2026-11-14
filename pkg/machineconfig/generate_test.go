// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package machineconfig

import (
	"testing"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/stretchr/testify/assert"
)

func testBundle(t *testing.T) *secrets.Bundle {
	t.Helper()

	contract, err := VersionContract("v1.14")
	if err != nil {
		t.Fatal(err)
	}

	bundle, err := secrets.NewBundle(secrets.NewFixedClock(time.Now()), contract)
	if err != nil {
		t.Fatal(err)
	}

	return bundle
}

func TestGenerateInit(t *testing.T) {
	data, patches, err := Generate(GenerateInput{
		ClusterName:          "test-cluster",
		ControlPlaneEndpoint: "https://cp.example.com:6443",
		SecretsBundle:        testBundle(t),
		GenerateType:         "init",
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := configloader.NewFromBytes([]byte(data))
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, machine.TypeInit, cfg.Machine().Type())
	assert.Contains(t, data, "kind: KubeletConfig")
	assert.Contains(t, data, "ghcr.io/siderolabs/kubelet:v"+constants.DefaultKubernetesVersion)
	assert.Len(t, patches, 1)
	assert.Contains(t, patches[0], "podSubnets")
	assert.NotContains(t, patches[0], "HostnameConfig")
}

func TestGenerateInitWithHostname(t *testing.T) {
	data, patches, err := Generate(GenerateInput{
		ClusterName:          "test-cluster",
		ControlPlaneEndpoint: "https://cp.example.com:6443",
		SecretsBundle:        testBundle(t),
		GenerateType:         "init",
		Hostname:             "node-1",
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := configloader.NewFromBytes([]byte(data))
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, machine.TypeInit, cfg.Machine().Type())
	assert.Len(t, patches, 2)
	assert.Contains(t, patches[1], "HostnameConfig")
	assert.Contains(t, patches[1], "hostname: node-1")
}

func TestGenerateControlplane(t *testing.T) {
	data, patches, err := Generate(GenerateInput{
		ClusterName:          "test-cluster",
		ControlPlaneEndpoint: "https://cp.example.com:6443",
		KubernetesVersion:    "1.31.0",
		DNSDomain:            "cluster.local",
		PodSubnets:           []string{"10.244.0.0/16"},
		ServiceSubnets:       []string{"10.96.0.0/12"},
		SecretsBundle:        testBundle(t),
		GenerateType:         "controlplane",
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := configloader.NewFromBytes([]byte(data))
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, machine.TypeControlPlane, cfg.Machine().Type())
	assert.Contains(t, data, "ghcr.io/siderolabs/kubelet:v1.31.0")
	assert.Contains(t, data, "clusterName: test-cluster")
	assert.Len(t, patches, 1)
	assert.Contains(t, patches[0], "10.244.0.0/16")
}

func TestGenerateWorker(t *testing.T) {
	data, patches, err := Generate(GenerateInput{
		ClusterName:          "test-cluster",
		ControlPlaneEndpoint: "https://cp.example.com:6443",
		SecretsBundle:        testBundle(t),
		GenerateType:         "worker",
		Hostname:             "worker-1",
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := configloader.NewFromBytes([]byte(data))
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, machine.TypeWorker, cfg.Machine().Type())
	assert.Len(t, patches, 2)
	assert.Contains(t, patches[1], "hostname: worker-1")
}

func TestGenerateDefaults(t *testing.T) {
	data, patches, err := Generate(GenerateInput{
		ClusterName:          "test-cluster",
		ControlPlaneEndpoint: "https://cp.example.com:6443",
		SecretsBundle:        testBundle(t),
		GenerateType:         "",
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := configloader.NewFromBytes([]byte(data))
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, machine.TypeWorker, cfg.Machine().Type())
	assert.Contains(t, data, "dnsDomain: "+constants.DefaultDNSDomain)
	assert.Contains(t, patches[0], constants.DefaultIPv4PodCIDR)
	assert.Contains(t, patches[0], constants.DefaultIPv4ServiceCIDR)
}
