package rusage

import (
	"flag"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/sirupsen/logrus"
	lslog "github.com/sirupsen/logrus/hooks/slog"
	"github.com/stretchr/testify/require"
	"go.podman.io/storage/pkg/reexec"
)

const (
	noopCommand = "noop"
)

func noopMain() {
}

func init() {
	reexec.Register(noopCommand, noopMain)
}

func TestMain(m *testing.M) {
	if reexec.Init() {
		return
	}
	flag.Parse()

	logrus.SetOutput(io.Discard)
	logrus.AddHook(lslog.NewHook(slog.Default(), nil))
	if testing.Verbose() {
		logrus.SetLevel(logrus.DebugLevel)
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}
	os.Exit(m.Run())
}

func TestRusage(t *testing.T) {
	t.Parallel()
	if !Supported() {
		t.Skip("not supported on this platform")
	}
	before, err := Get()
	require.Nil(t, err, "unexpected error from GetRusage before running child: %v", err)
	cmd := reexec.Command(noopCommand)
	err = cmd.Run()
	require.Nil(t, err, "unexpected error running child process: %v", err)
	after, err := Get()
	require.Nil(t, err, "unexpected error from GetRusage after running child: %v", err)
	t.Logf("rusage from child: %#v", FormatDiff(after.Subtract(before)))
	require.NotZero(t, after.Subtract(before), "running a child process didn't use any resources?")
}
