//go:build linux

package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pkg/sftp"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"go.podman.io/buildah"
	buildahcli "go.podman.io/buildah/pkg/cli"
	"go.podman.io/buildah/pkg/parse"
	"go.podman.io/buildah/util"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/reexec"
	"golang.org/x/sys/unix"
)

const (
	serveSftpInSubprocess      = "buildah-serve-sftp-chroot"
	serveSftp                  = "serve-sftp"
	servingSftpFragment        = "serving-sftp"
	serveSftpCheckInterval     = 100 * time.Millisecond
	serveSftpCheckIterations   = 100
	findDataDir                = "find-data-dir"
	mountSshfsMountFragment    = "buildah-sshfs-mount"
	mountSshfs                 = "mount-sshfs"
	umountSshfs                = "umount-sshfs"
	listSshfsMountDirs         = "list-sshfs-mount-dirs"
	mountUmountCheckInterval   = 100 * time.Millisecond
	mountUmountCheckIterations = 300
)

var (
	serveSftpDescription = "\n  Serves a container or image's rootfs using SFTP over stdio."
	serveSftpCommand     = &cobra.Command{
		Use:     serveSftp,
		Short:   "Serve a container or image's rootfs using SFTP over stdio",
		Long:    serveSftpDescription,
		RunE:    serveSftpCmd,
		Example: `buildah serve-sftp containerID|imageID [ro]`,
		GroupID: groupSystem,
		Args:    cobra.RangeArgs(1, 2),
		Hidden:  true,
	}
	serveSftpReadOnly bool

	findDataDirDescription = "\n  Find the data directory for a container or image."
	findDataDirCommand     = &cobra.Command{
		Use:     findDataDir,
		Short:   "Find the container's or image's data directory",
		Long:    findDataDirDescription,
		RunE:    findDataDirCmd,
		Example: `buildah find-data-dir containerID|imageID`,
		GroupID: groupSystem,
		Args:    cobra.ExactArgs(1),
		Hidden:  true,
	}

	mountSshfsDescription = "\n  Mounts a container or image's rootfs using sshfs."
	mountSshfsCommand     = &cobra.Command{
		Use:     mountSshfs,
		Short:   "Mount a container or image's rootfs using sshfs",
		Long:    mountSshfsDescription,
		RunE:    mountSshfsCmd,
		Example: `buildah mount-sshfs [-o options,...] containerID|imageID [...]`,
		GroupID: groupSystem,
		Args:    cobra.ArbitraryArgs,
		Hidden:  true,
	}
	mountSshfsOptions string

	umountSshfsDescription = "\n  Unmounts a container or image mounted using sshfs."
	umountSshfsCommand     = &cobra.Command{
		Use:     umountSshfs,
		Aliases: []string{"unmount-sshfs"},
		Short:   "Unmount a container or image mounted using sshfs",
		Long:    umountSshfsDescription,
		RunE:    umountSshfsCmd,
		Example: `buildah unmount-sshfs /path/to/mounted/root-filesystem`,
		GroupID: groupSystem,
		Args:    cobra.ArbitraryArgs,
		Hidden:  true,
	}
	umountSshfsAll bool

	listSshfsMountDirsDescription = "\n  List containers and images mounted with sshfs."
	listSshfsMountDirsCommand     = &cobra.Command{
		Use:     listSshfsMountDirs,
		Short:   "List containers and images mounted with sshfs",
		Long:    listSshfsMountDirsDescription,
		RunE:    listSshfsMountDirsCmd,
		Example: `buildah list-sshfs-mount-dirs`,
		GroupID: groupSystem,
		Args:    cobra.NoArgs,
		Hidden:  true,
	}
)

func init() {
	reexec.Register(serveSftpInSubprocess, serveSftpSubprocessMain)
}

