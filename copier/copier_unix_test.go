//go:build !windows

package copier

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	testModeMask           = int64(os.ModePerm)
	testIgnoreSymlinkDates = false
)

func TestPutChroot(t *testing.T) {
	if uid != 0 {
		t.Skip("chroot() requires root privileges, skipping")
	}
	couldChroot := canChroot
	canChroot = true
	testPut(t)
	canChroot = couldChroot
}

func TestStatChroot(t *testing.T) {
	if uid != 0 {
		t.Skip("chroot() requires root privileges, skipping")
	}
	couldChroot := canChroot
	canChroot = true
	testStat(t)
	canChroot = couldChroot
}

func TestGetSingleChroot(t *testing.T) {
	if uid != 0 {
		t.Skip("chroot() requires root privileges, skipping")
	}
	couldChroot := canChroot
	canChroot = true
	testGetSingle(t)
	canChroot = couldChroot
}

func TestGetMultipleChroot(t *testing.T) {
	if uid != 0 {
		t.Skip("chroot() requires root privileges, skipping")
	}
	couldChroot := canChroot
	canChroot = true
	testGetMultiple(t)
	canChroot = couldChroot
}

func TestEvalChroot(t *testing.T) {
	if uid != 0 {
		t.Skip("chroot() requires root privileges, skipping")
	}
	couldChroot := canChroot
	canChroot = true
	testEval(t)
	canChroot = couldChroot
}

func TestMkdirChroot(t *testing.T) {
	if uid != 0 {
		t.Skip("chroot() requires root privileges, skipping")
	}
	couldChroot := canChroot
	canChroot = true
	testMkdir(t)
	canChroot = couldChroot
}

func TestRemoveChroot(t *testing.T) {
	if uid != 0 {
		t.Skip("chroot() requires root privileges, skipping")
	}
	couldChroot := canChroot
	canChroot = true
	testRemove(t)
	canChroot = couldChroot
}

func TestEnsureChroot(t *testing.T) {
	if uid != 0 {
		t.Skip("chroot() requires root privileges, skipping")
	}
	couldChroot := canChroot
	canChroot = true
	testEnsure(t)
	canChroot = couldChroot
}

func TestStatDisallowWildcardChroot(t *testing.T) {
	if uid != 0 {
		t.Skip("chroot() requires root privileges, skipping")
	}
	couldChroot := canChroot
	canChroot = true
	testStatDisallowWildcard(t)
	canChroot = couldChroot
}

func TestStatAllowEmptyWildcardChroot(t *testing.T) {
	if uid != 0 {
		t.Skip("chroot() requires root privileges, skipping")
	}
	couldChroot := canChroot
	canChroot = true
	testStatAllowEmptyWildcard(t)
	canChroot = couldChroot
}

func TestGetDisallowWildcardChroot(t *testing.T) {
	if uid != 0 {
		t.Skip("chroot() requires root privileges, skipping")
	}
	couldChroot := canChroot
	canChroot = true
	testGetDisallowWildcard(t)
	canChroot = couldChroot
}

func TestGetAllowEmptyWildcardChroot(t *testing.T) {
	if uid != 0 {
		t.Skip("chroot() requires root privileges, skipping")
	}
	couldChroot := canChroot
	canChroot = true
	testGetAllowEmptyWildcard(t)
	canChroot = couldChroot
}

func TestConditionalRemoveChroot(t *testing.T) {
	if uid != 0 {
		t.Skip("chroot() requires root privileges, skipping")
	}
	couldChroot := canChroot
	canChroot = true
	testConditionalRemove(t)
	canChroot = couldChroot
}

func TestMkfileChroot(t *testing.T) {
	if uid != 0 {
		t.Skip("chroot() requires root privileges, skipping")
	}
	couldChroot := canChroot
	canChroot = true
	testMkfile(t)
	canChroot = couldChroot
}

func checkStatInfoOwnership(t *testing.T, result *StatForItem) {
	t.Helper()
	require.EqualValues(t, 0, result.UID, "expected the owning user to be reported")
	require.EqualValues(t, 0, result.GID, "expected the owning group to be reported")
}

func TestPutTimestampChroot(t *testing.T) {
	if uid != 0 {
		t.Skip("chroot() requires root privileges, skipping")
	}
	couldChroot := canChroot
	canChroot = true
	defer func() { canChroot = couldChroot }()
	testPutTimestamp(t)
}

func TestTarPutChroot(t *testing.T) {
	if uid != 0 {
		t.Skip("chroot() requires root privileges, skipping")
	}
	couldChroot := canChroot
	canChroot = true
	defer func() { canChroot = couldChroot }()
	testTarPut(t)
}

func TestPutCreateDestPathChroot(t *testing.T) {
	if uid != 0 {
		t.Skip("chroot() requires root privileges, skipping")
	}
	couldChroot := canChroot
	canChroot = true
	defer func() { canChroot = couldChroot }()
	testPutCreateDestPath(t)
}

func TestSymlinkChroot(t *testing.T) {
	if uid != 0 {
		t.Skip("chroot() requires root privileges, skipping")
	}
	couldChroot := canChroot
	canChroot = true
	testSymlink(t)
	canChroot = couldChroot
}

func TestAttemptUnpackDockerCompatibilityChroot(t *testing.T) {
	if uid != 0 {
		t.Skip("chroot() requires root privileges, skipping")
	}
	couldChroot := canChroot
	canChroot = true
	testAttemptUnpackDockerCompatibility(t)
	canChroot = couldChroot
}
