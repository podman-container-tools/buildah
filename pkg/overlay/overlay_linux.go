package overlay

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/opencontainers/selinux/go-selinux/label"
	"go.podman.io/storage/pkg/unshare"
	"golang.org/x/sys/unix"
)

// MountWithOptions creates ${contentDir}/merge, where ${contentDir} was
// presumably created and returned by a call to TempDir(), and either mounts a
// filesystem there and returns a mounts.Spec which bind-mounts the mountpoint
// to ${dest}, or returns a mounts.Spec which mounts a filesystem at ${dest}.
// Options allows the caller to configure a custom workdir and upperdir,
// indicate whether or not the overlay should be read-only, and provide the
// graph driver options that we'll search to determine whether or not we should
// be using a mount helper (i.e., fuse-overlayfs).
// This API is used by podman.
func MountWithOptions(contentDir, source, dest string, opts *Options) (mount specs.Mount, Err error) {
	if opts == nil {
		opts = &Options{}
	}
	mergeDir := filepath.Join(contentDir, "merge")

	// Create overlay mount options for rw/ro.
	var overlayOptions string
	if opts.ReadOnly {
		// Read-only overlay mounts require two lower layers.
		lowerTwo := filepath.Join(contentDir, "lower")
		if err := os.Mkdir(lowerTwo, 0o755); err != nil {
			return mount, err
		}
		overlayOptions = fmt.Sprintf("lowerdir=%s:%s,private", escapeColon(source), lowerTwo)
	} else {
		// Read-write overlay mounts want a lower, upper and a work layer.
		workDir := filepath.Join(contentDir, "work")
		upperDir := filepath.Join(contentDir, "upper")

		if opts.WorkDirOptionFragment != "" && opts.UpperDirOptionFragment != "" {
			workDir = opts.WorkDirOptionFragment
			if !filepath.IsAbs(workDir) {
				workDir = filepath.Join(contentDir, workDir)
			}
			upperDir = opts.UpperDirOptionFragment
			if !filepath.IsAbs(upperDir) {
				upperDir = filepath.Join(contentDir, upperDir)
			}
		} else {
			st, err := os.Stat(source)
			if err != nil {
				return mount, err
			}
			if err := os.Chmod(upperDir, st.Mode()); err != nil {
				return mount, err
			}
			if stat, ok := st.Sys().(*syscall.Stat_t); ok {
				if err := os.Chown(upperDir, int(stat.Uid), int(stat.Gid)); err != nil {
					if !errors.Is(err, syscall.EINVAL) {
						return mount, err
					}
					overflowed := false
					overflowUIDText, uerr := os.ReadFile("/proc/sys/kernel/overflowuid")
					overflowGIDText, gerr := os.ReadFile("/proc/sys/kernel/overflowgid")
					if uerr == nil && gerr == nil {
						overflowUID, uerr := strconv.Atoi(strings.TrimSpace(string(overflowUIDText)))
						overflowGID, gerr := strconv.Atoi(strings.TrimSpace(string(overflowGIDText)))
						if uerr == nil && gerr == nil && int(stat.Uid) == overflowUID && int(stat.Gid) == overflowGID {
							overflowed = true
						}
					}
					if !overflowed {
						return mount, err
					}
				}
				times := []syscall.Timespec{
					stat.Atim,
					stat.Mtim,
				}
				if err := syscall.UtimesNano(upperDir, times); err != nil {
					return mount, err
				}
			}
		}
		overlayOptions = fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s,private", escapeColon(source), upperDir, workDir)
	}
	if opts.MountLabel != "" {
		overlayOptions = overlayOptions + "," + label.FormatMountLabel("", opts.MountLabel)
	}

	mountProgram := findMountProgram(opts.GraphOpts)
	if mountProgram != "" {
		if err := mountWithMountProgram(mountProgram, overlayOptions, mergeDir); err != nil {
			return mount, err
		}

		mount.Source = mergeDir
		mount.Destination = dest
		mount.Type = "bind"
		mount.Options = []string{"bind", "slave"}
		return mount, nil
	}

	// A mount_program is not specified: fallback to try mounting native overlay.
	if unshare.IsRootless() {
		overlayOptions = fmt.Sprintf("%s,userxattr", overlayOptions)
	}
	mount.Source = mergeDir
	mount.Destination = dest
	mount.Type = "overlay"
	mount.Options = strings.Split(overlayOptions, ",")

	if opts.ForceMount {
		if err := mountNatively(overlayOptions, mergeDir); err != nil {
			if opts.ReadOnly {
				// We don't expect any kinds of error that we can respond to.
				return mount, err
			}
			if errors.Is(err, syscall.EINVAL) {
				// We couldn't do that with the kernel's built-in overlay
				// filesystem; check if it was because the upper we made is
				// already on an overlay filesystem, and if so, make a
				// last desperate effort to find fuse-overlayfs, which doesn't
				// mind us doing stuff like this.
				upperDir := filepath.Join(contentDir, "upper")
				var fs syscall.Statfs_t
				if err2 := syscall.Statfs(upperDir, &fs); err2 == nil && fs.Type == unix.OVERLAYFS_SUPER_MAGIC {
					if path, err3 := exec.LookPath("fuse-overlayfs"); err3 == nil {
						mountProgram = path
						err = mountWithMountProgram(mountProgram, overlayOptions, mergeDir)
					}
				}
			}
			if err != nil {
				return mount, err
			}
		}

		mount.Source = mergeDir
		mount.Destination = dest
		mount.Type = "bind"
		mount.Options = []string{"bind", "slave"}
		return mount, nil
	}

	return mount, nil
}