func sftpInit() {
	serveSftpCommand.SetUsageTemplate(UsageTemplate())
	flags := serveSftpCommand.Flags()
	flags.SetInterspersed(false)
	flags.BoolVar(&serveSftpReadOnly, "read-only", serveSftpReadOnly, "serve SFTP in read-only mode")
	rootCmd.AddCommand(serveSftpCommand)

	findDataDirCommand.SetUsageTemplate(UsageTemplate())
	flags = findDataDirCommand.Flags()
	flags.SetInterspersed(false)
	rootCmd.AddCommand(findDataDirCommand)

	mountSshfsCommand.SetUsageTemplate(UsageTemplate())
	flags = mountSshfsCommand.Flags()
	flags.SetInterspersed(false)
	flags.StringVarP(&mountSshfsOptions, "options", "o", mountSshfsOptions, "comma-separated list of mount options")
	rootCmd.AddCommand(mountSshfsCommand)

	umountSshfsCommand.SetUsageTemplate(UsageTemplate())
	flags = umountSshfsCommand.Flags()
	flags.SetInterspersed(false)
	flags.BoolVarP(&umountSshfsAll, "all", "a", false, "unmount all containers and images mounted with sshfs")
	rootCmd.AddCommand(umountSshfsCommand)

	listSshfsMountDirsCommand.SetUsageTemplate(UsageTemplate())
	flags = listSshfsMountDirsCommand.Flags()
	flags.SetInterspersed(false)
	rootCmd.AddCommand(listSshfsMountDirsCommand)
}

type pipeReadWriteCloser struct {
	io.ReadCloser
	io.WriteCloser
}

func (p pipeReadWriteCloser) Close() error {
	return errors.Join(p.ReadCloser.Close(), p.WriteCloser.Close())
}

// serveSftpCmd runs an SFTP service on stdio pointed at the container or
// image's rootfs.  Because it mounts a filesystem, we need to be running as
// root or in the user's user namespace, hopefully with the right ID mappings
// for the container.
func serveSftpCmd(c *cobra.Command, args []string) error {
	systemContext, err := parse.SystemContextFromOptions(c)
	if err != nil {
		return fmt.Errorf("initializing system context: %w", err)
	}
	store, err := getStore(c)
	if err != nil {
		return fmt.Errorf("opening storage: %w", err)
	}
	fd := -1
	defer func() {
		if fd != -1 {
			if err := unix.Close(fd); err != nil {
				logrus.Errorf("closing lock file: %v", err)
			}
		}
		if _, err := store.Shutdown(false); err != nil {
			logrus.Debugf("shutting down storage: %v", err)
		}
	}()

	toMount := args[0]
	var mountPoint string
	readOnly := serveSftpReadOnly
	builder, err := buildah.OpenBuilder(store, toMount)
	logrus.Debugf("opening builder %q: %v", toMount, err)
	if err == nil {
		cdir, err := store.ContainerDirectory(builder.ContainerID)
		if err != nil {
			return err
		}
		if fd, err = lockServingSFTP(cdir); err != nil {
			return fmt.Errorf("gaining exclusive access to mount build container %q: %w", toMount, err)
		}
		// mount a builder
		mp, err := builder.Mount("")
		if err != nil {
			return fmt.Errorf("mounting builder %q: %w", toMount, err)
		}
		defer func() {
			if err := builder.Unmount(); err != nil {
				logrus.Debugf("unmounting builder %q: %v", toMount, err)
			}
		}()
		mountPoint = mp
	} else if errors.Is(err, os.ErrNotExist) || errors.Is(err, storage.ErrLayerUnknown) {
		container, err := store.Container(toMount)
		if err == nil {
			cdir, err := store.ContainerDirectory(container.ID)
			if err != nil {
				return err
			}
			if fd, err = lockServingSFTP(cdir); err != nil {
				return fmt.Errorf("gaining exclusive access to mount container %q: %w", toMount, err)
			}
			// mount an arbitrary container
			mp, err := store.Mount(container.ID, "")
			if err != nil {
				return err
			}
			defer func() {
				if _, err := store.Unmount(container.ID, false); err != nil {
					logrus.Debugf("unmounting container %q: %v", toMount, err)
				}
			}()
			mountPoint = mp
		}
	} else if errors.Is(err, os.ErrNotExist) || errors.Is(err, storage.ErrContainerUnknown) {
		// mount an arbitrary image
		readOnly = true
		imageSpec := toMount
		_, img, err := util.FindImage(store, "", systemContext, imageSpec)
		if err != nil {
			return fmt.Errorf("mounting image %q: %w", toMount, err)
		}
		idir, err := store.ImageDirectory(img.ID)
		if err != nil {
			return err
		}
		if fd, err = lockServingSFTP(idir); err != nil {
			return fmt.Errorf("gaining exclusive access to mount image %q: %w", imageSpec, err)
		}
		mp, err := store.MountImage(img.ID, []string{"ro"}, "")
		if err != nil {
			return fmt.Errorf("mounting image %q: %w", toMount, err)
		}
		defer func() {
			if _, err := store.UnmountImage(img.ID, false); err != nil {
				logrus.Debugf("unmounting image %q: %v", toMount, err)
			}
		}()
		mountPoint = mp
	} else {
		// was not a container, and failed to mount an image
		return fmt.Errorf("mounting builder container, non-builder container, or image %q: %w", toMount, err)
	}

	logrus.Debugf("mounted %q, serving sftp", mountPoint)
	sftpServerArgs := []string{serveSftpInSubprocess, mountPoint}
	if readOnly {
		sftpServerArgs = append(sftpServerArgs, "ro")
	}
	subprocess := reexec.CommandContext(getContext(), sftpServerArgs...)
	subprocess.Stdin = os.Stdin
	subprocess.Stdout = os.Stdout
	subprocess.Stderr = os.Stderr
	subprocess.Dir = "/"
	err = subprocess.Start()
	if err != nil {
		return fmt.Errorf("starting sftp server: %w", err)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		for received := range signals {
			if err := subprocess.Process.Signal(received); err != nil && !errors.Is(err, os.ErrProcessDone) {
				logrus.Errorf("sending signal to sftp subprocess: %v", err)
			}
		}
	}()
	return subprocess.Wait()
}

