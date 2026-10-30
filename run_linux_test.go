//go:build linux

package buildah

import (
	"fmt"
	"slices"
	"testing"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/opencontainers/runtime-tools/generate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupSpecialMountSpecChangesMqueue(t *testing.T) {
	orig := isMqueueSupported
	t.Cleanup(func() { isMqueueSupported = orig })

	for _, supported := range []bool{true, false} {
		t.Run(fmt.Sprintf("mqueue supported=%v", supported), func(t *testing.T) {
			isMqueueSupported = func() bool { return supported }

			g, err := generate.New("linux")
			require.NoError(t, err)
			mounts, err := setupSpecialMountSpecChanges(g.Config, "65536k")
			require.NoError(t, err)

			hasMqueue := slices.ContainsFunc(mounts, func(m specs.Mount) bool {
				return m.Destination == "/dev/mqueue"
			})
			assert.Equal(t, supported, hasMqueue,
				"the /dev/mqueue mount should be present if and only if the kernel supports mqueue")
		})
	}
}
