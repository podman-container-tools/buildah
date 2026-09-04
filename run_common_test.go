package buildah

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/opencontainers/runtime-tools/generate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/common/pkg/config"
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

func TestBuildahProxyAndNoLeak(t *testing.T) {
	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "containers.conf")
	confContent := `
[containers]
http_proxy = false
env = [
  "http_proxy=http://host.containers.internal:1080",
  "https_proxy=http://host.containers.internal:1080",
  "CUSTOM_NON_PROXY_VAR=should_be_ignored"
]
`
	err := os.WriteFile(confPath, []byte(confContent), 0o644)
	require.NoError(t, err)

	// Reset the default container config cache on test cleanup. Register before t.Setenv so that
	// the cleanups caused by `t.Setenv` calls run before it (because cleanup happens in LIFO order).
	t.Cleanup(func() { _, _ = config.New(&config.Options{SetDefault: true}) })
	t.Setenv("CONTAINERS_CONF", confPath)
	t.Setenv("http_proxy", "http://127.0.0.1:1080")

	_, err = config.New(&config.Options{SetDefault: true})
	require.NoError(t, err)

	builder := &Builder{}
	builder.CommonBuildOpts = &CommonBuildOptions{
		HTTPProxy: false,
	}

	g, err := generate.New("linux")
	require.NoError(t, err)

	err = builder.configureEnvironment(&g, RunOptions{}, []string{"PATH=/usr/bin"})
	require.NoError(t, err)

	procEnv := g.Config.Process.Env
	t.Logf("Transient process env during RUN: %v", procEnv)

	assert.Contains(t, procEnv, "http_proxy=http://host.containers.internal:1080")
	assert.NotContains(t, procEnv, "http_proxy=http://127.0.0.1:1080")
	assert.NotContains(t, procEnv, "CUSTOM_NON_PROXY_VAR=should_be_ignored")

	imageEnv := builder.OCIv1.Config.Env
	assert.NotContains(t, imageEnv, "http_proxy=http://host.containers.internal:1080")
}

func TestBuildahProxyPrecedence(t *testing.T) {
	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "containers.conf")
	confContent := `
[containers]
http_proxy = false
env = [
  "http_proxy=http://host.containers.internal:1080"
]
`
	err := os.WriteFile(confPath, []byte(confContent), 0o644)
	require.NoError(t, err)

	// Reset the default container config cache on test cleanup. Register before t.Setenv so that
	// the cleanups caused by `t.Setenv` calls run before it (because cleanup happens in LIFO order).
	t.Cleanup(func() { _, _ = config.New(&config.Options{SetDefault: true}) })
	t.Setenv("CONTAINERS_CONF", confPath)
	t.Setenv("http_proxy", "http://127.0.0.1:1080")

	_, err = config.New(&config.Options{SetDefault: true})
	require.NoError(t, err)

	builder := &Builder{}

	// Case 1: HTTPProxy: true (Host env overrides containers.conf)
	builder.CommonBuildOpts = &CommonBuildOptions{HTTPProxy: true}
	g1, err := generate.New("linux")
	require.NoError(t, err)
	err = builder.configureEnvironment(&g1, RunOptions{}, []string{"PATH=/usr/bin"})
	require.NoError(t, err)
	assert.Contains(t, g1.Config.Process.Env, "http_proxy=http://127.0.0.1:1080")

	// Case 2: CLI Options.Env overrides both containers.conf and host env
	g2, err := generate.New("linux")
	require.NoError(t, err)
	err = builder.configureEnvironment(&g2, RunOptions{Env: []string{"http_proxy=http://cli.override:1080"}}, []string{"PATH=/usr/bin"})
	require.NoError(t, err)
	assert.Contains(t, g2.Config.Process.Env, "http_proxy=http://cli.override:1080")
}
