package imagebuildah

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"go.podman.io/buildah/define"
)

func TestFilesClosedProperlyByBuildDockerfiles(t *testing.T) {
	// create files in temp dir
	var paths []string
	for _, name := range []string{"Dockerfile", "Dockerfile.in"} {
		fpath, err := filepath.Abs(filepath.Join(t.TempDir(), name))
		assert.Nil(t, err)
		assert.Nil(t, os.WriteFile(fpath, []byte("FROM scratch"), 0o644))
		paths = append(paths, fpath)
	}

	// send files as above, and a missing one, so that we error early and return and don't try an actual build
	_, _, err := BuildDockerfiles(t.Context(), nil, define.BuildOptions{}, append(append(make([]string, 0, len(paths)), paths...), "missing")...)
	var pathErr *fs.PathError
	assert.True(t, errors.As(err, &pathErr))
	assert.Equal(t, "missing", pathErr.Path)

	// verify (as best we can) that we don't think these files are still open
	openFiles, err := currentOpenFiles()
	assert.Nil(t, err)
	for _, path := range paths {
		assert.NotContains(t, openFiles, path)
	}
}

func TestDockerfileSymlinkReRootedUnderContext(t *testing.T) {
	tmpDir := t.TempDir()
	contextDir := filepath.Join(tmpDir, "context")
	assert.NoError(t, os.Mkdir(contextDir, 0o755))

	// Create symlink: context/Dockerfile -> /tmp/test/Containerfile
	symlinkPath := filepath.Join(contextDir, "Dockerfile")
	assert.NoError(t, os.Symlink("/tmp/test/Containerfile", symlinkPath))

	// Case 1: When context/tmp/test/Containerfile does not exist,
	// BuildDockerfiles fails with not found (even if host has /tmp/test/Containerfile).
	_, _, err := BuildDockerfiles(t.Context(), nil, define.BuildOptions{ContextDirectory: contextDir}, symlinkPath)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, os.ErrNotExist))

	// Case 2: When context/tmp/test/Containerfile is created,
	// BuildDockerfiles successfully resolves the re-rooted file under context.
	assert.NoError(t, os.MkdirAll(filepath.Join(contextDir, "tmp/test"), 0o755))
	assert.NoError(t, os.WriteFile(filepath.Join(contextDir, "tmp/test/Containerfile"), []byte("FROM scratch\nLABEL test=ok\n"), 0o644))

	// Use missing path at the end to stop build execution after verifying Dockerfile is found and read
	_, _, err = BuildDockerfiles(t.Context(), nil, define.BuildOptions{ContextDirectory: contextDir}, symlinkPath, "nonexistent-stop-early")
	assert.Error(t, err)
	assert.True(t, errors.Is(err, os.ErrNotExist))
	assert.Contains(t, err.Error(), "nonexistent-stop-early")
}

func TestDockerfileSymlinkClampedToContextRoot(t *testing.T) {
	tmpDir := t.TempDir()
	contextDir := filepath.Join(tmpDir, "context")
	assert.NoError(t, os.Mkdir(contextDir, 0o755))

	// Create a file in context: context/file
	assert.NoError(t, os.WriteFile(filepath.Join(contextDir, "file"), []byte("FROM scratch\nLABEL test=ok\n"), 0o644))

	// Create a file outside context
	assert.NoError(t, os.WriteFile(filepath.Join(tmpDir, "outside_file"), []byte("FROM scratch\nLABEL test=outside\n"), 0o644))

	// Test 1: context/Dockerfile -> ../file
	// Under RESOLVE_IN_ROOT semantics, ".." is clamped to the context root, resolving to context/file.
	symlinkClamped := filepath.Join(contextDir, "Dockerfile.clamped")
	assert.NoError(t, os.Symlink("../file", symlinkClamped))

	_, _, err := BuildDockerfiles(t.Context(), nil, define.BuildOptions{ContextDirectory: contextDir}, symlinkClamped, "nonexistent-stop-early")
	assert.Error(t, err)
	assert.True(t, errors.Is(err, os.ErrNotExist))
	assert.Contains(t, err.Error(), "nonexistent-stop-early")

	// Test 2: context/Dockerfile -> ../context/file
	// Clamped to context root, this resolves to context/context/file, which does not exist.
	symlinkClampedMissing := filepath.Join(contextDir, "Dockerfile.missing")
	assert.NoError(t, os.Symlink("../context/file", symlinkClampedMissing))

	_, _, err = BuildDockerfiles(t.Context(), nil, define.BuildOptions{ContextDirectory: contextDir}, symlinkClampedMissing)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, os.ErrNotExist))

	// Test 3: context/Dockerfile -> ../outside_file
	// Cannot escape context: resolves to context/outside_file which does not exist.
	symlinkEscape := filepath.Join(contextDir, "Dockerfile.escape")
	assert.NoError(t, os.Symlink("../outside_file", symlinkEscape))

	_, _, err = BuildDockerfiles(t.Context(), nil, define.BuildOptions{ContextDirectory: contextDir}, symlinkEscape)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, os.ErrNotExist))
}

func TestDockerfileSymlinkOutsideContextSecurity(t *testing.T) {
	// Reproduce the scenario from issue #6861
	tmpDir := t.TempDir()

	secretContent := "FROM docker.io/library/alpine\nRUN echo SECRET\n"
	assert.NoError(t, os.WriteFile(filepath.Join(tmpDir, "secret_dockerfile"), []byte(secretContent), 0o644))

	contextDir := filepath.Join(tmpDir, "context")
	assert.NoError(t, os.Mkdir(contextDir, 0o755))

	// Create symlink: context/Dockerfile -> ../secret_dockerfile
	symlinkPath := filepath.Join(contextDir, "Dockerfile")
	assert.NoError(t, os.Symlink("../secret_dockerfile", symlinkPath))

	// This should fail because the symlink cannot escape the build context
	_, _, err := BuildDockerfiles(t.Context(), nil, define.BuildOptions{ContextDirectory: contextDir}, symlinkPath)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, os.ErrNotExist))
}

