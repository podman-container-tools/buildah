package conformance

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"text/tabwriter"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	archive "github.com/moby/go-archive"
	mobyclient "github.com/moby/moby/client"
	"github.com/moby/moby/client/pkg/versions"
	digest "github.com/opencontainers/go-digest"
	rspec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/openshift/imagebuilder"
	"github.com/openshift/imagebuilder/dockerclient"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/buildah"
	"go.podman.io/buildah/copier"
	"go.podman.io/buildah/define"
	"go.podman.io/buildah/imagebuildah"
	"go.podman.io/buildah/internal/config"
	"go.podman.io/image/v5/docker/daemon"
	"go.podman.io/image/v5/image"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/pkg/compression"
	is "go.podman.io/image/v5/storage"
	"go.podman.io/image/v5/transports"
	"go.podman.io/image/v5/transports/alltransports"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/ioutils"
	"go.podman.io/storage/pkg/reexec"
)

const (
	// See http://pubs.opengroup.org/onlinepubs/9699919799/utilities/pax.html#tag_20_92_13_06, from archive/tar
	cISUID = 0o4000 // Set uid, from archive/tar
	cISGID = 0o2000 // Set gid, from archive/tar
	cISVTX = 0o1000 // Save text (sticky bit), from archive/tar
	// xattrs in the PAXRecords map are namespaced with this prefix
	xattrPAXRecordNamespace = "SCHILY.xattr."
)

var (
	originalSkip = []string{
		"created",
		"container",
		"docker_version",
		"container_config:hostname",
		"config:hostname",
		"config:image",
		"container_config:cmd",
		"container_config:image",
		"history",
		"rootfs:diff_ids",
		"moby.buildkit.buildinfo.v1",
	}
	ociSkip = []string{
		"created",
		"history",
		"rootfs:diff_ids",
	}
	fsSkip = []string{
		// things that we volume mount or synthesize for RUN statements that currently bleed through
		"(dir):dev:mtime",  // we let the timestamp be changed, if it exists at all, and so does docker build
		"(dir):proc:mtime", // we let the timestamp be changed, and so does docker build, which creates it on RUN
		"(dir):sys:mtime",  // we let the timestamp be changed, and so does docker build, which creates it on RUN
		"(dir):etc:mtime",  // we try to preserve the timestamp on RUN, but docker build doesn't
	}
	testDate            = time.Unix(1485449953, 0)
	compareLayers       = false
	compareImagebuilder = false
	testDataDir         = ""
	dockerDir           = ""
	imagebuilderDir     = ""
	buildahDir          = ""
	contextCanDoXattrs  *bool
	storageCanDoXattrs  *bool
)

func TestMain(m *testing.M) {
	var logLevel string
	if reexec.Init() {
		return
	}

	cwd, err := os.Getwd()
	if err != nil {
		logrus.Fatalf("error finding current directory: %v", err)
	}
	testDataDir = filepath.Join(cwd, "testdata")

	flag.StringVar(&logLevel, "log-level", "error", "buildah logging log level")
	flag.BoolVar(&compareLayers, "compare-layers", compareLayers, "compare instruction-by-instruction")
	flag.BoolVar(&compareImagebuilder, "compare-imagebuilder", compareImagebuilder, "also compare using imagebuilder")
	flag.StringVar(&testDataDir, "testdata", testDataDir, "location of conformance testdata")
	flag.StringVar(&dockerDir, "docker-dir", dockerDir, "location to save docker build results")
	flag.StringVar(&imagebuilderDir, "imagebuilder-dir", imagebuilderDir, "location to save imagebuilder build results")
	flag.StringVar(&buildahDir, "buildah-dir", buildahDir, "location to save buildah build results")
	flag.Parse()
	var tempdir string
	if buildahDir == "" || dockerDir == "" || imagebuilderDir == "" {
		if tempdir == "" {
			if tempdir, err = os.MkdirTemp("", "conformance"); err != nil {
				logrus.Fatalf("creating temporary directory: %v", err)
				os.Exit(1)
			}
		}
	}
	if buildahDir == "" {
		buildahDir = filepath.Join(tempdir, "buildah")
	}
	if dockerDir == "" {
		dockerDir = filepath.Join(tempdir, "docker")
	}
	if imagebuilderDir == "" {
		imagebuilderDir = filepath.Join(tempdir, "imagebuilder")
	}
	level, err := logrus.ParseLevel(logLevel)
	if err != nil {
		logrus.Fatalf("error parsing log level %q: %v", logLevel, err)
	}
	logrus.SetLevel(level)
	result := m.Run()
	if err = os.RemoveAll(tempdir); err != nil {
		logrus.Errorf("removing temporary directory %q: %v", tempdir, err)
	}
	os.Exit(result)
}

func TestConformance(t *testing.T) {
	t.Parallel()
	dateStamp := fmt.Sprintf("%d", time.Now().UnixNano())
	for i := range internalTestCases {
		t.Run(internalTestCases[i].name, func(t *testing.T) {
			if internalTestCases[i].testUsingSetParent {
				t.Run("new-set-parent", func(t *testing.T) {
					testConformanceInternal(t, dateStamp, i, func(test *testCase) {
						test.dockerBuilderVersion = docker.BuilderBuildKit
						test.compatSetParent = types.OptionalBoolFalse
						test.compatScratchConfig = types.OptionalBoolFalse
					})
				})
				t.Run("old-set-parent", func(t *testing.T) {
					testConformanceInternal(t, dateStamp, i, func(test *testCase) {
						test.dockerBuilderVersion = docker.BuilderV1
						test.compatSetParent = types.OptionalBoolTrue
						test.compatScratchConfig = types.OptionalBoolTrue
					})
				})
			} else if internalTestCases[i].testUsingVolumes {
				t.Run("new-volumes", func(t *testing.T) {
					testConformanceInternal(t, dateStamp, i, func(test *testCase) {
						test.dockerBuilderVersion = docker.BuilderBuildKit
						test.compatVolumes = types.OptionalBoolFalse
						test.compatScratchConfig = types.OptionalBoolFalse
					})
				})
				t.Run("old-volumes", func(t *testing.T) {
					testConformanceInternal(t, dateStamp, i, func(test *testCase) {
						test.dockerBuilderVersion = docker.BuilderV1
						test.compatVolumes = types.OptionalBoolTrue
						test.compatScratchConfig = types.OptionalBoolTrue
						test.fsSkip = slices.Concat(test.fsSkip, test.fsSkipCompatVolumesTrue)
					})
				})
			} else {
				testConformanceInternal(t, dateStamp, i, nil)
			}
		})
	}
}

func testConformanceInternal(t *testing.T, dateStamp string, testIndex int, mutate func(*testCase)) {
	test := internalTestCases[testIndex]
	if mutate != nil {
		mutate(&test)
	}
	ctx := t.Context()

	cwd, err := os.Getwd()
	require.NoError(t, err, "error finding current directory")

	// create a temporary directory to hold our build context
	tempdir := t.TempDir()

	// create subdirectories to use as the build context and for buildah storage
	contextDir := filepath.Join(tempdir, "context")
	rootDir := filepath.Join(tempdir, "root")
	runrootDir := filepath.Join(tempdir, "runroot")

	// check if we can test xattrs where we're storing build contexts
	if contextCanDoXattrs == nil {
		testDir := filepath.Join(tempdir, "test")
		if err := os.Mkdir(testDir, 0o700); err != nil {
			require.NoErrorf(t, err, "error creating test directory to check if xattrs are testable: %v", err)
		}
		testFile := filepath.Join(testDir, "testfile")
		if err := os.WriteFile(testFile, []byte("whatever"), 0o600); err != nil {
			require.NoErrorf(t, err, "error creating test file to check if xattrs are testable: %v", err)
		}
		can := false
		if err := copier.Lsetxattrs(testFile, map[string]string{"user.test": "test"}); err == nil {
			can = true
		}
		contextCanDoXattrs = &can
	}

	// copy either a directory or just a Dockerfile into the temporary directory
	pipeReader, pipeWriter := io.Pipe()
	var getErr, putErr error
	var wg sync.WaitGroup
	wg.Go(func() {
		if test.contextDir != "" {
			getErr = copier.GetContext(t.Context(), "", testDataDir, copier.GetOptions{}, []string{test.contextDir}, pipeWriter)
		} else if test.dockerfile != "" {
			getErr = copier.GetContext(t.Context(), "", testDataDir, copier.GetOptions{}, []string{test.dockerfile}, pipeWriter)
		}
		pipeWriter.Close()
	})
	wg.Go(func() {
		if test.contextDir != "" || test.dockerfile != "" {
			putErr = copier.PutContext(t.Context(), "", contextDir, copier.PutOptions{}, pipeReader)
		} else {
			putErr = os.Mkdir(contextDir, 0o755)
		}
		pipeReader.Close()
	})
	wg.Wait()
	assert.NoErrorf(t, getErr, "error reading build info from %q", filepath.Join("testdata", test.dockerfile))
	assert.NoErrorf(t, putErr, "error writing build info to %q", contextDir)
	if t.Failed() {
		t.FailNow()
	}

	// construct the names that we want to assign to the images. these should be reasonably unique
	buildahImage := fmt.Sprintf("conformance-buildah:%s-%d", dateStamp, testIndex)
	dockerImage := fmt.Sprintf("conformance-docker:%s-%d", dateStamp, testIndex)
	imagebuilderImage := fmt.Sprintf("conformance-imagebuilder:%s-%d", dateStamp, testIndex)
	if mutate != nil {
		buildahImage += path.Base(t.Name())
		dockerImage += path.Base(t.Name())
		imagebuilderImage += path.Base(t.Name())
	}

	// compute the name of the Dockerfile in the build context directory
	var dockerfileName string
	if test.dockerfile != "" {
		dockerfileName = filepath.Join(contextDir, test.dockerfile)
	} else {
		dockerfileName = filepath.Join(contextDir, "Dockerfile")
	}

	// read the Dockerfile, for inclusion in failure messages
	dockerfileContents := []byte(test.dockerfileContents)
	if len(dockerfileContents) == 0 {
		// no inlined contents -> read them from the specified location
		contents, err := os.ReadFile(dockerfileName)
		require.NoErrorf(t, err, "error reading Dockerfile %q", filepath.Join(tempdir, dockerfileName))
		dockerfileContents = contents
	}

	// initialize storage for buildah
	options := storage.StoreOptions{
		GraphDriverName: os.Getenv("STORAGE_DRIVER"),
		GraphRoot:       rootDir,
		RunRoot:         runrootDir,
	}
	store, err := storage.GetStore(options)
	require.NoErrorf(t, err, "error creating buildah storage at %q", rootDir)
	defer func() {
		if store != nil {
			_, err := store.Shutdown(true)
			require.NoError(t, err, "error shutting down storage for buildah")
		}
	}()
	storageDriver := store.GraphDriverName()
	storageRoot := store.GraphRoot()

	// now that we have a Store, check if we can test xattrs in storage layers
	if storageCanDoXattrs == nil {
		layer, err := store.CreateLayer("", "", nil, "", true, nil)
		if err != nil {
			require.NoErrorf(t, err, "error creating test layer to check if xattrs are testable: %v", err)
		}
		mountPoint, err := store.Mount(layer.ID, "")
		if err != nil {
			require.NoErrorf(t, err, "error mounting test layer to check if xattrs are testable: %v", err)
		}
		testFile := filepath.Join(mountPoint, "testfile")
		if err := os.WriteFile(testFile, []byte("whatever"), 0o600); err != nil {
			require.NoErrorf(t, err, "error creating file in test layer to check if xattrs are testable: %v", err)
		}
		can := false
		if err := copier.Lsetxattrs(testFile, map[string]string{"user.test": "test"}); err == nil {
			can = true
		}
		storageCanDoXattrs = &can
		err = store.DeleteLayer(layer.ID)
		if err != nil {
			require.NoErrorf(t, err, "error removing test layer after checking if xattrs are testable: %v", err)
		}
	}

	// connect to dockerd using the docker client library
	mobyClient, err := mobyclient.New(mobyclient.FromEnv)
	require.NoError(t, err, "unable to initialize docker.client")
	_, err = mobyClient.Ping(ctx, mobyclient.PingOptions{
		NegotiateAPIVersion: true,
	})
	require.NoError(t, err)
	if test.dockerUseBuildKit || test.dockerBuilderVersion != "" {
		negotiatedVersion := mobyClient.ClientVersion()
		if versions.LessThan(negotiatedVersion, "1.38") {
			t.Skipf("negotiated version %q is too low", err)
		}
	}

	// connect to dockerd using go-dockerclient
	// Later, the client.BuildImage implementation chooses an API version based on
	// fields set in docker.BuildImageOptions. Because we don’t use _so_ new features,
	// that can use a fairly old version.
	//
	// If we don’t want this old version to be used, we must specify a version ourselves
	// (the API requires it to be specified when initializing the client). We could create
	// a client only to call .Version() and then create another client with the discovered version,
	// but that’s not really any more correct.
	//
	// As of 2026-02, the client is choosing 1.39, while our server supports 1.44–1.52.
	// Previously we used 1.51, so let’s hard-code that.
	client, err := docker.NewVersionedClientFromEnv("1.51")
	require.NoError(t, err, "unable to initialize docker client")
	var dockerVersion []string
	if version, err := client.Version(); err == nil {
		if version != nil {
			for _, s := range *version {
				dockerVersion = append(dockerVersion, s)
			}
		}
	} else {
		require.NoError(t, err, "unable to connect to docker daemon")
	}

	// make any last-minute tweaks to the build context directory that this test requires
	if test.tweakContextDir != nil {
		err = test.tweakContextDir(t, contextDir, storageDriver, storageRoot)
		require.NoErrorf(t, err, "error tweaking context directory using test-specific callback: %v", err)
	}

	// decide whether we're building just one image for this Dockerfile, or
	// one for each line in it after the first, which we'll assume is a FROM
	if compareLayers {
		// build and compare one line at a time
		line := 1
		for i := range dockerfileContents {
			// scan the byte slice for newlines or the end of the slice, and build using the contents up to that point
			if i == len(dockerfileContents)-1 || (dockerfileContents[i] == '\n' && (i == 0 || dockerfileContents[i-1] != '\\')) {
				if line > 1 || !bytes.HasPrefix(dockerfileContents, []byte("FROM ")) {
					// hack: skip trying to build just the first FROM line
					t.Run(fmt.Sprintf("%d", line), func(t *testing.T) {
						testConformanceInternalBuild(ctx, t, cwd, store, client, mobyClient, fmt.Sprintf("%s.%d", buildahImage, line), fmt.Sprintf("%s.%d", dockerImage, line), fmt.Sprintf("%s.%d", imagebuilderImage, line), contextDir, dockerfileName, dockerfileContents[:i+1], test, line, i == len(dockerfileContents)-1, dockerVersion)
					})
				}
				line++
			}
		}
	} else {
		// build to completion
		testConformanceInternalBuild(ctx, t, cwd, store, client, mobyClient, buildahImage, dockerImage, imagebuilderImage, contextDir, dockerfileName, dockerfileContents, test, 0, true, dockerVersion)
	}
}

func testConformanceInternalBuild(ctx context.Context, t *testing.T, cwd string, store storage.Store, client *docker.Client, mobyClient *mobyclient.Client, buildahImage, dockerImage, imagebuilderImage, contextDir, dockerfileName string, dockerfileContents []byte, test testCase, line int, finalOfSeveral bool, dockerVersion []string) {
	var buildahLog, dockerLog, imagebuilderLog []byte
	var buildahRef, dockerRef, imagebuilderRef types.ImageReference

	// overwrite the Dockerfile in the build context for this run using the
	// contents we were passed, which may only be an initial subset of the
	// original file, or inlined information, in which case the file didn't
	// necessarily exist
	err := os.WriteFile(dockerfileName, dockerfileContents, 0o644)
	require.NoErrorf(t, err, "error writing Dockerfile at %q", dockerfileName)
	err = os.Chtimes(dockerfileName, testDate, testDate)
	require.NoErrorf(t, err, "error resetting timestamp on Dockerfile at %q", dockerfileName)
	err = os.Chtimes(contextDir, testDate, testDate)
	require.NoErrorf(t, err, "error resetting timestamp on context directory at %q", contextDir)

	defer func() {
		if t.Failed() {
			if test.contextDir != "" {
				t.Logf("Context %q", filepath.Join(cwd, "testdata", test.contextDir))
			}
			if test.dockerfile != "" {
				if test.contextDir != "" {
					t.Logf("Dockerfile: %q", filepath.Join(cwd, "testdata", test.contextDir, test.dockerfile))
				} else {
					t.Logf("Dockerfile: %q", filepath.Join(cwd, "testdata", test.dockerfile))
				}
			}
			if !bytes.HasSuffix(dockerfileContents, []byte{'\n'}) && !bytes.HasSuffix(dockerfileContents, []byte{'\r'}) {
				dockerfileContents = append(dockerfileContents, []byte("\n(no final end-of-line)")...)
			}
			t.Logf("Dockerfile contents:\n%s", dockerfileContents)
			if dockerignoreContents, err := os.ReadFile(filepath.Join(contextDir, ".dockerignore")); err == nil {
				t.Logf(".dockerignore contents:\n%s", string(dockerignoreContents))
			}
		}
	}()

	// build using docker
	if !test.withoutDocker {
		dockerRef, dockerLog = buildUsingDocker(ctx, t, client, mobyClient, test, dockerImage, contextDir, dockerfileName, line, finalOfSeveral)
		if dockerRef != nil {
			defer func() {
				err := client.RemoveImageExtended(dockerImage, docker.RemoveImageOptions{
					Context: ctx,
					Force:   true,
				})
				assert.Nil(t, err, "error deleting newly-built-by-docker image %q", dockerImage)
			}()
		}
		saveReport(ctx, t, dockerRef, filepath.Join(dockerDir, t.Name()), dockerfileContents, dockerLog, dockerVersion)
		if finalOfSeveral && compareLayers {
			saveReport(ctx, t, dockerRef, filepath.Join(dockerDir, t.Name(), ".."), dockerfileContents, dockerLog, dockerVersion)
		}
	}

	if t.Failed() {
		t.FailNow()
	}

	// build using imagebuilder if we're testing with it, too
	if compareImagebuilder && !test.withoutImagebuilder {
		imagebuilderRef, imagebuilderLog = buildUsingImagebuilder(t, client, test, imagebuilderImage, contextDir, dockerfileName, line, finalOfSeveral)
		if imagebuilderRef != nil {
			defer func() {
				err := client.RemoveImageExtended(imagebuilderImage, docker.RemoveImageOptions{
					Context: ctx,
					Force:   true,
				})
				assert.Nil(t, err, "error deleting newly-built-by-imagebuilder image %q", imagebuilderImage)
			}()
		}
		saveReport(ctx, t, imagebuilderRef, filepath.Join(imagebuilderDir, t.Name()), dockerfileContents, imagebuilderLog, dockerVersion)
		if finalOfSeveral && compareLayers {
			saveReport(ctx, t, imagebuilderRef, filepath.Join(imagebuilderDir, t.Name(), ".."), dockerfileContents, imagebuilderLog, dockerVersion)
		}
	}

	if t.Failed() {
		t.FailNow()
	}

	// always build using buildah
	buildahRef, buildahLog = buildUsingBuildah(ctx, t, store, test, buildahImage, contextDir, dockerfileName, line, finalOfSeveral)
	if buildahRef != nil {
		defer func() {
			err := buildahRef.DeleteImage(ctx, nil)
			assert.Nil(t, err, "error deleting newly-built-by-buildah image %q", buildahImage)
		}()
	}
	saveReport(ctx, t, buildahRef, filepath.Join(buildahDir, t.Name()), dockerfileContents, buildahLog, nil)
	if finalOfSeveral && compareLayers {
		saveReport(ctx, t, buildahRef, filepath.Join(buildahDir, t.Name(), ".."), dockerfileContents, buildahLog, nil)
	}

	if t.Failed() {
		t.FailNow()
	}

	if test.shouldFailAt != 0 {
		// the build is expected to fail, so there's no point in comparing information about any images
		return
	}

	// the report on the buildah image should always be there
	_, originalBuildahConfig, ociBuildahConfig, fsBuildah := readReport(t, filepath.Join(buildahDir, t.Name()))
	if t.Failed() {
		t.FailNow()
	}
	deleteIdentityLabel := func(config map[string]any) {
		for _, configName := range []string{"config", "container_config"} {
			if configStruct, ok := config[configName]; ok {
				if configMap, ok := configStruct.(map[string]any); ok {
					if labels, ok := configMap["Labels"]; ok {
						if labelMap, ok := labels.(map[string]any); ok {
							delete(labelMap, buildah.BuilderIdentityAnnotation)
						}
					}
				}
			}
		}
	}
	deleteIdentityLabel(originalBuildahConfig)
	deleteIdentityLabel(ociBuildahConfig)

	var originalDockerConfig, ociDockerConfig, fsDocker map[string]any

	// the report on the docker image should be there if we expected the build to succeed
	if !test.withoutDocker {
		var mediaType string
		mediaType, originalDockerConfig, ociDockerConfig, fsDocker = readReport(t, filepath.Join(dockerDir, t.Name()))
		assert.Equal(t, manifest.DockerV2Schema2MediaType, mediaType, "Image built by docker build didn't use Docker MIME type - tests require update")
		if t.Failed() {
			t.FailNow()
		}

		// Some of the base images for our tests were built with buildah, too
		deleteIdentityLabel(originalDockerConfig)
		deleteIdentityLabel(ociDockerConfig)

		miss, left, diff, same := compareJSON(originalDockerConfig, originalBuildahConfig, originalSkip)
		if !same {
			assert.Failf(t, "Image configurations differ as committed in Docker format", configCompareResult(miss, left, diff, "buildah"))
		}
		miss, left, diff, same = compareJSON(ociDockerConfig, ociBuildahConfig, ociSkip)
		if !same {
			assert.Failf(t, "Image configurations differ when converted to OCI format", configCompareResult(miss, left, diff, "buildah"))
		}
		miss, left, diff, same = compareJSON(fsDocker, fsBuildah, append(fsSkip, test.fsSkip...))
		if !same {
			assert.Failf(t, "Filesystem contents differ", fsCompareResult(miss, left, diff, "buildah"))
		}
		if test.failOnExtraFSContent && len(left) > 0 {
			assert.Failf(t, "Extra filesystem content in buildah", fsCompareResult(nil, left, nil, "buildah"))
		}
	}

	// the report on the imagebuilder image should be there if we expected the build to succeed
	if compareImagebuilder && !test.withoutImagebuilder {
		_, originalDockerConfig, ociDockerConfig, fsDocker = readReport(t, filepath.Join(dockerDir, t.Name()))
		if t.Failed() {
			t.FailNow()
		}

		_, originalImagebuilderConfig, ociImagebuilderConfig, fsImagebuilder := readReport(t, filepath.Join(imagebuilderDir, t.Name()))
		if t.Failed() {
			t.FailNow()
		}

		// compare the reports between docker and imagebuilder
		miss, left, diff, same := compareJSON(originalDockerConfig, originalImagebuilderConfig, originalSkip)
		if !same {
			assert.Failf(t, "Image configurations differ as committed in Docker format", configCompareResult(miss, left, diff, "imagebuilder"))
		}
		miss, left, diff, same = compareJSON(ociDockerConfig, ociImagebuilderConfig, ociSkip)
		if !same {
			assert.Failf(t, "Image configurations differ when converted to OCI format", configCompareResult(miss, left, diff, "imagebuilder"))
		}
		miss, left, diff, same = compareJSON(fsDocker, fsImagebuilder, append(fsSkip, test.fsSkip...))
		if !same {
			assert.Failf(t, "Filesystem contents differ", fsCompareResult(miss, left, diff, "imagebuilder"))
		}
		if test.failOnExtraFSContent && len(left) > 0 {
			assert.Failf(t, "Extra filesystem content in imagebuilder", fsCompareResult(nil, left, nil, "imagebuilder"))
		}
	}
}

