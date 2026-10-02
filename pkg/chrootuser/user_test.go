//go:build linux || freebsd

package chrootuser

import (
	"bufio"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

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

// "+::::::" is an NIS compat entry, it has the right number of fields but no uid
var testPasswdData = `root:x:0:0::/root:/bin/sh
+::::::
notenoughfields:x:1
bob:x:1000:1000::/home/bob:/bin/sh
`

func TestParseNextPasswdSkipsBadLines(t *testing.T) {
	t.Parallel()
	// A line we cannot parse must not end the scan, the entries below it
	// still have to be found.
	rc := bufio.NewScanner(strings.NewReader(testPasswdData))
	expected := []lookupPasswdEntry{
		{"root", 0, 0, "/root"},
		{"bob", 1000, 1000, "/home/bob"},
	}
	for _, exp := range expected {
		pwd := parseNextPasswd(rc)
		assert.NotNil(t, pwd)
		assert.Equal(t, *pwd, exp)
	}
	assert.Nil(t, parseNextPasswd(rc))
}

var testGroupBadData = `root:x:0:
badgid:x:notanumber:
notenoughfields:x:1
wheel:x:10:bob
`

func TestParseNextGroupSkipsBadLines(t *testing.T) {
	t.Parallel()
	rc := bufio.NewScanner(strings.NewReader(testGroupBadData))
	expected := []lookupGroupEntry{
		{"root", 0, ""},
		{"wheel", 10, "bob"},
	}
	for _, exp := range expected {
		grp := parseNextGroup(rc)
		assert.NotNil(t, grp)
		assert.Equal(t, *grp, exp)
	}
	assert.Nil(t, parseNextGroup(rc))
}
