//go:build linux

package chroot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/containers/buildah/tests/testreport/types"
	"github.com/containers/buildah/util"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/opencontainers/runtime-tools/generate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/storage/pkg/mount"
	"go.podman.io/storage/pkg/reexec"
	"golang.org/x/sys/unix"
)

const (
	reportCommand = "testreport"
)

func TestMain(m *testing.M) {
	if reexec.Init() {
		return
	}
	os.Exit(m.Run())
}

func testMinimalWithPivot(t *testing.T, noPivot bool, modify func(g *generate.Generator, rootDir, bundleDir string), verify func(t *testing.T, report *types.TestReport)) {
	t.Helper()
	g, err := generate.New("linux")
	if err != nil {
		t.Fatalf("generate.New(%q): %v", "linux", err)
	}
	if err = setupSeccomp(g.Config, ""); err != nil {
		t.Fatalf("setupSeccomp(%q): %v", "", err)
	}

	// t.TempDir returns /tmp/TestName/001.
	// /tmp/TestName/001 has permission 0777, but /tmp/TestName is 0700
	tempDir := t.TempDir()
	if err = os.Chmod(filepath.Dir(tempDir), 0o711); err != nil {
		t.Fatalf("error loosening permissions on %q: %v", tempDir, err)
	}

	rootDir := filepath.Join(tempDir, "root")
	if err := os.Mkdir(rootDir, 0o711); err != nil {
		t.Fatalf("os.Mkdir(%q): %v", rootDir, err)
	}

	rootTmpDir := filepath.Join(rootDir, "tmp")
	if err := os.Mkdir(rootTmpDir, 0o1777); err != nil {
		t.Fatalf("os.Mkdir(%q): %v", rootTmpDir, err)
	}

	specPath := filepath.Join("..", "tests", reportCommand, reportCommand)
	specBinarySource, err := os.Open(specPath)
	if err != nil {
		t.Fatalf("open(%q): %v", specPath, err)
	}
	defer specBinarySource.Close()
	specBinary, err := os.OpenFile(filepath.Join(rootDir, reportCommand), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o711)
	if err != nil {
		t.Fatalf("open(%q): %v", filepath.Join(rootDir, reportCommand), err)
	}

	if _, err := io.Copy(specBinary, specBinarySource); err != nil {
		t.Fatalf("io.Copy error: %v", err)
	}
	specBinary.Close()

	g.SetRootPath(rootDir)
	g.SetProcessArgs([]string{"/" + reportCommand})

	bundleDir := filepath.Join(tempDir, "bundle")
	if err := os.Mkdir(bundleDir, 0o700); err != nil {
		t.Fatalf("os.Mkdir(%q): %v", bundleDir, err)
	}

	if modify != nil {
		modify(&g, rootDir, bundleDir)
	}

	uid, gid, err := util.GetHostRootIDs(g.Config)
	if err != nil {
		t.Fatalf("GetHostRootIDs: %v", err)
	}
	if err := os.Chown(rootDir, int(uid), int(gid)); err != nil {
		t.Fatalf("os.Chown(%q): %v", rootDir, err)
	}

	output := new(bytes.Buffer)
	if err := RunUsingChroot(g.Config, bundleDir, "/", new(bytes.Buffer), output, output, noPivot); err != nil {
		t.Fatalf("run(noPivot=%v): %v: %s", noPivot, err, output.String())
	}

	var report types.TestReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if verify != nil {
		verify(t, &report)
	}
}

func testMinimal(t *testing.T, modify func(g *generate.Generator, rootDir, bundleDir string), verify func(t *testing.T, report *types.TestReport)) {
	for _, noPivot := range []bool{false, true} {
		t.Run(fmt.Sprintf("noPivot=%v", noPivot), func(t *testing.T) {
			testMinimalWithPivot(t, noPivot, modify, verify)
		})
	}
}

func TestNoop(t *testing.T) {
	t.Parallel()
	if unix.Getuid() != 0 {
		t.Skip("tests need to be run as root")
	}
	testMinimal(t, nil, nil)
}

