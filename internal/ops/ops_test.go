package ops

import (
	"flag"
	"os"
	"os/exec"
	"testing"

	"github.com/sirupsen/logrus"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/reexec"
)

func TestMain(m *testing.M) {
	var logLevel string
	debug := false
	if reexec.Init() {
		return
	}
	flag.BoolVar(&debug, "debug", false, "turn on debug logging")
	flag.StringVar(&logLevel, "log-level", "error", "log level")
	flag.Parse()
	level, err := logrus.ParseLevel(logLevel)
	if err != nil {
		logrus.Fatalf("error parsing log level %q: %v", logLevel, err)
	}
	if debug && level < logrus.DebugLevel {
		level = logrus.DebugLevel
	}
	logrus.SetLevel(level)
	os.Exit(m.Run())
}

func podmanCommand(t *testing.T, store storage.Store, args ...string) *exec.Cmd {
	storageArgs := []string{
		"podman",
		"--storage-driver=" + store.GraphDriverName(),
		"--root=" + store.GraphRoot(),
		"--runroot=" + store.RunRoot(),
	}
	return exec.CommandContext(t.Context(), storageArgs[0], append(storageArgs[1:], args...)...)
}
