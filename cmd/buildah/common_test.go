package main

import (
	"flag"
	"io"
	"log/slog"
	"os"
	"os/user"
	"testing"

	"github.com/sirupsen/logrus"
	lslog "github.com/sirupsen/logrus/hooks/slog"
	"github.com/spf13/cobra"
	"go.podman.io/buildah"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
)

var (
	signaturePolicyPath = ""
	storeOptions, _     = storage.DefaultStoreOptions()
	testSystemContext   = types.SystemContext{
		SignaturePolicyPath:      "../../tests/policy.json",
		SystemRegistriesConfPath: "../../tests/registries.conf",
	}
)

func TestMain(m *testing.M) {
	flag.StringVar(&signaturePolicyPath, "signature-policy", "", "pathname of signature policy file (not usually used)")
	options := storage.StoreOptions{}
	debug := false
	flag.StringVar(&options.GraphRoot, "root", "", "storage root dir")
	flag.StringVar(&options.RunRoot, "runroot", "", "storage state dir")
	flag.StringVar(&options.GraphDriverName, "storage-driver", "", "storage driver")
	flag.StringVar(&options.ImageStore, "imagestore", "", "storage imagestore")
	flag.BoolVar(&options.TransientStore, "transient-store", false, "use transient storage")
	flag.StringVar(&testSystemContext.SystemRegistriesConfPath, "registries-conf", "", "registries list")
	flag.BoolVar(&debug, "debug", false, "turn on debug logging")
	flag.Parse()
	if options.GraphRoot != "" || options.RunRoot != "" || options.GraphDriverName != "" {
		storeOptions = options
	}
	if buildah.InitReexec() {
		return
	}
	logrus.SetOutput(io.Discard)
	logrus.AddHook(lslog.NewHook(slog.Default(), nil))
	logrus.SetLevel(logrus.ErrorLevel)
	slog.SetLogLoggerLevel(slog.LevelError)
	if debug {
		logrus.SetLevel(logrus.DebugLevel)
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}
	os.Exit(m.Run())
}

func TestGetStore(t *testing.T) {
	// Make sure the tests are running as root
	failTestIfNotRoot(t)
	testCmd := &cobra.Command{
		Use: "test",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := getStore(cmd)
			return err
		},
	}
	flags := testCmd.PersistentFlags()
	flags.String("root", storeOptions.GraphRoot, "")
	flags.String("runroot", storeOptions.RunRoot, "")
	flags.String("imagestore", storeOptions.ImageStore, "")
	flags.Bool("transient-store", storeOptions.TransientStore, "")
	flags.String("storage-driver", storeOptions.GraphDriverName, "")
	flags.String("signature-policy", "", "")
	if err := flags.MarkHidden("signature-policy"); err != nil {
		t.Error(err)
	}
	// The following flags had to be added or we get panics in common.go when
	// the lookups occur
	flags.StringSlice("storage-opt", []string{}, "")
	flags.String("registries-conf", "", "")
	flags.String("userns-uid-map", "", "")
	flags.String("userns-gid-map", "", "")
	err := testCmd.Execute()
	if err != nil {
		t.Error(err)
	}
}

func failTestIfNotRoot(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Log("Could not determine user.  Running without root may cause tests to fail")
	} else if u.Uid != "0" {
		t.Fatal("tests will fail unless run as root")
	}
}
