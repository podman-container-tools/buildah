package util //nolint:revive,nolintlint

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func absPath(t *testing.T, rel string) string {
	t.Helper()
	p, err := filepath.Abs(rel)
	require.NoError(t, err)
	return p
}

func TestDiscoverContainerfile(t *testing.T) {
	t.Parallel()
	_, err := DiscoverContainerfile("./bogus")
	assert.NotNil(t, err)

	_, err = DiscoverContainerfile("./")
	assert.NotNil(t, err)

	name, err := DiscoverContainerfile("test/test1/Dockerfile")
	assert.Nil(t, err)
	assert.Equal(t, absPath(t, "test/test1/Dockerfile"), name)

	name, err = DiscoverContainerfile("test/test1/Containerfile")
	assert.Nil(t, err)
	assert.Equal(t, absPath(t, "test/test1/Containerfile"), name)

	name, err = DiscoverContainerfile("test/test1")
	assert.Nil(t, err)
	assert.Equal(t, absPath(t, "test/test1/Containerfile"), name)

	name, err = DiscoverContainerfile("test/test2")
	assert.Nil(t, err)
	assert.Equal(t, absPath(t, "test/test2/Dockerfile"), name)
}

func TestDiscoverContainerfileRejectsSymlinkOutsideContext(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	secretFile := filepath.Join(tmpDir, "secret-Containerfile")
	require.NoError(t, os.WriteFile(secretFile, []byte("FROM scratch\n"), 0o644))

	contextDir := filepath.Join(tmpDir, "context")
	require.NoError(t, os.Mkdir(contextDir, 0o755))
	require.NoError(t, os.Symlink(secretFile, filepath.Join(contextDir, "Containerfile")))

	_, err := DiscoverContainerfile(contextDir)
	assert.Error(t, err)
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

func TestDiscoverContainerfileAcceptsSymlinkInsideContext(t *testing.T) {
	t.Parallel()
	contextDir := t.TempDir()

	subdir := filepath.Join(contextDir, "subdir")
	require.NoError(t, os.Mkdir(subdir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(subdir, "Containerfile.real"), []byte("FROM scratch\n"), 0o644))
	require.NoError(t, os.Symlink(filepath.Join("subdir", "Containerfile.real"), filepath.Join(contextDir, "Containerfile")))

	name, err := DiscoverContainerfile(contextDir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(contextDir, "subdir", "Containerfile.real"), name)
}

func TestDiscoverContainerfileAcceptsEscapeClampedToRoot(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	contextDir := filepath.Join(tmpDir, "context")
	require.NoError(t, os.Mkdir(contextDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(contextDir, "file"), []byte("FROM scratch\n"), 0o644))
	require.NoError(t, os.Symlink("../file", filepath.Join(contextDir, "Containerfile")))

	name, err := DiscoverContainerfile(contextDir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(contextDir, "file"), name)
}

func TestDiscoverContainerfileAcceptsAbsoluteSymlinkRerooted(t *testing.T) {
	t.Parallel()
	contextDir := t.TempDir()

	subdir := filepath.Join(contextDir, "subdirectory")
	require.NoError(t, os.Mkdir(subdir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(subdir, "real.file"), []byte("FROM scratch\n"), 0o644))
	require.NoError(t, os.Symlink("/subdirectory/real.file", filepath.Join(contextDir, "Containerfile")))

	name, err := DiscoverContainerfile(contextDir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(contextDir, "subdirectory", "real.file"), name)
}

func TestDiscoverContainerfileAcceptsMultiLevelEscapeClampedToRoot(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	contextDir := filepath.Join(tmpDir, "context")
	require.NoError(t, os.Mkdir(contextDir, 0o755))
	subdir := filepath.Join(contextDir, "subdir")
	require.NoError(t, os.Mkdir(subdir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(subdir, "file"), []byte("FROM scratch\n"), 0o644))
	require.NoError(t, os.Symlink("../../subdir/file", filepath.Join(contextDir, "Containerfile")))

	name, err := DiscoverContainerfile(contextDir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(contextDir, "subdir", "file"), name)
}

func TestDiscoverContainerfileAcceptsSymlinkedDirectory(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	realDir := filepath.Join(tmpDir, "real-context")
	require.NoError(t, os.Mkdir(realDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(realDir, "Containerfile"), []byte("FROM scratch\n"), 0o644))

	symlinkedDir := filepath.Join(tmpDir, "symlinked-context")
	require.NoError(t, os.Symlink(realDir, symlinkedDir))

	name, err := DiscoverContainerfile(symlinkedDir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(symlinkedDir, "Containerfile"), name)
}

func TestDiscoverContainerfileRejectsNonExistentClampedTarget(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	contextDir := filepath.Join(tmpDir, "context")
	require.NoError(t, os.Mkdir(contextDir, 0o755))
	require.NoError(t, os.Symlink("../nonexistent", filepath.Join(contextDir, "Containerfile")))

	_, err := DiscoverContainerfile(contextDir)
	assert.Error(t, err)
	assert.ErrorIs(t, err, fs.ErrNotExist)
}
