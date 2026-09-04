package imagebuildah

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
	_, _, err := BuildDockerfiles(context.Background(), nil, define.BuildOptions{}, append(append(make([]string, 0, len(paths)), paths...), "missing")...)
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

func TestDockerfileSymlinkOutsideContext(t *testing.T) {
	tmpDir := t.TempDir()
	contextDir := filepath.Join(tmpDir, "context")
	assert.NoError(t, os.Mkdir(contextDir, 0o755))

	// Create a file inside context for testing
	validFile := filepath.Join(contextDir, "file")
	assert.NoError(t, os.WriteFile(validFile, []byte("FROM scratch"), 0o644))

	dockerfileOutside := filepath.Join(tmpDir, "Dockerfile")
	assert.NoError(t, os.WriteFile(dockerfileOutside, []byte("FROM scratch"), 0o644))

	for _, testCase := range []struct {
		name      string
		target    string
		shouldFail bool
	}{
		{name: "relative-outside", target: "../Dockerfile", shouldFail: true},
		{name: "relative-inside", target: "file", shouldFail: false},
	} {
		linkPath := filepath.Join(contextDir, "Dockerfile."+testCase.name)
		assert.NoError(t, os.Symlink(testCase.target, linkPath))

		// Test the path validation logic directly
		contextAbs, err := filepath.Abs(contextDir)
		assert.NoError(t, err)

		dfileAbs, err := filepath.Abs(linkPath)
		assert.NoError(t, err)

		rel, err := filepath.Rel(contextAbs, dfileAbs)
		assert.NoError(t, err)
		assert.NotEqual(t, "..", rel)
		assert.False(t, strings.HasPrefix(rel, ".."+string(filepath.Separator)))

		resolved, err := filepath.EvalSymlinks(dfileAbs)
		assert.NoError(t, err)

		resolvedRel, err := filepath.Rel(contextAbs, resolved)
		assert.NoError(t, err)

		if testCase.shouldFail {
			assert.True(t, resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(filepath.Separator)))
		} else {
			assert.False(t, resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(filepath.Separator)))
		}
	}
}

func TestDockerfileSymlinkBehavior(t *testing.T) {
	tmpDir := t.TempDir()
	contextDir := filepath.Join(tmpDir, "context")
	assert.NoError(t, os.Mkdir(contextDir, 0o755))

	// Create test files as in the Docker examples
	fileInContext := filepath.Join(contextDir, "file")
	assert.NoError(t, os.WriteFile(fileInContext, []byte("FROM scratch"), 0o644))

	// Create file outside context for test3.bash behavior
	fileOutside := filepath.Join(tmpDir, "file")
	assert.NoError(t, os.WriteFile(fileOutside, []byte("FROM scratch"), 0o644))

	// Test case 1: context/Dockerfile -> ../context/file (should succeed - like Docker)
	symlinkToContextFile := filepath.Join(contextDir, "Dockerfile1")
	assert.NoError(t, os.Symlink("../context/file", symlinkToContextFile))

	// Test case 2: context/Dockerfile -> ../file (should fail - like Docker)
	symlinkToOutsideFile := filepath.Join(contextDir, "Dockerfile2")
	assert.NoError(t, os.Symlink("../file", symlinkToOutsideFile))

	// Test case 3: context/Dockerfile -> file (should succeed - valid symlink)
	symlinkToInsideFile := filepath.Join(contextDir, "Dockerfile3")
	assert.NoError(t, os.Symlink("file", symlinkToInsideFile))

	// Test the validation logic for each case
	testCases := []struct {
		name         string
		dockerfile   string
		shouldFail   bool
	}{
		{"symlink-to-context-file", symlinkToContextFile, false},
		{"symlink-to-outside-file", symlinkToOutsideFile, true},
		{"symlink-to-inside-file", symlinkToInsideFile, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			contextAbs, err := filepath.Abs(contextDir)
			assert.NoError(t, err)

			dfileAbs, err := filepath.Abs(tc.dockerfile)
			assert.NoError(t, err)

			// Check if Dockerfile path is within context
			rel, err := filepath.Rel(contextAbs, dfileAbs)
			assert.NoError(t, err)
			assert.NotEqual(t, "..", rel)
			assert.False(t, strings.HasPrefix(rel, ".."+string(filepath.Separator)))

			// Resolve symlink and check if it's within context
			resolved, err := filepath.EvalSymlinks(dfileAbs)
			assert.NoError(t, err)

			resolvedRel, err := filepath.Rel(contextAbs, resolved)
			assert.NoError(t, err)

			if tc.shouldFail {
				assert.True(t, resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(filepath.Separator)))
				assert.Contains(t, resolvedRel, "..")
			} else {
				assert.False(t, resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(filepath.Separator)))
				assert.NotContains(t, resolvedRel, "..")
			}
		})
	}
}

func TestDockerfileSymlinkOutsideContextMainIssue(t *testing.T) {
	// Reproduce the exact scenario from the main issue
	tmpDir := t.TempDir()

	// Create the exact test scenario from the issue
	catContent := "FROM docker.io/library/alpine"
	assert.NoError(t, os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte(catContent), 0o644))

	contextDir := filepath.Join(tmpDir, "context")
	assert.NoError(t, os.Mkdir(contextDir, 0o755))

	// Create the symlink: context/Dockerfile -> ../Dockerfile
	symlinkPath := filepath.Join(contextDir, "Dockerfile")
	assert.NoError(t, os.Symlink("../Dockerfile", symlinkPath))

	// This should fail (like Docker) because the symlink resolves outside the context
	_, _, err := BuildDockerfiles(context.Background(), nil, define.BuildOptions{ContextDirectory: contextDir}, symlinkPath)
	assert.ErrorContains(t, err, "outside of the build context")
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
