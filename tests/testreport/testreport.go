//go:build linux

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/moby/sys/capability"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	types "go.podman.io/buildah/tests/testreport/types"
	"go.podman.io/storage/pkg/mount"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

func getVersion(r *types.TestReport) {
	r.Spec.Version = fmt.Sprintf("%d.%d.%d%s", specs.VersionMajor, specs.VersionMinor, specs.VersionPatch, specs.VersionDev)
}

func getHostname(r *types.TestReport) error {
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("reading hostname: %w", err)
	}
	r.Spec.Hostname = hostname
	return nil
}

func getProcessTerminal(r *types.TestReport) error {
	r.Spec.Process.Terminal = term.IsTerminal(unix.Stdin)
	return nil
}

func getProcessConsoleSize(r *types.TestReport) error {
	if term.IsTerminal(unix.Stdin) {
		winsize, err := unix.IoctlGetWinsize(unix.Stdin, unix.TIOCGWINSZ)
		if err != nil {
			return fmt.Errorf("reading size of terminal on stdin: %w", err)
		}
		if r.Spec.Process.ConsoleSize == nil {
			r.Spec.Process.ConsoleSize = new(specs.Box)
		}
		r.Spec.Process.ConsoleSize.Height = uint(winsize.Row)
		r.Spec.Process.ConsoleSize.Width = uint(winsize.Col)
	}
	return nil
}

func getProcessUser(r *types.TestReport) error {
	r.Spec.Process.User.UID = uint32(unix.Getuid())
	r.Spec.Process.User.GID = uint32(unix.Getgid())
	groups, err := unix.Getgroups()
	if err != nil {
		return fmt.Errorf("reading supplemental groups list: %w", err)
	}
	for _, gid := range groups {
		r.Spec.Process.User.AdditionalGids = append(r.Spec.Process.User.AdditionalGids, uint32(gid))
	}
	return nil
}

func getProcessArgs(r *types.TestReport) error {
	r.Spec.Process.Args = slices.Clone(os.Args)
	return nil
}

func getProcessEnv(r *types.TestReport) error {
	r.Spec.Process.Env = os.Environ()
	return nil
}

func getProcessCwd(r *types.TestReport) error {
	cwd := make([]byte, 8192)
	n, err := unix.Getcwd(cwd)
	if err != nil {
		return fmt.Errorf("determining current working directory: %w", err)
	}
	for n > 0 && cwd[n-1] == 0 {
		n--
	}
	r.Spec.Process.Cwd = string(cwd[:n])
	return nil
}

func getProcessCapabilities(r *types.TestReport) error {
	capabilities, err := capability.NewPid2(0)
	if err != nil {
		return fmt.Errorf("reading current capabilities: %w", err)
	}
	if err := capabilities.Load(); err != nil {
		return fmt.Errorf("loading capabilities: %w", err)
	}
	if r.Spec.Process.Capabilities == nil {
		r.Spec.Process.Capabilities = new(specs.LinuxCapabilities)
	}
	caplistMap := map[capability.CapType]*[]string{
		capability.EFFECTIVE:   &r.Spec.Process.Capabilities.Effective,
		capability.PERMITTED:   &r.Spec.Process.Capabilities.Permitted,
		capability.INHERITABLE: &r.Spec.Process.Capabilities.Inheritable,
		capability.BOUNDING:    &r.Spec.Process.Capabilities.Bounding,
		capability.AMBIENT:     &r.Spec.Process.Capabilities.Ambient,
	}
	for capType, capList := range caplistMap {
		for _, cap := range capability.ListKnown() {
			if capabilities.Get(capType, cap) {
				*capList = append(*capList, strings.ToUpper("cap_"+cap.String()))
			}
		}
	}
	return nil
}

