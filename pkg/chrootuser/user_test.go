package chrootuser

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLookupEmptyTree(t *testing.T) {
	root := t.TempDir()

	groups, err := GetAdditionalGroupsForUser(root, 1)
	assert.Error(t, err, "GetAdditionalGroupsForUser(1)")
	assert.Empty(t, groups, "supplemental groups for undefined UID")

	group, err := GetGroup(root, "bin")
	assert.Error(t, err, `GetGroup("bin")`)
	assert.Empty(t, group, "information for group bin")

	group, err = GetGroup(root, "1")
	assert.NoError(t, err, `GetGroup("1")`)
	assert.Equal(t, uint32(1), group, "information for group bin")

	uid, gid, homedir, err := GetUser(root, "bin")
	assert.Error(t, err, `GetUser("bin")`)
	assert.Empty(t, uid, "uid for undefined user")
	assert.Empty(t, gid, "primary gid for undefined user")
	assert.Contains(t, []string{"", "/"}, homedir, "home directory for undefined user")

	uid, gid, homedir, err = GetUser(root, "1")
	assert.NoError(t, err, `GetUser("1")`)
	assert.Equal(t, uint32(1), uid, "uid for undefined user")
	assert.Equal(t, uint32(0), gid, "primary gid for undefined user")
	assert.Contains(t, []string{"", "/"}, homedir, "home directory for undefined user")
}

func TestLookupSymlinkOut(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "etc"), 0o700))
	require.NoError(t, os.Symlink("/etc/passwd", filepath.Join(root, "etc", "passwd")))

	groups, err := GetAdditionalGroupsForUser(root, 1)
	assert.Error(t, err, "GetAdditionalGroupsForUser(1)")
	assert.Empty(t, groups, "supplemental groups for undefined UID")

	group, err := GetGroup(root, "bin")
	assert.Error(t, err, `GetGroup("bin")`)
	assert.Empty(t, group, "information for group bin")

	group, err = GetGroup(root, "1")
	assert.NoError(t, err, `GetGroup("1")`)
	assert.Equal(t, uint32(1), group, "information for group 1")

	uid, gid, homedir, err := GetUser(root, "bin")
	assert.Error(t, err, `GetUser("bin")`)
	assert.Empty(t, uid, "uid for undefined user")
	assert.Empty(t, gid, "primary gid for undefined user")
	assert.Contains(t, []string{"", "/"}, homedir, "home directory for undefined user")

	uid, gid, homedir, err = GetUser(root, "1")
	assert.NoError(t, err, `GetUser("1")`)
	assert.Equal(t, uint32(1), uid, "uid for numbered user")
	assert.Equal(t, uint32(0), gid, "primary gid for numbered user")
	assert.Contains(t, []string{"", "/"}, homedir, "home directory for numbered user")
}

func TestLookupCircularSymlink(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "etc"), 0o700))
	require.NoError(t, os.Symlink("passwd", filepath.Join(root, "etc", "passwd")))

	groups, err := GetAdditionalGroupsForUser(root, 1)
	assert.Error(t, err, "GetAdditionalGroupsForUser(1)")
	assert.Empty(t, groups, "supplemental groups for undefined UID")

	group, err := GetGroup(root, "bin")
	assert.Error(t, err, `GetGroup("bin")`)
	assert.Empty(t, group, "information for group 1")

	group, err = GetGroup(root, "1")
	assert.NoError(t, err, `GetGroup("1")`)
	assert.Equal(t, uint32(1), group, "information for group 1")

	uid, gid, homedir, err := GetUser(root, "bin")
	assert.Error(t, err, `GetUser("bin")`)
	assert.Empty(t, uid, "uid for undefined user")
	assert.Empty(t, gid, "primary gid for undefined user")
	assert.Contains(t, []string{"", "/"}, homedir, "home directory for undefined user")

	uid, gid, homedir, err = GetUser(root, "1")
	assert.NoError(t, err, `GetUser("1")`)
	assert.Equal(t, uint32(1), uid, "uid for numeric user")
	assert.Equal(t, uint32(0), gid, "primary gid for numeric user")
	assert.Contains(t, []string{"", "/"}, homedir, "home directory for numeric user")
}

