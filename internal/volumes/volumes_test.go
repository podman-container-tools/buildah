package volumes

import (
	"os"
	"path/filepath"
	"testing"

	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/lockfile"
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
			_, _, _, _, err := GetBindMount(t.Context(), sys, []string{argNeeder}, tempDir, store, "", nil, tempDir, tempDir)
			assert.ErrorIsf(t, err, errBadOptionArg, "option %q was supposed to have an arg, but wasn't flagged when it didn't have one", argNeeder)
			_, _, _, _, err = GetBindMount(t.Context(), sys, []string{argNeeder + "="}, tempDir, store, "", nil, tempDir, tempDir)
			assert.ErrorIsf(t, err, errBadOptionArg, "option %q was supposed to have an arg, but wasn't flagged when it didn't have one", argNeeder)
		}
		for _, argHater := range []string{"bind-nonrecursive", "nodev", "noexec", "nosuid", "ro", "readonly", "rw", "readwrite", "shared", "rshared", "private", "rprivate", "slave", "rslave", "Z", "z", "U", "no-dereference"} {
			_, _, _, _, err := GetBindMount(t.Context(), sys, []string{argHater + "=nonce"}, tempDir, store, "", nil, tempDir, tempDir)
			assert.ErrorIsf(t, err, errBadOptionNoArg, "option %q is not supposed to have an arg, but wasn't flagged when it tried to supply one", argHater)
		}
	})

	t.Run("GetCacheMount", func(t *testing.T) {
		for _, argNeeder := range []string{"sharing", "id", "from", "bind-propagation", "src", "source", "target", "dst", "destination", "mode", "uid", "gid"} {
			_, _, _, _, _, err := GetCacheMount(t.Context(), sys, []string{argNeeder}, store, "", nil, emptyIDmap, emptyIDmap, tempDir, tempDir)
			assert.ErrorIsf(t, err, errBadOptionArg, "option %q was supposed to have an arg, but wasn't flagged when it didn't have one", argNeeder)
			_, _, _, _, _, err = GetCacheMount(t.Context(), sys, []string{argNeeder + "="}, store, "", nil, emptyIDmap, emptyIDmap, tempDir, tempDir)
			assert.ErrorIsf(t, err, errBadOptionArg, "option %q was supposed to have an arg, but wasn't flagged when it didn't have one", argNeeder)
		}
		for _, argHater := range []string{"nodev", "noexec", "nosuid", "U", "rw", "readwrite", "ro", "readonly", "shared", "Z", "z", "rshared", "private", "rprivate", "slave", "rslave"} {
			_, _, _, _, _, err := GetCacheMount(t.Context(), sys, []string{argHater + "=nonce"}, store, "", nil, emptyIDmap, emptyIDmap, tempDir, tempDir)
			assert.ErrorIsf(t, err, errBadOptionNoArg, "option %q is not supposed to have an arg, but wasn't flagged when it tried to supply one", argHater)
		}
		_, _, _, _, _, err := GetCacheMount(t.Context(), sys, []string{"sharing=nonce"}, store, "", nil, emptyIDmap, emptyIDmap, tempDir, tempDir)
		assert.ErrorIs(t, err, errBadMntOption, "sharing modes other than shared, private and locked should be rejected")
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

func TestGetCacheMountSharing(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	tempDir := t.TempDir()
	sys := &types.SystemContext{}
	// map the container's root to ourselves, so that creating cache
	// directories doesn't require being able to chown them to root
	uidmap := []specs.LinuxIDMapping{{ContainerID: 0, HostID: uint32(os.Getuid()), Size: 1}}
	gidmap := []specs.LinuxIDMapping{{ContainerID: 0, HostID: uint32(os.Getgid()), Size: 1}}

	// mounts a cache, returning the directory it was given and the lock it took
	mountCache := func(args ...string) (string, *lockfile.LockFile) {
		t.Helper()
		mount, _, _, _, lock, err := GetCacheMount(t.Context(), sys, args, nil, "", nil, uidmap, gidmap, tempDir, tempDir)
		require.NoError(t, err)
		require.DirExists(t, mount.Source)
		return filepath.Base(mount.Source), lock
	}

	private := []string{"type=cache", "target=/cache", "sharing=private"}

	// three builds using this cache at the same time, none of them finished
	first, firstLock := mountCache(private...)
	require.NotNil(t, firstLock, "a private cache mount should have locked its directory")
	second, secondLock := mountCache(private...)
	third, thirdLock := mountCache(private...)
	t.Cleanup(secondLock.Unlock)
	t.Cleanup(thirdLock.Unlock)
	assert.Equal(t, first+"-1", second, "the second user of a busy cache should get the next directory in its pool")
	assert.Equal(t, first+"-2", third, "and the third should get the one after that")

	// the first build finishes, putting its directory back in the pool
	firstLock.Unlock()
	fourth, fourthLock := mountCache(private...)
	t.Cleanup(fourthLock.Unlock)
	assert.Equal(t, first, fourth, "a directory which nobody is using any more should be reused")

	_, sharedLock := mountCache("type=cache", "target=/cache")
	assert.Nil(t, sharedLock, "a shared cache mount, which is the default, should not lock anything")
}
