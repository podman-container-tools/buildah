package buildah

import (
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapContainerNameToHostname(t *testing.T) {
	cases := [][2]string{
		{"trivial", "trivial"},
		{"Nottrivial", "Nottrivial"},
		{"0Nottrivial", "0Nottrivial"},
		{"0Nottrivi-al", "0Nottrivi-al"},
		{"-0Nottrivi-al", "0Nottrivi-al"},
		{".-0Nottrivi-.al", "0Nottrivi-.al"},
		{".-0Nottrivi-.al0123456789", "0Nottrivi-.al0123456789"},
		{".-0Nottrivi-.al0123456789+0123456789", "0Nottrivi-.al01234567890123456789"},
		{".-0Nottrivi-.al0123456789+0123456789/0123456789", "0Nottrivi-.al012345678901234567890123456789"},
		{".-0Nottrivi-.al0123456789+0123456789/0123456789%0123456789", "0Nottrivi-.al0123456789012345678901234567890123456789"},
		{".-0Nottrivi-.al0123456789+0123456789/0123456789%0123456789_0123456789", "0Nottrivi-.al01234567890123456789012345678901234567890123456789"},
		{".-0Nottrivi-.al0123456789+0123456789/0123456789%0123456789_0123456789:0123456", "0Nottrivi-.al012345678901234567890123456789012345678901234567890"},
		{".-0Nottrivi-.al0123456789+0123456789/0123456789%0123456789_0123456789:0123456789", "0Nottrivi-.al012345678901234567890123456789012345678901234567890"},
	}
	for i := range cases {
		t.Run(cases[i][0], func(t *testing.T) {
			sanitized := mapContainerNameToHostname(cases[i][0])
			assert.Equalf(t, cases[i][1], sanitized, "mapping container name %q to a valid hostname", cases[i][0])
		})
	}
}

func TestAwaitContainerStopPoll(t *testing.T) {
	fc := (<-chan struct{})(make(chan struct{}))
	var deadline <-chan time.Time
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	timedOut := awaitContainerStop(&deadline, &fc, time.Second, ticker.C)
	assert.False(t, timedOut, "should not time out on normal poll")
}

func TestAwaitContainerStopCopyDoneStartsDeadline(t *testing.T) {
	ch := make(chan struct{}, 1)
	ch <- struct{}{}
	fc := (<-chan struct{})(ch)
	var deadline <-chan time.Time

	pollCh := make(chan time.Time)
	timedOut := awaitContainerStop(&deadline, &fc, time.Second, pollCh)
	assert.False(t, timedOut, "finishedCopy should not report timeout")
	assert.NotNil(t, deadline, "deadline should be set after finishedCopy")
	assert.Nil(t, fc, "finishedCopy should be nil after first receive")
}

func TestAwaitContainerStopDeadlineFires(t *testing.T) {
	fc := (<-chan struct{})(nil)
	deadline := time.After(time.Millisecond)
	time.Sleep(5 * time.Millisecond)

	pollCh := make(chan time.Time)
	timedOut := awaitContainerStop(&deadline, &fc, time.Second, pollCh)
	assert.True(t, timedOut, "should report timeout when deadline expires")
}

func TestAwaitContainerStopClosedChannelNoResetDeadline(t *testing.T) {
	ch := make(chan struct{})
	close(ch)
	fc := (<-chan struct{})(ch)
	var deadline <-chan time.Time

	pollCh := make(chan time.Time)
	timedOut := awaitContainerStop(&deadline, &fc, 100*time.Millisecond, pollCh)
	assert.False(t, timedOut)
	assert.NotNil(t, deadline)
	assert.Nil(t, fc, "finishedCopy must be nil after first receive to prevent deadline reset")

	// Wait for deadline to expire
	time.Sleep(150 * time.Millisecond)

	timedOut = awaitContainerStop(&deadline, &fc, time.Second, pollCh)
	assert.True(t, timedOut, "deadline must fire when finishedCopy is nil (bug fix validation)")
}

func TestCheckExitCodeError(t *testing.T) {
	exitErr := exec.Command("false").Run()
	require.Error(t, exitErr)
	var ee *exec.ExitError
	require.True(t, errors.As(exitErr, &ee))
	require.Equal(t, 1, ee.ExitCode())

	for _, tc := range []struct {
		name           string
		err            error
		validExitCodes []int32
		expectNil      bool
	}{
		{"nil error, nil codes", nil, nil, true},
		{"nil error, code 0 listed", nil, []int32{0}, true},
		{"nil error, code 0 not listed", nil, []int32{1}, false},
		{"exit error, nil codes", exitErr, nil, false},
		{"exit error, empty codes", exitErr, []int32{}, false},
		{"exit error, matching code", exitErr, []int32{1}, true},
		{"exit error, matching with others", exitErr, []int32{0, 1, 2}, true},
		{"exit error, non-matching code", exitErr, []int32{2}, false},
		{"regular error, nil codes", errors.New("test"), nil, false},
		{"regular error, with codes", errors.New("test"), []int32{1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := checkExitCodeError(tc.err, tc.validExitCodes)
			if tc.expectNil {
				assert.NoError(t, result)
			} else {
				assert.Error(t, result)
			}
		})
	}
}