func TestLookupValidNoGroups(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "etc"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "etc", "passwd"), []byte("bin:*:1:1:Super Duper User:/bin:/bin/sh"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "etc", "group"), []byte("bin:*:1:"), 0o644))

	groups, err := GetAdditionalGroupsForUser(root, 1)
	assert.NoError(t, err, "GetAdditionalGroupsForUser(1)")
	assert.Empty(t, groups, "no supplemental groups for UID 1")

	group, err := GetGroup(root, "bin")
	assert.NoError(t, err, `GetGroup("bin")`)
	assert.Equal(t, uint32(1), group, "GID for group bin")

	group, err = GetGroup(root, "1")
	assert.NoError(t, err, `GetGroup("1")`)
	assert.Equal(t, uint32(1), group, "GID for group 1")

	uid, gid, homedir, err := GetUser(root, "bin")
	assert.NoError(t, err, `GetUser("bin")`)
	assert.Equal(t, uint32(1), uid, "uid for bin")
	assert.Equal(t, uint32(1), gid, "primary gid for bin")
	assert.Equal(t, "/bin", homedir, "home directory for bin")

	uid, gid, homedir, err = GetUser(root, "1")
	assert.NoError(t, err, `GetUser("1")`)
	assert.Equal(t, uint32(1), uid, "uid for 1")
	assert.Equal(t, uint32(1), gid, "primary gid for 1")
	assert.Equal(t, "/bin", homedir, "home directory for 1")
}

func TestLookupValidSpecs(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "etc"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "etc", "passwd"), []byte("bin:*:1:1:Super Duper User:/bin:/bin/sh"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "etc", "group"), []byte("bin:*:1:"), 0o644))

	uid, gid, homedir, err := GetUser(root, "bin:0")
	assert.NoError(t, err, `GetUser("bin:0")`)
	assert.Equal(t, uint32(1), uid, "uid for bin:0")
	assert.Equal(t, uint32(0), gid, "primary gid for bin:0")
	assert.Equal(t, "/bin", homedir, "home directory for bin:0")

	uid, gid, homedir, err = GetUser(root, "1:10")
	assert.NoError(t, err, `GetUser("1:10")`)
	assert.Equal(t, uint32(1), uid, "uid for 1:10")
	assert.Equal(t, uint32(10), gid, "primary gid for 1:10")
	assert.Equal(t, "/bin", homedir, "home directory for 1")

	uid, gid, homedir, err = GetUser(root, "0:bin")
	assert.NoError(t, err, `GetUser("0:bin")`)
	assert.Equal(t, uint32(0), uid, "uid for 0:bin")
	assert.Equal(t, uint32(1), gid, "primary gid for 0:bin")
	assert.Contains(t, []string{"", "/"}, homedir, "home directory for 0:bin")
}

func TestLookupValidWithGroups(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "etc"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "etc", "passwd"), []byte("bin:*:1:1:Super Duper User:/bin:/bin/sh"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "etc", "group"), []byte("bin:*:1:\nother:*:2:bin\nanother:*:3:bin\n"), 0o644))

	groups, err := GetAdditionalGroupsForUser(root, 1)
	assert.NoError(t, err, "GetAdditionalGroupsForUser(1)")
	assert.ElementsMatch(t, []uint32{2, 3}, groups, "supplemental groups for UID 1")

	group, err := GetGroup(root, "bin")
	assert.NoError(t, err, `GetGroup("bin")`)
	assert.Equal(t, uint32(1), group, "GID for group bin")

	group, err = GetGroup(root, "1")
	assert.NoError(t, err, `GetGroup("1")`)
	assert.Equal(t, uint32(1), group, "GID for group 1")

	uid, gid, homedir, err := GetUser(root, "bin")
	assert.NoError(t, err, `GetUser("bin")`)
	assert.Equal(t, uint32(1), uid, "uid for bin")
	assert.Equal(t, uint32(1), gid, "primary gid for bin")
	assert.Equal(t, "/bin", homedir, "home directory for bin")

	uid, gid, homedir, err = GetUser(root, "1")
	assert.NoError(t, err, `GetUser("1")`)
	assert.Equal(t, uint32(1), uid, "uid for 1")
	assert.Equal(t, uint32(1), gid, "primary gid for 1")
	assert.Equal(t, "/bin", homedir, "home directory for 1")
}

var testGroupData = `# comment
  # indented comment
wheel:*:0:root
daemon:*:1:
kmem:*:2:
`

func TestParseStripComments(t *testing.T) {
	t.Parallel()
	// Test reading group file, ignoring comment lines
	rc := bufio.NewScanner(strings.NewReader(testGroupData))
	line, ok := scanWithoutComments(rc)
	assert.Equal(t, ok, true)
	assert.Equal(t, line, "wheel:*:0:root")
}

func TestParseNextGroup(t *testing.T) {
	t.Parallel()
	// Test parsing group file
	rc := bufio.NewScanner(strings.NewReader(testGroupData))
	expected := []lookupGroupEntry{
		{"wheel", 0, "root"},
		{"daemon", 1, ""},
		{"kmem", 2, ""},
	}
	for _, exp := range expected {
		grp := parseNextGroup(rc)
		assert.NotNil(t, grp)
		assert.Equal(t, *grp, exp)
	}
	assert.Nil(t, parseNextGroup(rc))
}
