//go:build linux

package binfmt

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"go.podman.io/storage/pkg/reexec"
	"golang.org/x/sys/unix"
)

func TestMain(m *testing.M) {
	if reexec.Init() {
		return
	}
	// call flag.Parse() here if TestMain uses flags
	m.Run()
}

const (
	testEmulationCommand = "test-emulation-with-namespace"
)

func init() {
	reexec.Register(testEmulationCommand, testEmulationMain)
}

func TestRegister(t *testing.T) {
	var args []string
	if testing.Verbose() {
		args = []string{"trace"}
	}
	cmd := reexec.Command(append([]string{testEmulationCommand}, args...)...)
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Unshareflags = syscall.CLONE_NEWUSER | syscall.CLONE_NEWNS
	cmd.SysProcAttr.UidMappings = []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getuid(), Size: 1}}
	cmd.SysProcAttr.GidMappings = []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getgid(), Size: 1}}
	output, err := cmd.CombinedOutput()
	t.Logf("%s", string(output))
	assert.NoError(t, err)
}

func testEmulationMain() {
	if len(os.Args) > 1 {
		if level, err := logrus.ParseLevel(os.Args[1]); err == nil {
			logrus.SetLevel(level)
		}
	}
	// mount a new binfmt_misc filesystem in our new mount namespace that
	// belongs to a new user namespace, and expect to not have any working
	// emulation, at least not at first
	if err := unix.Mount("binfmt_misc", "/proc/sys/fs/binfmt_misc", "binfmt_misc", 0, ""); err != nil {
		logrus.Fatalf("mounting binfmt_misc on /proc/sys/fs/binfmt_misc: %v", err)
	}
	logrus.Debugf("mounted binfmt_misc")
	if err := unix.Unmount("/proc/sys/fs/binfmt_misc", 0); err != nil {
		logrus.Fatalf("unmounting /proc/sys/fs/binfmt_misc: %v", err)
	}
	logrus.Debugf("unmounted binfmt_misc")
	if err := emulationWorksAlready(); err == nil {
		logrus.Fatal("expected attempt to run binaries for other architectures to fail, but succeeded")
	}
	logrus.Debugf("registering emulation (inner)")
	written, err := register(nil)
	if err != nil {
		logrus.Fatalf("expected attempt to register emulators to succeed: %v", err)
	}
	if len(written) == 0 {
		logrus.Infof("no configured emulators available, skipping the rest of the test")
		return
	}
	if err = filepath.Walk("/proc/sys/fs/binfmt_misc", func(path string, info fs.FileInfo, err error) error {
		if info.IsDir() || filepath.Base(path) == "register" {
			return nil
		}
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		logrus.Debugf("registration %q: %s", filepath.Base(path), string(contents))
		return nil
	}); err != nil {
		logrus.Fatalf("walking /proc/sys/fs/binfmt_misc: %v", err)
	}
	if err := emulationWorksAlready(); err != nil {
		logrus.Fatalf("expected this attempt to run binaries for other architectures to succeed: %v", err)
	}
	logrus.Infof("ok")
}
