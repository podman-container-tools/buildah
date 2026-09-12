package urlsource

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsCommitSHAPrefix(t *testing.T) {
	cases := map[string]bool{
		"":       false,
		"a1b2c3": true,
		"A1B2C3": true,
		"a1b2c3d4e5f60718293a4b5c6d7e8f9012345678":  true,
		"sha256:e3b0c44298fc1c149afbf4c8996fb92427": false,
		"not-hex!": false,
		"g1b2c3":   false,
	}
	for input, want := range cases {
		assert.Equal(t, want, IsCommitSHAPrefix(input), "IsCommitSHAPrefix(%q)", input)
	}
}

func TestResolveCommit(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		output, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "git %v: %s", args, output)
	}
	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	run("commit", "--allow-empty", "-m", "initial commit")

	headCmd := exec.Command("git", "rev-parse", "HEAD")
	headCmd.Dir = dir
	expected, err := headCmd.Output()
	assert.NoError(t, err)
	expectedSHA := strings.TrimSpace(string(expected))

	sha, err := ResolveCommit(dir)
	require.NoError(t, err)
	require.NotEmpty(t, sha)
	require.Equal(t, expectedSHA, sha)

	subdir := dir + "/sub"
	require.NoError(t, exec.Command("mkdir", subdir).Run())
	shaFromSubdir, err := ResolveCommit(subdir)
	require.NoError(t, err)
	require.Equal(t, expectedSHA, shaFromSubdir)

	_, err = ResolveCommit(t.TempDir())
	require.Error(t, err)
}
