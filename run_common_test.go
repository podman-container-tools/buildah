package buildah

import (
	"errors"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/buildah/define"
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

func TestGetSecretMount(t *testing.T) {
	const invalidSyntax = "secret should have syntax id=id[,target=path,required=bool,mode=uint,uid=uint,gid=uint,env=dstVarName]"

	t.Run("valid-options", func(t *testing.T) {
		validOptions := []struct {
			name   string
			tokens []string
		}{
			{"id-only", []string{"type=secret", "id=example"}},
			{"absolute-target", []string{"type=secret", "id=example", "target=/run/secrets/example"}},
			{"relative-target", []string{"type=secret", "id=example", "target=secret"}},
			{"mode", []string{"type=secret", "id=example", "mode=0400"}},
			{"ownership-and-required", []string{"type=secret", "id=example", "uid=1000", "gid=1000", "required=false"}},
			{"environment", []string{"type=secret", "id=example", "env=SECRET"}},
		}
		for i := range validOptions {
			t.Run(validOptions[i].name, func(t *testing.T) {
				_, err := (&Builder{}).getSecretMount(validOptions[i].tokens, map[string]define.Secret{}, IDMaps{}, "/work")
				require.NoError(t, err)
			})
		}
	})

	t.Run("bare-required", func(t *testing.T) {
		_, err := (&Builder{}).getSecretMount([]string{"type=secret", "id=example", "required"}, map[string]define.Secret{}, IDMaps{}, "/work")
		require.EqualError(t, err, `secret required but no secret with id "example" found`)
	})

	t.Run("target-provides-id", func(t *testing.T) {
		_, err := (&Builder{}).getSecretMount([]string{"type=secret", "target=secret", "required=true"}, map[string]define.Secret{}, IDMaps{}, "/work")
		require.EqualError(t, err, `secret required but no secret with id "secret" found`)
	})

	t.Run("required-true", func(t *testing.T) {
		t.Setenv("BUILDAH_TEST_SECRET", "secret-value")
		secrets := map[string]define.Secret{
			"example": {
				ID:         "example",
				Source:     "BUILDAH_TEST_SECRET",
				SourceType: "env",
			},
		}
		result, err := (&Builder{}).getSecretMount([]string{"type=secret", "id=example", "required=true", "env=SECRET_VALUE"}, secrets, IDMaps{}, "/work")
		require.NoError(t, err)
		require.Equal(t, "SECRET_VALUE=secret-value", result.EnvVariable)
	})

	t.Run("invalid-options", func(t *testing.T) {
		invalidOptions := []string{
			"type",
			"id",
			"target",
			"dst",
			"destination",
			"mode",
			"uid",
			"gid",
			"env",
			"id=",
			"env=",
			"required=invalid",
			"mode=invalid",
			"uid=invalid",
			"gid=invalid",
		}
		for i := range invalidOptions {
			t.Run(invalidOptions[i], func(t *testing.T) {
				_, err := (&Builder{}).getSecretMount([]string{"type=secret", invalidOptions[i]}, map[string]define.Secret{}, IDMaps{}, "/work")
				require.EqualError(t, err, invalidSyntax)
			})
		}
	})
}
