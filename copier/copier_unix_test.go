//go:build !windows

package copier

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	testModeMask           = int64(os.ModePerm)
	testIgnoreSymlinkDates = false
)

func TestPutChrootNoCancel(t *testing.T) {
	if uid != 0 {
		t.Skip("chroot() requires root privileges, skipping")
	}
	couldChroot := canChroot
	canChroot = true
	testPut(t.Context(), t, nil)
	canChroot = couldChroot
}

func TestPutChrootCancel(t *testing.T) {
	if uid != 0 {
		t.Skip("chroot() requires root privileges, skipping")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(1 * time.Millisecond)
	couldChroot := canChroot
	canChroot = true
	testPut(ctx, t, context.DeadlineExceeded)
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

func TestGetSingleChrootNoCancel(t *testing.T) {
	if uid != 0 {
		t.Skip("chroot() requires root privileges, skipping")
	}
	couldChroot := canChroot
	canChroot = true
	testGetSingle(t.Context(), t, nil)
	canChroot = couldChroot
}

func TestGetSingleChrootCancel(t *testing.T) {
	if uid != 0 {
		t.Skip("chroot() requires root privileges, skipping")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(1 * time.Millisecond)
	couldChroot := canChroot
	canChroot = true
	testGetSingle(ctx, t, context.DeadlineExceeded)
	canChroot = couldChroot
}

func TestGetMultipleChrootNoCancel(t *testing.T) {
	if uid != 0 {
		t.Skip("chroot() requires root privileges, skipping")
	}
	couldChroot := canChroot
	canChroot = true
	testGetMultiple(t.Context(), t, nil)
	canChroot = couldChroot
}

func TestGetMultipleChrootCancel(t *testing.T) {
	if uid != 0 {
		t.Skip("chroot() requires root privileges, skipping")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(1 * time.Millisecond)
	couldChroot := canChroot
	canChroot = true
	testGetMultiple(ctx, t, context.DeadlineExceeded)
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

func TestTarPutChrootNoCancel(t *testing.T) {
	if uid != 0 {
		t.Skip("chroot() requires root privileges, skipping")
	}
	couldChroot := canChroot
	canChroot = true
	defer func() { canChroot = couldChroot }()
	testTarPut(t.Context(), t, nil)
}

func TestTarPutChrootCancel(t *testing.T) {
	if uid != 0 {
		t.Skip("chroot() requires root privileges, skipping")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(1 * time.Millisecond)
	couldChroot := canChroot
	canChroot = true
	defer func() { canChroot = couldChroot }()
	testTarPut(ctx, t, context.DeadlineExceeded)
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