func TestMinimalSkeleton(t *testing.T) {
	t.Parallel()
	if unix.Getuid() != 0 {
		t.Skip("tests need to be run as root")
	}
	testMinimal(t,
		func(_ *generate.Generator, _, _ string) {
		},
		func(_ *testing.T, _ *types.TestReport) {
		},
	)
}

func TestProcessTerminal(t *testing.T) {
	t.Parallel()
	if unix.Getuid() != 0 {
		t.Skip("tests need to be run as root")
	}
	for _, terminal := range []bool{false, true} {
		testMinimal(t,
			func(g *generate.Generator, _, _ string) {
				g.SetProcessTerminal(terminal)
			},
			func(t *testing.T, report *types.TestReport) {
				if report.Spec.Process.Terminal != terminal {
					t.Fatalf("expected terminal = %v, got %v", terminal, report.Spec.Process.Terminal)
				}
			},
		)
	}
}

func TestProcessConsoleSize(t *testing.T) {
	t.Parallel()
	if unix.Getuid() != 0 {
		t.Skip("tests need to be run as root")
	}
	for _, size := range [][2]uint{{80, 25}, {132, 50}} {
		testMinimal(t,
			func(g *generate.Generator, _, _ string) {
				g.SetProcessTerminal(true)
				g.SetProcessConsoleSize(size[0], size[1])
			},
			func(t *testing.T, report *types.TestReport) {
				if report.Spec.Process.ConsoleSize.Width != size[0] {
					t.Fatalf("expected console width = %v, got %v", size[0], report.Spec.Process.ConsoleSize.Width)
				}
				if report.Spec.Process.ConsoleSize.Height != size[1] {
					t.Fatalf("expected console height = %v, got %v", size[1], report.Spec.Process.ConsoleSize.Height)
				}
			},
		)
	}
}

func TestProcessUser(t *testing.T) {
	t.Parallel()
	if unix.Getuid() != 0 {
		t.Skip("tests need to be run as root")
	}
	for _, id := range []uint32{0, 1000} {
		testMinimal(t,
			func(g *generate.Generator, _, _ string) {
				g.SetProcessUID(id)
				g.SetProcessGID(id + 1)
				g.AddProcessAdditionalGid(id + 2)
			},
			func(t *testing.T, report *types.TestReport) {
				if report.Spec.Process.User.UID != id {
					t.Fatalf("expected UID %v, got %v", id, report.Spec.Process.User.UID)
				}
				if report.Spec.Process.User.GID != id+1 {
					t.Fatalf("expected GID %v, got %v", id+1, report.Spec.Process.User.GID)
				}
			},
		)
	}
}

func TestProcessEnv(t *testing.T) {
	t.Parallel()
	if unix.Getuid() != 0 {
		t.Skip("tests need to be run as root")
	}
	e := fmt.Sprintf("PARENT_TEST_PID=%d", unix.Getpid())
	testMinimal(t,
		func(g *generate.Generator, _, _ string) {
			g.ClearProcessEnv()
			g.AddProcessEnv("PARENT_TEST_PID", strconv.Itoa(unix.Getpid()))
		},
		func(t *testing.T, report *types.TestReport) {
			if slices.Contains(report.Spec.Process.Env, e) {
				return
			}
			t.Fatalf("expected environment variable %q", e)
		},
	)
}

func TestProcessCwd(t *testing.T) {
	t.Parallel()
	if unix.Getuid() != 0 {
		t.Skip("tests need to be run as root")
	}
	testMinimal(t,
		func(g *generate.Generator, rootDir, _ string) {
			if err := os.Mkdir(filepath.Join(rootDir, "/no-such-directory"), 0o700); err != nil {
				t.Fatalf("mkdir(%q): %v", filepath.Join(rootDir, "/no-such-directory"), err)
			}
			g.SetProcessCwd("/no-such-directory")
		},
		func(t *testing.T, report *types.TestReport) {
			if report.Spec.Process.Cwd != "/no-such-directory" {
				t.Fatalf("expected %q, got %q", "/no-such-directory", report.Spec.Process.Cwd)
			}
		},
	)
}

