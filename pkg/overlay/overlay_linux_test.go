package overlay

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/storage/pkg/mount"
	"go.podman.io/storage/pkg/unshare"
)

func TestMountWithOptionsVolatile(t *testing.T) {
	if unshare.IsRootless() {
		t.Skip("mounting an overlay needs root")
	}
	for _, tc := range []struct {
		name     string
		volatile bool
	}{
		{name: "default", volatile: false},
		{name: "volatile", volatile: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			source := filepath.Join(tmp, "source")
			require.NoError(t, os.Mkdir(source, 0o755))
			contentDir, err := TempDir(tmp, 0, 0)
			require.NoError(t, err)
			_, err = MountWithOptions(contentDir, source, filepath.Join(tmp, "dest"), &Options{
				ForceMount: true,
				Volatile:   tc.volatile,
			})
			require.NoError(t, err)
			t.Cleanup(func() { assert.NoError(t, RemoveTemp(contentDir)) })

			mergeDir := filepath.Join(contentDir, "merge")
			mounts, err := mount.GetMounts()
			require.NoError(t, err)
			var superOptions string
			found := false
			for _, m := range mounts {
				if m.Mountpoint == mergeDir {
					superOptions, found = m.VFSOptions, true
				}
			}
			require.True(t, found, "no mount at %s", mergeDir)
			// Older kernels list the option as "volatile", newer ones as
			// "fsync=volatile".
			options := strings.Split(superOptions, ",")
			isVolatile := slices.Contains(options, "volatile") || slices.Contains(options, "fsync=volatile")
			assert.Equal(t, tc.volatile, isVolatile, superOptions)
		})
	}
}