// serveSftpSubprocessMain chroots into a mountpoint (os.Args[1]) and runs an
// SFTP service on stdio.  Subsequent arguments are nominally SFTP server
// options, but any value than "ro" is currently discarded.
func serveSftpSubprocessMain() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "expected directory name and optional flags")
		os.Exit(1)
	}
	mountPoint := os.Args[1]

	var flags []string
	if len(os.Args) > 2 {
		flags = os.Args[2:]
	}
	var sftpServerOptions []sftp.ServerOption
	for _, flag := range flags {
		switch flag {
		case "ro":
			sftpServerOptions = append(sftpServerOptions, sftp.ReadOnly())
		default:
			fmt.Fprintln(os.Stderr, "unrecognized flag", flag)
			os.Exit(1)
		}
	}

	sftpPipe := pipeReadWriteCloser{
		ReadCloser:  os.Stdin,
		WriteCloser: os.Stdout,
	}
	sftpServer, err := sftp.NewServer(&sftpPipe, sftpServerOptions...)
	if err != nil {
		fmt.Fprintln(os.Stderr, "initializing sftp server:", err)
		os.Exit(1)
	}

	if err := unix.Chdir(mountPoint); err != nil {
		fmt.Fprintln(os.Stderr, &os.PathError{Op: "chdir", Path: mountPoint, Err: err})
		os.Exit(1)
	}
	if err := unix.Chroot(mountPoint); err != nil {
		fmt.Fprintln(os.Stderr, &os.PathError{Op: "chroot", Path: mountPoint, Err: err})
		os.Exit(1)
	}
	if err := unix.Chdir("/"); err != nil {
		fmt.Fprintln(os.Stderr, &os.PathError{Op: "chdir", Path: "/", Err: err})
		os.Exit(1)
	}

	signals := make(chan os.Signal, 10)
	signal.Notify(signals, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)
	var whichSig os.Signal
	go func() {
		whichSig = <-signals
		switch whichSig {
		case syscall.SIGHUP, syscall.SIGTERM:
			os.Exit(0)
		default:
			fmt.Fprintln(os.Stderr, "sftp server:", whichSig)
			os.Exit(1)
		}
	}()
	err = sftpServer.Serve()
	if err != nil && whichSig != syscall.SIGHUP && whichSig != syscall.SIGTERM {
		fmt.Fprintln(os.Stderr, "sftp server:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// flagServingSFTP returns an error if it looks like an image or container is
// already mounted for use by an sftp server.  That would create an overlay
// mount in its own mount namespace, and operations that would also try to
// mount the filesystem will either fail outright because the kernel won't
// allow its upper directory to be mounted more than once at a time, or will
// fail in maddening ways because the kernel allows it but overlay doesn't
// guarantee that anything will work right if you use the same upper directory
// for more than one mount at the same time.
func flagServingSFTP(datadir string) error {
	lockfile := filepath.Join(datadir, servingSftpFragment)
	fd, err := unix.Open(lockfile, unix.O_RDWR, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return &os.PathError{Op: "open", Path: lockfile, Err: err}
	}
	defer func() {
		if err := unix.Close(fd); err != nil {
			logrus.Debugf("closing %q: %v", lockfile, err)
		}
	}()
	for i := range serveSftpCheckIterations {
		if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err == nil {
			break
		}
		if i < serveSftpCheckIterations-1 {
			time.Sleep(serveSftpCheckInterval)
		}
	}
	if err != nil {
		return &os.PathError{Op: "flock", Path: lockfile, Err: err}
	}
	return nil
}

// lockServingSFTP takes a lock on a file to signal to other processes that the
// filesystem is mounted for use by an sftp server.  Returns the file descriptor on
// which the lock is placed (which must eventually be closed) or -1 and an error.
func lockServingSFTP(datadir string) (int, error) {
	lockfile := filepath.Join(datadir, servingSftpFragment)
	fd, err := unix.Open(lockfile, unix.O_RDWR|unix.O_TRUNC|unix.O_CREAT|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return -1, &os.PathError{Op: "open", Path: lockfile, Err: err}
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		err2 := unix.Close(fd)
		if err2 != nil {
			err2 = fmt.Errorf("closing %q after failing to obtain a lock: %w", lockfile, err2)
		}
		return -1, errors.Join(&os.PathError{Op: "flock", Path: lockfile, Err: err}, err2)
	}
	return fd, nil
}

// findDataDirCmd locates the builder, container, or image named on its command
// line and prints the location of its data directory.  We run this from a
// process which is not in the user's namespace, so that it can set up the
// user's namespace if it has to, so that it can examine the storage contents.
func findDataDirCmd(c *cobra.Command, args []string) error {
	systemContext, err := parse.SystemContextFromOptions(c)
	if err != nil {
		return fmt.Errorf("initializing system context: %w", err)
	}
	store, err := getStore(c)
	if err != nil {
		return fmt.Errorf("opening storage: %w", err)
	}
	defer func() {
		if _, err := store.Shutdown(false); err != nil {
			logrus.Debugf("shutting down storage: %v", err)
		}
	}()

	var dataDir string
	toFind := args[0]
	builder, err := buildah.OpenBuilder(store, toFind)
	if err == nil {
		// it's a builder
		logrus.Debugf("%q is a builder", toFind)
		dd, err := store.ContainerDirectory(builder.ContainerID)
		if err != nil {
			return fmt.Errorf("finding userdata directory for builder %q: %w", toFind, err)
		}
		dataDir = dd
	} else if errors.Is(err, os.ErrNotExist) || errors.Is(err, storage.ErrContainerUnknown) {
		// it's a random container
		container, err := store.Container(toFind)
		if err == nil {
			logrus.Debugf("%q is a non-builder container", toFind)
			dd, err := store.ContainerDirectory(container.ID)
			if err != nil {
				return fmt.Errorf("finding userdata directory for builder or container %q: %w", toFind, err)
			}
			dataDir = dd
		} else if errors.Is(err, os.ErrNotExist) || errors.Is(err, storage.ErrContainerUnknown) {
			// it's an image
			_, img, err := util.FindImage(store, "", systemContext, toFind)
			if err != nil {
				return fmt.Errorf("locating container or image %q: %w", toFind, err)
			}
			logrus.Debugf("%q is an image", toFind)
			dd, err := store.ImageDirectory(img.ID)
			if err != nil {
				return fmt.Errorf("finding userdata directory for image %q: %w", toFind, err)
			}
			dataDir = dd
		} else {
			// was not a container, and failed to find an image
			return fmt.Errorf("locating container %q: %w", toFind, err)
		}
	} else {
		// failed to find a builder
		return fmt.Errorf("locating builder %q: %w", toFind, err)
	}

	fmt.Fprintln(os.Stdout, dataDir)
	return nil
}

// listSshfsMountDirsCmd prints pairs of lines: the ID of a container or image,
// and the name of where its rootfs would be mounted, if it were.
func listSshfsMountDirsCmd(c *cobra.Command, _ []string) error {
	store, err := getStore(c)
	if err != nil {
		return fmt.Errorf("opening storage: %w", err)
	}
	defer func() {
		if _, err := store.Shutdown(false); err != nil {
			logrus.Debugf("shutting down storage: %v", err)
		}
	}()
	whatAndWhere := make(map[string]string)
	containers, err := store.Containers()
	if err != nil {
		return err
	}
	images, err := store.Images()
	if err != nil {
		return err
	}
	for _, container := range containers {
		cdir, err := store.ContainerDirectory(container.ID)
		if err != nil {
			return err
		}
		whatAndWhere[container.ID] = filepath.Join(cdir, mountSshfsMountFragment)
		logrus.Debugf("container %q would be mounted at %q", container.ID, whatAndWhere[container.ID])
	}
	for _, image := range images {
		idir, err := store.ImageDirectory(image.ID)
		if err != nil {
			return err
		}
		whatAndWhere[image.ID] = filepath.Join(idir, mountSshfsMountFragment)
		logrus.Debugf("image %q would be mounted at %q", image.ID, whatAndWhere[image.ID])
	}
	for k, v := range whatAndWhere {
		fmt.Println(k)
		fmt.Println(v)
	}
	return nil
}

// listSshfsMounts returns a list of currently-used mountpoints, and a map from
// those to the IDs of the containers or images which we expect they hold.
func listSshfsMounts(inheritedFlags []string, selfExecutable string) ([]string, map[string]string, error) {
	var listSshfsMountDirsStdout bytes.Buffer
	listSshfsMountDirsArgs := append(slices.Clone(inheritedFlags), listSshfsMountDirs)
	listSshfsMountDirs := exec.CommandContext(getContext(), selfExecutable, listSshfsMountDirsArgs...)
	listSshfsMountDirs.Stdout = &listSshfsMountDirsStdout
	listSshfsMountDirs.Stderr = os.Stderr
	logrus.Debugf("running %v to get a list of possible sshfs mountpoints", listSshfsMountDirsArgs)
	if err := listSshfsMountDirs.Run(); err != nil {
		return nil, nil, fmt.Errorf("listing candidate locations where containers or images would be mounted: %w", err)
	}
	namesOfMountPoints := make(map[string]string)
	var mountPoints []string
	scanner := bufio.NewScanner(&listSshfsMountDirsStdout)
	for scanner.Scan() {
		whatName := scanner.Text()
		if whatName == "" {
			break
		}
		if !scanner.Scan() {
			break
		}
		mountPoint := scanner.Text()
		if mountPoint == "" {
			break
		}
		var parentStat, mountPointStat unix.Stat_t
		if err := unix.Stat(filepath.Dir(mountPoint), &parentStat); err != nil {
			return nil, nil, &os.PathError{Op: "stat", Path: filepath.Dir(mountPoint), Err: err}
		}
		if err := unix.Stat(mountPoint, &mountPointStat); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue // doesn't look like it could be mounted
			}
			return nil, nil, &os.PathError{Op: "stat", Path: mountPoint, Err: err}
		}
		if parentStat.Dev != mountPointStat.Dev {
			logrus.Debugf("%q is a mountpoint", mountPoint)
			mountPoints = append(mountPoints, mountPoint)
			namesOfMountPoints[mountPoint] = whatName
		} else {
			logrus.Debugf("%q is not a mountpoint", mountPoint)
		}
	}
	return mountPoints, namesOfMountPoints, scanner.Err()
}