func buildUsingBuildah(ctx context.Context, t *testing.T, store storage.Store, test testCase, buildahImage, contextDir, dockerfileName string, line int, finalOfSeveral bool) (buildahRef types.ImageReference, buildahLog []byte) {
	// buildah tests might be using transient mounts. replace "@@TEMPDIR@@"
	// in such specifications with the path of the context directory
	var transientMounts []string
	for _, mount := range test.transientMounts {
		transientMounts = append(transientMounts, strings.Replace(mount, "@@TEMPDIR@@", contextDir, 1))
	}
	// set up build options
	output := &bytes.Buffer{}
	if test.compatSetParent != types.OptionalBoolUndefined {
		compat := "default"
		switch test.compatSetParent {
		case types.OptionalBoolFalse:
			compat = "false"
		case types.OptionalBoolTrue:
			compat = "true"
		}
		t.Logf("using buildah flag CompatSetParent = %s", compat)
	}
	if test.compatVolumes != types.OptionalBoolUndefined {
		compat := "default"
		switch test.compatVolumes {
		case types.OptionalBoolFalse:
			compat = "false"
		case types.OptionalBoolTrue:
			compat = "true"
		}
		t.Logf("using buildah flag CompatVolumes = %s", compat)
	}
	if test.compatScratchConfig != types.OptionalBoolUndefined {
		compat := "default"
		switch test.compatScratchConfig {
		case types.OptionalBoolFalse:
			compat = "false"
		case types.OptionalBoolTrue:
			compat = "true"
		}
		t.Logf("using buildah flag CompatScratchConfig = %s", compat)
	}
	options := define.BuildOptions{
		ContextDirectory: contextDir,
		CommonBuildOpts:  &define.CommonBuildOptions{},
		NamespaceOptions: []define.NamespaceOption{{
			Name: string(rspec.NetworkNamespace),
			Host: true,
		}},
		TransientMounts:         transientMounts,
		Output:                  buildahImage,
		OutputFormat:            buildah.Dockerv2ImageManifest,
		Out:                     output,
		Err:                     output,
		Layers:                  true,
		NoCache:                 true,
		RemoveIntermediateCtrs:  true,
		ForceRmIntermediateCtrs: true,
		CompatSetParent:         test.compatSetParent,
		CompatVolumes:           test.compatVolumes,
		CompatScratchConfig:     test.compatScratchConfig,
		CompatLayerOmissions:    test.compatLayerOmissions,
		Args:                    maps.Clone(test.buildArgs),
	}
	// build the image and gather output. log the output if the build part of the test failed
	imageID, _, err := imagebuildah.BuildDockerfiles(ctx, store, options, dockerfileName)
	if err != nil {
		output.WriteString("\n" + err.Error())
	}

	outputString := output.String()
	defer func() {
		if t.Failed() {
			t.Logf("buildah output:\n%s", outputString)
		}
	}()

	buildPost(t, test, err, "buildah", outputString, test.buildahRegex, test.buildahErrRegex, line, finalOfSeveral)

	// return a reference to the new image, if we succeeded
	if err == nil {
		buildahRef, err = is.Transport.ParseStoreReference(store, imageID)
		assert.Nil(t, err, "error parsing reference to newly-built image with ID %q", imageID)
	}
	return buildahRef, []byte(outputString)
}

func pullImageIfMissing(t *testing.T, client *docker.Client, image string) {
	if _, err := client.InspectImage(image); err != nil {
		repository, tag := docker.ParseRepositoryTag(image)
		if tag == "" {
			tag = "latest"
		}
		pullOptions := docker.PullImageOptions{
			Repository: repository,
			Tag:        tag,
		}
		pullAuths := docker.AuthConfiguration{}
		if err := client.PullImage(pullOptions, pullAuths); err != nil {
			t.Fatalf("while pulling %q: %v", image, err)
		}
	}
}

func buildUsingDocker(ctx context.Context, t *testing.T, client *docker.Client, mobyClient *mobyclient.Client, test testCase, dockerImage, contextDir, dockerfileName string, line int, finalOfSeveral bool) (dockerRef types.ImageReference, dockerLog []byte) {
	// compute the path of the dockerfile relative to the build context
	dockerfileRelativePath, err := filepath.Rel(contextDir, dockerfileName)
	require.NoErrorf(t, err, "unable to compute path of dockerfile %q relative to context directory %q", dockerfileName, contextDir)

	// read the Dockerfile so that we can pull base images
	dockerfileContent, err := os.ReadFile(dockerfileName)
	require.NoErrorf(t, err, "reading dockerfile %q", dockerfileName)
	for line := range strings.SplitSeq(string(dockerfileContent), "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "# syntax="); ok {
			pullImageIfMissing(t, client, after)
		}
	}
	parsed, err := imagebuilder.ParseDockerfile(bytes.NewReader(dockerfileContent))
	require.NoErrorf(t, err, "parsing dockerfile %q", dockerfileName)
	dummyBuilder := imagebuilder.NewBuilder(nil)
	stages, err := imagebuilder.NewStages(parsed, dummyBuilder)
	require.NoErrorf(t, err, "breaking dockerfile %q up into stages", dockerfileName)
	for i := range stages {
		stageBase, err := dummyBuilder.From(stages[i].Node)
		require.NoErrorf(t, err, "parsing base image for stage %d in %q", i, dockerfileName)
		if stageBase == "" || stageBase == imagebuilder.NoBaseImageSpecifier {
			continue
		}
		needToEnsureBase := true
		for j := range i {
			if stageBase == stages[j].Name {
				needToEnsureBase = false
			}
		}
		if !needToEnsureBase {
			continue
		}
		pullImageIfMissing(t, client, stageBase)
	}

	excludes, err := imagebuilder.ParseDockerignore(contextDir)
	require.NoErrorf(t, err, "parsing ignores file in %q", contextDir)
	excludes = append(excludes, "!"+dockerfileRelativePath, "!.dockerignore")
	tarOptions := &archive.TarOptions{
		ExcludePatterns: excludes,
		ChownOpts:       &archive.ChownOpts{UID: 0, GID: 0},
	}
	input, err := archive.TarWithOptions(contextDir, tarOptions)
	require.NoErrorf(t, err, "archiving context directory %q", contextDir)
	defer input.Close()

	var buildArgs []docker.BuildArg
	for k, v := range test.buildArgs {
		buildArgs = append(buildArgs, docker.BuildArg{Name: k, Value: v})
	}
	// set up build options
	output := &bytes.Buffer{}
	options := docker.BuildImageOptions{
		Context:             ctx,
		Dockerfile:          dockerfileRelativePath,
		InputStream:         input,
		OutputStream:        output,
		Name:                dockerImage,
		NoCache:             true,
		RmTmpContainer:      true,
		ForceRmTmpContainer: true,
		BuildArgs:           buildArgs,
	}
	if test.dockerUseBuildKit || test.dockerBuilderVersion != "" {
		if test.dockerBuilderVersion != "" {
			var version string
			switch test.dockerBuilderVersion {
			case docker.BuilderBuildKit:
				version = "BuildKit"
			case docker.BuilderV1:
				version = "V1 (classic)"
			default:
				version = "(unknown)"
			}
			t.Logf("requesting docker builder %s", version)
			options.Version = test.dockerBuilderVersion
		} else {
			t.Log("requesting docker builder BuildKit")
			options.Version = docker.BuilderBuildKit
		}
	}
	// build the image and gather output. log the output if the build part of the test failed
	err = client.BuildImage(options)
	if err != nil {
		output.WriteString("\n" + err.Error())
	}
	if _, err := mobyClient.BuildCachePrune(ctx, mobyclient.BuildCachePruneOptions{All: true}); err != nil {
		t.Logf("docker build cache prune: %v", err)
	}

	outputString := output.String()
	defer func() {
		if t.Failed() {
			t.Logf("docker build output:\n%s", outputString)
		}
	}()

	buildPost(t, test, err, "docker build", outputString, test.dockerRegex, test.dockerErrRegex, line, finalOfSeveral)

	// return a reference to the new image, if we succeeded
	if err == nil {
		dockerRef, err = daemon.ParseReference(dockerImage)
		assert.Nil(t, err, "error parsing reference to newly-built image with name %q", dockerImage)
	}
	return dockerRef, []byte(outputString)
}

func buildUsingImagebuilder(t *testing.T, client *docker.Client, test testCase, imagebuilderImage, contextDir, dockerfileName string, line int, finalOfSeveral bool) (imagebuilderRef types.ImageReference, imagebuilderLog []byte) {
	// compute the path of the dockerfile relative to the build context
	dockerfileRelativePath, err := filepath.Rel(contextDir, dockerfileName)
	require.NoErrorf(t, err, "unable to compute path of dockerfile %q relative to context directory %q", dockerfileName, contextDir)
	// set up build options
	output := &bytes.Buffer{}
	executor := dockerclient.NewClientExecutor(client)
	executor.Directory = contextDir
	executor.Tag = imagebuilderImage
	executor.AllowPull = true
	executor.Out = output
	executor.ErrOut = output
	executor.LogFn = func(format string, args ...any) {
		fmt.Fprintf(output, "--> %s\n", fmt.Sprintf(format, args...))
	}
	// buildah tests might be using transient mounts. replace "@@TEMPDIR@@"
	// in such specifications with the path of the context directory
	for _, mount := range test.transientMounts {
		var src, dest string
		mountSpec := strings.SplitN(strings.Replace(mount, "@@TEMPDIR@@", contextDir, 1), ":", 2)
		if len(mountSpec) > 1 {
			src = mountSpec[0]
		}
		dest = mountSpec[len(mountSpec)-1]
		executor.TransientMounts = append(executor.TransientMounts, dockerclient.Mount{
			SourcePath:      src,
			DestinationPath: dest,
		})
	}
	// build the image and gather output. log the output if the build part of the test failed
	builder := imagebuilder.NewBuilder(maps.Clone(test.buildArgs))
	node, err := imagebuilder.ParseFile(filepath.Join(contextDir, dockerfileRelativePath))
	if err != nil {
		assert.Nil(t, err, "error parsing Dockerfile: %v", err)
	}
	if _, err = os.Stat(filepath.Join(contextDir, ".dockerignore")); err == nil {
		if builder.Excludes, err = imagebuilder.ParseDockerignore(contextDir); err != nil {
			assert.Nil(t, err, "error parsing .dockerignore file: %v", err)
		}
	}
	stages, err := imagebuilder.NewStages(node, builder)
	if err != nil {
		assert.Nil(t, err, "error breaking Dockerfile into stages")
	} else {
		if finalExecutor, err := executor.Stages(builder, stages, ""); err != nil {
			output.WriteString("\n" + err.Error())
		} else {
			if err = finalExecutor.Commit(stages[len(stages)-1].Builder); err != nil {
				assert.Nil(t, err, "error committing final stage: %v", err)
			}
		}
	}

	outputString := output.String()
	defer func() {
		if t.Failed() {
			t.Logf("imagebuilder build output:\n%s", outputString)
		}
		for err := range executor.Release() {
			t.Logf("imagebuilder build post-error: %v", err)
		}
	}()

	buildPost(t, test, err, "imagebuilder", outputString, test.imagebuilderRegex, test.imagebuilderErrRegex, line, finalOfSeveral)

	// return a reference to the new image, if we succeeded
	if err == nil {
		imagebuilderRef, err = daemon.ParseReference(imagebuilderImage)
		assert.Nil(t, err, "error parsing reference to newly-built image with name %q", imagebuilderImage)
	}
	return imagebuilderRef, []byte(outputString)
}

func buildPost(t *testing.T, test testCase, err error, buildTool, outputString, stdoutRegex, stderrRegex string, line int, finalOfSeveral bool) {
	// check if the build succeeded or failed, whichever was expected
	if test.shouldFailAt != 0 && (line == 0 || line >= test.shouldFailAt) {
		// this is expected to fail, and we're either at/past
		// the line where it should fail, or we're not going
		// line-by-line
		assert.NotNil(t, err, fmt.Sprintf("%s build was expected to fail, but succeeded", buildTool))
	} else {
		assert.Nil(t, err, fmt.Sprintf("%s build was expected to succeed, but failed", buildTool))
	}

	// if the build failed, and we have an error message we expected, check for it
	if err != nil && test.failureRegex != "" {
		outputTokens := strings.Join(strings.Fields(err.Error()), " ")
		assert.Regexpf(t, regexp.MustCompile(test.failureRegex), outputTokens, "build failure did not match %q", test.failureRegex)
	}

	// if this is the last image we're building for this case, we can scan
	// the build log for expected messages
	if finalOfSeveral {
		outputTokens := strings.Join(strings.Fields(outputString), " ")
		// check for expected output
		if stdoutRegex != "" {
			assert.Regexpf(t, regexp.MustCompile(stdoutRegex), outputTokens, "build output did not match %q", stdoutRegex)
		}
		if stderrRegex != "" {
			assert.Regexpf(t, regexp.MustCompile(stderrRegex), outputTokens, "build error did not match %q", stderrRegex)
		}
	}
}

// FSTree holds the information we have about an image's filesystem
type FSTree struct {
	Layers []Layer `json:"layers,omitempty"`
	Tree   FSEntry `json:"tree"`
}

// Layer keeps track of the digests and contents of a layer blob
type Layer struct {
	UncompressedDigest digest.Digest `json:"uncompressed-digest,omitempty"`
	CompressedDigest   digest.Digest `json:"compressed-digest,omitempty"`
	Headers            []FSHeader    `json:"-"`
}

// FSHeader is the parts of the tar.Header for an entry in a layer blob that
// are relevant
type FSHeader struct {
	Typeflag byte              `json:"typeflag,omitempty"`
	Name     string            `json:"name,omitempty"`
	Linkname string            `json:"linkname,omitempty"`
	Size     int64             `json:"size"`
	Mode     int64             `json:"mode,omitempty"`
	UID      int               `json:"uid"`
	GID      int               `json:"gid"`
	ModTime  time.Time         `json:"mtime"`
	Devmajor int64             `json:"devmajor,omitempty"`
	Devminor int64             `json:"devminor,omitempty"`
	Xattrs   map[string]string `json:"xattrs,omitempty"`
	Digest   digest.Digest     `json:"digest,omitempty"`
}

// FSEntry stores one item in a filesystem tree.  If it represents a directory,
// its contents are stored as its children
type FSEntry struct {
	FSHeader
	Children map[string]*FSEntry `json:"(dir),omitempty"`
}

// fsHeaderForEntry converts a tar header to an FSHeader, in the process
// discarding some fields which we don't care to compare
func fsHeaderForEntry(hdr *tar.Header) FSHeader {
	return FSHeader{
		Typeflag: hdr.Typeflag,
		Name:     hdr.Name,
		Linkname: hdr.Linkname,
		Size:     hdr.Size,
		Mode:     (hdr.Mode & int64(fs.ModePerm)),
		UID:      hdr.Uid,
		GID:      hdr.Gid,
		ModTime:  hdr.ModTime,
		Devmajor: hdr.Devmajor,
		Devminor: hdr.Devminor,
		Xattrs:   hdr.Xattrs, //nolint:staticcheck
	}
}