func getProcessRLimits(r *types.TestReport) error {
	limitsMap := map[string]int{
		"RLIMIT_AS":         unix.RLIMIT_AS,
		"RLIMIT_CORE":       unix.RLIMIT_CORE,
		"RLIMIT_CPU":        unix.RLIMIT_CPU,
		"RLIMIT_DATA":       unix.RLIMIT_DATA,
		"RLIMIT_FSIZE":      unix.RLIMIT_FSIZE,
		"RLIMIT_LOCKS":      unix.RLIMIT_LOCKS,
		"RLIMIT_MEMLOCK":    unix.RLIMIT_MEMLOCK,
		"RLIMIT_MSGQUEUE":   unix.RLIMIT_MSGQUEUE,
		"RLIMIT_NICE":       unix.RLIMIT_NICE,
		"RLIMIT_NOFILE":     unix.RLIMIT_NOFILE,
		"RLIMIT_NPROC":      unix.RLIMIT_NPROC,
		"RLIMIT_RSS":        unix.RLIMIT_RSS,
		"RLIMIT_RTPRIO":     unix.RLIMIT_RTPRIO,
		"RLIMIT_RTTIME":     unix.RLIMIT_RTTIME,
		"RLIMIT_SIGPENDING": unix.RLIMIT_SIGPENDING,
		"RLIMIT_STACK":      unix.RLIMIT_STACK,
	}
	for resourceName, resource := range limitsMap {
		var rlim unix.Rlimit
		if err := unix.Getrlimit(resource, &rlim); err != nil {
			return fmt.Errorf("reading %s limit: %w", resourceName, err)
		}
		if rlim.Cur == unix.RLIM_INFINITY && rlim.Max == unix.RLIM_INFINITY {
			continue
		}
		rlimit := specs.POSIXRlimit{
			Type: resourceName,
			Soft: rlim.Cur,
			Hard: rlim.Max,
		}
		found := false
		for i := range r.Spec.Process.Rlimits {
			if r.Spec.Process.Rlimits[i].Type == resourceName {
				r.Spec.Process.Rlimits[i] = rlimit
				found = true
			}
		}
		if !found {
			r.Spec.Process.Rlimits = append(r.Spec.Process.Rlimits, rlimit)
		}
	}
	return nil
}

func getProcessNoNewPrivileges(r *types.TestReport) error {
	// We'd scan /proc/self/status here, but the "NoNewPrivs" line wasn't added until 4.10,
	// and we want to succeed on older kernels.
	r1, err := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)
	if err != nil {
		return fmt.Errorf("reading no-new-privs bit: %w", err)
	}
	r.Spec.Process.NoNewPrivileges = (r1 != 0)
	return nil
}

func getProcessAppArmorProfile(_ *types.TestReport) error {
	// TODO
	return nil
}

func getProcessOOMScoreAdjust(r *types.TestReport) error {
	node := "/proc/self/oom_score_adj"
	score, err := os.ReadFile(node)
	if err != nil {
		return fmt.Errorf("reading %q: %w", node, err)
	}
	fields := strings.Fields(string(score))
	if len(fields) != 1 {
		return fmt.Errorf("badly formatted line %q in %q: expected to find only one field", string(score), node)
	}
	oom, err := strconv.Atoi(fields[0])
	if err != nil {
		return fmt.Errorf("parsing %q in line %q in %q: %w", fields[0], string(score), node, err)
	}
	if oom != 0 {
		r.Spec.Process.OOMScoreAdj = &oom
	}
	return nil
}

func getProcessSeLinuxLabel(_ *types.TestReport) error {
	// TODO
	return nil
}

func getProcess(r *types.TestReport) error {
	if r.Spec.Process == nil {
		r.Spec.Process = new(specs.Process)
	}
	if err := getProcessTerminal(r); err != nil {
		return err
	}
	if err := getProcessConsoleSize(r); err != nil {
		return err
	}
	if err := getProcessUser(r); err != nil {
		return err
	}
	if err := getProcessArgs(r); err != nil {
		return err
	}
	if err := getProcessEnv(r); err != nil {
		return err
	}
	if err := getProcessCwd(r); err != nil {
		return err
	}
	if err := getProcessCapabilities(r); err != nil {
		return err
	}
	if err := getProcessRLimits(r); err != nil {
		return err
	}
	if err := getProcessNoNewPrivileges(r); err != nil {
		return err
	}
	if err := getProcessAppArmorProfile(r); err != nil {
		return err
	}
	if err := getProcessOOMScoreAdjust(r); err != nil {
		return err
	}
	return getProcessSeLinuxLabel(r)
}

func getMounts(r *types.TestReport) error {
	infos, err := mount.GetMounts()
	if err != nil {
		return fmt.Errorf("reading current list of mounts: %w", err)
	}
	for _, info := range infos {
		mount := specs.Mount{
			Destination: info.Mountpoint,
			Type:        info.FSType,
			Source:      info.Source,
			Options:     strings.Split(info.Options, ","),
		}
		r.Spec.Mounts = append(r.Spec.Mounts, mount)
	}
	return nil
}

