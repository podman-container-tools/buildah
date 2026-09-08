package imagebuildah

import (
	"context"
	"testing"

	"github.com/openshift/imagebuilder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/semaphore"
)

func testExecutorWithJobToken(t *testing.T) (*executor, *semaphore.Weighted, imagebuilder.Stages) {
	t.Helper()
	sem := semaphore.NewWeighted(1)
	require.NoError(t, sem.Acquire(context.Background(), 1))
	stages := imagebuilder.Stages{{Position: 0, Name: "base"}}
	b := &executor{
		terminatedStage: make(map[int]error),
		stagesSemaphore: sem,
	}
	return b, sem, stages
}

func assertCallerStillHoldsJobToken(t *testing.T, sem *semaphore.Weighted) {
	t.Helper()
	assert.False(t, sem.TryAcquire(1), "caller must still hold the job-semaphore token")
	require.NotPanics(t, func() { sem.Release(1) })
}

func TestWaitForStageCancelledContextDoesNotOverRelease(t *testing.T) {
	t.Parallel()
	b, sem, stages := testExecutorWithJobToken(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	found, err := b.waitForStage(ctx, "base", stages)
	require.True(t, found)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assertCallerStillHoldsJobToken(t, sem)
}

func TestWaitForStageAlreadyTerminatedKeepsToken(t *testing.T) {
	t.Parallel()
	b, sem, stages := testExecutorWithJobToken(t)
	b.terminatedStage[0] = nil

	found, err := b.waitForStage(context.Background(), "base", stages)
	require.True(t, found)
	require.NoError(t, err)
	assertCallerStillHoldsJobToken(t, sem)
}