// mountSshfsCmd mounts a container or image on a container- or image-specific
// location using sshfs.
func mountSshfsCmd(c *cobra.Command, args []string) error {
	inheritedFlags := getInheritedFlags(c)

	selfExecutable := reexec.Self()

	if len(args) == 0 {
		// Just list what's mounted right now.
		mountPoints, namesOfMountPoints, err := listSshfsMounts(inheritedFlags, selfExecutable)
		if err != nil {
			return err
		}
		for _, mountPoint := range mountPoints {
			fmt.Println(namesOfMountPoints[mountPoint], mountPoint)
		}
		return nil
	}

	var absSshfs string
	var err error
	if absSshfs, err = exec.LookPath("sshfs"); err != nil {
		return fmt.Errorf(`looking for "sshfs" in $PATH: %w`, err)
	}

	// Work out where the mount points for the various items that we want
	// to mount would be.
	whatGoesWhere := make(map[string]string)
	for _, toMount := range args {
		var mountParentDirBuffer bytes.Buffer
		findDataDirArgs := append(slices.Clone(inheritedFlags), findDataDir, toMount)
		findDataDir := exec.CommandContext(getContext(), selfExecutable, findDataDirArgs...)
		findDataDir.Stdin = nil
		findDataDir.Stdout = &mountParentDirBuffer
		findDataDir.Stderr = os.Stderr
		findDataDir.Dir = "/"
		if err := findDataDir.Run(); err != nil {
			return fmt.Errorf("determining where %q would be mounted: %w", toMount, err)
		}

		mountParentDir := strings.TrimSpace(mountParentDirBuffer.String())
		if mountParentDir == "" {
			return fmt.Errorf("determining parent of mountpoint %q: %q", toMount, mountParentDir)
		}

		if err := flagServingSFTP(mountParentDir); err != nil {
			return err
		}

		mountPoint := filepath.Join(mountParentDir, mountSshfsMountFragment)
		var parentStat, mountPointStat unix.Stat_t
		if err := unix.Mkdir(mountPoint, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("mkdir(%q): %w", mountPoint, err)
		}
		if err := unix.Stat(mountPoint, &mountPointStat); err != nil {
			return &os.PathError{Op: "stat", Path: mountPoint, Err: err}
		}
		if err := unix.Stat(mountParentDir, &parentStat); err != nil {
			return &os.PathError{Op: "stat", Path: mountParentDir, Err: err}
		}
		if mountPointStat.Dev != parentStat.Dev {
			return fmt.Errorf("filesystem (possibly for %q) already mounted on %s", toMount, mountPoint)
		}

		whatGoesWhere[toMount] = mountPoint
	}

	signals := make(chan os.Signal, 10)
	signal.Notify(signals, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)

	// Launch each pair.
	serveSftpStderr := make([]bytes.Buffer, len(args))
	mountSshfsStderr := make([]bytes.Buffer, len(args))
	var wg sync.WaitGroup
	var whatWentWrong sync.Map
	for i, toMount := range args {
		wg.Go(func() {
			succeeded := false

			r1, w1, err := os.Pipe()
			if err != nil {
				whatWentWrong.Store(toMount, fmt.Errorf("pipe: %w", err))
				return
			}
			defer func() {
				if !succeeded {
					r1.Close()
					w1.Close()
				}
			}()

			r2, w2, err := os.Pipe()
			if err != nil {
				whatWentWrong.Store(toMount, fmt.Errorf("pipe: %w", err))
				return
			}
			defer func() {
				if !succeeded {
					r2.Close()
					w2.Close()
				}
			}()

			serveSftpArgs := append(slices.Clone(inheritedFlags), serveSftp, toMount)
			serveSftp := exec.Command(selfExecutable, serveSftpArgs...)
			serveSftp.Stdin = r1
			serveSftp.Stdout = w2
			serveSftp.Stderr = &serveSftpStderr[i]
			serveSftp.Dir = "/"
			if serveSftp.SysProcAttr == nil {
				serveSftp.SysProcAttr = &syscall.SysProcAttr{}
			}
			if err := serveSftp.Start(); err != nil {
				whatWentWrong.Store(toMount, fmt.Errorf("starting sftp server: %w", err))
				return
			}
			r1.Close()
			w2.Close()
			defer func() {
				if !succeeded {
					if err := serveSftp.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
						logrus.Errorf("terminating sftp server for %q after startup error: %v", toMount, err)
					}
				}
			}()

			if err := os.Mkdir(whatGoesWhere[toMount], 0o700); err != nil && !errors.Is(err, os.ErrExist) {
				whatWentWrong.Store(toMount, fmt.Errorf("creating mount point at %q: %v", whatGoesWhere[toMount], err))
				return
			}

			options := append(strings.Split(mountSshfsOptions, ","), "passive")
			options = slices.DeleteFunc(options, func(s string) bool { return s == "" })
			mountSshfs := exec.Command(absSshfs, "-f", "-o", strings.Join(options, ","), toMount+":/", whatGoesWhere[toMount])
			mountSshfs.Stdin = r2
			mountSshfs.Stdout = w1
			mountSshfs.Stderr = &mountSshfsStderr[i]
			mountSshfs.Dir = "/"
			if mountSshfs.SysProcAttr == nil {
				mountSshfs.SysProcAttr = &syscall.SysProcAttr{}
			}
			if err := mountSshfs.Start(); err != nil {
				whatWentWrong.Store(toMount, fmt.Errorf("mounting using sshfs: %w", err))
				return
			}
			r2.Close()
			w1.Close()

			errs := make([]error, 2)
			succeeded = true
			var wg sync.WaitGroup
			go func() {
				whichSig, ok := <-signals
				if !ok {
					whichSig = syscall.SIGTERM
				}
				if err := mountSshfs.Process.Signal(whichSig); err != nil && !errors.Is(err, os.ErrProcessDone) {
					logrus.Errorf("sending signal to sshfs: %v", err)
				}
				if err := serveSftp.Process.Signal(whichSig); err != nil && !errors.Is(err, os.ErrProcessDone) {
					logrus.Errorf("sending signal to sftp server: %v", err)
				}
			}()

			sftpDone := make(chan struct{}, 1)
			sshfsDone := make(chan struct{}, 1)
			wg.Go(func() { errs[0] = serveSftp.Wait(); signal.Stop(signals); sftpDone <- struct{}{} })
			wg.Go(func() { errs[1] = mountSshfs.Wait(); signal.Stop(signals); sshfsDone <- struct{}{} })
			wg.Go(func() {
				<-sftpDone
				if err := mountSshfs.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
					logrus.Errorf("terminating sshfs after sftp server exited: %v", err)
				}
			})
			wg.Go(func() {
				<-sshfsDone
				if err := serveSftp.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
					logrus.Errorf("terminating sftp server after sshfs exited: %v", err)
				}
			})

			go func() {
				wg.Wait()
				if errs[0] != nil {
					errs[0] = fmt.Errorf("sftp server: %w", errs[0])
				}
				if errs[1] != nil {
					errs[1] = fmt.Errorf("sshfs: %w", errs[1])
				}
				if err := errors.Join(errs[:]...); err != nil {
					whatWentWrong.Store(toMount, err)
				}
			}()
		})
		fmt.Println(whatGoesWhere[toMount])
	}

	// Wait for the mountpoints to have filesystems on them, so that when
	// we return, the caller can access their contents immediately.
	for toMount, where := range whatGoesWhere {
		var parentStat, mountPointStat unix.Stat_t
		mountParentDir := filepath.Dir(where)
		if err := unix.Stat(mountParentDir, &parentStat); err != nil {
			return &os.PathError{Op: "stat", Path: mountParentDir, Err: err}
		}
		for range mountUmountCheckIterations {
			if anyerr, ok := whatWentWrong.Load(toMount); ok {
				if _, ok := anyerr.(error); ok {
					break
				}
			}
			if err := unix.Stat(where, &mountPointStat); err != nil {
				return &os.PathError{Op: "stat", Path: where, Err: err}
			}
			if mountPointStat.Dev != parentStat.Dev {
				break
			}
			logrus.Debugf("%q is not (yet?) a mountpoint, waiting %s", where, mountUmountCheckInterval)
			time.Sleep(mountUmountCheckInterval)
		}
	}

	// Forward any error messages to the caller before exiting from main.
	for i := range args {
		prefix := ""
		if len(args) > 0 {
			prefix = args[i] + ": "
		}
		if serveSftpStderr[i].Len() > 0 {
			fmt.Fprint(os.Stderr, prefix+serveSftpStderr[i].String())
		}
		if mountSshfsStderr[i].Len() > 0 {
			fmt.Fprint(os.Stderr, prefix+mountSshfsStderr[i].String())
		}
	}

	return nil
}