func TestProcessCapabilities(t *testing.T) {
	t.Parallel()
	if unix.Getuid() != 0 {
		t.Skip("tests need to be run as root")
	}
	testMinimal(t,
		func(g *generate.Generator, _, _ string) {
			g.ClearProcessCapabilities()
		},
		func(t *testing.T, report *types.TestReport) {
			if len(report.Spec.Process.Capabilities.Permitted) != 0 {
				t.Fatalf("expected no permitted capabilities, got %#v", report.Spec.Process.Capabilities.Permitted)
			}
		},
	)
	testMinimal(t,
		func(g *generate.Generator, _, _ string) {
			g.ClearProcessCapabilities()
			if err := g.AddProcessCapabilityEffective("CAP_IPC_LOCK"); err != nil {
				t.Fatalf("%v", err)
			}
			if err := g.AddProcessCapabilityPermitted("CAP_IPC_LOCK"); err != nil {
				t.Fatalf("%v", err)
			}
			if err := g.AddProcessCapabilityInheritable("CAP_IPC_LOCK"); err != nil {
				t.Fatalf("%v", err)
			}
			if err := g.AddProcessCapabilityBounding("CAP_IPC_LOCK"); err != nil {
				t.Fatalf("%v", err)
			}
			if err := g.AddProcessCapabilityAmbient("CAP_IPC_LOCK"); err != nil {
				t.Fatalf("%v", err)
			}
		},
		func(t *testing.T, report *types.TestReport) {
			if len(report.Spec.Process.Capabilities.Permitted) != 1 {
				t.Fatalf("expected one permitted capability, got %#v", report.Spec.Process.Capabilities.Permitted)
			}
			if report.Spec.Process.Capabilities.Permitted[0] != "CAP_IPC_LOCK" {
				t.Fatalf("expected one capability CAP_IPC_LOCK, got %#v", report.Spec.Process.Capabilities.Permitted)
			}
		},
	)
}

func TestProcessRlimits(t *testing.T) {
	t.Parallel()
	if unix.Getuid() != 0 {
		t.Skip("tests need to be run as root")
	}
	for _, limit := range []uint64{100 * 1024 * 1024 * 1024, 200 * 1024 * 1024 * 1024, unix.RLIM_INFINITY} {
		testMinimal(t,
			func(g *generate.Generator, _, _ string) {
				g.ClearProcessRlimits()
				if limit != unix.RLIM_INFINITY {
					g.AddProcessRlimits("rlimit_as", limit, limit)
				}
			},
			func(t *testing.T, report *types.TestReport) {
				var rlim *specs.POSIXRlimit
				for i := range report.Spec.Process.Rlimits {
					if strings.ToUpper(report.Spec.Process.Rlimits[i].Type) == "RLIMIT_AS" {
						rlim = &report.Spec.Process.Rlimits[i]
					}
				}
				if limit == unix.RLIM_INFINITY && rlim != nil && (rlim.Soft != unix.RLIM_INFINITY || rlim.Hard != unix.RLIM_INFINITY) {
					t.Fatalf("wasn't supposed to set limit on number of open files: %#v", rlim)
				}
				if limit != unix.RLIM_INFINITY && rlim == nil {
					t.Fatalf("was supposed to set limit on number of open files")
				}
				if rlim != nil {
					if rlim.Soft != limit {
						t.Fatalf("soft limit was set to %d, not %d", rlim.Soft, limit)
					}
					if rlim.Hard != limit {
						t.Fatalf("hard limit was set to %d, not %d", rlim.Hard, limit)
					}
				}
			},
		)
	}
}

func TestProcessNoNewPrivileges(t *testing.T) {
	t.Parallel()
	if unix.Getuid() != 0 {
		t.Skip("tests need to be run as root")
	}
	if !seccompAvailable {
		t.Skip("not built with seccomp support")
	}
	for _, nope := range []bool{false, true} {
		testMinimal(t,
			func(g *generate.Generator, _, _ string) {
				g.SetProcessNoNewPrivileges(nope)
			},
			func(t *testing.T, report *types.TestReport) {
				if report.Spec.Process.NoNewPrivileges != nope {
					t.Fatalf("expected no-new-privs to be %v, got %v", nope, report.Spec.Process.NoNewPrivileges)
				}
			},
		)
	}
}