func TestDockerfileSymlinkEscapeViaSymlinkedContext(t *testing.T) {
	// Reproduce the bypass scenario reported by Honny1:
	// When the context directory itself is accessed via a symlink,
	// a symlinked Dockerfile inside the context could escape the context.
	//
	// Setup:
	//   real=/tmp/.../real   (actual context dir)
	//   link=/tmp/.../link -> real
	//   real/Dockerfile -> ../secret
	//   /tmp/.../secret  (file outside context)
	//
	// buildah build -f link/Dockerfile real
	// must reject the Dockerfile symlink pointing outside context.
	tmpDir := t.TempDir()

	secretContent := "FROM docker.io/library/alpine\nRUN echo SECRET\n"
	assert.NoError(t, os.WriteFile(filepath.Join(tmpDir, "secret"), []byte(secretContent), 0o644))

	realDir := filepath.Join(tmpDir, "real")
	assert.NoError(t, os.Mkdir(realDir, 0o755))

	symlinkPath := filepath.Join(realDir, "Dockerfile")
	assert.NoError(t, os.Symlink("../secret", symlinkPath))

	linkDir := filepath.Join(tmpDir, "link")
	assert.NoError(t, os.Symlink("real", linkDir))

	_, _, err := BuildDockerfiles(t.Context(), nil, define.BuildOptions{ContextDirectory: realDir}, filepath.Join(linkDir, "Dockerfile"))
	assert.Error(t, err)
	assert.True(t, errors.Is(err, os.ErrNotExist))

	// However, a valid regular Dockerfile accessed through symlinked context must succeed.
	assert.NoError(t, os.WriteFile(filepath.Join(realDir, "Dockerfile.valid"), []byte("FROM scratch\nLABEL valid=true\n"), 0o644))
	_, _, err = BuildDockerfiles(t.Context(), nil, define.BuildOptions{ContextDirectory: realDir}, filepath.Join(linkDir, "Dockerfile.valid"), "nonexistent-stop-early")
	assert.Error(t, err)
	assert.True(t, errors.Is(err, os.ErrNotExist))
	assert.Contains(t, err.Error(), "nonexistent-stop-early")
}

func TestDockerfileExternalFlagReadsHostPath(t *testing.T) {
	// When -f points outside the context directory, it is read as a
	// host path (Docker/man-page behavior). RESOLVE_IN_ROOT is only
	// applied when the containerfile path is under the context.
	tmpDir := t.TempDir()

	// Create a Dockerfile outside the context
	externalDF := filepath.Join(tmpDir, "External.Dockerfile")
	assert.NoError(t, os.WriteFile(externalDF, []byte("FROM scratch\nLABEL test=external\n"), 0o644))

	// Create context directory with its own Dockerfile
	contextDir := filepath.Join(tmpDir, "context")
	assert.NoError(t, os.Mkdir(contextDir, 0o755))
	assert.NoError(t, os.WriteFile(filepath.Join(contextDir, "Dockerfile"), []byte("FROM scratch\nLABEL test=context\n"), 0o644))

	// -f with external path should read the host file, not clamp it into context.
	// Use a second missing path to stop execution early after reading the Dockerfile.
	_, _, err := BuildDockerfiles(t.Context(), nil, define.BuildOptions{ContextDirectory: contextDir}, externalDF, "nonexistent-stop-early")
	assert.Error(t, err)
	assert.True(t, errors.Is(err, os.ErrNotExist))
	assert.Contains(t, err.Error(), "nonexistent-stop-early")
}

func TestDockerfileDirectoryErrors(t *testing.T) {
	tmpDir := t.TempDir()
	contextDir := filepath.Join(tmpDir, "context")
	assert.NoError(t, os.Mkdir(contextDir, 0o755))

	// 1. -f pointing to a directory within context should fail with "cannot be path to a directory"
	subDir := filepath.Join(contextDir, "subdir")
	assert.NoError(t, os.Mkdir(subDir, 0o755))
	_, _, err := BuildDockerfiles(t.Context(), nil, define.BuildOptions{ContextDirectory: contextDir}, subDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be path to a directory")

	// 2. -f with a non-directory entry in path should fail with "not a directory"
	nonDirFile := filepath.Join(contextDir, "regular_file")
	assert.NoError(t, os.WriteFile(nonDirFile, []byte("hello"), 0o644))
	nonDirPath := filepath.Join(nonDirFile, "Dockerfile")
	_, _, err = BuildDockerfiles(t.Context(), nil, define.BuildOptions{ContextDirectory: contextDir}, nonDirPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

// currentOpenFiles makes an effort at returning a map of which files are currently
// open by our process. We don't fail if we can't follow symlinks from fds as this
// perhaps they now longer exist between when we read them and when we tried to use
// them. Instead we just ignore.
func currentOpenFiles() (map[string]struct{}, error) {
	rd := "/proc/self/fd"
	es, err := os.ReadDir(rd)
	if err != nil {
		return nil, err
	}
	rv := make(map[string]struct{})
	for _, de := range es {
		if de.Type()&fs.ModeSymlink == fs.ModeSymlink {
			dest, err := os.Readlink(filepath.Join(rd, de.Name()))
			if err != nil {
				fmt.Fprintf(os.Stderr, "cannot follow symlink, ignoring: %v\n", err)
				continue
			}
			rv[dest] = struct{}{}
		}
	}
	return rv, nil
}
