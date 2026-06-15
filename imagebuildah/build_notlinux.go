//go:build !linux

package imagebuildah

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/sirupsen/logrus"
	"go.podman.io/buildah/copier"
	"go.podman.io/buildah/define"
	"go.podman.io/buildah/internal/tmpdir"
	"go.podman.io/buildah/pkg/parse"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/idtools"
)

// platformSetupContextDirectoryOverlay() either creates a temporary copy of
// the default build context directory.  Returns the location which should be
// used as the default build context; an empty process label and mount label
// for the build that makes more sense elsewhere; a boolean value that
// indicates whether the caller can write directly to the location; and a
// cleanup function which should be called when the location is no longer
// needed (on success). Returned errors should be treated as fatal.
func platformSetupContextDirectoryOverlay(store storage.Store, containerFiles []string, options *define.BuildOptions) (string, string, string, bool, func(), error) {
	var succeeded bool
	var tmpDir string
	cleanup := func() {
		if tmpDir != "" {
			if err := os.Remove(tmpDir); err != nil && !errors.Is(err, fs.ErrNotExist) {
				logrus.Debugf("removing should-be-empty temporary directory %q: %v", tmpDir, err)
			}
		}
	}
	defer func() {
		if !succeeded {
			cleanup()
		}
	}()
	// double-check that the context directory location is an absolute path
	contextDirectoryAbsolute, err := filepath.Abs(options.ContextDirectory)
	if err != nil {
		return "", "", "", false, nil, fmt.Errorf("determining absolute path of %q: %w", options.ContextDirectory, err)
	}
	// create a temporary parent directory for the copy of the build context
	tmpDir, err = os.MkdirTemp(tmpdir.GetTempDir(), "buildah-context-")
	if err != nil {
		return "", "", "", false, nil, fmt.Errorf("creating temporary directory: %w", err)
	}
	// create a subdirectory under it
	tmpContextDir := filepath.Join(tmpDir, "build-context")
	if err := os.Mkdir(tmpContextDir, 0o755); err != nil {
		return "", "", "", false, nil, fmt.Errorf("creating a temporary directory under %s: %w", tmpDir, err)
	}
	// copy the contents of the default build context to the new location so that it can be written to more or less safely
	excludes, _, err := parse.ContainerIgnoreFile(contextDirectoryAbsolute, options.IgnoreFile, containerFiles)
	if err != nil {
		return "", "", "", false, nil, fmt.Errorf("parsing ignore file under context directory %s: %w", contextDirectoryAbsolute, err)
	}
	getOptions := copier.GetOptions{
		ChownDirs:  &idtools.IDPair{UID: 0, GID: 0},
		ChownFiles: &idtools.IDPair{UID: 0, GID: 0},
		Excludes:   excludes,
	}
	var wg sync.WaitGroup
	var putErr, getErr error
	copyReader, copyWriter := io.Pipe()
	wg.Add(1)
	go func() {
		defer wg.Done()
		putErr = copier.Put(tmpContextDir, tmpContextDir, copier.PutOptions{}, copyReader)
		copyReader.Close()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		getErr = copier.Get(contextDirectoryAbsolute, contextDirectoryAbsolute, getOptions, []string{"."}, copyWriter)
		copyWriter.Close()
	}()
	wg.Wait()
	var errs []error
	if getErr != nil {
		errs = append(errs, getErr)
	}
	if putErr != nil {
		errs = append(errs, putErr)
	}
	if len(errs) > 0 {
		grouped := errs[0]
		if len(errs) > 1 {
			grouped = errors.Join(errs...)
		}
		return "", "", "", false, nil, fmt.Errorf("creating copy of build context directory: %w", grouped)
	}
	logrus.Debugf("created a copy of %q at %q", contextDirectoryAbsolute, tmpContextDir)
	succeeded = true
	return tmpContextDir, "", "", true, cleanup, nil
}
