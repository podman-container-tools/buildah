//go:build linux || freebsd

package buildah

import (
	"errors"
	"os/exec"
	"testing"

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
