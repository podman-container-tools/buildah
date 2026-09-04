package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"runtime/pprof"
	"strings"
	"syscall"

	ispecs "github.com/opencontainers/image-spec/specs-go"
	rspecs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	lslog "github.com/sirupsen/logrus/hooks/slog"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.podman.io/buildah"
	"go.podman.io/buildah/define"
	"go.podman.io/buildah/pkg/cli"
	"go.podman.io/buildah/pkg/parse"
	"go.podman.io/common/pkg/config"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/unshare"
	"golang.org/x/term"
)

const (
	// command group IDs
	groupImages     = "images"
	groupContainers = "containers"
	groupRegistries = "registries"
	groupSystem     = "system"
)

var commandGroups = []*cobra.Group{
	{ID: groupImages, Title: "Image Commands:"},
	{ID: groupContainers, Title: "Container Commands:"},
	{ID: groupRegistries, Title: "Registry Commands:"},
	{ID: groupSystem, Title: "System Commands:"},
}

type globalFlags struct {
	Debug                      bool
	LogLevel                   string
	Root                       string
	RunRoot                    string
	ImageStore                 string
	TransientStore             bool
	StorageDriver              string
	RegistriesConf             string
	RegistriesConfDir          string
	DefaultMountsFile          string
	StorageOpts                []string
	UserNSUID                  []string
	UserNSGID                  []string
	CPUProfile                 string
	cpuProfileFile             *os.File
	MemoryProfile              string
	UserShortNameAliasConfPath string
	CgroupManager              string
}

var rootCmd = &cobra.Command{
	Use:  "buildah",
	Long: "A tool that facilitates building OCI images",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return cmd.Help()
	},
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		return before(cmd)
	},
	PersistentPostRunE: func(cmd *cobra.Command, _ []string) error {
		return after(cmd)
	},
	SilenceUsage:  true,
	SilenceErrors: true,
}

var (
	globalFlagResults      globalFlags
	exitCode               int
	defaultContainerConfig *config.Config
)