// save information about the specified image to the specified directory
func saveReport(ctx context.Context, t *testing.T, ref types.ImageReference, directory string, dockerfileContents []byte, buildLog []byte, version []string) {
	imageName := ""
	// make sure the directory exists
	err := os.MkdirAll(directory, 0o755)
	require.NoErrorf(t, err, "error ensuring directory %q exists for storing a report", directory)
	// save the Dockerfile that was used to generate the image
	err = os.WriteFile(filepath.Join(directory, "Dockerfile"), dockerfileContents, 0o644)
	require.NoErrorf(t, err, "error saving Dockerfile for image %q", imageName)
	// save the log generated while building the image
	err = os.WriteFile(filepath.Join(directory, "build.log"), buildLog, 0o644)
	require.NoErrorf(t, err, "error saving build log for image %q", imageName)
	// save the version information
	if len(version) > 0 {
		err = os.WriteFile(filepath.Join(directory, "version"), []byte(strings.Join(version, "\n")+"\n"), 0o644)
		require.NoErrorf(t, err, "error saving builder version information for image %q", imageName)
	}
	// open the image for reading
	if ref == nil {
		return
	}
	imageName = transports.ImageName(ref)
	src, err := ref.NewImageSource(ctx, nil)
	require.NoErrorf(t, err, "error opening image %q as source to read its configuration", imageName)
	closer := io.Closer(src)
	defer func() {
		closer.Close()
	}()
	img, err := image.FromSource(ctx, nil, src)
	require.NoErrorf(t, err, "error opening image %q to read its configuration", imageName)
	closer = img
	// read the manifest in its original form
	rawManifest, _, err := img.Manifest(ctx)
	require.NoErrorf(t, err, "error reading raw manifest from image %q", imageName)
	// read the config blob in its original form
	rawConfig, err := img.ConfigBlob(ctx)
	require.NoErrorf(t, err, "error reading configuration from image %q", imageName)
	// read the config blob, converted to OCI format by the image library, and re-encode it
	ociConfig, err := img.OCIConfig(ctx)
	require.NoErrorf(t, err, "error reading OCI-format configuration from image %q", imageName)
	encodedConfig, err := json.Marshal(ociConfig)
	require.NoErrorf(t, err, "error encoding OCI-format configuration from image %q for saving", imageName)
	// save the manifest in its original form
	err = os.WriteFile(filepath.Join(directory, "manifest.json"), rawManifest, 0o644)
	require.NoErrorf(t, err, "error saving original manifest from image %q", imageName)
	// save the config blob in the OCI format
	err = os.WriteFile(filepath.Join(directory, "oci-config.json"), encodedConfig, 0o644)
	require.NoErrorf(t, err, "error saving OCI-format configuration from image %q", imageName)
	// save the config blob in its original format
	err = os.WriteFile(filepath.Join(directory, "config.json"), rawConfig, 0o644)
	require.NoErrorf(t, err, "error saving original configuration from image %q", imageName)
	// start pulling layer information
	layerBlobInfos, err := img.LayerInfosForCopy(ctx)
	require.NoErrorf(t, err, "error reading blob infos for image %q", imageName)
	if len(layerBlobInfos) == 0 {
		layerBlobInfos = img.LayerInfos()
	}
	fstree := FSTree{Tree: FSEntry{Children: make(map[string]*FSEntry)}}
	// grab digest and header information from the layer blob
	for _, layerBlobInfo := range layerBlobInfos {
		rc, _, err := src.GetBlob(ctx, layerBlobInfo, nil)
		require.NoErrorf(t, err, "error reading blob %+v for image %q", layerBlobInfo, imageName)
		defer rc.Close()
		layer := summarizeLayer(t, imageName, layerBlobInfo, rc)
		fstree.Layers = append(fstree.Layers, layer)
	}
	// apply the header information from blobs, in the order they're listed
	// in the config blob, to produce what we think the filesystem tree
	// would look like
	for _, diffID := range ociConfig.RootFS.DiffIDs {
		var layer *Layer
		for i := range fstree.Layers {
			if fstree.Layers[i].CompressedDigest == diffID {
				layer = &fstree.Layers[i]
				break
			}
			if fstree.Layers[i].UncompressedDigest == diffID {
				layer = &fstree.Layers[i]
				break
			}
		}
		if layer == nil {
			require.Failf(t, "missing layer diff", "config for image %q specifies a layer with diffID %q, but we found no layer blob matching that digest", imageName, diffID)
		}
		applyLayerToFSTree(t, layer, &fstree.Tree)
	}
	// encode the filesystem tree information and save it to a file,
	// discarding the layer summaries because different tools may choose
	// between marking a directory as opaque and removing each of its
	// contents individually, which would produce the same result, so
	// there's no point in saving them for comparison later
	encodedFSTree, err := json.Marshal(fstree.Tree)
	require.NoErrorf(t, err, "error encoding filesystem tree from image %q for saving", imageName)
	err = os.WriteFile(filepath.Join(directory, "fs.json"), encodedFSTree, 0o644)
	require.NoErrorf(t, err, "error saving filesystem tree from image %q", imageName)
}

// summarizeLayer reads a blob and returns a summary of the parts of its contents that we care about
func summarizeLayer(t *testing.T, imageName string, blobInfo types.BlobInfo, reader io.Reader) (layer Layer) {
	compressedDigest := digest.Canonical.Digester()
	counter := ioutils.NewWriteCounter(compressedDigest.Hash())
	compressionAlgorithm, _, reader, err := compression.DetectCompressionFormat(reader)
	require.NoErrorf(t, err, "error checking if blob %+v for image %q is compressed", blobInfo, imageName)
	uncompressedBlob, wasCompressed, err := compression.AutoDecompress(io.TeeReader(reader, counter))
	require.NoErrorf(t, err, "error decompressing blob %+v for image %q", blobInfo, imageName)
	defer uncompressedBlob.Close()
	uncompressedDigest := digest.Canonical.Digester()
	tarToRead := io.TeeReader(uncompressedBlob, uncompressedDigest.Hash())
	tr := tar.NewReader(tarToRead)
	hdr, err := tr.Next()
	for err == nil {
		header := fsHeaderForEntry(hdr)
		if hdr.Size != 0 {
			contentDigest := digest.Canonical.Digester()
			n, err := io.Copy(contentDigest.Hash(), tr)
			require.NoErrorf(t, err, "error digesting contents of %q from layer %+v for image %q", hdr.Name, blobInfo, imageName)
			require.Equal(t, hdr.Size, n, "error reading contents of %q from layer %+v for image %q: wrong size", hdr.Name, blobInfo, imageName)
			header.Digest = contentDigest.Digest()
		}
		layer.Headers = append(layer.Headers, header)
		hdr, err = tr.Next()
	}
	require.Equal(t, io.EOF, err, "unexpected error reading layer contents %+v for image %q", blobInfo, imageName)
	_, err = io.Copy(io.Discard, tarToRead)
	require.NoError(t, err, "reading out any not-usually-present zero padding at the end")
	layer.CompressedDigest = compressedDigest.Digest()
	blobFormatDescription := "uncompressed"
	if wasCompressed {
		if compressionAlgorithm.Name() != "" {
			blobFormatDescription = "compressed with " + compressionAlgorithm.Name()
		} else {
			blobFormatDescription = "compressed (?)"
		}
	}
	require.Equalf(t, blobInfo.Digest, layer.CompressedDigest, "calculated digest of %s blob didn't match expected digest (expected length %d, actual length %d)", blobFormatDescription, blobInfo.Size, counter.Count)
	layer.UncompressedDigest = uncompressedDigest.Digest()
	return layer
}

// applyLayerToFSTree updates the in-memory summary of a tree to incorporate
// changes described in the layer.  This is a little naive, in that we don't
// expect the pathname to include symlinks, which we don't resolve, as
// components, but tools that currently generate layer diffs don't create
// those.
func applyLayerToFSTree(t *testing.T, layer *Layer, root *FSEntry) {
	for i, entry := range layer.Headers {
		if entry.Typeflag == tar.TypeLink {
			// if the entry is a hard link, replace it with the
			// contents of the hard-linked file
			replaced := false
			name := entry.Name
			for j, otherEntry := range layer.Headers {
				if j >= i {
					break
				}
				if otherEntry.Name == entry.Linkname {
					entry = otherEntry
					entry.Name = name
					replaced = true
					break
				}
			}
			if !replaced {
				require.Fail(t, "layer diff error", "hardlink entry referenced a file that isn't in the layer")
			}
		}
		// parse the name from the entry, and don't get tripped up by a final '/'
		dirEntry := root
		components := strings.Split(strings.Trim(entry.Name, string(os.PathSeparator)), string(os.PathSeparator))
		require.NotEmpty(t, entry.Name, "layer diff error", "entry has no name")
		require.NotZerof(t, len(components), "entry name %q has no components", entry.Name)
		require.NotZerof(t, components[0], "entry name %q has no components", entry.Name)
		// "split" the final part of the path from the rest
		base := components[len(components)-1]
		components = components[:len(components)-1]
		// find the directory that contains this entry
		for i, component := range components {
			// this should be a parent directory, so check if it looks like a parent directory
			if dirEntry.Children == nil {
				require.Failf(t, "layer diff error", "layer diff %q includes entry for %q, but %q is not a directory", layer.UncompressedDigest, entry.Name, strings.Join(components[:i], string(os.PathSeparator)))
			}
			// if the directory is already there, move into it
			if child, ok := dirEntry.Children[component]; ok {
				dirEntry = child
				continue
			}
			// if the directory should be there, but we haven't
			// created it yet, blame the tool that generated this
			// layer diff
			require.Failf(t, "layer diff error", "layer diff %q includes entry for %q, but %q doesn't exist", layer.UncompressedDigest, entry.Name, strings.Join(components[:i], string(os.PathSeparator)))
		}
		// if the current directory is marked as "opaque", remove all
		// of its contents
		if base == ".wh..opq" {
			dirEntry.Children = make(map[string]*FSEntry)
			continue
		}
		// if the item is a whiteout, strip the "this is a whiteout
		// entry" prefix and remove the item it names
		if after, ok := strings.CutPrefix(base, ".wh."); ok {
			delete(dirEntry.Children, after)
			continue
		}
		// if the item already exists, make sure we don't get confused
		// by replacing a directory with a non-directory or vice-versa
		if child, ok := dirEntry.Children[base]; ok {
			if child.Children != nil {
				// it's a directory
				if entry.Typeflag == tar.TypeDir {
					// new entry is a directory, too. no
					// sweat, just update the metadata
					child.FSHeader = entry
					continue
				}
				// nope, a directory no longer
			} else {
				// it's not a directory
				if entry.Typeflag != tar.TypeDir {
					// new entry is not a directory, too.
					// no sweat, just update the metadata
					dirEntry.Children[base].FSHeader = entry
					continue
				}
				// well, it's a directory now
			}
		}
		// the item doesn't already exist, or it needs to be replaced, so we need to add it
		var children map[string]*FSEntry
		if entry.Typeflag == tar.TypeDir {
			// only directory entries can hold items
			children = make(map[string]*FSEntry)
		}
		dirEntry.Children[base] = &FSEntry{FSHeader: entry, Children: children}
	}
}

// read information about the specified image from the specified directory
func readReport(t *testing.T, directory string) (manifestType string, original, oci, fs map[string]any) {
	// read the manifest in the as-committed format, whatever that is
	originalManifest, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	require.NoErrorf(t, err, "error reading manifest %q", filepath.Join(directory, "manifest.json"))
	// dump it into a map
	manifest := make(map[string]any)
	err = json.Unmarshal(originalManifest, &manifest)
	require.NoErrorf(t, err, "error decoding manifest %q", filepath.Join(directory, "manifest.json"))
	if str, ok := manifest["mediaType"].(string); ok {
		manifestType = str
	}
	// read the config in the as-committed (docker) format
	originalConfig, err := os.ReadFile(filepath.Join(directory, "config.json"))
	require.NoErrorf(t, err, "error reading configuration file %q", filepath.Join(directory, "config.json"))
	// dump it into a map
	original = make(map[string]any)
	err = json.Unmarshal(originalConfig, &original)
	require.NoErrorf(t, err, "error decoding configuration from file %q", filepath.Join(directory, "config.json"))
	// read the config in converted-to-OCI format
	ociConfig, err := os.ReadFile(filepath.Join(directory, "oci-config.json"))
	require.NoErrorf(t, err, "error reading OCI configuration file %q", filepath.Join(directory, "oci-config.json"))
	// dump it into a map
	oci = make(map[string]any)
	err = json.Unmarshal(ociConfig, &oci)
	require.NoErrorf(t, err, "error decoding OCI configuration from file %q", filepath.Join(directory, "oci.json"))
	// read the filesystem
	fsInfo, err := os.ReadFile(filepath.Join(directory, "fs.json"))
	require.NoErrorf(t, err, "error reading filesystem summary file %q", filepath.Join(directory, "fs.json"))
	// dump it into a map for comparison
	fs = make(map[string]any)
	err = json.Unmarshal(fsInfo, &fs)
	require.NoErrorf(t, err, "error decoding filesystem summary from file %q", filepath.Join(directory, "fs.json"))
	// return both
	return manifestType, original, oci, fs
}

// contains is used to check if item exists in []string or not, ignoring case
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if strings.EqualFold(s, item) {
			return true
		}
	}
	return false
}

// addPrefix prepends the given prefix to each string in []string.
// The prefix and the string are joined with ":"
func addPrefix(a []string, prefix string) []string {
	b := make([]string, 0, len(a))
	for _, s := range a {
		b = append(b, prefix+":"+s)
	}
	return b
}

// diffDebug returns a row for a tabwriter that summarizes a field name and the
// values for that field in two documents
func diffDebug(k string, a, b any) string {
	if k == "mode" {
		// force modes to be displayed in octal instead of decimal
		a, aok := a.(float64)
		b, bok := b.(float64)
		if aok && bok {
			return fmt.Sprintf("%v\t0%o\t0%o\n", k, int64(a), int64(b))
		}
	}
	return fmt.Sprintf("%v\t%v\t%v\n", k, a, b)
}

// compareJSON compares two parsed JSON structures.  missKeys and leftKeys are
// lists of field names present only in the first map or the second,
// respectively, while diffKeys is a list of items which are present in both
// maps, but which have different values, formatted with diffDebug.
func compareJSON(a, b map[string]any, skip []string) (missKeys, leftKeys, diffKeys []string, isSame bool) {
	isSame = true

	for k, v := range a {
		vb, ok := b[k]
		if ok {
			// remove this item from b. when we're done, all that's
			// left in b will be the items that weren't also in a.
			delete(b, k)
		}
		if contains(skip, k) {
			continue
		}
		if !ok {
			// key is in a, but not in b
			missKeys = append(missKeys, k)
			isSame = false
			continue
		}
		if reflect.TypeOf(v) != reflect.TypeOf(vb) {
			if reflect.TypeOf(v) == nil && reflect.ValueOf(vb).Len() == 0 {
				continue
			}
			if reflect.TypeOf(vb) == nil && reflect.ValueOf(v).Len() == 0 {
				continue
			}
			diffKeys = append(diffKeys, diffDebug(k, v, vb))
			isSame = false
			continue
		}
		switch v.(type) {
		case map[string]any:
			// this field in the object is itself an object (e.g.
			// "config" or "container_config"), so recursively
			// compare them
			var nextSkip []string
			prefix := k + ":"
			for _, s := range skip {
				if after, ok0 := strings.CutPrefix(s, prefix); ok0 {
					nextSkip = append(nextSkip, after)
				}
			}
			submiss, subleft, subdiff, ok := compareJSON(v.(map[string]any), vb.(map[string]any), nextSkip)
			missKeys = append(missKeys, addPrefix(submiss, k)...)
			leftKeys = append(leftKeys, addPrefix(subleft, k)...)
			diffKeys = append(diffKeys, addPrefix(subdiff, k)...)
			if !ok {
				isSame = false
			}
		case []any:
			// this field in the object is an array; make sure both
			// arrays have the same set of elements, which is more
			// or less correct for labels and environment
			// variables.
			// this will break if it tries to compare an array of
			// objects like "history", since maps, slices, and
			// functions can't be used as keys in maps
			tmpa := v.([]any)
			tmpb := vb.([]any)
			if len(tmpa) != len(tmpb) {
				diffKeys = append(diffKeys, diffDebug(k, v, vb))
				isSame = false
				break
			}
			m := make(map[any]struct{})
			for i := range len(tmpb) {
				m[tmpb[i]] = struct{}{}
			}
			for i := range len(tmpa) {
				if _, ok := m[tmpa[i]]; !ok {
					diffKeys = append(diffKeys, diffDebug(k, v, vb))
					isSame = false
					break
				}
			}
		default:
			// this field in the object is neither an object nor an
			// array, so assume it's a scalar item
			if !reflect.DeepEqual(v, vb) {
				diffKeys = append(diffKeys, diffDebug(k, v, vb))
				isSame = false
			}
		}
	}

	if len(b) > 0 {
		for k := range b {
			if !contains(skip, k) {
				leftKeys = append(leftKeys, k)
			}
		}
	}

	return slices.Clone(missKeys), slices.Clone(leftKeys), slices.Clone(diffKeys), isSame
}

// configCompareResult summarizes the output of compareJSON for display
func configCompareResult(miss, left, diff []string, notDocker string) string {
	var buffer bytes.Buffer
	if len(miss) > 0 {
		fmt.Fprintf(&buffer, "Fields missing from %s version: %s\n", notDocker, strings.Join(miss, " "))
	}
	if len(left) > 0 {
		fmt.Fprintf(&buffer, "Fields which only exist in %s version: %s\n", notDocker, strings.Join(left, " "))
	}
	if len(diff) > 0 {
		buffer.WriteString("Fields present in both versions have different values:\n")
		tw := tabwriter.NewWriter(&buffer, 1, 1, 8, ' ', 0)
		if _, err := fmt.Fprintf(tw, "Field\tDocker\t%s\n", notDocker); err != nil {
			panic(err)
		}
		for _, d := range diff {
			if _, err := tw.Write([]byte(d)); err != nil {
				panic(err)
			}
		}
		tw.Flush()
	}
	return buffer.String()
}

// fsCompareResult summarizes the output of compareJSON for display
func fsCompareResult(miss, left, diff []string, notDocker string) string {
	var buffer bytes.Buffer
	fixup := func(names []string) []string {
		n := make([]string, 0, len(names))
		for _, name := range names {
			n = append(n, strings.ReplaceAll(strings.ReplaceAll(name, ":(dir):", "/"), "(dir):", "/"))
		}
		return n
	}
	if len(miss) > 0 {
		fmt.Fprintf(&buffer, "Content missing from %s version: %s\n", notDocker, strings.Join(fixup(miss), " "))
	}
	if len(left) > 0 {
		fmt.Fprintf(&buffer, "Content which only exists in %s version: %s\n", notDocker, strings.Join(fixup(left), " "))
	}
	if len(diff) > 0 {
		buffer.WriteString("File attributes in both versions have different values:\n")
		tw := tabwriter.NewWriter(&buffer, 1, 1, 8, ' ', 0)
		if _, err := fmt.Fprintf(tw, "File:attr\tDocker\t%s\n", notDocker); err != nil {
			panic(err)
		}
		for _, d := range fixup(diff) {
			if _, err := tw.Write([]byte(d)); err != nil {
				panic(err)
			}
		}
		tw.Flush()
	}
	return buffer.String()
}

type (
	testCaseTweakContextDirFn func(*testing.T, string, string, string) error
	testCase                  struct {
		name                 string                    // name of the test
		dockerfileContents   string                    // inlined Dockerfile content to use instead of possible file in the build context
		dockerfile           string                    // name of the Dockerfile, relative to contextDir, if not Dockerfile
		contextDir           string                    // name of context subdirectory, if there is one to be copied
		tweakContextDir      testCaseTweakContextDirFn // callback to make updates to the temporary build context before we build it
		shouldFailAt         int                       // line where a build is expected to fail (starts with 1, 0 if it should succeed
		buildahRegex         string                    // if set, expect this to be present in output
		dockerRegex          string                    // if set, expect this to be present in output
		imagebuilderRegex    string                    // if set, expect this to be present in output
		buildahErrRegex      string                    // if set, expect this to be present in output
		dockerErrRegex       string                    // if set, expect this to be present in output
		imagebuilderErrRegex string                    // if set, expect this to be present in output
		failureRegex         string                    // if set, expect this to be present in output when the build fails
		withoutImagebuilder  bool                      // don't build this with imagebuilder, because it depends on a buildah-specific feature
		withoutDocker        bool                      // don't build this with docker, because it depends on a buildah-specific feature
		dockerUseBuildKit    bool                      // if building with docker, request that dockerd use buildkit
		dockerBuilderVersion docker.BuilderVersion     // if building with docker, request the specific builder
		testUsingSetParent   bool                      // test both with old (gets set) and new (left blank) config.Parent behavior
		compatSetParent      types.OptionalBool        // placeholder for a value to set for the buildah CompatSetParent flag
		testUsingVolumes     bool                      // test both with old (preserved) and new (just a config note) volume behavior
		compatVolumes        types.OptionalBool        // placeholder for a value to set for the buildah CompatVolumes flag
		compatScratchConfig  types.OptionalBool        // placeholder for a value to set for the buildah CompatScratchConfig flag
		compatLayerOmissions types.OptionalBool        // value to set for the buildah CompatLayerOmissions flag
		transientMounts      []string                  // one possible buildah-specific feature
		fsSkip               []string                  // expected filesystem differences, typically timestamps on files or directories we create or modify during the build and don't reset
		failOnExtraFSContent bool                      // if set, fail when buildah produces filesystem entries that Docker doesn't

		fsSkipCompatVolumesTrue []string          // more expected filesystem differences when compatVolumes=true
		buildArgs               map[string]string // build args to supply, as if --build-arg was used
	}
)

