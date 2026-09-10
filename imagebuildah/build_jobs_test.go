package imagebuildah

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/buildah/define"
	"go.podman.io/storage"
	storageTypes "go.podman.io/storage/types"
)

// concurrentStageCount is high enough that several stages are still copying when the
// stage that cannot succeed reports its failure, which is what gives the teardown
// something to race against.
const concurrentStageCount = 24

// synchronizedBuffer collects output from every stage running in parallel, which a plain
// bytes.Buffer cannot do safely.
type synchronizedBuffer struct {
	lock     sync.Mutex
	contents bytes.Buffer
}

func (b *synchronizedBuffer) Write(p []byte) (int, error) {
	b.lock.Lock()
	defer b.lock.Unlock()
	return b.contents.Write(p)
}

func (b *synchronizedBuffer) String() string {
	b.lock.Lock()
	defer b.lock.Unlock()
	return b.contents.String()
}

// newTestStore returns a store backed by temporary directories, so the test does not
// disturb the caller's containers and needs no privileges beyond writing to its own
// scratch space.
func newTestStore(t *testing.T) storage.Store {
	t.Helper()
	store, err := storage.GetStore(storageTypes.StoreOptions{
		RunRoot:         t.TempDir(),
		GraphRoot:       t.TempDir(),
		GraphDriverName: "vfs",
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		if _, err := store.Shutdown(true); err != nil {
			t.Logf("shutting down test store: %v", err)
		}
	})
	return store
}

// writeConcurrentFailureContext writes a build context holding many independent stages
// which each copy a payload, one stage which cannot succeed, and a final stage which
// pulls from all of them so that none are skipped as unused.
func writeConcurrentFailureContext(t *testing.T) string {
	t.Helper()
	contextDirectory := t.TempDir()

	// Large enough that copying it takes long enough to overlap with the failing stage,
	// small enough to keep the test quick.
	payload := bytes.Repeat([]byte("buildah"), 1024*1024)
	require.NoError(t, os.WriteFile(filepath.Join(contextDirectory, "payload"), payload, 0o644))

	var containerfile strings.Builder
	for stage := range concurrentStageCount {
		fmt.Fprintf(&containerfile, "FROM scratch AS worker%d\nCOPY payload /payload%d\n\n", stage, stage)
	}

	// Fails while the workers above are still running: the file is absent from the
	// context, so the copier reports the error almost immediately.
	containerfile.WriteString("FROM scratch AS broken\nCOPY this-file-is-not-in-the-context /\n\n")

	containerfile.WriteString("FROM scratch\n")
	for stage := range concurrentStageCount {
		fmt.Fprintf(&containerfile, "COPY --from=worker%d /payload%d /worker%d\n", stage, stage, stage)
	}
	containerfile.WriteString("COPY --from=broken / /broken\n")

	containerfilePath := filepath.Join(contextDirectory, "Containerfile")
	require.NoError(t, os.WriteFile(containerfilePath, []byte(containerfile.String()), 0o644))
	return contextDirectory
}

// TestConcurrentStageFailureReportsErrorWithoutCrashing covers a build where one stage
// fails while its siblings are still running. Tearing the build down used to delete the
// working containers of the stages that had not finished yet, so a surviving stage either
// dereferenced the builder that had just been cleared, taking the process down with a
// SIGSEGV, or lost its layers partway through an instruction and reported "layer not
// known". The build is expected to fail here - what matters is that it fails by returning
// the error from the stage that could not succeed.
func TestConcurrentStageFailureReportsErrorWithoutCrashing(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("mounting an overlay over the build context requires root privileges, skipping")
	}
	store := newTestStore(t)
	contextDirectory := writeConcurrentFailureContext(t)

	jobs := 8
	var buildOutput synchronizedBuffer

	// Tearing a running stage down reports itself through the logger rather than
	// through the error returned to us, so collect that too.
	previousLogOutput := logrus.StandardLogger().Out
	logrus.SetOutput(&buildOutput)
	t.Cleanup(func() { logrus.SetOutput(previousLogOutput) })

	_, _, err := BuildDockerfiles(context.Background(), store, define.BuildOptions{
		ContextDirectory: contextDirectory,
		Jobs:             &jobs,
		// None of the stages RUN anything, so the build needs no network and should
		// not depend on the helper binaries that setting one up would require.
		ConfigureNetwork: define.NetworkDisabled,
		Out:              &buildOutput,
		Err:              &buildOutput,
		ReportWriter:     &buildOutput,
	}, filepath.Join(contextDirectory, "Containerfile"))

	require.Error(t, err, "the build should fail, because one of its stages cannot succeed")

	// The failure has to be the one the Containerfile asks for. Reporting a deleted
	// working container or a missing layer instead means a stage was torn down while
	// it was still running.
	assert.Contains(t, err.Error(), "this-file-is-not-in-the-context",
		"build should fail on the missing file, not on teardown of a running stage")
	for _, teardownSymptom := range []string{
		"layer not known",
		"identifier is not a container",
		"working container was deleted while the stage was still running",
	} {
		assert.NotContains(t, err.Error(), teardownSymptom,
			"a stage was torn down while it was still running")
		assert.NotContains(t, buildOutput.String(), teardownSymptom,
			"a stage was torn down while it was still running")
	}
}

// TestRecordWorkingContainerAfterDeleteFailsStage covers the guard on its own, since the
// race above only reaches it once the timing lines up. A stage whose working container has
// been deleted has to report that rather than dereference the builder that Delete cleared.
func TestRecordWorkingContainerAfterDeleteFailsStage(t *testing.T) {
	t.Parallel()
	stage := &stageExecutor{name: "worker0"}

	err := stage.recordWorkingContainer()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "worker0")
	assert.Empty(t, stage.containerIDs, "nothing should be recorded when there is no working container")
}