func mainInit() {
	var defaultStoreDriverOptions []string
	storageOptions, err := storage.DefaultStoreOptions()
	if err != nil {
		logrus.Error(err.Error())
		os.Exit(1)
	}

	if len(storageOptions.GraphDriverOptions) > 0 {
		optionSlice := storageOptions.GraphDriverOptions[:]
		defaultStoreDriverOptions = optionSlice
	}

	defaultContainerConfig, err = config.Default()
	if err != nil {
		logrus.Error(err.Error())
		os.Exit(1)
	}
	defaultContainerConfig.CheckCgroupsAndAdjustConfig()

	cobra.OnInitialize(initConfig)
	// Hide the implicit `completion` command in cobra.
	rootCmd.CompletionOptions.HiddenDefaultCmd = true
	// rootCmd.TraverseChildren = true
	rootCmd.Version = fmt.Sprintf("%s (image-spec %s, runtime-spec %s)", define.Version, ispecs.Version, rspecs.Version)
	rootCmd.PersistentFlags().BoolVar(&globalFlagResults.Debug, "debug", false, "print debugging information")
	// TODO Need to allow for environment variable
	rootCmd.PersistentFlags().StringVar(&globalFlagResults.RegistriesConf, "registries-conf", "", "path to registries.conf file (not usually used)")
	rootCmd.PersistentFlags().StringVar(&globalFlagResults.RegistriesConfDir, "registries-conf-dir", "", "path to registries.conf.d directory (not usually used)")
	rootCmd.PersistentFlags().StringVar(&globalFlagResults.UserShortNameAliasConfPath, "short-name-alias-conf", "", "path to short name alias cache file (not usually used)")
	rootCmd.PersistentFlags().StringVar(&globalFlagResults.Root, "root", storageOptions.GraphRoot, "storage root dir")
	rootCmd.PersistentFlags().StringVar(&globalFlagResults.RunRoot, "runroot", storageOptions.RunRoot, "storage state dir")
	rootCmd.PersistentFlags().StringVar(&globalFlagResults.CgroupManager, "cgroup-manager", defaultContainerConfig.Engine.CgroupManager, "cgroup manager")
	rootCmd.PersistentFlags().StringVar(&globalFlagResults.StorageDriver, "storage-driver", storageOptions.GraphDriverName, "storage-driver")
	rootCmd.PersistentFlags().StringVar(&globalFlagResults.ImageStore, "imagestore", storageOptions.ImageStore, "storage imagestore")
	rootCmd.PersistentFlags().BoolVar(&globalFlagResults.TransientStore, "transient-store", storageOptions.TransientStore, "store some information in transient storage")
	rootCmd.PersistentFlags().StringSliceVar(&globalFlagResults.StorageOpts, "storage-opt", defaultStoreDriverOptions, "storage driver option")
	rootCmd.PersistentFlags().StringSliceVar(&globalFlagResults.UserNSUID, "userns-uid-map", []string{}, "default `ctrID:hostID:length` UID mapping to use")
	rootCmd.PersistentFlags().StringSliceVar(&globalFlagResults.UserNSGID, "userns-gid-map", []string{}, "default `ctrID:hostID:length` GID mapping to use")
	rootCmd.PersistentFlags().StringVar(&globalFlagResults.DefaultMountsFile, "default-mounts-file", "", "path to default mounts file")
	rootCmd.PersistentFlags().StringVar(&globalFlagResults.LogLevel, logLevel, "warn", `the log level to be used, one of "trace", "debug", "info", "warn", "error", "fatal", or "panic"`)
	rootCmd.PersistentFlags().StringVar(&globalFlagResults.CPUProfile, "cpu-profile", "", "`file` to write CPU profile")
	rootCmd.PersistentFlags().StringVar(&globalFlagResults.MemoryProfile, "memory-profile", "", "`file` to write memory profile")

	if err := rootCmd.PersistentFlags().MarkHidden("cpu-profile"); err != nil {
		logrus.Fatalf("unable to mark cpu-profile flag as hidden: %v", err)
	}
	if err := rootCmd.PersistentFlags().MarkHidden("debug"); err != nil {
		logrus.Fatalf("unable to mark debug flag as hidden: %v", err)
	}
	if err := rootCmd.PersistentFlags().MarkHidden("default-mounts-file"); err != nil {
		logrus.Fatalf("unable to mark default-mounts-file flag as hidden: %v", err)
	}
	if err := rootCmd.PersistentFlags().MarkHidden("memory-profile"); err != nil {
		logrus.Fatalf("unable to mark memory-profile flag as hidden: %v", err)
	}
	rootCmd.AddGroup(commandGroups...)
}

func getInheritedFlags(c *cobra.Command) []string {
	var flags []string
	c.InheritedFlags().VisitAll(func(f *pflag.Flag) {
		if f.Changed {
			flags = append(flags, fmt.Sprintf("--%s=%s", f.Name, f.Value.String()))
		}
	})
	return flags
}

func initConfig() {
	// TODO Cobra allows us to do extra stuff here at init
	// time if we ever want to take advantage.
}

const logLevel = "log-level"