var internalTestCases = []testCase{
	{
		name:         "shell test",
		dockerfile:   "Dockerfile.shell",
		buildahRegex: "(?s)[0-9a-z]+(.*)--",
		dockerRegex:  "(?s)RUN env.*?Running in [0-9a-z]+(.*?)---",
	},

	{
		name:       "copy-escape-glob",
		contextDir: "copy-escape-glob",
		fsSkip:     []string{"(dir):app:mtime", "(dir):app2:mtime", "(dir):app3:mtime", "(dir):app4:mtime", "(dir):app5:mtime"},
		tweakContextDir: func(t *testing.T, contextDir, _, _ string) error {
			appDir := filepath.Join(contextDir, "app")
			jklDir := filepath.Join(appDir, "jkl?")
			require.NoError(t, os.Mkdir(jklDir, 0o700))
			jklFile := filepath.Join(jklDir, "file.txt")
			require.NoError(t, os.WriteFile(jklFile, []byte("another"), 0o600))
			nopeDir := filepath.Join(appDir, "n?pe")
			require.NoError(t, os.Mkdir(nopeDir, 0o700))
			nopeFile := filepath.Join(nopeDir, "file.txt")
			require.NoError(t, os.WriteFile(nopeFile, []byte("and also"), 0o600))
			stuvDir := filepath.Join(appDir, "st*uv")
			require.NoError(t, os.Mkdir(stuvDir, 0o700))
			stuvFile := filepath.Join(stuvDir, "file.txt")
			require.NoError(t, os.WriteFile(stuvFile, []byte("and yet"), 0o600))
			return nil
		},
		dockerUseBuildKit: true,
	},

	{
		name:         "copy file to root",
		dockerfile:   "Dockerfile.copyfrom_1",
		buildahRegex: "[-rw]+.*?/a",
		fsSkip:       []string{"(dir):a:mtime"},
	},

	{
		name:         "copy file to same file",
		dockerfile:   "Dockerfile.copyfrom_2",
		buildahRegex: "[-rw]+.*?/a",
		fsSkip:       []string{"(dir):a:mtime"},
	},

	{
		name:         "copy file to workdir",
		dockerfile:   "Dockerfile.copyfrom_3",
		buildahRegex: "[-rw]+.*?/b/a",
		fsSkip:       []string{"(dir):b:mtime", "(dir):b:(dir):a:mtime"},
	},

	{
		name:         "copy file to workdir rename",
		dockerfile:   "Dockerfile.copyfrom_3_1",
		buildahRegex: "[-rw]+.*?/b/b",
		fsSkip:       []string{"(dir):b:mtime", "(dir):b:(dir):a:mtime"},
	},

	{
		name:            "copy folder contents to higher level",
		dockerfile:      "Dockerfile.copyfrom_4",
		buildahRegex:    "(?s)[-rw]+.*?/b/1.*?[-rw]+.*?/b/2.*?/b.*?[-rw]+.*?1.*?[-rw]+.*?2",
		buildahErrRegex: "/a: No such file or directory",
		fsSkip:          []string{"(dir):b:mtime"},
	},

	{
		name:            "copy wildcard folder contents to higher level",
		dockerfile:      "Dockerfile.copyfrom_5",
		buildahRegex:    "(?s)[-rw]+.*?/b/1.*?[-rw]+.*?/b/2.*?/b.*?[-rw]+.*?1.*?[-rw]+.*?2",
		buildahErrRegex: "(?s)/a: No such file or directory.*?/b/a: No such file or directory.*?/b/b: No such file or director",
		fsSkip:          []string{"(dir):b:mtime", "(dir):b:(dir):1:mtime", "(dir):b:(dir):2:mtime"},
	},

	{
		name:            "copy folder with dot contents to higher level",
		dockerfile:      "Dockerfile.copyfrom_6",
		buildahRegex:    "(?s)[-rw]+.*?/b/1.*?[-rw]+.*?/b/2.*?/b.*?[-rw]+.*?1.*?[-rw]+.*?2",
		buildahErrRegex: "(?s)/a: No such file or directory.*?/b/a: No such file or directory.*?/b/b: No such file or director",
		fsSkip:          []string{"(dir):b:mtime", "(dir):b:(dir):1:mtime", "(dir):b:(dir):2:mtime"},
	},

	{
		name:            "copy root file to different root name",
		dockerfile:      "Dockerfile.copyfrom_7",
		buildahRegex:    "[-rw]+.*?/a",
		buildahErrRegex: "/b: No such file or directory",
		fsSkip:          []string{"(dir):a:mtime"},
	},

	{
		name:            "copy nested file to different root name",
		dockerfile:      "Dockerfile.copyfrom_8",
		buildahRegex:    "[-rw]+.*?/a",
		buildahErrRegex: "/b: No such file or directory",
		fsSkip:          []string{"(dir):a:mtime"},
	},

	{
		name:            "copy file to deeper directory with explicit slash",
		dockerfile:      "Dockerfile.copyfrom_9",
		buildahRegex:    "[-rw]+.*?/a/b/c/1",
		buildahErrRegex: "/a/b/1: No such file or directory",
		fsSkip:          []string{"(dir):a:mtime", "(dir):a:(dir):b:mtime", "(dir):a:(dir):b:(dir):c:mtime", "(dir):a:(dir):b:(dir):c:(dir):1:mtime"},
	},

	{
		name:            "copy file to deeper directory without explicit slash",
		dockerfile:      "Dockerfile.copyfrom_10",
		buildahRegex:    "[-rw]+.*?/a/b/c",
		buildahErrRegex: "/a/b/1: No such file or directory",
		fsSkip:          []string{"(dir):a:mtime", "(dir):a:(dir):b:mtime", "(dir):a:(dir):b:(dir):c:mtime"},
	},

	{
		name:            "copy directory to deeper directory without explicit slash",
		dockerfile:      "Dockerfile.copyfrom_11",
		buildahRegex:    "[-rw]+.*?/a/b/c/1",
		buildahErrRegex: "/a/b/1: No such file or directory",
		fsSkip: []string{
			"(dir):a:mtime", "(dir):a:(dir):b:mtime", "(dir):a:(dir):b:(dir):c:mtime",
			"(dir):a:(dir):b:(dir):c:(dir):1:mtime",
		},
	},

	{
		name:            "copy directory to root without explicit slash",
		dockerfile:      "Dockerfile.copyfrom_12",
		buildahRegex:    "[-rw]+.*?/a/1",
		buildahErrRegex: "/a/a: No such file or directory",
		fsSkip:          []string{"(dir):a:mtime", "(dir):a:(dir):1:mtime"},
	},

	{
		name:            "copy directory trailing to root without explicit slash",
		dockerfile:      "Dockerfile.copyfrom_13",
		buildahRegex:    "[-rw]+.*?/a/1",
		buildahErrRegex: "/a/a: No such file or directory",
		fsSkip:          []string{"(dir):a:mtime", "(dir):a:(dir):1:mtime"},
	},

	{
		name:         "multi stage base",
		dockerfile:   "Dockerfile.reusebase",
		buildahRegex: "[0-9a-z]+ /1",
		fsSkip:       []string{"(dir):1:mtime"},
	},

	{
		name:       "directory",
		contextDir: "dir",
		fsSkip:     []string{"(dir):dir:mtime", "(dir):test:mtime"},
	},

	{
		name:       "copy to dir",
		contextDir: "copy",
		fsSkip:     []string{"(dir):usr:(dir):bin:mtime"},
	},

	{
		name:       "copy dir",
		contextDir: "copydir",
		fsSkip:     []string{"(dir):dir"},
	},

	{
		name:       "copy from symlink source",
		contextDir: "copysymlink",
	},

	{
		name:       "copy-symlink-2",
		contextDir: "copysymlink",
		dockerfile: "Dockerfile2",
	},

	{
		name:       "copy from subdir to new directory",
		contextDir: "copydir",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"COPY dir/file /subdir/",
		}, "\n"),
		fsSkip:              []string{"(dir):subdir"},
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name:       "copy to renamed file",
		contextDir: "copyrename",
		fsSkip:     []string{"(dir):usr:(dir):bin:mtime"},
	},

	{
		name:       "copy with --chown",
		contextDir: "copychown",
		fsSkip:     []string{"(dir):usr:(dir):bin:mtime", "(dir):usr:(dir):local:(dir):bin:mtime"},
	},

	{
		name:       "directory with slash",
		contextDir: "overlapdirwithslash",
	},

	{
		name:       "directory without slash",
		contextDir: "overlapdirwithoutslash",
	},

	{
		name:         "environment",
		dockerfile:   "Dockerfile.env",
		shouldFailAt: 12,
	},

	{
		name:       "edgecases",
		dockerfile: "Dockerfile.edgecases",
		fsSkip: []string{
			"(dir):test:mtime", "(dir):test:(dir):copy:mtime", "(dir):test2:mtime", "(dir):test3:mtime",
			"(dir):test3:(dir):copy:mtime",
			"(dir):test3:(dir):test:mtime", "(dir):tmp:mtime", "(dir):tmp:(dir):passwd:mtime",
		},
	},

	{
		name:               "exposed default",
		dockerfile:         "Dockerfile.exposedefault",
		testUsingSetParent: true,
	},

	{
		name:       "add",
		dockerfile: "Dockerfile.add",
		fsSkip:     []string{"(dir):b:mtime", "(dir):tmp:mtime"},
	},

	{
		name:         "run with JSON",
		dockerfile:   "Dockerfile.run.args",
		buildahRegex: "(first|third|fifth|inner) (second|fourth|sixth|outer)",
		dockerRegex:  "Running in [0-9a-z]+.*?(first|third|fifth|inner) (second|fourth|sixth|outer)",
	},

	{
		name:       "wildcard",
		contextDir: "wildcard",
		fsSkip:     []string{"(dir):usr:mtime", "(dir):usr:(dir):test:mtime"},
	},

	{
		name:             "volume",
		contextDir:       "volume",
		fsSkip:           []string{"(dir):var:mtime", "(dir):var:(dir):www:mtime"},
		testUsingVolumes: true,
	},

	{
		name:             "volumerun",
		contextDir:       "volumerun",
		fsSkip:           []string{"(dir):var:mtime", "(dir):var:(dir):www:mtime"},
		testUsingVolumes: true,
	},

	{
		name:            "mount",
		contextDir:      "mount",
		buildahRegex:    "/tmp/test/file.*?regular file.*?/tmp/test/file2.*?regular file",
		withoutDocker:   true,
		transientMounts: []string{"@@TEMPDIR@@:/tmp/test" + selinuxMountFlag()},
	},

	{
		name:          "transient-mount",
		contextDir:    "transientmount",
		buildahRegex:  "file2.*?FROM mirror.gcr.io/busybox ENV name value",
		withoutDocker: true,
		transientMounts: []string{
			"@@TEMPDIR@@:/mountdir" + selinuxMountFlag(),
			"@@TEMPDIR@@/Dockerfile.env:/mountfile" + selinuxMountFlag(),
		},
	},

	{
		// from internal team chat
		name: "ci-pipeline-modified",
		dockerfileContents: strings.Join([]string{
			"FROM mirror.gcr.io/busybox",
			"WORKDIR /go/src/github.com/openshift/ocp-release-operator-sdk/",
			"ENV GOPATH=/go",
			"RUN env | grep -E -v '^(HOSTNAME|OLDPWD)=' | LANG=C sort | tee /env-contents.txt\n",
		}, "\n"),
		fsSkip: []string{
			"(dir):go:mtime",
			"(dir):go:(dir):src:mtime",
			"(dir):go:(dir):src:(dir):github.com:mtime",
			"(dir):go:(dir):src:(dir):github.com:(dir):openshift:mtime",
			"(dir):go:(dir):src:(dir):github.com:(dir):openshift:(dir):ocp-release-operator-sdk:mtime",
			"(dir):env-contents.txt:mtime",
		},
	},

	{
		name:          "add-permissions",
		withoutDocker: true,
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"# Does ADD preserve permissions differently for archives and files?",
			"ADD archive.tar subdir1/",
			"ADD archive/ subdir2/",
		}, "\n"),
		tweakContextDir: func(_ *testing.T, contextDir, _, _ string) (err error) {
			content := []byte("test content")

			if err := os.Mkdir(filepath.Join(contextDir, "archive"), 0o755); err != nil {
				return fmt.Errorf("creating subdirectory of temporary context directory: %w", err)
			}
			filename := filepath.Join(contextDir, "archive", "should-be-owned-by-root")
			if err = os.WriteFile(filename, content, 0o640); err != nil {
				return fmt.Errorf("creating file owned by root in temporary context directory: %w", err)
			}
			if err = os.Chown(filename, 0, 0); err != nil {
				return fmt.Errorf("setting ownership on file owned by root in temporary context directory: %w", err)
			}
			if err = os.Chtimes(filename, testDate, testDate); err != nil {
				return fmt.Errorf("setting date on file owned by root file in temporary context directory: %w", err)
			}
			filename = filepath.Join(contextDir, "archive", "should-be-owned-by-99")
			if err = os.WriteFile(filename, content, 0o640); err != nil {
				return fmt.Errorf("creating file owned by 99 in temporary context directory: %w", err)
			}
			if err = os.Chown(filename, 99, 99); err != nil {
				return fmt.Errorf("setting ownership on file owned by 99 in temporary context directory: %w", err)
			}
			if err = os.Chtimes(filename, testDate, testDate); err != nil {
				return fmt.Errorf("setting date on file owned by 99 in temporary context directory: %w", err)
			}

			filename = filepath.Join(contextDir, "archive.tar")
			f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY, 0o644)
			if err != nil {
				return fmt.Errorf("creating archive file: %w", err)
			}
			defer f.Close()
			tw := tar.NewWriter(f)
			defer tw.Close()
			err = tw.WriteHeader(&tar.Header{
				Name:     "archive/should-be-owned-by-root",
				Typeflag: tar.TypeReg,
				Size:     int64(len(content)),
				ModTime:  testDate,
				Mode:     0o640,
				Uid:      0,
				Gid:      0,
			})
			if err != nil {
				return fmt.Errorf("writing archive file header: %w", err)
			}
			n, err := tw.Write(content)
			if err != nil {
				return fmt.Errorf("writing archive file contents: %w", err)
			}
			if n != len(content) {
				return errors.New("short write writing archive file contents")
			}
			err = tw.WriteHeader(&tar.Header{
				Name:     "archive/should-be-owned-by-99",
				Typeflag: tar.TypeReg,
				Size:     int64(len(content)),
				ModTime:  testDate,
				Mode:     0o640,
				Uid:      99,
				Gid:      99,
			})
			if err != nil {
				return fmt.Errorf("writing archive file header: %w", err)
			}
			n, err = tw.Write(content)
			if err != nil {
				return fmt.Errorf("writing archive file contents: %w", err)
			}
			if n != len(content) {
				return errors.New("short write writing archive file contents")
			}
			return nil
		},
		fsSkip: []string{"(dir):subdir1:mtime", "(dir):subdir2:mtime"},
	},

	{
		name: "copy-permissions",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"# Does COPY --chown change permissions on already-present directories?",
			"COPY subdir/ subdir/",
			"COPY --chown=99:99 subdir/ subdir/",
		}, "\n"),
		tweakContextDir: func(_ *testing.T, contextDir, _, _ string) (err error) {
			content := []byte("test content")

			if err := os.Mkdir(filepath.Join(contextDir, "subdir"), 0o755); err != nil {
				return fmt.Errorf("creating subdirectory of temporary context directory: %w", err)
			}
			filename := filepath.Join(contextDir, "subdir", "would-be-owned-by-root")
			if err = os.WriteFile(filename, content, 0o640); err != nil {
				return fmt.Errorf("creating file owned by root in temporary context directory: %w", err)
			}
			if err = os.Chown(filename, 0, 0); err != nil {
				return fmt.Errorf("setting ownership on file owned by root in temporary context directory: %w", err)
			}
			if err = os.Chtimes(filename, testDate, testDate); err != nil {
				return fmt.Errorf("setting date on file owned by root file in temporary context directory: %w", err)
			}
			filename = filepath.Join(contextDir, "subdir", "would-be-owned-by-99")
			if err = os.WriteFile(filename, content, 0o640); err != nil {
				return fmt.Errorf("creating file owned by 99 in temporary context directory: %w", err)
			}
			if err = os.Chown(filename, 99, 99); err != nil {
				return fmt.Errorf("setting ownership on file owned by 99 in temporary context directory: %w", err)
			}
			if err = os.Chtimes(filename, testDate, testDate); err != nil {
				return fmt.Errorf("setting date on file owned by 99 in temporary context directory: %w", err)
			}
			return nil
		},
		fsSkip:              []string{"(dir):subdir:mtime"},
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name: "copy-permissions-implicit",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"# Does COPY --chown change permissions on already-present directories?",
			"COPY --chown=99:99 subdir/ subdir/",
			"COPY subdir/ subdir/",
		}, "\n"),
		tweakContextDir: func(_ *testing.T, contextDir, _, _ string) (err error) {
			content := []byte("test content")

			if err := os.Mkdir(filepath.Join(contextDir, "subdir"), 0o755); err != nil {
				return fmt.Errorf("creating subdirectory of temporary context directory: %w", err)
			}
			filename := filepath.Join(contextDir, "subdir", "would-be-owned-by-root")
			if err = os.WriteFile(filename, content, 0o640); err != nil {
				return fmt.Errorf("creating file owned by root in temporary context directory: %w", err)
			}
			if err = os.Chown(filename, 0, 0); err != nil {
				return fmt.Errorf("setting ownership on file owned by root in temporary context directory: %w", err)
			}
			if err = os.Chtimes(filename, testDate, testDate); err != nil {
				return fmt.Errorf("setting date on file owned by root file in temporary context directory: %w", err)
			}
			filename = filepath.Join(contextDir, "subdir", "would-be-owned-by-99")
			if err = os.WriteFile(filename, content, 0o640); err != nil {
				return fmt.Errorf("creating file owned by 99 in temporary context directory: %w", err)
			}
			if err = os.Chown(filename, 99, 99); err != nil {
				return fmt.Errorf("setting ownership on file owned by 99 in temporary context directory: %w", err)
			}
			if err = os.Chtimes(filename, testDate, testDate); err != nil {
				return fmt.Errorf("setting date on file owned by 99 in temporary context directory: %w", err)
			}
			return nil
		},
		fsSkip:              []string{"(dir):subdir:mtime"},
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		// the digest just ensures that we can handle a digest
		// reference to a manifest list; the digest of any manifest
		// list in the image repository would do
		name: "stage-container-as-source-plus-hardlinks",
		dockerfileContents: strings.Join([]string{
			"FROM mirror.gcr.io/busybox@sha256:9ae97d36d26566ff84e8893c64a6dc4fe8ca6d1144bf5b87b2b85a32def253c7 AS build",
			"RUN mkdir -p /target/subdir",
			"RUN cp -p /etc/passwd /target/",
			"RUN ln /target/passwd /target/subdir/passwd",
			"RUN ln /target/subdir/passwd /target/subdir/passwd2",
			"FROM scratch",
			"COPY --from=build /target/subdir /subdir",
		}, "\n"),
		fsSkip:              []string{"(dir):subdir:mtime"},
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name:                "dockerfile-in-subdirectory",
		dockerfile:          "subdir/Dockerfile",
		contextDir:          "subdir",
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name: "setuid-file-in-context",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			fmt.Sprintf("# Does the setuid file (0%o) in the context dir end up setuid in the image?", syscall.S_ISUID),
			"COPY . subdir1",
			"ADD  . subdir2",
		}, "\n"),
		tweakContextDir: func(_ *testing.T, contextDir, _, _ string) (err error) {
			filename := filepath.Join(contextDir, "should-be-setuid-file")
			if err = os.WriteFile(filename, []byte("test content"), 0o644); err != nil {
				return fmt.Errorf("creating setuid test file in temporary context directory: %w", err)
			}
			if err = syscall.Chmod(filename, syscall.S_ISUID|0o755); err != nil {
				return fmt.Errorf("setting setuid bit on test file in temporary context directory: %w", err)
			}
			if err = os.Chtimes(filename, testDate, testDate); err != nil {
				return fmt.Errorf("setting date on setuid test file in temporary context directory: %w", err)
			}
			filename = filepath.Join(contextDir, "should-be-setgid-file")
			if err = os.WriteFile(filename, []byte("test content"), 0o644); err != nil {
				return fmt.Errorf("creating setgid test file in temporary context directory: %w", err)
			}
			if err = syscall.Chmod(filename, syscall.S_ISGID|0o755); err != nil {
				return fmt.Errorf("setting setgid bit on test file in temporary context directory: %w", err)
			}
			if err = os.Chtimes(filename, testDate, testDate); err != nil {
				return fmt.Errorf("setting date on setgid test file in temporary context directory: %w", err)
			}
			filename = filepath.Join(contextDir, "should-be-sticky-file")
			if err = os.WriteFile(filename, []byte("test content"), 0o644); err != nil {
				return fmt.Errorf("creating sticky test file in temporary context directory: %w", err)
			}
			if err = syscall.Chmod(filename, syscall.S_ISVTX|0o755); err != nil {
				return fmt.Errorf("setting permissions on sticky test file in temporary context directory: %w", err)
			}
			if err = os.Chtimes(filename, testDate, testDate); err != nil {
				return fmt.Errorf("setting date on sticky test file in temporary context directory: %w", err)
			}
			filename = filepath.Join(contextDir, "should-not-be-setuid-setgid-sticky-file")
			if err = os.WriteFile(filename, []byte("test content"), 0o644); err != nil {
				return fmt.Errorf("creating non-suid non-sgid non-sticky test file in temporary context directory: %w", err)
			}
			if err = syscall.Chmod(filename, 0o640); err != nil {
				return fmt.Errorf("setting permissions on plain test file in temporary context directory: %w", err)
			}
			if err = os.Chtimes(filename, testDate, testDate); err != nil {
				return fmt.Errorf("setting date on plain test file in temporary context directory: %w", err)
			}
			return nil
		},
		fsSkip:              []string{"(dir):subdir1:mtime", "(dir):subdir2:mtime"},
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name: "xattrs-file-in-context",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"# Do the xattrs on a file in the context dir end up in the image?",
			"COPY . subdir1",
			"ADD  . subdir2",
		}, "\n"),
		tweakContextDir: func(t *testing.T, contextDir, storageDriver, storageRoot string) (err error) {
			if !*contextCanDoXattrs {
				t.Skipf("test context directory %q doesn't support xattrs", contextDir)
			}
			if !*storageCanDoXattrs {
				t.Skipf("test storage driver %q and directory %q don't support xattrs together", storageDriver, storageRoot)
			}

			filename := filepath.Join(contextDir, "xattrs-file")
			if err = os.WriteFile(filename, []byte("test content"), 0o644); err != nil {
				return fmt.Errorf("creating test file with xattrs in temporary context directory: %w", err)
			}
			if err = copier.Lsetxattrs(filename, map[string]string{"user.a": "test"}); err != nil {
				return fmt.Errorf("setting xattrs on test file in temporary context directory: %w", err)
			}
			if err = syscall.Chmod(filename, 0o640); err != nil {
				return fmt.Errorf("setting permissions on test file in temporary context directory: %w", err)
			}
			if err = os.Chtimes(filename, testDate, testDate); err != nil {
				return fmt.Errorf("setting date on test file in temporary context directory: %w", err)
			}
			return nil
		},
		fsSkip:              []string{"(dir):subdir1:mtime", "(dir):subdir2:mtime"},
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name: "from-scratch-simple-file", // Fix On ARM64: fields missing from buildah version: variant
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"COPY file-a.txt /",
		}, "\n"),
		contextDir:          "dockerignore/populated",
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name: "setuid-file-in-archive",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			fmt.Sprintf("# Do the setuid/setgid/sticky files in this archive end up setuid(0%o)/setgid(0%o)/sticky(0%o)?", syscall.S_ISUID, syscall.S_ISGID, syscall.S_ISVTX),
			"ADD archive.tar .",
		}, "\n"),
		tweakContextDir: func(_ *testing.T, contextDir, _, _ string) (err error) {
			filename := filepath.Join(contextDir, "archive.tar")
			f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY, 0o644)
			if err != nil {
				return fmt.Errorf("creating new archive file in temporary context directory: %w", err)
			}
			defer f.Close()
			tw := tar.NewWriter(f)
			defer tw.Close()
			hdr := tar.Header{
				Name:     "setuid-file",
				Uid:      0,
				Gid:      0,
				Typeflag: tar.TypeReg,
				Size:     8,
				Mode:     cISUID | 0o755,
				ModTime:  testDate,
			}
			if err = tw.WriteHeader(&hdr); err != nil {
				return fmt.Errorf("writing tar archive header: %w", err)
			}
			if _, err = io.Copy(tw, bytes.NewReader([]byte("whatever"))); err != nil {
				return fmt.Errorf("writing tar archive content: %w", err)
			}
			hdr = tar.Header{
				Name:     "setgid-file",
				Uid:      0,
				Gid:      0,
				Typeflag: tar.TypeReg,
				Size:     8,
				Mode:     cISGID | 0o755,
				ModTime:  testDate,
			}
			if err = tw.WriteHeader(&hdr); err != nil {
				return fmt.Errorf("writing tar archive header: %w", err)
			}
			if _, err = io.Copy(tw, bytes.NewReader([]byte("whatever"))); err != nil {
				return fmt.Errorf("writing tar archive content: %w", err)
			}
			hdr = tar.Header{
				Name:     "sticky-file",
				Uid:      0,
				Gid:      0,
				Typeflag: tar.TypeReg,
				Size:     8,
				Mode:     cISVTX | 0o755,
				ModTime:  testDate,
			}
			if err = tw.WriteHeader(&hdr); err != nil {
				return fmt.Errorf("writing tar archive header: %w", err)
			}
			if _, err = io.Copy(tw, bytes.NewReader([]byte("whatever"))); err != nil {
				return fmt.Errorf("writing tar archive content: %w", err)
			}
			hdr = tar.Header{
				Name:     "setuid-dir",
				Uid:      0,
				Gid:      0,
				Typeflag: tar.TypeDir,
				Size:     0,
				Mode:     cISUID | 0o755,
				ModTime:  testDate,
			}
			if err = tw.WriteHeader(&hdr); err != nil {
				return fmt.Errorf("error writing tar archive header: %w", err)
			}
			hdr = tar.Header{
				Name:     "setgid-dir",
				Uid:      0,
				Gid:      0,
				Typeflag: tar.TypeDir,
				Size:     0,
				Mode:     cISGID | 0o755,
				ModTime:  testDate,
			}
			if err = tw.WriteHeader(&hdr); err != nil {
				return fmt.Errorf("error writing tar archive header: %w", err)
			}
			hdr = tar.Header{
				Name:     "sticky-dir",
				Uid:      0,
				Gid:      0,
				Typeflag: tar.TypeDir,
				Size:     0,
				Mode:     cISVTX | 0o755,
				ModTime:  testDate,
			}
			if err = tw.WriteHeader(&hdr); err != nil {
				return fmt.Errorf("error writing tar archive header: %w", err)
			}
			return nil
		},
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name: "xattrs-file-in-archive",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"# Do the xattrs on a file in an archive end up in the image?",
			"ADD archive.tar .",
		}, "\n"),
		tweakContextDir: func(t *testing.T, contextDir, storageDriver, storageRoot string) (err error) {
			if !*contextCanDoXattrs {
				t.Skipf("test context directory %q doesn't support xattrs", contextDir)
			}
			if !*storageCanDoXattrs {
				t.Skipf("test storage driver %q and directory %q don't support xattrs together", storageDriver, storageRoot)
			}

			filename := filepath.Join(contextDir, "archive.tar")
			f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY, 0o644)
			if err != nil {
				return fmt.Errorf("creating new archive file in temporary context directory: %w", err)
			}
			defer f.Close()
			tw := tar.NewWriter(f)
			defer tw.Close()
			hdr := tar.Header{
				Name:     "xattr-file",
				Uid:      0,
				Gid:      0,
				Typeflag: tar.TypeReg,
				Size:     8,
				Mode:     0o640,
				ModTime:  testDate,
				PAXRecords: map[string]string{
					xattrPAXRecordNamespace + "user.a": "test",
				},
			}
			if err = tw.WriteHeader(&hdr); err != nil {
				return fmt.Errorf("writing tar archive header: %w", err)
			}
			if _, err = io.Copy(tw, bytes.NewReader([]byte("whatever"))); err != nil {
				return fmt.Errorf("writing tar archive content: %w", err)
			}
			return nil
		},
		fsSkip:              []string{"(dir):subdir1:mtime", "(dir):subdir2:mtime"},
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		// docker build apparently stopped preserving this bit somewhere between 18.09.7 and 19.03,
		// possibly around https://github.com/moby/moby/pull/38599
		name: "setuid-file-in-other-stage",
		dockerfileContents: strings.Join([]string{
			"FROM mirror.gcr.io/busybox",
			"RUN mkdir /a && echo whatever > /a/setuid && chmod u+xs /a/setuid && touch -t @1485449953 /a/setuid",
			"RUN mkdir /b && echo whatever > /b/setgid && chmod g+xs /b/setgid && touch -t @1485449953 /b/setgid",
			"RUN mkdir /c && echo whatever > /c/sticky && chmod o+x /c/sticky && chmod +t /c/sticky && touch -t @1485449953 /c/sticky",
			"FROM scratch",
			fmt.Sprintf("# Does this setuid/setgid/sticky file copied from another stage end up setuid/setgid/sticky (0%o/0%o/0%o)?", syscall.S_ISUID, syscall.S_ISGID, syscall.S_ISVTX),
			"COPY --from=0 /a/setuid /b/setgid /c/sticky /",
		}, "\n"),
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name: "xattrs-file-in-other-stage",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"COPY . .",
			"FROM scratch",
			"# Do the xattrs on a file in another stage end up in the image?",
			"COPY --from=0 /xattrs-file /",
		}, "\n"),
		tweakContextDir: func(t *testing.T, contextDir, storageDriver, storageRoot string) (err error) {
			if !*contextCanDoXattrs {
				t.Skipf("test context directory %q doesn't support xattrs", contextDir)
			}
			if !*storageCanDoXattrs {
				t.Skipf("test storage driver %q and directory %q don't support xattrs together", storageDriver, storageRoot)
			}

			filename := filepath.Join(contextDir, "xattrs-file")
			if err = os.WriteFile(filename, []byte("test content"), 0o644); err != nil {
				return fmt.Errorf("creating test file with xattrs in temporary context directory: %w", err)
			}
			if err = copier.Lsetxattrs(filename, map[string]string{"user.a": "test"}); err != nil {
				return fmt.Errorf("setting xattrs on test file in temporary context directory: %w", err)
			}
			if err = syscall.Chmod(filename, 0o640); err != nil {
				return fmt.Errorf("setting permissions on test file in temporary context directory: %w", err)
			}
			if err = os.Chtimes(filename, testDate, testDate); err != nil {
				return fmt.Errorf("setting date on test file in temporary context directory: %w", err)
			}
			return nil
		},
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name: "copy-multiple-some-missing",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"COPY file-a.txt subdir-a file-z.txt subdir-z subdir/",
		}, "\n"),
		contextDir:   "dockerignore/populated",
		shouldFailAt: 2,
	},

	{
		name: "copy-multiple-missing-file-with-glob",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"COPY file-z.txt subdir-* subdir/",
		}, "\n"),
		contextDir:   "dockerignore/populated",
		shouldFailAt: 2,
	},

	{
		name: "copy-multiple-missing-file-with-nomatch-on-glob",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"COPY missing* subdir/",
		}, "\n"),
		contextDir:   "dockerignore/populated",
		shouldFailAt: 2,
	},

	{
		name: "copy-multiple-some-missing-glob",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"COPY file-a.txt subdir-* file-?.txt missing* subdir/",
		}, "\n"),
		contextDir:           "dockerignore/populated",
		fsSkip:               []string{"(dir):subdir:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolTrue,
	},

	{
		name: "file-in-workdir-in-other-stage",
		dockerfileContents: strings.Join([]string{
			"FROM scratch AS base",
			"COPY . /subdir/",
			"WORKDIR /subdir",
			"FROM base",
			"COPY --from=base . .", // --from=otherstage ignores that stage's WORKDIR
		}, "\n"),
		contextDir: "dockerignore/populated",
		fsSkip:     []string{"(dir):subdir:mtime", "(dir):subdir:(dir):subdir:mtime"},
	},

	{
		name:         "copy-integration1",
		contextDir:   "dockerignore/integration1",
		shouldFailAt: 3,
		failureRegex: "(no such file or directory)|(file not found)|(file does not exist)",
	},

	{
		name:                "copy-integration2",
		contextDir:          "dockerignore/integration2",
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name:         "copy-integration3",
		contextDir:   "dockerignore/integration3",
		shouldFailAt: 4,
		failureRegex: "(no such file or directory)|(file not found)|(file does not exist)",
	},

	{
		name:       "copy-empty-1",
		contextDir: "copyempty",
		dockerfile: "Dockerfile",
		fsSkip:     []string{"(dir):usr:(dir):local:mtime", "(dir):usr:(dir):local:(dir):tmp:mtime"},
	},

	{
		name:       "copy-empty-2",
		contextDir: "copyempty",
		dockerfile: "Dockerfile2",
		fsSkip:     []string{"(dir):usr:(dir):local:mtime", "(dir):usr:(dir):local:(dir):tmp:mtime"},
	},

	{
		name:       "copy-absolute-directory-1",
		contextDir: "copyblahblub",
		dockerfile: "Dockerfile",
		fsSkip:     []string{"(dir):var:mtime"},
	},

	{
		name:       "copy-absolute-directory-2",
		contextDir: "copyblahblub",
		dockerfile: "Dockerfile2",
		fsSkip:     []string{"(dir):var:mtime"},
	},

	{
		name:       "copy-absolute-directory-3",
		contextDir: "copyblahblub",
		dockerfile: "Dockerfile3",
		fsSkip:     []string{"(dir):var:mtime"},
	},

	{
		name: "multi-stage-through-base",
		dockerfileContents: strings.Join([]string{
			"FROM mirror.gcr.io/alpine AS base",
			"RUN touch -t @1485449953 /1",
			"ENV LOCAL=/1",
			"RUN find $LOCAL",
			"FROM base",
			"RUN find $LOCAL",
		}, "\n"),
		fsSkip: []string{"(dir):root:mtime", "(dir):1:mtime"},
	},

	{
		name: "multi-stage-derived", // from #2415
		dockerfileContents: strings.Join([]string{
			"FROM mirror.gcr.io/busybox as layer",
			"RUN touch /root/layer",
			"FROM layer as derived",
			"RUN touch -t @1485449953 /root/derived ; rm /root/layer",
			"FROM mirror.gcr.io/busybox AS output",
			"COPY --from=layer /root /root",
		}, "\n"),
		fsSkip: []string{"(dir):root:mtime", "(dir):root:(dir):layer:mtime"},
	},

	{
		name:          "dockerignore-minimal-test", // from #2237
		contextDir:    "dockerignore/minimal_test",
		withoutDocker: true,
		fsSkip:        []string{"(dir):tmp:mtime", "(dir):tmp:(dir):stuff:mtime"},
	},

	{
		name:                 "dockerignore-is-even-there",
		contextDir:           "dockerignore/empty",
		fsSkip:               []string{"(dir):subdir:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolTrue,
	},

	{
		name:       "dockerignore-irrelevant",
		contextDir: "dockerignore/empty",
		tweakContextDir: func(_ *testing.T, contextDir, _, _ string) (err error) {
			dockerignore := []byte(strings.Join([]string{"**/*-a", "!**/*-c"}, "\n"))
			if err := os.WriteFile(filepath.Join(contextDir, ".dockerignore"), dockerignore, 0o600); err != nil {
				return fmt.Errorf("writing .dockerignore file: %w", err)
			}
			if err = os.Chtimes(filepath.Join(contextDir, ".dockerignore"), testDate, testDate); err != nil {
				return fmt.Errorf("setting date on .dockerignore file in temporary context directory: %w", err)
			}
			return nil
		},
		fsSkip:               []string{"(dir):subdir:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolTrue,
	},

	{
		name: "dockerignore-exceptions-dot",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"COPY . subdir/",
		}, "\n"),
		contextDir: "dockerignore/populated",
		tweakContextDir: func(_ *testing.T, contextDir, _, _ string) (err error) {
			dockerignore := []byte(strings.Join([]string{"**/*-a", "!**/*-c"}, "\n"))
			if err := os.WriteFile(filepath.Join(contextDir, ".dockerignore"), dockerignore, 0o644); err != nil {
				return fmt.Errorf("writing .dockerignore file: %w", err)
			}
			if err = os.Chtimes(filepath.Join(contextDir, ".dockerignore"), testDate, testDate); err != nil {
				return fmt.Errorf("setting date on .dockerignore file in temporary context directory: %w", err)
			}
			return nil
		},
		fsSkip:               []string{"(dir):subdir:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolTrue,
	},

	{
		name: "dockerignore-extensions-star",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"COPY * subdir/",
		}, "\n"),
		contextDir: "dockerignore/populated",
		tweakContextDir: func(_ *testing.T, contextDir, _, _ string) (err error) {
			dockerignore := []byte(strings.Join([]string{"**/*-a", "!**/*-c"}, "\n"))
			if err := os.WriteFile(filepath.Join(contextDir, ".dockerignore"), dockerignore, 0o600); err != nil {
				return fmt.Errorf("writing .dockerignore file: %w", err)
			}
			if err = os.Chtimes(filepath.Join(contextDir, ".dockerignore"), testDate, testDate); err != nil {
				return fmt.Errorf("setting date on .dockerignore file in temporary context directory: %w", err)
			}
			return nil
		},
		fsSkip:               []string{"(dir):subdir:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolTrue,
	},

	{
		name: "dockerignore-includes-dot",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"COPY . subdir/",
		}, "\n"),
		contextDir: "dockerignore/populated",
		tweakContextDir: func(_ *testing.T, contextDir, _, _ string) (err error) {
			dockerignore := []byte(strings.Join([]string{"!**/*-c"}, "\n"))
			if err := os.WriteFile(filepath.Join(contextDir, ".dockerignore"), dockerignore, 0o640); err != nil {
				return fmt.Errorf("writing .dockerignore file: %w", err)
			}
			if err = os.Chtimes(filepath.Join(contextDir, ".dockerignore"), testDate, testDate); err != nil {
				return fmt.Errorf("setting date on .dockerignore file in temporary context directory: %w", err)
			}
			return nil
		},
		fsSkip:               []string{"(dir):subdir:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolTrue,
	},

	{
		name: "add--exclude-includes-star",
		dockerfileContents: strings.Join([]string{
			"# syntax=mirror.gcr.io/docker/dockerfile:1.9-labs",
			"FROM scratch",
			"ADD --exclude=**/*-c ./ subdir/",
		}, "\n"),
		contextDir:           "dockerignore/populated",
		fsSkip:               []string{"(dir):subdir:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolFalse,
		dockerUseBuildKit:    true,
	},

	{
		name: "add--exclude-includes-slash",
		dockerfileContents: strings.Join([]string{
			"# syntax=mirror.gcr.io/docker/dockerfile:1.9-labs",
			"FROM scratch",
			"ADD --exclude=*.txt / subdir/",
		}, "\n"),
		contextDir:           "dockerignore/populated",
		fsSkip:               []string{"(dir):subdir:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolFalse,
		dockerUseBuildKit:    true,
	},

	{
		name: "add--exclude-includes-dot",
		dockerfileContents: strings.Join([]string{
			"# syntax=mirror.gcr.io/docker/dockerfile:1.9-labs",
			"FROM scratch",
			"ADD --exclude=*-c . subdir/",
		}, "\n"),
		contextDir:           "dockerignore/populated",
		fsSkip:               []string{"(dir):subdir:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolFalse,
		dockerUseBuildKit:    true,
	},
	{
		name: "copy--exclude-includes-subdir-slash",
		dockerfileContents: strings.Join([]string{
			"# syntax=mirror.gcr.io/docker/dockerfile:1.9-labs",
			"FROM scratch",
			"COPY --exclude=**/*-c / subdir/",
		}, "\n"),
		contextDir:           "dockerignore/populated",
		fsSkip:               []string{"(dir):subdir:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolFalse,
		dockerUseBuildKit:    true,
	},

	{
		name: "copy--exclude-includes-dot-slash",
		dockerfileContents: strings.Join([]string{
			"# syntax=mirror.gcr.io/docker/dockerfile:1.9-labs",
			"FROM scratch",
			"COPY --exclude='!**/*-c' ./ subdir/",
		}, "\n"),
		contextDir:           "dockerignore/populated",
		fsSkip:               []string{"(dir):subdir:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolFalse,
		dockerUseBuildKit:    true,
	},

	{
		name: "copy--exclude-includes-slash",
		dockerfileContents: strings.Join([]string{
			"# syntax=mirror.gcr.io/docker/dockerfile:1.9-labs",
			"FROM scratch",
			"COPY --exclude='!**/*-c' . subdir/",
		}, "\n"),
		contextDir:           "dockerignore/populated",
		fsSkip:               []string{"(dir):subdir:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolFalse,
		dockerUseBuildKit:    true,
	},

	{
		// https://github.com/containers/buildah/issues/6726
		name: "copy--exclude-subdir-under-source",
		dockerfileContents: strings.Join([]string{
			"# syntax=mirror.gcr.io/docker/dockerfile:1.9-labs",
			"FROM scratch",
			"COPY --exclude=storage/views app/ app/",
		}, "\n"),
		contextDir:           "dockerignore/exclude_subdir_under_source",
		fsSkip:               []string{"(dir):app:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolFalse,
		dockerUseBuildKit:    true,
	},

	{
		// https://github.com/containers/buildah/issues/6726
		name: "copy--form-and--exclude-subdir-under-source",
		dockerfileContents: strings.Join([]string{
			"# syntax=mirror.gcr.io/docker/dockerfile:1.9-labs",
			"FROM scratch as base",
			"COPY . .",
			"FROM scratch",
			"COPY --from=base --exclude=storage/views app/ app/",
		}, "\n"),
		contextDir:           "dockerignore/exclude_subdir_under_source",
		fsSkip:               []string{"(dir):app:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolFalse,
		dockerUseBuildKit:    true,
	},

	{
		name: "copy--exclude-exception-under-source",
		dockerfileContents: strings.Join([]string{
			"# syntax=mirror.gcr.io/docker/dockerfile:1.9-labs",
			"FROM scratch",
			"COPY --exclude=storage/views/test_file --exclude=!storage/views/exception app/ app/",
		}, "\n"),
		contextDir:           "dockerignore/exclude_subdir_under_source",
		fsSkip:               []string{"(dir):app:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolFalse,
		dockerUseBuildKit:    true,
	},

	{
		name: "copy--parents",
		dockerfileContents: strings.Join([]string{
			"# syntax=mirror.gcr.io/docker/dockerfile:1.7-labs",
			"FROM scratch",
			"COPY ./x/a.txt ./y/a.txt /no_parents/",
			"COPY --parents ./x/a.txt ./y/a.txt /parents/",
		}, "\n"),
		contextDir:          "copy-parents",
		fsSkip:              []string{"(dir):parents:mtime", "(dir):no_parents:mtime", "(dir):parents:(dir):x:mtime", "(dir):parents:(dir):y:mtime"},
		compatScratchConfig: types.OptionalBoolFalse,
		dockerUseBuildKit:   true,
	},

	{
		name: "copy--parents-directories",
		dockerfileContents: strings.Join([]string{
			"# syntax=mirror.gcr.io/docker/dockerfile:1.7-labs",
			"FROM scratch",
			"COPY --parents ./x /parents/",
		}, "\n"),
		contextDir:          "copy-parents",
		fsSkip:              []string{"(dir):parents:mtime", "(dir):parents:(dir):x:mtime"},
		compatScratchConfig: types.OptionalBoolFalse,
		dockerUseBuildKit:   true,
	},

	{
		name: "copy--parents-dir-with-links",
		dockerfileContents: strings.Join([]string{
			"# syntax=mirror.gcr.io/docker/dockerfile:1.7-labs",
			"FROM mirror.gcr.io/alpine as base",
			"COPY . .",

			"# Symlink and hardlink part",
			"RUN ln -s /x/z/b.txt /x/z/symlink-b.txt",
			"RUN ln /x/z/a.txt /x/z/hardlink-a.txt",
			"RUN ln /x/y/b.txt /x/z/hardlink-y-b.txt",

			"FROM scratch",
			"COPY --from=base --parents ./x/ /parents",
			"COPY --from=base --parents ./x/./z/ /parents_dir_point/",
		}, "\n"),
		contextDir: "copy-parents",
		fsSkip: []string{
			"(dir):parents:mtime",
			"(dir):parents:(dir):x:mtime",
			"(dir):parents:(dir):x:(dir):z:mtime",
			"(dir):parents:(dir):x:(dir):z:(dir):symlink-b.txt:mtime",
			"(dir):parents_dir_point:mtime",
			"(dir):parents_dir_point:(dir):z:mtime",
			"(dir):parents_dir_point:(dir):z:(dir):symlink-b.txt:mtime",
		},
		compatScratchConfig: types.OptionalBoolFalse,
		dockerUseBuildKit:   true,
	},

	{
		name: "copy--parents-all-directories",
		dockerfileContents: strings.Join([]string{
			"# syntax=mirror.gcr.io/docker/dockerfile:1.7-labs",
			"FROM scratch",
			"COPY --parents ./ /parents/",
		}, "\n"),
		contextDir:          "copy-parents",
		fsSkip:              []string{"(dir):parents:mtime", "(dir):parents:(dir):x:mtime"},
		compatScratchConfig: types.OptionalBoolFalse,
		dockerUseBuildKit:   true,
	},
	{
		name: "copy--parents-directory-with-parent-directory",
		dockerfileContents: strings.Join([]string{
			"# syntax=mirror.gcr.io/docker/dockerfile:1.7-labs",
			"FROM scratch",
			"COPY --parents ./x/y/ /parents/",
		}, "\n"),
		contextDir:          "copy-parents",
		fsSkip:              []string{"(dir):parents:mtime", "(dir):parents:(dir):x:mtime", "(dir):parents:(dir):x:(dir):y:mtime"},
		compatScratchConfig: types.OptionalBoolFalse,
		dockerUseBuildKit:   true,
	},
	{
		name: "copy--parents-path-with-star",
		dockerfileContents: strings.Join([]string{
			"# syntax=mirror.gcr.io/docker/dockerfile:1.7-labs",
			"FROM scratch",
			"COPY --parents ./x/*/a.txt ./*/b.txt /parents/",
		}, "\n"),
		contextDir:          "copy-parents",
		fsSkip:              []string{"(dir):parents:mtime", "(dir):parents:(dir):x:mtime", "(dir):parents:(dir):y:mtime", "(dir):parents:(dir):x:(dir):y:mtime", "(dir):parents:(dir):x:(dir):z:mtime"},
		compatScratchConfig: types.OptionalBoolFalse,
		dockerUseBuildKit:   true,
	},

	{
		name: "copy--parents-two-parent-directories",
		dockerfileContents: strings.Join([]string{
			"# syntax=mirror.gcr.io/docker/dockerfile:1.7-labs",
			"FROM scratch",
			"COPY --parents ./x/y/*.txt /parents/",
		}, "\n"),
		contextDir:          "copy-parents",
		fsSkip:              []string{"(dir):parents:mtime", "(dir):parents:(dir):x:mtime", "(dir):parents:(dir):x:(dir):y:mtime"},
		compatScratchConfig: types.OptionalBoolFalse,
		dockerUseBuildKit:   true,
	},

	{
		name: "copy--parents-unknown-count-parent-directories",
		dockerfileContents: strings.Join([]string{
			"# syntax=mirror.gcr.io/docker/dockerfile:1.7-labs",
			"FROM scratch",
			"COPY --parents ./x/* /parents/",
		}, "\n"),
		contextDir:          "copy-parents",
		fsSkip:              []string{"(dir):parents:mtime", "(dir):parents:(dir):x:mtime", "(dir):parents:(dir):x:(dir):y:mtime", "(dir):parents:(dir):x:(dir):z:mtime"},
		compatScratchConfig: types.OptionalBoolFalse,
		dockerUseBuildKit:   true,
	},

	{
		name: "copy--parents-directory-limit-parents-unknown-count-parent-directories",
		dockerfileContents: strings.Join([]string{
			"# syntax=mirror.gcr.io/docker/dockerfile:1.7-labs",
			"FROM scratch",
			"COPY --parents ./x/./ /parents/",
		}, "\n"),
		contextDir:          "copy-parents",
		fsSkip:              []string{"(dir):parents:mtime", "(dir):parents:(dir):y:mtime", "(dir):parents:(dir):z:mtime"},
		compatScratchConfig: types.OptionalBoolFalse,
		dockerUseBuildKit:   true,
	},
	{
		name: "copy--parents-limit-parents-unknown-count-parent-directories",
		dockerfileContents: strings.Join([]string{
			"# syntax=mirror.gcr.io/docker/dockerfile:1.7-labs",
			"FROM scratch",
			"COPY --parents ./x/./* /parents/",
		}, "\n"),
		contextDir:          "copy-parents",
		fsSkip:              []string{"(dir):parents:mtime", "(dir):parents:(dir):y:mtime", "(dir):parents:(dir):z:mtime"},
		compatScratchConfig: types.OptionalBoolFalse,
		dockerUseBuildKit:   true,
	},

	{
		name: "copy--parents-limit-parent-directories",
		dockerfileContents: strings.Join([]string{
			"# syntax=mirror.gcr.io/docker/dockerfile:1.7-labs",
			"FROM scratch",
			"COPY --parents ./x/./y/*.txt /parents/",
			/*
				Build context:
				./x/y/a.txt
				./x/y/b.txt

				Output:
				/parents/y/a.txt
				/parents/y/b.txt
			*/
		}, "\n"),
		contextDir:          "copy-parents",
		fsSkip:              []string{"(dir):parents:mtime", "(dir):parents:(dir):y:mtime"},
		compatScratchConfig: types.OptionalBoolFalse,
		dockerUseBuildKit:   true,
	},

	{
		name: "copy--parents-limit-parent-directories-1",
		dockerfileContents: strings.Join([]string{
			"# syntax=mirror.gcr.io/docker/dockerfile:1.7-labs",
			"FROM scratch",
			"COPY --parents ./x/y/./*.txt /parents/",
			/*
				Build context:
				./x/y/a.txt
				./x/y/b.txt

				Output:
				/parents/a.txt
				/parents/b.txt
			*/
		}, "\n"),
		contextDir:          "copy-parents",
		fsSkip:              []string{"(dir):parents:mtime"},
		compatScratchConfig: types.OptionalBoolFalse,
		dockerUseBuildKit:   true,
	},

	{
		name: "dockerignore-includes-star",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"COPY * subdir/",
		}, "\n"),
		contextDir: "dockerignore/populated",
		tweakContextDir: func(_ *testing.T, contextDir, _, _ string) (err error) {
			dockerignore := []byte("!**/*-c\n")
			if err := os.WriteFile(filepath.Join(contextDir, ".dockerignore"), dockerignore, 0o100); err != nil {
				return fmt.Errorf("writing .dockerignore file: %w", err)
			}
			if err = os.Chtimes(filepath.Join(contextDir, ".dockerignore"), testDate, testDate); err != nil {
				return fmt.Errorf("setting date on .dockerignore file in temporary context directory: %w", err)
			}
			return nil
		},
		fsSkip:               []string{"(dir):subdir:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolTrue,
	},

	{
		name: "dockerignore-exclude-plain-dot",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"COPY . subdir/",
		}, "\n"),
		contextDir: "dockerignore/populated",
		tweakContextDir: func(_ *testing.T, contextDir, _, _ string) (err error) {
			dockerignore := []byte("subdir-c")
			if err := os.WriteFile(filepath.Join(contextDir, ".dockerignore"), dockerignore, 0o200); err != nil {
				return fmt.Errorf("writing .dockerignore file: %w", err)
			}
			if err = os.Chtimes(filepath.Join(contextDir, ".dockerignore"), testDate, testDate); err != nil {
				return fmt.Errorf("setting date on .dockerignore file in temporary context directory: %w", err)
			}
			return nil
		},
		fsSkip:               []string{"(dir):subdir:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolTrue,
	},

	{
		name: "dockerignore-exclude-plain-star",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"COPY * subdir/",
		}, "\n"),
		contextDir: "dockerignore/populated",
		tweakContextDir: func(_ *testing.T, contextDir, _, _ string) (err error) {
			dockerignore := []byte("subdir-c")
			if err := os.WriteFile(filepath.Join(contextDir, ".dockerignore"), dockerignore, 0o400); err != nil {
				return fmt.Errorf("writing .dockerignore file: %w", err)
			}
			if err = os.Chtimes(filepath.Join(contextDir, ".dockerignore"), testDate, testDate); err != nil {
				return fmt.Errorf("setting date on .dockerignore file in temporary context directory: %w", err)
			}
			return nil
		},
		fsSkip:               []string{"(dir):subdir:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolTrue,
	},

	{
		name: "dockerignore-exclude-plain-dot",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"COPY . subdir/",
		}, "\n"),
		contextDir: "dockerignore/populated",
		tweakContextDir: func(_ *testing.T, contextDir, _, _ string) (err error) {
			dockerignore := []byte("**/subdir-c")
			if err := os.WriteFile(filepath.Join(contextDir, ".dockerignore"), dockerignore, 0o200); err != nil {
				return fmt.Errorf("writing .dockerignore file: %w", err)
			}
			if err = os.Chtimes(filepath.Join(contextDir, ".dockerignore"), testDate, testDate); err != nil {
				return fmt.Errorf("setting date on .dockerignore file in temporary context directory: %w", err)
			}
			return nil
		},
		fsSkip:               []string{"(dir):subdir:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolTrue,
	},

	{
		name: "dockerignore-exclude-plain-star",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"COPY * subdir/",
		}, "\n"),
		contextDir: "dockerignore/populated",
		tweakContextDir: func(_ *testing.T, contextDir, _, _ string) (err error) {
			dockerignore := []byte("**/subdir-c")
			if err := os.WriteFile(filepath.Join(contextDir, ".dockerignore"), dockerignore, 0o400); err != nil {
				return fmt.Errorf("writing .dockerignore file: %w", err)
			}
			if err = os.Chtimes(filepath.Join(contextDir, ".dockerignore"), testDate, testDate); err != nil {
				return fmt.Errorf("setting date on .dockerignore file in temporary context directory: %w", err)
			}
			return nil
		},
		fsSkip:               []string{"(dir):subdir:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolTrue,
	},

	{
		name: "dockerignore-exclude-wildcard-dot",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"COPY . subdir/",
		}, "\n"),
		contextDir: "dockerignore/populated",
		tweakContextDir: func(_ *testing.T, contextDir, _, _ string) (err error) {
			dockerignore := []byte("subdir-*")
			if err := os.WriteFile(filepath.Join(contextDir, ".dockerignore"), dockerignore, 0o000); err != nil {
				return fmt.Errorf("writing .dockerignore file: %w", err)
			}
			if err = os.Chtimes(filepath.Join(contextDir, ".dockerignore"), testDate, testDate); err != nil {
				return fmt.Errorf("setting date on .dockerignore file in temporary context directory: %w", err)
			}
			return nil
		},
		fsSkip:               []string{"(dir):subdir:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolTrue,
	},

	{
		name: "dockerignore-exclude-wildcard-star",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"COPY * subdir/",
		}, "\n"),
		contextDir: "dockerignore/populated",
		tweakContextDir: func(_ *testing.T, contextDir, _, _ string) (err error) {
			dockerignore := []byte("subdir-*")
			if err := os.WriteFile(filepath.Join(contextDir, ".dockerignore"), dockerignore, 0o660); err != nil {
				return fmt.Errorf("writing .dockerignore file: %w", err)
			}
			if err = os.Chtimes(filepath.Join(contextDir, ".dockerignore"), testDate, testDate); err != nil {
				return fmt.Errorf("setting date on .dockerignore file in temporary context directory: %w", err)
			}
			return nil
		},
		fsSkip:               []string{"(dir):subdir:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolTrue,
	},

	{
		name: "dockerignore-exclude-deep-wildcard-dot",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"COPY . subdir/",
		}, "\n"),
		contextDir: "dockerignore/populated",
		tweakContextDir: func(_ *testing.T, contextDir, _, _ string) (err error) {
			dockerignore := []byte("**/subdir-*")
			if err := os.WriteFile(filepath.Join(contextDir, ".dockerignore"), dockerignore, 0o000); err != nil {
				return fmt.Errorf("writing .dockerignore file: %w", err)
			}
			if err = os.Chtimes(filepath.Join(contextDir, ".dockerignore"), testDate, testDate); err != nil {
				return fmt.Errorf("setting date on .dockerignore file in temporary context directory: %w", err)
			}
			return nil
		},
		fsSkip:               []string{"(dir):subdir:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolTrue,
	},

	{
		name: "dockerignore-exclude-deep-wildcard-star",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"COPY * subdir/",
		}, "\n"),
		contextDir: "dockerignore/populated",
		tweakContextDir: func(_ *testing.T, contextDir, _, _ string) (err error) {
			dockerignore := []byte("**/subdir-*")
			if err := os.WriteFile(filepath.Join(contextDir, ".dockerignore"), dockerignore, 0o660); err != nil {
				return fmt.Errorf("writing .dockerignore file: %w", err)
			}
			if err = os.Chtimes(filepath.Join(contextDir, ".dockerignore"), testDate, testDate); err != nil {
				return fmt.Errorf("setting date on .dockerignore file in temporary context directory: %w", err)
			}
			return nil
		},
		fsSkip:               []string{"(dir):subdir:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolTrue,
	},

	{
		name: "dockerignore-exclude-deep-subdir-dot",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"COPY . subdir/",
		}, "\n"),
		contextDir: "dockerignore/populated",
		tweakContextDir: func(_ *testing.T, contextDir, _, _ string) (err error) {
			dockerignore := []byte("**/subdir-f")
			if err := os.WriteFile(filepath.Join(contextDir, ".dockerignore"), dockerignore, 0o666); err != nil {
				return fmt.Errorf("writing .dockerignore file: %w", err)
			}
			if err = os.Chtimes(filepath.Join(contextDir, ".dockerignore"), testDate, testDate); err != nil {
				return fmt.Errorf("setting date on .dockerignore file in temporary context directory: %w", err)
			}
			return nil
		},
		fsSkip:               []string{"(dir):subdir:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolTrue,
	},

	{
		name: "dockerignore-exclude-deep-subdir-star",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"COPY * subdir/",
		}, "\n"),
		contextDir: "dockerignore/populated",
		tweakContextDir: func(_ *testing.T, contextDir, _, _ string) (err error) {
			dockerignore := []byte("**/subdir-f")
			if err := os.WriteFile(filepath.Join(contextDir, ".dockerignore"), dockerignore, 0o640); err != nil {
				return fmt.Errorf("writing .dockerignore file: %w", err)
			}
			if err = os.Chtimes(filepath.Join(contextDir, ".dockerignore"), testDate, testDate); err != nil {
				return fmt.Errorf("setting date on .dockerignore file in temporary context directory: %w", err)
			}
			return nil
		},
		fsSkip:               []string{"(dir):subdir:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolTrue,
	},

	{
		name: "dockerignore-exclude-not-so-deep-subdir-dot",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"COPY . subdir/",
		}, "\n"),
		contextDir: "dockerignore/populated",
		tweakContextDir: func(_ *testing.T, contextDir, _, _ string) (err error) {
			dockerignore := []byte("**/subdir-b")
			if err := os.WriteFile(filepath.Join(contextDir, ".dockerignore"), dockerignore, 0o705); err != nil {
				return fmt.Errorf("writing .dockerignore file: %w", err)
			}
			if err = os.Chtimes(filepath.Join(contextDir, ".dockerignore"), testDate, testDate); err != nil {
				return fmt.Errorf("setting date on .dockerignore file in temporary context directory: %w", err)
			}
			return nil
		},
		fsSkip:               []string{"(dir):subdir:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolTrue,
	},

	{
		name: "dockerignore-exclude-not-so-deep-subdir-star",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"COPY * subdir/",
		}, "\n"),
		contextDir: "dockerignore/populated",
		tweakContextDir: func(_ *testing.T, contextDir, _, _ string) (err error) {
			dockerignore := []byte("**/subdir-b")
			if err := os.WriteFile(filepath.Join(contextDir, ".dockerignore"), dockerignore, 0o750); err != nil {
				return fmt.Errorf("writing .dockerignore file: %w", err)
			}
			if err = os.Chtimes(filepath.Join(contextDir, ".dockerignore"), testDate, testDate); err != nil {
				return fmt.Errorf("setting date on .dockerignore file in temporary context directory: %w", err)
			}
			return nil
		},
		fsSkip:               []string{"(dir):subdir:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolTrue,
	},

	{
		name: "dockerignore-exclude-kind-of-deep-subdir-dot",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"COPY . subdir/",
		}, "\n"),
		contextDir: "dockerignore/populated",
		tweakContextDir: func(_ *testing.T, contextDir, _, _ string) (err error) {
			dockerignore := []byte(strings.Join([]string{"**/subdir-e", "!**/subdir-f"}, "\n"))
			if err := os.WriteFile(filepath.Join(contextDir, ".dockerignore"), dockerignore, 0o750); err != nil {
				return fmt.Errorf("writing .dockerignore file: %w", err)
			}
			if err = os.Chtimes(filepath.Join(contextDir, ".dockerignore"), testDate, testDate); err != nil {
				return fmt.Errorf("setting date on .dockerignore file in temporary context directory: %w", err)
			}
			return nil
		},
		fsSkip:               []string{"(dir):subdir:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolTrue,
	},

	{
		name: "dockerignore-exclude-kind-of-deep-subdir-star",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"COPY * subdir/",
		}, "\n"),
		contextDir: "dockerignore/populated",
		tweakContextDir: func(_ *testing.T, contextDir, _, _ string) (err error) {
			dockerignore := []byte(strings.Join([]string{"**/subdir-e", "!**/subdir-f"}, "\n"))
			if err := os.WriteFile(filepath.Join(contextDir, ".dockerignore"), dockerignore, 0o750); err != nil {
				return fmt.Errorf("writing .dockerignore file: %w", err)
			}
			if err = os.Chtimes(filepath.Join(contextDir, ".dockerignore"), testDate, testDate); err != nil {
				return fmt.Errorf("setting date on .dockerignore file in temporary context directory: %w", err)
			}
			return nil
		},
		fsSkip:               []string{"(dir):subdir:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolTrue,
	},

	{
		name: "dockerignore-exclude-deep-subdir-dot",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"COPY . subdir/",
		}, "\n"),
		contextDir: "dockerignore/populated",
		tweakContextDir: func(_ *testing.T, contextDir, _, _ string) (err error) {
			dockerignore := []byte(strings.Join([]string{"**/subdir-f", "!**/subdir-g"}, "\n"))
			if err := os.WriteFile(filepath.Join(contextDir, ".dockerignore"), dockerignore, 0o750); err != nil {
				return fmt.Errorf("writing .dockerignore file: %w", err)
			}
			if err = os.Chtimes(filepath.Join(contextDir, ".dockerignore"), testDate, testDate); err != nil {
				return fmt.Errorf("setting date on .dockerignore file in temporary context directory: %w", err)
			}
			return nil
		},
		fsSkip:               []string{"(dir):subdir:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolTrue,
	},

	{
		name: "dockerignore-exclude-deep-subdir-star",
		dockerfileContents: strings.Join([]string{
			"FROM scratch",
			"COPY * subdir/",
		}, "\n"),
		contextDir: "dockerignore/populated",
		tweakContextDir: func(_ *testing.T, contextDir, _, _ string) (err error) {
			dockerignore := []byte(strings.Join([]string{"**/subdir-f", "!**/subdir-g"}, "\n"))
			if err := os.WriteFile(filepath.Join(contextDir, ".dockerignore"), dockerignore, 0o750); err != nil {
				return fmt.Errorf("writing .dockerignore file: %w", err)
			}
			if err = os.Chtimes(filepath.Join(contextDir, ".dockerignore"), testDate, testDate); err != nil {
				return fmt.Errorf("setting date on .dockerignore file in temporary context directory: %w", err)
			}
			return nil
		},
		fsSkip:               []string{"(dir):subdir:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolTrue,
	},

	{
		name:       "env-whitespace",
		contextDir: "copy",
		dockerfileContents: strings.Join([]string{
			`FROM scratch`,
			`COPY script .`,
			`ENV name value`,
		}, "\n"),
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name:       "env-simple",
		contextDir: "copy",
		dockerfileContents: strings.Join([]string{
			`FROM scratch`,
			`COPY script .`,
			`ENV name=value`,
		}, "\n"),
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name:       "env-unquoted-list",
		contextDir: "copy",
		dockerfileContents: strings.Join([]string{
			`FROM scratch`,
			`COPY script .`,
			`ENV name=value name2=value2`,
		}, "\n"),
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name:       "env-dquoted-list",
		contextDir: "copy",
		dockerfileContents: strings.Join([]string{
			`FROM scratch`,
			`COPY script .`,
			`ENV name="value value1"`,
		}, "\n"),
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name:       "env-escaped-value",
		contextDir: "copy",
		dockerfileContents: strings.Join([]string{
			`FROM scratch`,
			`COPY script .`,
			`ENV name=value\ value2`,
		}, "\n"),
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name:       "env-squote-in-dquote",
		contextDir: "copy",
		dockerfileContents: strings.Join([]string{
			`FROM scratch`,
			`COPY script .`,
			`ENV name="value'quote space'value2"`,
		}, "\n"),
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name:       "env-dquote-in-squote",
		contextDir: "copy",
		dockerfileContents: strings.Join([]string{
			`FROM scratch`,
			`COPY script .`,
			`ENV name='value"double quote"value2'`,
		}, "\n"),
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name:       "env-escaped-list",
		contextDir: "copy",
		dockerfileContents: strings.Join([]string{
			`FROM scratch`,
			`COPY script .`,
			`ENV name=value\ value2 name2=value2\ value3`,
		}, "\n"),
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name:       "env-eddquote",
		contextDir: "copy",
		dockerfileContents: strings.Join([]string{
			`FROM scratch`,
			`COPY script .`,
			`ENV name="a\"b"`,
		}, "\n"),
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name:       "env-invalid-ssquote",
		contextDir: "copy",
		dockerfileContents: strings.Join([]string{
			`FROM scratch`,
			`COPY script .`,
			`ENV name='a\'b'`,
		}, "\n"),
		shouldFailAt: 3,
	},

	{
		name:       "env-esdquote",
		contextDir: "copy",
		dockerfileContents: strings.Join([]string{
			`FROM scratch`,
			`COPY script .`,
			`ENV name="a\'b"`,
		}, "\n"),
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name:       "env-essquote",
		contextDir: "copy",
		dockerfileContents: strings.Join([]string{
			`FROM scratch`,
			`COPY script .`,
			`ENV name='a\'b''`,
		}, "\n"),
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name:       "env-edsquote",
		contextDir: "copy",
		dockerfileContents: strings.Join([]string{
			`FROM scratch`,
			`COPY script .`,
			`ENV name='a\"b'`,
		}, "\n"),
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name:       "env-empty-squote-in-empty-dquote",
		contextDir: "copy",
		dockerfileContents: strings.Join([]string{
			`FROM scratch`,
			`COPY script .`,
			`ENV name="''"`,
		}, "\n"),
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name:       "env-multiline",
		contextDir: "copy",
		dockerfileContents: strings.Join([]string{
			`FROM scratch`,
			`COPY script .`,
			`# don't put anything after the next line - it must be the last line of the`,
			`# Dockerfile and it must end with \`,
			`ENV name=value \`,
			`    name1=value1 \`,
			`    name2="value2a \`,
			`           value2b" \`,
			`    name3="value3a\n\"value3b\"" \`,
			`        name4="value4a\\nvalue4b" \`,
		}, "\n"),
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name: "copy-from-owner", // from issue #2518
		dockerfileContents: strings.Join([]string{
			`FROM mirror.gcr.io/alpine`,
			`RUN set -ex; touch -t @1485449953 /test; chown 65:65 /test`,
			`FROM scratch`,
			`USER 66:66`,
			`COPY --from=0 /test /test`,
		}, "\n"),
		fsSkip:              []string{"test:mtime"},
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name: "copy-from-owner-with-chown", // issue #2518, but with chown to override
		dockerfileContents: strings.Join([]string{
			`FROM mirror.gcr.io/alpine`,
			`RUN set -ex; touch -t @1485449953 /test; chown 65:65 /test`,
			`FROM scratch`,
			`USER 66:66`,
			`COPY --from=0 --chown=1:1 /test /test`,
		}, "\n"),
		fsSkip:              []string{"test:mtime"},
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name:       "copy-for-user", // flip side of issue #2518
		contextDir: "copy",
		dockerfileContents: strings.Join([]string{
			`FROM mirror.gcr.io/alpine`,
			`USER 66:66`,
			`COPY /script /script`,
		}, "\n"),
	},

	{
		name:       "copy-for-user-with-chown", // flip side of issue #2518, but with chown to override
		contextDir: "copy",
		dockerfileContents: strings.Join([]string{
			`FROM mirror.gcr.io/alpine`,
			`USER 66:66`,
			`COPY --chown=1:1 /script /script`,
		}, "\n"),
	},

	{
		name:                "add-parent-symlink",
		contextDir:          "add/parent-symlink",
		fsSkip:              []string{"(dir):testsubdir:mtime", "(dir):testsubdir:(dir):etc:mtime"},
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name:       "add-parent-dangling",
		contextDir: "add/parent-dangling",
		fsSkip:     []string{"(dir):symlink:mtime", "(dir):symlink-target:mtime", "(dir):symlink-target:(dir):subdirectory:mtime"},
	},

	{
		name:       "add-parent-clean",
		contextDir: "add/parent-clean",
		fsSkip:     []string{"(dir):symlink:mtime", "(dir):symlink-target:mtime", "(dir):symlink-target:(dir):subdirectory:mtime"},
	},

	{
		name:                "add-archive-1",
		contextDir:          "add/archive",
		dockerfile:          "Dockerfile.1",
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name:                "add-archive-2",
		contextDir:          "add/archive",
		dockerfile:          "Dockerfile.2",
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name:                "add-archive-3",
		contextDir:          "add/archive",
		dockerfile:          "Dockerfile.3",
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name:                "add-archive-4",
		contextDir:          "add/archive",
		dockerfile:          "Dockerfile.4",
		fsSkip:              []string{"(dir):sub:mtime"},
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name:                "add-archive-5",
		contextDir:          "add/archive",
		dockerfile:          "Dockerfile.5",
		fsSkip:              []string{"(dir):sub:mtime"},
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name:                "add-archive-6",
		contextDir:          "add/archive",
		dockerfile:          "Dockerfile.6",
		fsSkip:              []string{"(dir):sub:mtime"},
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name:                "add-archive-7",
		contextDir:          "add/archive",
		dockerfile:          "Dockerfile.7",
		fsSkip:              []string{"(dir):sub:mtime"},
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name:       "add-dir-not-dir",
		contextDir: "add/dir-not-dir",
	},

	{
		name:       "add-not-dir-dir",
		contextDir: "add/not-dir-dir",
	},

	{
		name:       "add-populated-dir-not-dir",
		contextDir: "add/populated-dir-not-dir",
	},

	{
		name:         "dockerignore-allowlist-subdir-nofile-dir",
		contextDir:   "dockerignore/allowlist/subdir-nofile",
		shouldFailAt: 2,
		failureRegex: "(no such file or directory)|(file not found)|(file does not exist)",
	},

	{
		name:         "dockerignore-allowlist-subdir-nofile-file",
		contextDir:   "dockerignore/allowlist/subdir-nofile",
		shouldFailAt: 2,
		failureRegex: "(no such file or directory)|(file not found)|(file does not exist)",
	},

	{
		name:                 "dockerignore-allowlist-subdir-file-dir",
		contextDir:           "dockerignore/allowlist/subdir-file",
		fsSkip:               []string{"(dir):f1:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolTrue,
	},

	{
		name:                 "dockerignore-allowlist-subdir-file-file",
		contextDir:           "dockerignore/allowlist/subdir-file",
		fsSkip:               []string{"(dir):f1:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolTrue,
	},

	{
		name:                 "dockerignore-allowlist-nothing-dot",
		contextDir:           "dockerignore/allowlist/nothing-dot",
		fsSkip:               []string{"file:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolTrue,
	},

	{
		name:                 "dockerignore-allowlist-nothing-slash",
		contextDir:           "dockerignore/allowlist/nothing-slash",
		fsSkip:               []string{"file:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolTrue,
	},

	{
		// the directories are excluded, so entries for them don't get
		// included in the build context archive, so they only get
		// created implicitly when extracted, so there's no point in us
		// trying to preserve any of that, either
		name:          "dockerignore-allowlist-subsubdir-file",
		contextDir:    "dockerignore/allowlist/subsubdir-file",
		withoutDocker: true,
		fsSkip:        []string{"(dir):folder:mtime", "(dir):folder:(dir):subfolder:mtime", "file:mtime"},
	},

	{
		name:                 "dockerignore-allowlist-subsubdir-nofile",
		contextDir:           "dockerignore/allowlist/subsubdir-nofile",
		fsSkip:               []string{"file:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolTrue,
	},

	{
		name:                 "dockerignore-allowlist-subsubdir-nosubdir",
		contextDir:           "dockerignore/allowlist/subsubdir-nosubdir",
		fsSkip:               []string{"file:mtime"},
		failOnExtraFSContent: true,
		compatScratchConfig:  types.OptionalBoolTrue,
	},

	{
		name:          "dockerignore-allowlist-alternating",
		contextDir:    "dockerignore/allowlist/alternating",
		withoutDocker: true,
		fsSkip: []string{
			"(dir):subdir1:mtime",
			"(dir):subdir1:(dir):subdir2:(dir):subdir3:mtime",
			"(dir):subdir1:(dir):subdir2:(dir):subdir3:(dir):subdir4:(dir):subdir5:mtime",
			"(dir):subdir2:(dir):subdir3:mtime",
			"(dir):subdir2:(dir):subdir3:(dir):subdir4:(dir):subdir5:mtime",
			"(dir):subdir3:mtime",
			"(dir):subdir3:(dir):subdir4:(dir):subdir5:mtime",
			"(dir):subdir4:(dir):subdir5:mtime",
			"(dir):subdir5:mtime",
		},
	},

	{
		name:         "dockerignore-allowlist-alternating-nothing",
		contextDir:   "dockerignore/allowlist/alternating-nothing",
		shouldFailAt: 7,
		failureRegex: "(no such file or directory)|(file not found)|(file does not exist)",
	},

	{
		name:         "dockerignore-allowlist-alternating-other",
		contextDir:   "dockerignore/allowlist/alternating-other",
		shouldFailAt: 7,
		failureRegex: "(no such file or directory)|(file not found)|(file does not exist)",
	},

	{
		name:          "tar-g",
		contextDir:    "tar-g",
		withoutDocker: true,
		fsSkip:        []string{"(dir):tmp:mtime"},
	},

	{
		name:                 "dockerignore-exceptions-skip",
		contextDir:           "dockerignore/exceptions-skip",
		fsSkip:               []string{"(dir):volume:mtime"},
		failOnExtraFSContent: true,
	},

	{
		name:                 "dockerignore-exceptions-weirdness-1",
		contextDir:           "dockerignore/exceptions-weirdness-1",
		fsSkip:               []string{"(dir):newdir:mtime", "(dir):newdir:(dir):subdir:mtime"},
		failOnExtraFSContent: true,
	},

	{
		name:                 "dockerignore-exceptions-weirdness-2",
		contextDir:           "dockerignore/exceptions-weirdness-2",
		fsSkip:               []string{"(dir):newdir:mtime", "(dir):newdir:(dir):subdir:mtime"},
		failOnExtraFSContent: true,
	},

	{
		name:              "multistage-builtin-args", // By default, BUILDVARIANT/TARGETVARIANT should be empty.
		dockerfile:        "Dockerfile.margs",
		dockerUseBuildKit: true,
	},

	{
		name:              "heredoc-copy",
		dockerfile:        "Dockerfile.heredoc_copy",
		dockerUseBuildKit: true,
		contextDir:        "heredoc",
		fsSkip: []string{
			"(dir):test:mtime",
			"(dir):test2:mtime",
			"(dir):test:(dir):humans.txt:mtime",
			"(dir):test:(dir):robots.txt:mtime",
			"(dir):test2:(dir):humans.txt:mtime",
			"(dir):test2:(dir):robots.txt:mtime",
			"(dir):test2:(dir):image_file:mtime",
			"(dir):etc:(dir):hostname", /* buildkit does not contains /etc/hostname like buildah */
		},
	},

	{
		name:                "replace-symlink-with-directory",
		contextDir:          "replace/symlink-with-directory",
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name:                "replace-directory-with-symlink",
		contextDir:          "replace/symlink-with-directory",
		dockerfile:          "Dockerfile.2",
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name:                "replace-symlink-with-directory-subdir",
		contextDir:          "replace/symlink-with-directory",
		dockerfile:          "Dockerfile.3",
		fsSkip:              []string{"(dir):tree:mtime"},
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name:                "replace-directory-with-symlink-subdir",
		contextDir:          "replace/symlink-with-directory",
		dockerfile:          "Dockerfile.4",
		fsSkip:              []string{"(dir):tree:mtime"},
		compatScratchConfig: types.OptionalBoolTrue,
	},

	{
		name: "workdir-owner", // from issue #3620
		dockerfileContents: strings.Join([]string{
			`# syntax=mirror.gcr.io/docker/dockerfile:1.4`,
			`FROM mirror.gcr.io/alpine`,
			`USER daemon`,
			`WORKDIR /created/directory`,
			`RUN ls /created`,
		}, "\n"),
		fsSkip:            []string{"(dir):created:mtime", "(dir):created:(dir):directory:mtime"},
		dockerUseBuildKit: true,
	},

	{
		name: "buildin-args",
		dockerfileContents: strings.Join([]string{
			"FROM mirror.gcr.io/alpine",
			"ARG BUILDPLATFORM",
			"ARG BUILDOS",
			"ARG BUILDARCH",
			"ARG BUILDVARIANT",
			"ARG TARGETPLATFORM",
			"ARG TARGETOS",
			"ARG TARGETARCH",
			"ARG TARGETVARIANT",
			"RUN echo ${BUILDPLATFORM}  > buildplatform",
			"RUN echo ${BUILDOS}        > buildos",
			"RUN echo ${BUILDARCH}      > buildarch",
			"RUN echo ${BUILDVARIANT}   > buildvariant",
			"RUN echo ${TARGETPLATFORM} > targetplatform",
			"RUN echo ${TARGETOS}       > targetos",
			"RUN echo ${TARGETARCH}     > targetarch",
			"RUN echo ${TARGETVARIANT}  > targetvariant",
		}, "\n"),
		dockerUseBuildKit: true,
		fsSkip: []string{
			"(dir):targetarch:mtime",
			"(dir):targetos:mtime",
			"(dir):buildos:mtime",
			"(dir):buildvariant:mtime",
			"(dir):targetvariant:mtime",
			"(dir):targetplatform:mtime",
			"(dir):buildplatform:mtime",
			"(dir):buildarch:mtime",
		},
	},

	{
		name:              "env-precedence",
		contextDir:        "env/precedence",
		dockerUseBuildKit: true,
	},

	{
		name:              "multistage-copyback",
		contextDir:        "multistage/copyback",
		dockerUseBuildKit: true,
	},

	{
		name:              "heredoc-quoting",
		dockerfile:        "Dockerfile.heredoc-quoting",
		dockerUseBuildKit: true,
		fsSkip:            []string{"(dir):etc:(dir):hostname"}, // buildkit does not create a phantom /etc/hostname
	},

	{
		name: "workdir with trailing separator",
		dockerfileContents: strings.Join([]string{
			"FROM mirror.gcr.io/busybox",
			"USER daemon",
			"WORKDIR /tmp/",
		}, "\n"),
	},

	{
		name: "workdir without trailing separator",
		dockerfileContents: strings.Join([]string{
			"FROM mirror.gcr.io/busybox",
			"USER daemon",
			"WORKDIR /tmp",
		}, "\n"),
	},

	{
		name:                    "chown-volume", // from podman #22530
		contextDir:              "chown-volume",
		testUsingVolumes:        true,
		fsSkipCompatVolumesTrue: []string{"(dir):volumea:mtime", "(dir):volumeb:mtime", "(dir):volumec:mtime"},
	},

	{
		name:              "builtins",
		contextDir:        "builtins",
		dockerUseBuildKit: true,
		buildArgs:         map[string]string{"SOURCE": "source", "BUSYBOX": "mirror.gcr.io/busybox", "ALPINE": "mirror.gcr.io/alpine", "OWNERID": "0", "SECONDBASE": "localhost/no-such-image"},
	},

	{
		name:              "header-builtin",
		contextDir:        "header-builtin",
		dockerUseBuildKit: true,
	},

	{
		name:              "copyglob-1",
		contextDir:        "copyglob",
		dockerUseBuildKit: true,
		buildArgs:         map[string]string{"SOURCE": "**/*.txt"},
	},
	{
		name:              "copyglob-2",
		contextDir:        "copyglob",
		dockerUseBuildKit: true,
		buildArgs:         map[string]string{"SOURCE": "**/sub/*.txt"},
	},
	{
		name:              "copyglob-3",
		contextDir:        "copyglob",
		dockerUseBuildKit: true,
		buildArgs:         map[string]string{"SOURCE": "e/**/*sub/*.txt"},
	},
	{
		name:              "copyglob-4",
		contextDir:        "copyglob",
		dockerUseBuildKit: true,
		buildArgs:         map[string]string{"SOURCE": "e/**/**/*sub/*.txt"},
	},
	{
		name:              "mount-cache-by-ownership",
		dockerUseBuildKit: true,
		dockerfileContents: strings.Join([]string{
			"FROM mirror.gcr.io/busybox",
			"USER 10",
			"RUN --mount=type=cache,uid=10,target=/cache touch /cache/10.txt",
			"USER 0",
			"RUN --mount=type=cache,target=/cache touch /cache/0.txt",
			"RUN mkdir -m 770 /results /results/0 /results/10 /results/0+10",
			"RUN chown -R 10 /results",
			"RUN --mount=type=cache,target=/cache cp -a /cache/* /results/0",
			"USER 10",
			"RUN --mount=type=cache,uid=10,target=/cache cp -a /cache/* /results/10",
			"USER 0",
			"RUN --mount=type=cache,uid=10,target=/cache cp -a /cache/* /results/0+10",
			"RUN touch -r /bin `find /results -print`",
		}, "\n"),
	},

	{
		name:              "mount-targets-new",
		contextDir:        "mount-targets",
		dockerUseBuildKit: true,
	},

	{
		name:                 "mount-targets-old",
		contextDir:           "mount-targets",
		dockerUseBuildKit:    false,
		compatScratchConfig:  types.OptionalBoolTrue,
		compatLayerOmissions: types.OptionalBoolTrue,
	},

	{
		name:              "mount-targets-mount",
		contextDir:        "mount-targets",
		dockerfile:        "Dockerfile.mount",
		dockerUseBuildKit: true,
	},

	{
		name:              "quoted and inherited arg",
		dockerfile:        "Dockerfile.quoted-arg",
		fsSkip:            []string{"(dir):arg-expansion.txt:mtime"},
		dockerUseBuildKit: true,
	},

	{
		name: "build-arg overrides header arg for FROM",
		dockerfileContents: strings.Join([]string{
			"ARG BASE=mirror.gcr.io/alpine",
			"FROM $BASE",
			"RUN echo $BASE > /base.txt",
		}, "\n"),
		buildArgs: map[string]string{"BASE": "mirror.gcr.io/busybox"},
		fsSkip:    []string{"(dir):base.txt:mtime"},
	},

	{
		name: "header arg default for FROM when no build-arg",
		dockerfileContents: strings.Join([]string{
			"ARG BASE=mirror.gcr.io/alpine",
			"FROM $BASE",
			"RUN echo $BASE > /base.txt",
		}, "\n"),
		fsSkip: []string{"(dir):base.txt:mtime"},
	},

	{
		name: "stage arg overrides header arg in same stage",
		dockerfileContents: strings.Join([]string{
			"ARG FOO=header",
			"FROM mirror.gcr.io/busybox",
			"ARG FOO=stage",
			"RUN echo $FOO > /foo.txt",
		}, "\n"),
		fsSkip: []string{"(dir):foo.txt:mtime"},
	},

	{
		name: "multiple ARGs in single instruction",
		dockerfileContents: strings.Join([]string{
			"FROM mirror.gcr.io/alpine",
			"ARG foo=0 bar=1",
			"RUN echo $foo $bar > /out.txt",
		}, "\n"),
		fsSkip: []string{"(dir):out.txt:mtime"},
	},

	{
		name: "multiple ARGs in single instruction with build-arg override",
		dockerfileContents: strings.Join([]string{
			"FROM mirror.gcr.io/alpine",
			"ARG foo=0 bar=1",
			"RUN echo $foo $bar > /out.txt",
		}, "\n"),
		buildArgs: map[string]string{"foo": "9", "bar": "8"},
		fsSkip:    []string{"(dir):out.txt:mtime"},
	},
}