func TestProcessOOMScoreAdj(t *testing.T) {
	t.Parallel()
	if unix.Getuid() != 0 {
		t.Skip("tests need to be run as root")
	}
	for _, adj := range []int{0, 1, 2, 3} {
		testMinimal(t,
			func(g *generate.Generator, _, _ string) {
				g.SetProcessOOMScoreAdj(adj)
			},
			func(t *testing.T, report *types.TestReport) {
				adjusted := 0
				if report.Spec.Process.OOMScoreAdj != nil {
					adjusted = *report.Spec.Process.OOMScoreAdj
				}
				if adjusted != adj {
					t.Fatalf("expected oom-score-adj to be %v, got %v", adj, adjusted)
				}
			},
		)
	}
}

func TestHostname(t *testing.T) {
	t.Parallel()
	if unix.Getuid() != 0 {
		t.Skip("tests need to be run as root")
	}
	hostname := fmt.Sprintf("host%d", unix.Getpid())
	testMinimal(t,
		func(g *generate.Generator, _, _ string) {
			g.SetHostname(hostname)
		},
		func(t *testing.T, report *types.TestReport) {
			if report.Spec.Hostname != hostname {
				t.Fatalf("expected %q, got %q", hostname, report.Spec.Hostname)
			}
		},
	)
}