func before(cmd *cobra.Command) error {
	strLvl, err := cmd.Flags().GetString(logLevel)
	if err != nil {
		return err
	}
	logrusLvl, err := logrus.ParseLevel(strLvl)
	if err != nil {
		return fmt.Errorf("unable to parse log level: %w", err)
	}
	logrus.SetOutput(io.Discard)
	logrus.AddHook(lslog.NewHook(slog.Default(), nil))
	logrus.SetLevel(logrusLvl)
	slog.SetLogLoggerLevel(lslog.Level(logrusLvl).Level())
	if globalFlagResults.Debug {
		logrus.SetLevel(logrus.DebugLevel)
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}
	if term.IsTerminal(int(os.Stderr.Fd())) {
		log.SetFlags(0) // We don’t want date/time in interactive output
	}

	switch cmd.Use {
	case "", "help", "version", "mount", mountSshfs, umountSshfs:
		return nil
	}
	debugCapabilities()
	unshare.MaybeReexecUsingUserNamespace(false)
	if globalFlagResults.CPUProfile != "" {
		globalFlagResults.cpuProfileFile, err = os.Create(globalFlagResults.CPUProfile)
		if err != nil {
			logrus.Fatalf("could not create CPU profile %s: %v", globalFlagResults.CPUProfile, err)
		}
		if err = pprof.StartCPUProfile(globalFlagResults.cpuProfileFile); err != nil {
			logrus.Fatalf("error starting CPU profiling: %v", err)
		}
	}

	defaultContainerConfig, err := config.Default()
	if err != nil {
		return err
	}

	for _, env := range defaultContainerConfig.Engine.Env.Get() {
		splitEnv := strings.SplitN(env, "=", 2)
		if len(splitEnv) != 2 {
			return fmt.Errorf("invalid environment variable %q from containers.conf, valid configuration is KEY=value pair", env)
		}
		// skip if the env is already defined
		if _, ok := os.LookupEnv(splitEnv[0]); ok {
			logrus.Debugf("environment variable %q is already defined, skip the settings from containers.conf", splitEnv[0])
			continue
		}
		if err := os.Setenv(splitEnv[0], splitEnv[1]); err != nil {
			return err
		}
	}

	return nil
}

func shutdownStore(cmd *cobra.Command) error {
	if needToShutdownStore {
		store, err := getStore(cmd)
		if err != nil {
			return err
		}
		logrus.Debugf("shutting down the store")
		needToShutdownStore = false
		if _, err = store.Shutdown(false); err != nil {
			if errors.Is(err, storage.ErrLayerUsedByContainer) {
				logrus.Infof("failed to shutdown storage: %q", err)
			} else {
				logrus.Warnf("failed to shutdown storage: %q", err)
			}
		}
	}
	return nil
}

func after(cmd *cobra.Command) error {
	if err := shutdownStore(cmd); err != nil {
		return err
	}

	if globalFlagResults.CPUProfile != "" {
		pprof.StopCPUProfile()
		globalFlagResults.cpuProfileFile.Close()
	}
	if globalFlagResults.MemoryProfile != "" {
		memoryProfileFile, err := os.Create(globalFlagResults.MemoryProfile)
		if err != nil {
			logrus.Fatalf("could not create memory profile %s: %v", globalFlagResults.MemoryProfile, err)
		}
		defer memoryProfileFile.Close()
		runtime.GC()
		if err := pprof.Lookup("heap").WriteTo(memoryProfileFile, 1); err != nil {
			logrus.Fatalf("could not write memory profile %s: %v", globalFlagResults.MemoryProfile, err)
		}
	}
	return nil
}

func main() {
	if buildah.InitReexec() {
		return
	}

	mainInit()

	addcopyInit()
	buildInit()
	commitInit()
	configInit()
	containersInit()
	dumpboltInit()
	fromInit()
	imagesInit()
	infoInit()
	inspectInit()
	loginInit()
	logoutInit()
	manifestInit()
	mkcwInit()
	mountInit()
	pruneInit()
	pullInit()
	pushInit()
	renameInit()
	rmiInit()
	rmInit()
	rpcInit()
	runInit()
	sftpInit()
	sourceInit()
	tagInit()
	umountInit()
	unshareInit()
	versionInit()

	// Hard code TMPDIR functions to use $TMPDIR or /var/tmp
	os.Setenv("TMPDIR", parse.GetTempDir())

	if err := rootCmd.Execute(); err != nil {
		if logrus.IsLevelEnabled(logrus.TraceLevel) {
			fmt.Fprintf(os.Stderr, "Error: %+v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		exitCode = cli.ExecErrorCodeGeneric
		if ee, ok := errors.AsType[*exec.ExitError](err); ok {
			if w, ok := ee.Sys().(syscall.WaitStatus); ok {
				exitCode = w.ExitStatus()
			}
		}
		if err := shutdownStore(rootCmd); err != nil {
			logrus.Warnf("failed to shutdown storage: %q", err)
		}
	}
	os.Exit(exitCode)
}