func TestCommit(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		description             string
		baseImage               string
		changes, derivedChanges []string
		config, derivedConfig   *docker.Config
	}{
		{
			description: "defaults",
			baseImage:   "mirror.gcr.io/busybox",
		},
		{
			description: "empty change",
			baseImage:   "mirror.gcr.io/busybox",
			changes:     []string{""},
		},
		{
			description: "empty config",
			baseImage:   "mirror.gcr.io/busybox",
			config:      &docker.Config{},
		},
		{
			description: "cmd just changes",
			baseImage:   "mirror.gcr.io/busybox",
			changes:     []string{"CMD /bin/imaginarySh"},
		},
		{
			description: "cmd just config",
			baseImage:   "mirror.gcr.io/busybox",
			config: &docker.Config{
				Cmd: []string{"/usr/bin/imaginarySh"},
			},
		},
		{
			description: "cmd conflict",
			baseImage:   "mirror.gcr.io/busybox",
			changes:     []string{"CMD /bin/imaginarySh"},
			config: &docker.Config{
				Cmd: []string{"/usr/bin/imaginarySh"},
			},
		},
		{
			description: "entrypoint just changes",
			baseImage:   "mirror.gcr.io/busybox",
			changes:     []string{"ENTRYPOINT /bin/imaginarySh"},
		},
		{
			description: "entrypoint just config",
			baseImage:   "mirror.gcr.io/busybox",
			config: &docker.Config{
				Entrypoint: []string{"/usr/bin/imaginarySh"},
			},
		},
		{
			description: "entrypoint conflict",
			baseImage:   "mirror.gcr.io/busybox",
			changes:     []string{"ENTRYPOINT /bin/imaginarySh"},
			config: &docker.Config{
				Entrypoint: []string{"/usr/bin/imaginarySh"},
			},
		},
		{
			description: "environment just changes",
			baseImage:   "mirror.gcr.io/busybox",
			changes:     []string{"ENV A=1", "ENV C=2"},
		},
		{
			description: "environment just config",
			baseImage:   "mirror.gcr.io/busybox",
			config: &docker.Config{
				Env: []string{"A=B"},
			},
		},
		{
			description: "environment with conflict union",
			baseImage:   "mirror.gcr.io/busybox",
			changes:     []string{"ENV A=1", "ENV C=2"},
			config: &docker.Config{
				Env: []string{"A=B"},
			},
		},
		{
			description: "expose just changes",
			baseImage:   "mirror.gcr.io/busybox",
			changes:     []string{"EXPOSE 12345"},
		},
		{
			description: "expose just config",
			baseImage:   "mirror.gcr.io/busybox",
			config: &docker.Config{
				ExposedPorts: map[docker.Port]struct{}{"23456/tcp": {}},
			},
		},
		{
			description: "expose union implicit",
			baseImage:   "mirror.gcr.io/busybox",
			changes:     []string{"EXPOSE 12345"},
			config: &docker.Config{
				ExposedPorts: map[docker.Port]struct{}{"23456/tcp": {}},
			},
		},
		{
			description: "expose union explicit",
			baseImage:   "mirror.gcr.io/busybox",
			changes:     []string{"EXPOSE 12345/tcp"},
			config: &docker.Config{
				ExposedPorts: map[docker.Port]struct{}{"23456/tcp": {}},
			},
		},
		{
			description: "healthcheck just changes",
			baseImage:   "mirror.gcr.io/busybox",
			changes:     []string{`HEALTHCHECK --interval=1s --timeout=1s --start-period=1s --retries=1 CMD ["/bin/false"]`},
		},
		{
			description: "healthcheck just config",
			baseImage:   "mirror.gcr.io/busybox",
			config: &docker.Config{
				Healthcheck: &docker.HealthConfig{
					Test:        []string{"/bin/true"},
					Interval:    2 * time.Second,
					Timeout:     2 * time.Second,
					StartPeriod: 2 * time.Second,
					Retries:     2,
				},
			},
		},
		{
			description: "healthcheck conflict",
			baseImage:   "mirror.gcr.io/busybox",
			changes:     []string{`HEALTHCHECK --interval=1s --timeout=1s --start-period=1s --retries=1 CMD ["/bin/false"]`},
			config: &docker.Config{
				Healthcheck: &docker.HealthConfig{
					Test:        []string{"/bin/true"},
					Interval:    2 * time.Second,
					Timeout:     2 * time.Second,
					StartPeriod: 2 * time.Second,
					Retries:     2,
				},
			},
		},
		{
			description: "label just changes",
			baseImage:   "mirror.gcr.io/busybox",
			changes:     []string{"LABEL A=1 C=2"},
		},
		{
			description: "label just config",
			baseImage:   "mirror.gcr.io/busybox",
			config: &docker.Config{
				Labels: map[string]string{"A": "B"},
			},
		},
		{
			description: "label with conflict union",
			baseImage:   "mirror.gcr.io/busybox",
			changes:     []string{"LABEL A=1 C=2"},
			config: &docker.Config{
				Labels: map[string]string{"A": "B"},
			},
		},
		// n.b. dockerd didn't like a MAINTAINER change, so no test for it, and it's not in a config blob
		{
			description:    "onbuild just changes",
			baseImage:      "mirror.gcr.io/busybox",
			changes:        []string{"ONBUILD USER alice", "ONBUILD LABEL A=1"},
			derivedChanges: []string{"LABEL C=3"},
		},
		{
			description: "onbuild just config",
			baseImage:   "mirror.gcr.io/busybox",
			config: &docker.Config{
				OnBuild: []string{"USER bob", `CMD ["/bin/smash"]`, "LABEL B=2"},
			},
			derivedChanges: []string{"LABEL C=3"},
		},
		{
			description: "onbuild conflict",
			baseImage:   "mirror.gcr.io/busybox",
			changes:     []string{"ONBUILD USER alice", "ONBUILD LABEL A=1"},
			config: &docker.Config{
				OnBuild: []string{"USER bob", `CMD ["/bin/smash"]`, "LABEL B=2"},
			},
			derivedChanges: []string{"LABEL C=3"},
		},
		// n.b. dockerd didn't like a SHELL change, so no test for it or a conflict with a config blob
		{
			description: "shell just config",
			baseImage:   "mirror.gcr.io/busybox",
			config: &docker.Config{
				Shell: []string{"/usr/bin/imaginarySh"},
			},
		},
		{
			description: "stop signal conflict",
			baseImage:   "mirror.gcr.io/busybox",
			changes:     []string{"STOPSIGNAL SIGTERM"},
			config: &docker.Config{
				StopSignal: "SIGKILL",
			},
		},
		{
			description: "stop timeout=0",
			baseImage:   "mirror.gcr.io/busybox",
			config: &docker.Config{
				StopTimeout: 0,
			},
		},
		{
			description: "stop timeout=15",
			baseImage:   "mirror.gcr.io/busybox",
			config: &docker.Config{
				StopTimeout: 15,
			},
		},
		{
			description: "stop timeout=15, then 0",
			baseImage:   "mirror.gcr.io/busybox",
			config: &docker.Config{
				StopTimeout: 15,
			},
			derivedConfig: &docker.Config{
				StopTimeout: 0,
			},
		},
		{
			description: "stop timeout=0, then 15",
			baseImage:   "mirror.gcr.io/busybox",
			config: &docker.Config{
				StopTimeout: 0,
			},
			derivedConfig: &docker.Config{
				StopTimeout: 15,
			},
		},
		{
			description: "user just changes",
			baseImage:   "mirror.gcr.io/busybox",
			changes:     []string{"USER 1001:1001"},
		},
		{
			description: "user just config",
			baseImage:   "mirror.gcr.io/busybox",
			config: &docker.Config{
				User: "1000:1000",
			},
		},
		{
			description: "user with conflict",
			baseImage:   "mirror.gcr.io/busybox",
			changes:     []string{"USER 1001:1001"},
			config: &docker.Config{
				User: "1000:1000",
			},
		},
		{
			description: "volume just changes",
			baseImage:   "mirror.gcr.io/busybox",
			changes:     []string{"VOLUME /a-volume"},
		},
		{
			description: "volume just config",
			baseImage:   "mirror.gcr.io/busybox",
			config: &docker.Config{
				Volumes: map[string]struct{}{"/b-volume": {}},
			},
		},
		{
			description: "volume union",
			baseImage:   "mirror.gcr.io/busybox",
			changes:     []string{"VOLUME /a-volume"},
			config: &docker.Config{
				Volumes: map[string]struct{}{"/b-volume": {}},
			},
		},
		{
			description: "workdir just changes",
			baseImage:   "mirror.gcr.io/busybox",
			changes:     []string{"WORKDIR /yeah"},
		},
		{
			description: "workdir just config",
			baseImage:   "mirror.gcr.io/busybox",
			config: &docker.Config{
				WorkingDir: "/naw",
			},
		},
		{
			description: "workdir with conflict",
			baseImage:   "mirror.gcr.io/busybox",
			changes:     []string{"WORKDIR /yeah"},
			config: &docker.Config{
				WorkingDir: "/naw",
			},
		},
	}

	var tempdir string
	buildahDir := buildahDir
	if buildahDir == "" {
		if tempdir == "" {
			tempdir = t.TempDir()
		}
		buildahDir = filepath.Join(tempdir, "buildah")
	}
	dockerDir := dockerDir
	if dockerDir == "" {
		if tempdir == "" {
			tempdir = t.TempDir()
		}
		dockerDir = filepath.Join(tempdir, "docker")
	}

	ctx := t.Context()

	// connect to dockerd using go-dockerclient
	client, err := docker.NewClientFromEnv()
	require.NoErrorf(t, err, "unable to initialize docker client")
	var dockerVersion []string
	if version, err := client.Version(); err == nil {
		if version != nil {
			for _, s := range *version {
				dockerVersion = append(dockerVersion, s)
			}
		}
	} else {
		require.NoErrorf(t, err, "unable to connect to docker daemon")
	}

	// find a new place to store buildah builds
	tempdir = t.TempDir()

	// create subdirectories to use for buildah storage
	rootDir := filepath.Join(tempdir, "root")
	runrootDir := filepath.Join(tempdir, "runroot")

	// initialize storage for buildah
	options := storage.StoreOptions{
		GraphDriverName: os.Getenv("STORAGE_DRIVER"),
		GraphRoot:       rootDir,
		RunRoot:         runrootDir,
	}
	store, err := storage.GetStore(options)
	require.NoErrorf(t, err, "error creating buildah storage at %q", rootDir)
	defer func() {
		if store != nil {
			_, err := store.Shutdown(true)
			require.NoErrorf(t, err, "error shutting down storage for buildah")
		}
	}()

	// walk through test cases
	for testIndex, testCase := range testCases {
		t.Run(testCase.description, func(t *testing.T) {
			test := testCases[testIndex]

			// create the test container, then commit it, using the docker client
			baseImage := test.baseImage
			repository, tag := docker.ParseRepositoryTag(baseImage)
			if tag == "" {
				tag = "latest"
			}
			baseImage = repository + ":" + tag
			if _, err := client.InspectImage(test.baseImage); err != nil && errors.Is(err, docker.ErrNoSuchImage) {
				// oh, we need to pull the base image
				err = client.PullImage(docker.PullImageOptions{
					Repository: repository,
					Tag:        tag,
				}, docker.AuthConfiguration{})
				require.NoErrorf(t, err, "pulling base image")
			}
			container, err := client.CreateContainer(docker.CreateContainerOptions{
				Context: ctx,
				Config: &docker.Config{
					Image: baseImage,
				},
			})
			require.NoErrorf(t, err, "creating the working container with docker")
			if err == nil {
				defer func(containerName string) {
					err := client.RemoveContainer(docker.RemoveContainerOptions{
						ID:    containerName,
						Force: true,
					})
					assert.Nil(t, err, "error deleting working docker container %q", containerName)
				}(container.ID)
			}
			dockerImageName := "committed:" + strconv.Itoa(testIndex)
			dockerImage, err := client.CommitContainer(docker.CommitContainerOptions{
				Container:  container.ID,
				Changes:    test.changes,
				Run:        test.config,
				Repository: dockerImageName,
			})
			assert.NoErrorf(t, err, "committing the working container with docker")
			if err == nil {
				defer func(dockerImageName string) {
					err := client.RemoveImageExtended(dockerImageName, docker.RemoveImageOptions{
						Context: ctx,
						Force:   true,
					})
					assert.Nil(t, err, "error deleting newly-built docker image %q", dockerImage.ID)
				}(dockerImageName)
			}
			dockerRef, err := alltransports.ParseImageName("docker-daemon:" + dockerImageName)
			assert.NoErrorf(t, err, "parsing name of newly-committed docker image")

			if len(test.derivedChanges) > 0 || test.derivedConfig != nil {
				container, err := client.CreateContainer(docker.CreateContainerOptions{
					Context: ctx,
					Config: &docker.Config{
						Image: dockerImage.ID,
					},
				})
				require.NoErrorf(t, err, "creating the derived container with docker")
				if err == nil {
					defer func(containerName string) {
						err := client.RemoveContainer(docker.RemoveContainerOptions{
							ID:    containerName,
							Force: true,
						})
						assert.Nil(t, err, "error deleting derived docker container %q", containerName)
					}(container.ID)
				}
				derivedImageName := "derived:" + strconv.Itoa(testIndex)
				derivedImage, err := client.CommitContainer(docker.CommitContainerOptions{
					Container:  container.ID,
					Changes:    test.derivedChanges,
					Run:        test.derivedConfig,
					Repository: derivedImageName,
				})
				assert.NoErrorf(t, err, "committing the derived container with docker")
				defer func(derivedImageName string) {
					err := client.RemoveImageExtended(derivedImageName, docker.RemoveImageOptions{
						Context: ctx,
						Force:   true,
					})
					assert.Nil(t, err, "error deleting newly-derived docker image %q", derivedImage.ID)
				}(derivedImageName)
				dockerRef, err = alltransports.ParseImageName("docker-daemon:" + derivedImageName)
				assert.NoErrorf(t, err, "parsing name of newly-derived docker image")
			}

			// create the test container, then commit it, using the buildah API
			builder, err := buildah.NewBuilder(ctx, store, buildah.BuilderOptions{
				FromImage: baseImage,
			})
			require.NoErrorf(t, err, "creating the working container with buildah")
			defer func(builder *buildah.Builder) {
				err := builder.Delete()
				assert.NoErrorf(t, err, "removing the working container")
			}(builder)
			var overrideConfig *manifest.Schema2Config
			if test.config != nil {
				overrideConfig = config.Schema2ConfigFromGoDockerclientConfig(test.config)
			}
			buildahID, _, _, err := builder.Commit(ctx, nil, buildah.CommitOptions{
				PreferredManifestType: manifest.DockerV2Schema2MediaType,
				OverrideChanges:       test.changes,
				OverrideConfig:        overrideConfig,
			})
			assert.NoErrorf(t, err, "committing buildah image")
			buildahRef, err := is.Transport.NewStoreReference(store, nil, buildahID)
			assert.NoErrorf(t, err, "parsing name of newly-built buildah image")

			if len(test.derivedChanges) > 0 || test.derivedConfig != nil {
				derivedBuilder, err := buildah.NewBuilder(ctx, store, buildah.BuilderOptions{
					FromImage: buildahID,
				})
				require.NoErrorf(t, err, "creating the derived container with buildah")
				defer func(builder *buildah.Builder) {
					err := builder.Delete()
					assert.NoErrorf(t, err, "removing the derived container")
				}(derivedBuilder)
				var overrideConfig *manifest.Schema2Config
				if test.derivedConfig != nil {
					overrideConfig = config.Schema2ConfigFromGoDockerclientConfig(test.derivedConfig)
				}
				derivedID, _, _, err := builder.Commit(ctx, nil, buildah.CommitOptions{
					PreferredManifestType: manifest.DockerV2Schema2MediaType,
					OverrideChanges:       test.derivedChanges,
					OverrideConfig:        overrideConfig,
				})
				assert.NoErrorf(t, err, "committing derived buildah image")
				buildahRef, err = is.Transport.NewStoreReference(store, nil, derivedID)
				assert.NoErrorf(t, err, "parsing name of newly-derived buildah image")
			}

			// scan the images
			saveReport(ctx, t, dockerRef, filepath.Join(dockerDir, t.Name()), []byte{}, []byte{}, dockerVersion)
			saveReport(ctx, t, buildahRef, filepath.Join(buildahDir, t.Name()), []byte{}, []byte{}, dockerVersion)
			// compare the scans
			_, originalDockerConfig, ociDockerConfig, fsDocker := readReport(t, filepath.Join(dockerDir, t.Name()))
			_, originalBuildahConfig, ociBuildahConfig, fsBuildah := readReport(t, filepath.Join(buildahDir, t.Name()))
			miss, left, diff, same := compareJSON(originalDockerConfig, originalBuildahConfig, originalSkip)
			if !same {
				assert.Failf(t, "Image configurations differ as committed in Docker format", configCompareResult(miss, left, diff, "buildah"))
			}
			miss, left, diff, same = compareJSON(ociDockerConfig, ociBuildahConfig, ociSkip)
			if !same {
				assert.Failf(t, "Image configurations differ when converted to OCI format", configCompareResult(miss, left, diff, "buildah"))
			}
			miss, left, diff, same = compareJSON(fsDocker, fsBuildah, fsSkip)
			if !same {
				assert.Failf(t, "Filesystem contents differ", fsCompareResult(miss, left, diff, "buildah"))
			}
		})
	}
}