// umountSshfsCmd unmounts a container or image which was previously mounted by
// mountSshfsCmd().
func umountSshfsCmd(c *cobra.Command, args []string) error {
	selfExecutable := reexec.Self()

	inheritedFlags := getInheritedFlags(c)

	if len(args) == 0 && !umountSshfsAll {
		return errors.New("at least one container or image ID must be specified")
	}
	if len(args) > 0 && umountSshfsAll {
		return errors.New("when using the --all switch, you may not pass any container or image IDs")
	}
	if err := buildahcli.VerifyFlagsArgsOrder(args); err != nil {
		return err
	}

	var mountPoints []string
	if umountSshfsAll {
		var err error
		if mountPoints, _, err = listSshfsMounts(inheritedFlags, selfExecutable); err != nil {
			return err
		}
	} else {
		mountPoints = make([]string, 0, len(args))
		for _, toUnmount := range args {
			var mountParentDirBuffer bytes.Buffer

			if err := fileutils.Exists(toUnmount); err != nil && errors.Is(err, os.ErrNotExist) {
				findDataDirArgs := append(slices.Clone(inheritedFlags), findDataDir, toUnmount)
				findDataDir := exec.CommandContext(getContext(), selfExecutable, findDataDirArgs...)
				findDataDir.Stdin = nil
				findDataDir.Stdout = &mountParentDirBuffer
				findDataDir.Stderr = os.Stderr
				findDataDir.Dir = "/"
				if err := findDataDir.Run(); err != nil {
					return fmt.Errorf("determining where %q would be mounted: %w", toUnmount, err)
				}
			} else {
				abs, err := filepath.Abs(toUnmount)
				if err != nil {
					return fmt.Errorf("converting %q to an absolute path: %w", toUnmount, err)
				}
				if filepath.Base(abs) != mountSshfsMountFragment {
					return fmt.Errorf("%q doesn't look like an sshfs mountpoint", toUnmount)
				}
				mountParentDirBuffer.WriteString(filepath.Dir(abs))
			}

			mountParentDir := strings.TrimSpace(mountParentDirBuffer.String())
			if mountParentDir == "" {
				return fmt.Errorf("determining parent of mountpoint %q: %q", toUnmount, mountParentDir)
			}
			mountPoint := filepath.Join(mountParentDir, mountSshfsMountFragment)

			var parentStat, mountPointStat unix.Stat_t
			if err := unix.Stat(filepath.Dir(mountPoint), &parentStat); err != nil {
				return &os.PathError{Op: "stat", Path: filepath.Dir(mountPoint), Err: err}
			}
			if err := unix.Stat(mountPoint, &mountPointStat); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					// yeah, that'll do
					return nil
				}
				return &os.PathError{Op: "stat", Path: mountPoint, Err: err}
			}
			if mountPointStat.Dev == parentStat.Dev {
				continue
			}

			mountPoints = append(mountPoints, mountPoint)
		}
	}

	var errs []error
	for _, mountPoint := range mountPoints {
		if mountError := unix.Unmount(mountPoint, unix.MNT_DETACH); mountError != nil {
			errs = append(errs, fmt.Errorf("umount(%q): %w", mountPoint, mountError))
			logrus.Debugf("umount(%q): %v", mountPoint, mountError)

			umount := exec.CommandContext(getContext(), "umount", "-l", mountPoint)
			umount.Stdin = &bytes.Buffer{}
			umount.Stdout = os.Stdout
			umount.Stderr = os.Stderr
			umount.Dir = "/"
			umountError := umount.Run()

			if umountError != nil {
				errs = append(errs, fmt.Errorf("umount %s: %w", mountPoint, umountError))
				logrus.Debugf("umount(%q): %v", mountPoint, umountError)
				fumount := exec.CommandContext(getContext(), "fuserumount3", "-u", mountPoint)
				umount.Stdin = &bytes.Buffer{}
				fumount.Stdout = os.Stdout
				fumount.Stderr = os.Stderr
				fumount.Dir = "/"
				fumountError := fumount.Run()
				if fumountError != nil {
					logrus.Debugf("umount(%q): %v", mountPoint, fumountError)
					errs = append(errs, fmt.Errorf("fusermount3 -u %s: %w", mountPoint, fumountError))
				} else {
					errs = nil
				}
			} else {
				errs = nil
			}
		}
	}

	for _, mountPoint := range mountPoints {
		var parentStat, mountPointStat unix.Stat_t
		// Wait for the mountpoint to have a filesystem on it, so that when we
		// return, the caller can access its contents immediately.
		for range mountUmountCheckIterations {
			if err := unix.Stat(filepath.Dir(mountPoint), &parentStat); err != nil {
				return &os.PathError{Op: "stat", Path: filepath.Dir(mountPoint), Err: err}
			}
			if err := unix.Stat(mountPoint, &mountPointStat); err != nil {
				return &os.PathError{Op: "stat", Path: mountPoint, Err: err}
			}
			if mountPointStat.Dev == parentStat.Dev {
				break
			}
			logrus.Debugf("%q is still a mountpoint, waiting %s", mountPoint, mountUmountCheckInterval)
			time.Sleep(mountUmountCheckInterval)
		}
	}

	return errors.Join(errs...)
}
