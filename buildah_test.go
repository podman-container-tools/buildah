package buildah

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/sirupsen/logrus"
	lslog "github.com/sirupsen/logrus/hooks/slog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	imagetypes "go.podman.io/image/v5/types"
	"go.podman.io/storage"
	"go.podman.io/storage/types"
)

var testSystemContext = imagetypes.SystemContext{
	SignaturePolicyPath:      "tests/policy.json",
	SystemRegistriesConfPath: "tests/registries.conf",
}

func TestMain(m *testing.M) {
	var logLevel string
	debug := false
	if InitReexec() {
		return
	}
	flag.BoolVar(&debug, "debug", false, "turn on debug logging")
	flag.StringVar(&logLevel, "log-level", "error", "log level")
	flag.Parse()
	level, err := logrus.ParseLevel(logLevel)
	if err != nil {
		logrus.Fatalf("error parsing log level %q: %v", logLevel, err)
	}
	if debug && level < logrus.DebugLevel {
		level = logrus.DebugLevel
	}
	logrus.SetOutput(io.Discard)
	logrus.AddHook(lslog.NewHook(slog.Default(), nil))
	logrus.SetLevel(level)
	slog.SetLogLoggerLevel(lslog.Level(level).Level())
	os.Exit(m.Run())
}

func TestOpenBuilderCommonBuildOpts(t *testing.T) {
	// This test cannot be parallelized as this uses NewBuilder()
	// which eventually and indirectly accesses a global variable
	// defined in `go-selinux`, this must be fixed at `go-selinux`
	// or builder must enable sometime of locking mechanism i.e if
	// routine is creating Builder other's must wait for it.
	// Tracked here: https://github.com/containers/buildah/issues/5967
	ctx := context.TODO()
	store, err := storage.GetStore(types.StoreOptions{
		RunRoot:         t.TempDir(),
		GraphRoot:       t.TempDir(),
		GraphDriverName: "vfs",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _, err := store.Shutdown(true); assert.NoError(t, err) })
	b, err := NewBuilder(ctx, store, BuilderOptions{})
	require.NoError(t, err)
	require.NotNil(t, b.CommonBuildOpts)
	b.CommonBuildOpts = nil
	builderContainerID := b.ContainerID
	err = b.Save()
	require.NoError(t, err)
	b, err = OpenBuilder(store, builderContainerID)
	require.NoError(t, err)
	require.NotNil(t, b.CommonBuildOpts)
	builders, err := OpenAllBuilders(store)
	require.NoError(t, err)
	for _, b := range builders {
		require.NotNil(t, b.CommonBuildOpts)
	}
	imageID, _, _, err := b.Commit(ctx, nil, CommitOptions{})
	require.NoError(t, err)
	b, err = ImportBuilderFromImage(ctx, store, ImportFromImageOptions{
		Image: imageID,
	})
	require.NoError(t, err)
	require.NotNil(t, b.CommonBuildOpts)
	container, err := store.CreateContainer("", nil, imageID, "", "", &storage.ContainerOptions{})
	require.NoError(t, err)
	require.NotNil(t, container)
	b, err = ImportBuilder(ctx, store, ImportOptions{
		Container:           container.ID,
		SignaturePolicyPath: testSystemContext.SignaturePolicyPath,
	})
	require.NoError(t, err)
	require.NotNil(t, b.CommonBuildOpts)
}
