package tmpdir

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteFileInRoot(t *testing.T) {
	t.Parallel()

	t.Run("creates file normally", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		err := writeFileInRoot(root, "Dockerfile", []byte("FROM scratch\n"), 0o600)
		require.NoError(t, err)
		content, err := os.ReadFile(filepath.Join(root, "Dockerfile"))
		require.NoError(t, err)
		assert.Equal(t, "FROM scratch\n", string(content))
	})

	t.Run("overwrites existing regular file", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		err := os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("old"), 0o600)
		require.NoError(t, err)
		err = writeFileInRoot(root, "Dockerfile", []byte("new"), 0o600)
		require.NoError(t, err)
		content, err := os.ReadFile(filepath.Join(root, "Dockerfile"))
		require.NoError(t, err)
		assert.Equal(t, "new", string(content))
	})

	t.Run("does not follow symlink escaping root", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()

		targetDir := t.TempDir()
		target := filepath.Join(targetDir, "pwned")
		err := os.WriteFile(target, []byte("original"), 0o600)
		require.NoError(t, err)

		err = os.Symlink(target, filepath.Join(root, "Dockerfile"))
		require.NoError(t, err)

		err = writeFileInRoot(root, "Dockerfile", []byte("attacker content"), 0o600)
		require.NoError(t, err)

		content, err := os.ReadFile(target)
		require.NoError(t, err)
		assert.Equal(t, "original", string(content))

		info, err := os.Lstat(filepath.Join(root, "Dockerfile"))
		require.NoError(t, err)
		assert.True(t, info.Mode().IsRegular())
	})

	t.Run("does not follow relative symlink escaping root", func(t *testing.T) {
		t.Parallel()
		// Create structure: parentDir/root/ and parentDir/secret
		parentDir := t.TempDir()
		root := filepath.Join(parentDir, "root")
		err := os.Mkdir(root, 0o755)
		require.NoError(t, err)
		secret := filepath.Join(parentDir, "secret")
		err = os.WriteFile(secret, []byte("sensitive data"), 0o600)
		require.NoError(t, err)

		err = os.Symlink("../secret", filepath.Join(root, "Dockerfile"))
		require.NoError(t, err)

		err = writeFileInRoot(root, "Dockerfile", []byte("attacker content"), 0o600)
		require.NoError(t, err)

		content, err := os.ReadFile(secret)
		require.NoError(t, err)
		assert.Equal(t, "sensitive data", string(content))
	})
}

func TestParseGitBuildContext(t *testing.T) {
	t.Parallel()

	t.Run("should parse url without fragment", func(t *testing.T) {
		repo, subdir, branch := parseGitBuildContext("https://github.com/containers/repo.git")
		assert.Equal(t, repo, "https://github.com/containers/repo.git")
		assert.Equal(t, subdir, "")
		assert.Equal(t, branch, "")
	})

	t.Run("should parse url with branch fragment", func(t *testing.T) {
		repo, subdir, branch := parseGitBuildContext("https://github.com/containers/repo.git#main")
		assert.Equal(t, repo, "https://github.com/containers/repo.git")
		assert.Equal(t, subdir, "")
		assert.Equal(t, branch, "main")
	})

	t.Run("should parse url with subdir fragment", func(t *testing.T) {
		repo, subdir, branch := parseGitBuildContext("https://github.com/containers/repo.git#:mydir")
		assert.Equal(t, repo, "https://github.com/containers/repo.git")
		assert.Equal(t, subdir, "mydir")
		assert.Equal(t, branch, "")
	})

	t.Run("should parse url with branch and subdir fragment", func(t *testing.T) {
		repo, subdir, branch := parseGitBuildContext("https://github.com/containers/repo.git#main:mydir")
		assert.Equal(t, repo, "https://github.com/containers/repo.git")
		assert.Equal(t, subdir, "mydir")
		assert.Equal(t, branch, "main")

		repo, subdir, branch = parseGitBuildContext("https://github.com/containers/repo.git#main#branch:mydir")
		assert.Equal(t, repo, "https://github.com/containers/repo.git")
		assert.Equal(t, subdir, "mydir")
		assert.Equal(t, branch, "main#branch")

		repo, subdir, branch = parseGitBuildContext("https://github.com/containers/repo.git#main:my:dir")
		assert.Equal(t, repo, "https://github.com/containers/repo.git")
		assert.Equal(t, subdir, "my:dir")
		assert.Equal(t, branch, "main")
	})
}

func TestForURLFailedDownloadCleanup(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	_, _, err := ForURL(context.Background(), parent, "context-", server.URL, nil)
	assert.ErrorContains(t, err, "invalid response status 404")
	entries, err := os.ReadDir(parent)
	require.NoError(t, err)
	assert.Empty(t, entries, "failed download leaked temporary directories")
}
