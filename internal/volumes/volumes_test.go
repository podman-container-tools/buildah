package volumes

import (
	"testing"

	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
	storageTypes "go.podman.io/storage/types"
)

func TestGetMount(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	rootDir := t.TempDir()
	runDir := t.TempDir()
	sys := &types.SystemContext{}
	var emptyIDmap []specs.LinuxIDMapping

	store, err := storage.GetStore(storageTypes.StoreOptions{
		GraphDriverName: "vfs",
		GraphRoot:       rootDir,
		RunRoot:         runDir,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		if _, err := store.Shutdown(true); err != nil {
			t.Logf("shutting down temporary store: %v", err)
		}
	})

	t.Run("GetBindMount", func(t *testing.T) {
		for _, argNeeder := range []string{"from", "bind-propagation", "src", "source", "target", "dst", "destination", "relabel"} {
			_, _, _, _, err := GetBindMount(sys, []string{argNeeder}, tempDir, store, "", nil, tempDir, tempDir, nil)
			assert.ErrorIsf(t, err, errBadOptionArg, "option %q was supposed to have an arg, but wasn't flagged when it didn't have one", argNeeder)
			_, _, _, _, err = GetBindMount(sys, []string{argNeeder + "="}, tempDir, store, "", nil, tempDir, tempDir, nil)
			assert.ErrorIsf(t, err, errBadOptionArg, "option %q was supposed to have an arg, but wasn't flagged when it didn't have one", argNeeder)
		}
		for _, argHater := range []string{"bind-nonrecursive", "nodev", "noexec", "nosuid", "ro", "readonly", "rw", "readwrite", "shared", "rshared", "private", "rprivate", "slave", "rslave", "Z", "z", "U", "no-dereference"} {
			_, _, _, _, err := GetBindMount(sys, []string{argHater + "=nonce"}, tempDir, store, "", nil, tempDir, tempDir, nil)
			assert.ErrorIsf(t, err, errBadOptionNoArg, "option %q is not supposed to have an arg, but wasn't flagged when it tried to supply one", argHater)
		}
	})

	t.Run("GetCacheMount", func(t *testing.T) {
		for _, argNeeder := range []string{"sharing", "id", "from", "bind-propagation", "src", "source", "target", "dst", "destination", "mode", "uid", "gid"} {
			_, _, _, _, _, err := GetCacheMount(sys, []string{argNeeder}, store, "", nil, emptyIDmap, emptyIDmap, tempDir, tempDir)
			assert.ErrorIsf(t, err, errBadOptionArg, "option %q was supposed to have an arg, but wasn't flagged when it didn't have one", argNeeder)
			_, _, _, _, _, err = GetCacheMount(sys, []string{argNeeder + "="}, store, "", nil, emptyIDmap, emptyIDmap, tempDir, tempDir)
			assert.ErrorIsf(t, err, errBadOptionArg, "option %q was supposed to have an arg, but wasn't flagged when it didn't have one", argNeeder)
		}
		for _, argHater := range []string{"nodev", "noexec", "nosuid", "U", "rw", "readwrite", "ro", "readonly", "shared", "Z", "z", "rshared", "private", "rprivate", "slave", "rslave"} {
			_, _, _, _, _, err := GetCacheMount(sys, []string{argHater + "=nonce"}, store, "", nil, emptyIDmap, emptyIDmap, tempDir, tempDir)
			assert.ErrorIsf(t, err, errBadOptionNoArg, "option %q is not supposed to have an arg, but wasn't flagged when it tried to supply one", argHater)
		}
	})

	t.Run("GetTmpfsMount", func(t *testing.T) {
		for _, argNeeder := range []string{"tmpfs-mode", "tmpfs-size", "target", "dst", "destination"} {
			_, err := GetTmpfsMount([]string{argNeeder}, tempDir)
			assert.ErrorIsf(t, err, errBadOptionArg, "option %q was supposed to have an arg, but wasn't flagged when it didn't have one", argNeeder)
			_, err = GetTmpfsMount([]string{argNeeder + "="}, tempDir)
			assert.ErrorIsf(t, err, errBadOptionArg, "option %q was supposed to have an arg, but wasn't flagged when it didn't have one", argNeeder)
		}
		for _, argHater := range []string{"nodev", "noexec", "nosuid", "ro", "readonly", "tmpcopyup"} {
			_, err := GetTmpfsMount([]string{argHater + "=nonce"}, tempDir)
			assert.ErrorIsf(t, err, errBadOptionNoArg, "option %q is not supposed to have an arg, but wasn't flagged when it tried to supply one", argHater)
		}
	})
}