func TestMounts(t *testing.T) {
	t.Parallel()
	if unix.Getuid() != 0 {
		t.Skip("tests need to be run as root")
	}
	t.Run("tmpfs", func(t *testing.T) {
		testMinimal(t,
			func(g *generate.Generator, _, _ string) {
				g.AddMount(specs.Mount{
					Source:      "tmpfs",
					Destination: "/was-not-there-before",
					Type:        "tmpfs",
					Options:     []string{"ro", "size=0"},
				})
			},
			func(t *testing.T, report *types.TestReport) {
				found := false
				for _, mount := range report.Spec.Mounts {
					if mount.Destination == "/was-not-there-before" && mount.Type == "tmpfs" {
						found = true
					}
				}
				if !found {
					t.Errorf("added mount for /was-not-there-before not found in %#v", report.Spec.Mounts)
				}
			},
		)
	})
	// apparently we can do anything except turn read-only into read-write
	binds := []struct {
		name         string
		tmpfsOptions string
		destination  string
		fsType       string
		options      []string
		require      []string
		reject       []string
	}{
		{
			name:        "nodev",
			destination: "/nodev",
			options:     []string{"nodev"},
			reject:      []string{"dev"},
		},
		{
			name:        "noexec",
			destination: "/noexec",
			options:     []string{"noexec"},
			reject:      []string{"exec"},
		},
		{
			name:        "nosuid",
			destination: "/nosuid",
			options:     []string{"nosuid"},
			reject:      []string{"suid"},
		},
		{
			name:        "nodev,noexec",
			destination: "/nodev,noexec",
			options:     []string{"nodev", "noexec"},
			reject:      []string{"dev", "exec"},
		},
		{
			name:        "nodev,noexec,nosuid",
			destination: "/nodev,noexec,nosuid",
			options:     []string{"nodev", "noexec", "nosuid"},
			reject:      []string{"dev", "exec", "suid"},
		},
		{
			name:        "nodev,noexec,nosuid,ro",
			destination: "/nodev,noexec,nosuid,ro",
			options:     []string{"nodev", "noexec", "nosuid", "ro"},
			reject:      []string{"dev", "exec", "suid", "rw"},
		},
		{
			name:        "nodev,noexec,nosuid,rw",
			destination: "/nodev,noexec,nosuid,rw",
			options:     []string{"nodev", "noexec", "nosuid", "rw"},
			reject:      []string{"dev", "exec", "suid", "ro"},
		},
		{
			name:         "dev,exec,suid,rw",
			tmpfsOptions: "nodev,noexec,nosuid",
			destination:  "/dev,exec,suid,rw",
			options:      []string{"dev", "exec", "suid", "rw"},
			require:      []string{"rw"},
			reject:       []string{"nodev", "noexec", "nosuid", "ro"},
		},
		{
			name:         "nodev,noexec,nosuid,ro,flip",
			tmpfsOptions: "dev,exec,suid,rw",
			destination:  "/nodev,noexec,nosuid,ro",
			options:      []string{"nodev", "noexec", "nosuid", "ro"},
			reject:       []string{"dev", "exec", "suid", "rw"},
		},
	}
	for _, bind := range binds {
		t.Run(bind.name, func(t *testing.T) {
			// mount a tmpfs over the temp dir, which may be on a nodev/noexec/nosuid filesystem
			tmpfsMount := t.TempDir()
			t.Cleanup(func() { _ = unix.Unmount(tmpfsMount, unix.MNT_FORCE|unix.MNT_DETACH) })
			tmpfsOptions := "rw,size=1m"
			if bind.tmpfsOptions != "" {
				tmpfsOptions += ("," + bind.tmpfsOptions)
			}
			tmpfsFlags, tmpfsOptions := mount.ParseOptions(tmpfsOptions)
			require.NoErrorf(t, unix.Mount("none", tmpfsMount, "tmpfs", uintptr(tmpfsFlags), tmpfsOptions), "error mounting a tmpfs with flags=%#x,options=%q at %s", tmpfsFlags, tmpfsOptions, tmpfsMount)
			testMinimal(t,
				func(g *generate.Generator, _, _ string) {
					fsType := bind.fsType
					if fsType == "" {
						fsType = "bind"
					}
					g.AddMount(specs.Mount{
						Source:      tmpfsMount,
						Destination: bind.destination,
						Type:        fsType,
						Options:     bind.options,
					})
				},
				func(t *testing.T, report *types.TestReport) {
					foundBindDestinationMount := false
					for _, mount := range report.Spec.Mounts {
						if mount.Destination == bind.destination {
							allRequired := true
							requiredFlags := bind.require
							if len(requiredFlags) == 0 {
								requiredFlags = bind.options
							}
							for _, required := range requiredFlags {
								if !slices.Contains(mount.Options, required) {
									allRequired = false
								}
							}
							anyRejected := false
							for _, rejected := range bind.reject {
								if slices.Contains(mount.Options, rejected) {
									anyRejected = true
								}
							}
							if allRequired && !anyRejected {
								foundBindDestinationMount = true
							}
						}
					}
					if !foundBindDestinationMount {
						t.Errorf("added mount for %s not found with the right flags (%v) in %+v", bind.destination, bind.options, report.Spec.Mounts)
					}
				},
			)
			// okay, just make sure we didn't change anything about the tmpfs mount point outside of the chroot
			var fs unix.Statfs_t
			require.NoErrorf(t, unix.Statfs(tmpfsMount, &fs), "fstat")
			assert.Equalf(t, tmpfsFlags&unix.MS_NODEV == unix.MS_NODEV, fs.Flags&unix.ST_NODEV == unix.ST_NODEV, "nodev flag")
			assert.Equalf(t, tmpfsFlags&unix.MS_NOEXEC == unix.MS_NOEXEC, fs.Flags&unix.ST_NOEXEC == unix.ST_NOEXEC, "noexec flag")
			assert.Equalf(t, tmpfsFlags&unix.MS_NOSUID == unix.MS_NOSUID, fs.Flags&unix.ST_NOSUID == unix.ST_NOSUID, "nosuid flag")
			assert.Equalf(t, tmpfsFlags&unix.MS_RDONLY == unix.MS_RDONLY, fs.Flags&unix.ST_RDONLY == unix.ST_RDONLY, "readonly flag")
		})
	}
}

