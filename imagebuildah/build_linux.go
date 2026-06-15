package imagebuildah

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/opencontainers/selinux/go-selinux/label"
	"github.com/sirupsen/logrus"
	"go.podman.io/buildah/copier"
	"go.podman.io/buildah/define"
	"go.podman.io/buildah/internal/tmpdir"
	"go.podman.io/buildah/pkg/overlay"
	"go.podman.io/buildah/pkg/parse"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/idtools"
	"golang.org/x/sys/unix"
)

// includeDirectoryAnyway returns true if "path" is a prefix for an exception
// known to "pm".  If "path" is a directory that "pm" claims matches its list
// of patterns, but "pm"'s list of exclusions contains a pattern for which
// "path" is a prefix, then IncludeDirectoryAnyway() will return true.
// This is not always correct, because it relies on the directory part of any
// exception paths to be specified without wildcards.
func includeDirectoryAnyway(path string, pm *fileutils.PatternMatcher) bool {
	if !pm.Exclusions() {
		return false
	}
	prefix := strings.TrimPrefix(path, string(os.PathSeparator)) + string(os.PathSeparator)
	for _, pattern := range pm.Patterns() {
		if !pattern.Exclusion() {
			continue
		}
		spec := strings.TrimPrefix(pattern.String(), string(os.PathSeparator))
		if strings.HasPrefix(spec, prefix) {
			return true
		}
	}
	return false
}

// platformSetupContextDirectoryOverlay() either sets up an overlay _over_ the
// build context directory, or creates a temporary copy of it, and sorts out
// labeling.  Returns the location which should be used as the default build
// context; the process label and mount label for the build, if any; a boolean
// value that indicates whether the caller can write directly to the location;
// and a cleanup function which should be called when the location is no longer
// needed (on success). Returned errors should be treated as fatal.
func platformSetupContextDirectoryOverlay(store storage.Store, containerFiles []string, options *define.BuildOptions) (string, string, string, bool, func(), error) {
	var succeeded bool
	var tmpDir, tmpContextDir, contentDir string
	cleanup := func() {
		if contentDir != "" {
			if err := overlay.CleanupContent(tmpDir); err != nil {
				logrus.Debugf("cleaning up overlay scaffolding for build context directory: %v", err)
			}
		}
		if tmpContextDir != "" {
			if err := os.RemoveAll(tmpContextDir); err != nil && !errors.Is(err, fs.ErrNotExist) {
				logrus.Debugf("removing temporary directory tree %q: %v", tmpContextDir, err)
			}
		}
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
	// figure out the labeling situation
	processLabel, mountLabel, err := label.InitLabels(options.CommonBuildOpts.LabelOpts)
	if err != nil {
		return "", "", "", false, nil, err
	}
	// create a temporary directory for whatever we're doing
	tmpDir, err = os.MkdirTemp(tmpdir.GetTempDir(), "buildah-context-")
	if err != nil {
		return "", "", "", false, nil, fmt.Errorf("creating temporary directory: %w", err)
	}
	switch store.GraphDriverName() {
	case "overlay":
		// create the scaffolding for an overlay mount under it
		contentDir, err = overlay.TempDir(tmpDir, 0, 0)
		if err != nil {
			return "", "", "", false, nil, fmt.Errorf("creating overlay scaffolding for build context directory: %w", err)
		}
		// mount an overlay that uses it as a lower
		overlayOptions := overlay.Options{
			GraphOpts:  slices.Clone(store.GraphOptions()),
			ForceMount: true,
			MountLabel: mountLabel,
		}
		targetDir := filepath.Join(contentDir, "target")
		contextDirMountSpec, err := overlay.MountWithOptions(contentDir, contextDirectoryAbsolute, targetDir, &overlayOptions)
		if err != nil {
			return "", "", "", false, nil, fmt.Errorf("creating overlay scaffolding for build context directory: %w", err)
		}
		// while we're in here, we might as well process exclusions
		excludes, ignoresFile, err := parse.ContainerIgnoreFile(contextDirectoryAbsolute, options.IgnoreFile, containerFiles)
		if err != nil {
			return "", "", "", false, nil, fmt.Errorf("parsing ignore file under context directory %s: %w", contextDirectoryAbsolute, err)
		}
		if len(excludes) > 0 {
			pm, err := fileutils.NewPatternMatcher(excludes)
			if err != nil {
				return "", "", "", false, nil, fmt.Errorf("parsing ignores for build context directory: %w", err)
			}
			dates := make(map[string][]unix.Timespec)
			modified := make(map[string]struct{})
			if err := filepath.WalkDir(contextDirMountSpec.Source, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				rel, err := filepath.Rel(contextDirMountSpec.Source, p)
				if err != nil {
					return fmt.Errorf("computing the path of %q relative to %q: %w", p, contextDirMountSpec.Source, err)
				}
				if rel == "." {
					return nil
				}
				if d.IsDir() {
					fileInfo, err := d.Info()
					if err != nil {
						return fmt.Errorf("reading info about %q: %w", p, err)
					}
					ts, err := unix.TimeToTimespec(fileInfo.ModTime())
					if err != nil {
						return fmt.Errorf("converting datestamp on %q: %w", p, err)
					}
					dates[p] = []unix.Timespec{ts, ts}
				}
				excluded, err := pm.Matches(rel) //nolint:staticcheck
				if err != nil {
					return fmt.Errorf("checking if %q under %q should be excluded: %w", rel, contextDirectoryAbsolute, err)
				}
				if excluded && (!d.IsDir() || !includeDirectoryAnyway(rel, pm)) {
					modified[filepath.Dir(p)] = struct{}{}
					err = os.RemoveAll(p)
					if err == nil {
						logrus.Debugf("%s filtered out using %s", rel, ignoresFile)
						logrus.Debugf("Skipping excluded path: %s", rel)
						if d.IsDir() {
							return fs.SkipDir
						}
					}
					return err
				}
				return nil
			}); err != nil {
				return "", "", "", false, nil, fmt.Errorf("processing ignores for build context directory: %w", err)
			}
			for modifiedDirectory := range modified {
				if ts, ok := dates[modifiedDirectory]; ok {
					if err := unix.UtimesNano(modifiedDirectory, ts); err != nil {
						return "", "", "", false, nil, fmt.Errorf("resetting datestamp on %q: %w", modifiedDirectory, err)
					}
				}
			}
		}
		// going forward, pretend that the merged directory is the actual context directory
		logrus.Debugf("mounted an overlay at %q over %q", contextDirMountSpec.Source, contextDirectoryAbsolute)
		succeeded = true
		return contextDirMountSpec.Source, processLabel, mountLabel, true, cleanup, nil
	default:
		// create a subdirectory under it
		tmpContextDir = filepath.Join(tmpDir, "build-context")
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
		var putErr, getErr, labelErr error
		copyReader, copyWriter := io.Pipe()
		wg.Add(1)
		go func() {
			defer wg.Done()
			putErr = copier.Put(tmpContextDir, tmpContextDir, copier.PutOptions{}, copyReader)
			copyReader.Close()
			labelErr = label.Relabel(tmpContextDir, mountLabel, true)
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
		if labelErr != nil {
			errs = append(errs, labelErr)
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
		return tmpContextDir, processLabel, mountLabel, true, cleanup, nil
	}
}
