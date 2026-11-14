// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package machineconfig

import (
	"testing"

	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/stretchr/testify/assert"
)

func TestApplyPatchesNoPatches(t *testing.T) {
	out, err := ApplyPatches(minimalConfig, PatchInput{})
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, minimalConfig, out)

	if _, err := configloader.NewFromBytes([]byte(out)); err != nil {
		t.Fatalf("config without patches does not parse: %v", err)
	}
}
