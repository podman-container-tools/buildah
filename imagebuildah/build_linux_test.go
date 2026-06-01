package imagebuildah

import (
	"context"
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
	// create files in in temp dir
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
	dockerfile := filepath.Join(tmpDir, "Dockerfile")
	assert.NoError(t, os.WriteFile(dockerfile, []byte("FROM scratch"), 0o644))
	assert.NoError(t, os.Symlink("../Dockerfile", filepath.Join(contextDir, "Dockerfile")))

	_, _, err := BuildDockerfiles(context.Background(), nil, define.BuildOptions{ContextDirectory: contextDir}, filepath.Join(contextDir, "Dockerfile"))
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