func getLinuxIDMappings(r *types.TestReport) error {
	getIDMapping := func(node string) ([]specs.LinuxIDMapping, error) {
		var mappings []specs.LinuxIDMapping
		mapfile, err := os.Open(node)
		if err != nil {
			return nil, fmt.Errorf("opening %q: %w", node, err)
		}
		defer mapfile.Close()
		scanner := bufio.NewScanner(mapfile)
		for scanner.Scan() {
			line := scanner.Text()
			fields := strings.Fields(line)
			if len(fields) != 3 {
				return nil, fmt.Errorf("badly formatted line %q in %q: expected to find exactly three fields", line, node)
			}
			cid, err := strconv.ParseUint(fields[0], 10, 32)
			if err != nil {
				return nil, fmt.Errorf("parsing %q in line %q in %q: %w", fields[0], line, node, err)
			}
			hid, err := strconv.ParseUint(fields[1], 10, 32)
			if err != nil {
				return nil, fmt.Errorf("parsing %q in line %q in %q: %w", fields[1], line, node, err)
			}
			size, err := strconv.ParseUint(fields[2], 10, 32)
			if err != nil {
				return nil, fmt.Errorf("parsing %q in line %q in %q: %w", fields[2], line, node, err)
			}
			mappings = append(mappings, specs.LinuxIDMapping{ContainerID: uint32(cid), HostID: uint32(hid), Size: uint32(size)})
		}
		return mappings, nil
	}
	uidmap, err := getIDMapping("/proc/self/uid_map")
	if err != nil {
		return err
	}
	gidmap, err := getIDMapping("/proc/self/gid_map")
	if err != nil {
		return err
	}
	r.Spec.Linux.UIDMappings = uidmap
	r.Spec.Linux.GIDMappings = gidmap
	return nil
}

func getLinuxSysctl(r *types.TestReport) error {
	if r.Spec.Linux.Sysctl == nil {
		r.Spec.Linux.Sysctl = make(map[string]string)
	}
	walk := func(path string, info os.FileInfo, _ error) error {
		if info.IsDir() {
			return nil
		}
		value, err := os.ReadFile(path)
		if err != nil {
			if pe, ok := err.(*os.PathError); ok {
				if errno, ok := pe.Err.(syscall.Errno); ok {
					switch errno {
					case syscall.EACCES, syscall.EINVAL, syscall.EIO, syscall.EPERM:
						return nil
					}
				}
			}
			return fmt.Errorf("reading sysctl %q: %w", path, err)
		}
		path = strings.TrimPrefix(path, "/proc/sys/")
		sysctl := strings.ReplaceAll(path, "/", ".")
		val := strings.TrimRight(string(value), "\r\n")
		if strings.ContainsAny(val, "\r\n") {
			val = string(value)
		}
		r.Spec.Linux.Sysctl[sysctl] = val
		return nil
	}
	return filepath.Walk("/proc/sys", walk)
}

func getLinuxResources(_ *types.TestReport) error {
	// TODO
	return nil
}

func getLinuxCgroupsPath(_ *types.TestReport) error {
	// TODO
	return nil
}

func getLinuxNamespaces(_ *types.TestReport) error {
	// TODO
	return nil
}

func getLinuxDevices(_ *types.TestReport) error {
	// TODO
	return nil
}

func getLinuxRootfsPropagation(_ *types.TestReport) error {
	// TODO
	return nil
}

func getLinuxMaskedPaths(_ *types.TestReport) error {
	// TODO
	return nil
}

func getLinuxReadOnlyPaths(_ *types.TestReport) error {
	// TODO
	return nil
}

func getLinuxMountLabel(_ *types.TestReport) error {
	// TODO
	return nil
}

func getLinuxIntelRdt(_ *types.TestReport) error {
	// TODO
	return nil
}

func getLinux(r *types.TestReport) error {
	if r.Spec.Linux == nil {
		r.Spec.Linux = new(specs.Linux)
	}
	if err := getLinuxIDMappings(r); err != nil {
		return err
	}
	if err := getLinuxSysctl(r); err != nil {
		return err
	}
	if err := getLinuxResources(r); err != nil {
		return err
	}
	if err := getLinuxCgroupsPath(r); err != nil {
		return err
	}
	if err := getLinuxNamespaces(r); err != nil {
		return err
	}
	if err := getLinuxDevices(r); err != nil {
		return err
	}
	if err := getLinuxRootfsPropagation(r); err != nil {
		return err
	}
	if err := getLinuxMaskedPaths(r); err != nil {
		return err
	}
	if err := getLinuxReadOnlyPaths(r); err != nil {
		return err
	}
	if err := getLinuxMountLabel(r); err != nil {
		return err
	}
	return getLinuxIntelRdt(r)
}

func main() {
	var r types.TestReport

	if r.Spec == nil {
		r.Spec = new(specs.Spec)
	}
	getVersion(&r)
	if err := getProcess(&r); err != nil {
		logrus.Errorf("%v", err)
		os.Exit(1)
	}
	if err := getHostname(&r); err != nil {
		logrus.Errorf("%v", err)
		os.Exit(1)
	}
	if err := getMounts(&r); err != nil {
		logrus.Errorf("%v", err)
		os.Exit(1)
	}
	if err := getLinux(&r); err != nil {
		logrus.Errorf("%v", err)
		os.Exit(1)
	}

	if err := json.NewEncoder(os.Stdout).Encode(r); err != nil {
		logrus.Errorf("%v", err)
		os.Exit(1)
	}
}
