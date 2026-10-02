package binfmt

import (
	"bufio"
	"bytes"
	"embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/sirupsen/logrus"
	"go.podman.io/buildah/internal/tmpdir"
	"go.podman.io/storage/pkg/unshare"
	"golang.org/x/sys/unix"
)

//go:embed "embed/ok_amd64"
//go:embed "embed/ok_arm64"
//go:embed "embed/ok_ppc64le"
//go:embed "embed/ok_riscv64"
//go:embed "embed/ok_s390x"
var testBinaries embed.FS

func emulationWorksAlready() error {
	tmpdir, err := os.MkdirTemp(tmpdir.GetTempDir(), "exec")
	if err != nil {
		return fmt.Errorf("creating temporary directory: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpdir); err != nil {
			logrus.Warnf("removing temporary directory %q: %v", tmpdir, err)
		}
	}()
	var errs []error
	testBinary := filepath.Join(tmpdir, "test-emulation")
	for arch, embeddedBinary := range map[string]string{
		"amd64":   "embed/ok_amd64",
		"arm64":   "embed/ok_arm64",
		"ppc64le": "embed/ok_ppc64le",
		"riscv64": "embed/ok_riscv64",
		"s390x":   "embed/ok_s390x",
	} {
		var buffer bytes.Buffer
		binary, err := testBinaries.ReadFile(embeddedBinary)
		if err != nil {
			return fmt.Errorf("locating embedded binary %q: %w", embeddedBinary, err)
		}
		if err := os.WriteFile(testBinary, binary, 0o755); err != nil {
			return fmt.Errorf("writing temporary %s file at %s: %w", arch, testBinary, err)
		}
		cmd := exec.Command(testBinary)
		cmd.Stdout = &buffer
		if err := cmd.Run(); err != nil {
			logrus.Debugf("running %s test binary (%d bytes): %v", arch, len(binary), err)
			errs = append(errs, err)
			continue
		}
		if output := strings.TrimSpace(buffer.String()); output != "OK" {
			logrus.Debugf("%s test binary output %q", output, err)
			errs = append(errs, fmt.Errorf(`%s test binary output %q, expected "OK"`, arch, output))
			continue
		}
	}
	return errors.Join(errs...)
}

// MaybeRegister() calls Register() if the conditions are right:
// If $BUILDAH_REGISTER_BINFMT is set to "true", "yes", "on", or "1", it will
// always try to register binfmt using configuration files in the current mount
// namespace.
// If $BUILDAH_REGISTER_BINFMT is unset or set to "auto" or "default", it will
// call Register() if the current context is a rootless one, if the "container"
// environment variable suggests that we're in a container, and we can't run
// some trivial binaries built for various architectures.
// If $BUILDAH_REGISTER_BINFMT is set to any other value, it will do nothing.
func MaybeRegister(configurationSearchDirectories []string) error {
	shouldRegister, ok := os.LookupEnv("BUILDAH_REGISTER_BINFMT")
	if !ok {
		shouldRegister = "auto"
	}
	switch strings.ToLower(shouldRegister) {
	case "auto", "default":
		if !unshare.IsRootless() {
			logrus.Debug("defaulting to not touching binfmt for root")
			return nil
		}
		if os.Getenv("container") == "" {
			logrus.Debug("$container not set, defaulting to not configuring binfmt outside of a container")
			return nil
		}
		// we're rootless and we _also_ own our own mount namespace
		err := emulationWorksAlready()
		if err == nil {
			// oh. never mind, then
			logrus.Debug("defaulting to not touching binfmt, appears already configured")
			return nil
		}
		logrus.Debugf("checking on emulation status: %v", err)
		fallthrough
	case "on", "true", "yes", "1":
		return Register(configurationSearchDirectories)
	}
	return nil
}

// Register() registers binfmt.d emulators described by configuration files in
// the passed-in slice of directories, or in the union of /etc/binfmt.d,
// /run/binfmt.d, and /usr/lib/binfmt.d if the slice has no items.
// If any configuration files are found, it will attempt to mount a binfmt_misc
// filesystem in the current mount namespace first, ignoring only EPERM and
// EACCES errors.
func Register(configurationSearchDirectories []string) error {
	_, err := register(configurationSearchDirectories)
	return err
}

func register(configurationSearchDirectories []string) ([]string, error) {
	if len(configurationSearchDirectories) == 0 {
		configurationSearchDirectories = []string{"/etc/binfmt.d", "/run/binfmt.d", "/usr/lib/binfmt.d"}
	}
	mounted := false
	written := []string{}
	for _, searchDir := range configurationSearchDirectories {
		globs, err := filepath.Glob(filepath.Join(searchDir, "*.conf"))
		if err != nil {
			return written, fmt.Errorf("looking for binfmt.d configuration in %q: %w", searchDir, err)
		}
		for _, conf := range globs {
			f, err := os.Open(conf)
			if err != nil {
				return written, fmt.Errorf("reading binfmt.d configuration: %w", err)
			}
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if len(line) == 0 || line[0] == ';' || line[0] == '#' {
					continue
				}
				if !mounted {
					if err = unix.Mount("none", "/proc/sys/fs/binfmt_misc", "binfmt_misc", 0, ""); err != nil {
						if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
							// well, we tried. no need to make a stink about it
							return nil, nil
						}
						return written, fmt.Errorf("mounting binfmt_misc: %w", err)
					}
					mounted = true
				}
				reg, err := os.OpenFile("/proc/sys/fs/binfmt_misc/register", os.O_WRONLY, 0o600)
				if err != nil {
					return written, fmt.Errorf("registering(open): %w", err)
				}
				if _, err = fmt.Fprintf(reg, "%s\n", line); err != nil {
					return written, fmt.Errorf("registering(write): %w", err)
				}
				written = append(written, line)
				logrus.Tracef("registered binfmt %q", line)
				if err = reg.Close(); err != nil {
					return written, fmt.Errorf("registering(close): %w", err)
				}
			}
			if err := f.Close(); err != nil {
				return written, fmt.Errorf("reading binfmt.d configuration: %w", err)
			}
			if err := scanner.Err(); err != nil {
				return written, fmt.Errorf("parsing binfmt.d configuration: %w", err)
			}
		}
	}
	return written, nil
}