func TestLinuxIDMapping(t *testing.T) {
	t.Parallel()
	if unix.Getuid() != 0 {
		t.Skip("tests need to be run as root")
	}
	testMinimal(t,
		func(g *generate.Generator, _, _ string) {
			g.ClearLinuxUIDMappings()
			g.ClearLinuxGIDMappings()
			g.AddLinuxUIDMapping(uint32(unix.Getuid()), 0, 1)
			g.AddLinuxGIDMapping(uint32(unix.Getgid()), 0, 1)
		},
		func(t *testing.T, report *types.TestReport) {
			if len(report.Spec.Linux.UIDMappings) != 1 {
				t.Fatalf("expected 1 uid mapping, got %d", len(report.Spec.Linux.UIDMappings))
			}
			if report.Spec.Linux.UIDMappings[0].HostID != uint32(unix.Getuid()) {
				t.Fatalf("expected host uid mapping to be %d, got %d", unix.Getuid(), report.Spec.Linux.UIDMappings[0].HostID)
			}
			if report.Spec.Linux.UIDMappings[0].ContainerID != 0 {
				t.Fatalf("expected container uid mapping to be 0, got %d", report.Spec.Linux.UIDMappings[0].ContainerID)
			}
			if report.Spec.Linux.UIDMappings[0].Size != 1 {
				t.Fatalf("expected container uid map size to be 1, got %d", report.Spec.Linux.UIDMappings[0].Size)
			}
			if report.Spec.Linux.GIDMappings[0].HostID != uint32(unix.Getgid()) {
				t.Fatalf("expected host uid mapping to be %d, got %d", unix.Getgid(), report.Spec.Linux.GIDMappings[0].HostID)
			}
			if report.Spec.Linux.GIDMappings[0].ContainerID != 0 {
				t.Fatalf("expected container gid mapping to be 0, got %d", report.Spec.Linux.GIDMappings[0].ContainerID)
			}
			if report.Spec.Linux.GIDMappings[0].Size != 1 {
				t.Fatalf("expected container gid map size to be 1, got %d", report.Spec.Linux.GIDMappings[0].Size)
			}
		},
	)
}

func TestLinuxIDMappingShift(t *testing.T) {
	t.Parallel()
	if unix.Getuid() != 0 {
		t.Skip("tests need to be run as root")
	}
	testMinimal(t,
		func(g *generate.Generator, _, _ string) {
			g.ClearLinuxUIDMappings()
			g.ClearLinuxGIDMappings()
			g.AddLinuxUIDMapping(uint32(unix.Getuid())+1, 0, 1)
			g.AddLinuxGIDMapping(uint32(unix.Getgid())+1, 0, 1)
		},
		func(t *testing.T, report *types.TestReport) {
			if len(report.Spec.Linux.UIDMappings) != 1 {
				t.Fatalf("expected 1 uid mapping, got %d", len(report.Spec.Linux.UIDMappings))
			}
			if report.Spec.Linux.UIDMappings[0].HostID != uint32(unix.Getuid()+1) {
				t.Fatalf("expected host uid mapping to be %d, got %d", unix.Getuid()+1, report.Spec.Linux.UIDMappings[0].HostID)
			}
			if report.Spec.Linux.UIDMappings[0].ContainerID != 0 {
				t.Fatalf("expected container uid mapping to be 0, got %d", report.Spec.Linux.UIDMappings[0].ContainerID)
			}
			if report.Spec.Linux.UIDMappings[0].Size != 1 {
				t.Fatalf("expected container uid map size to be 1, got %d", report.Spec.Linux.UIDMappings[0].Size)
			}
			if report.Spec.Linux.GIDMappings[0].HostID != uint32(unix.Getgid()+1) {
				t.Fatalf("expected host gid mapping to be %d, got %d", unix.Getgid()+1, report.Spec.Linux.GIDMappings[0].HostID)
			}
			if report.Spec.Linux.GIDMappings[0].ContainerID != 0 {
				t.Fatalf("expected container gid mapping to be 0, got %d", report.Spec.Linux.GIDMappings[0].ContainerID)
			}
			if report.Spec.Linux.GIDMappings[0].Size != 1 {
				t.Fatalf("expected container gid map size to be 1, got %d", report.Spec.Linux.GIDMappings[0].Size)
			}
		},
	)
}
