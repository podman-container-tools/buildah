package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"go.podman.io/buildah"
	"go.podman.io/buildah/internal/tmpdir"
	"go.podman.io/buildah/internal/volumes"
	buildahcli "go.podman.io/buildah/pkg/cli"
	"go.podman.io/buildah/pkg/overlay"
	"go.podman.io/buildah/pkg/parse"
	"go.podman.io/buildah/util"
	"go.podman.io/storage/pkg/mount"
)

type runInputOptions struct {
	addHistory     bool
	capAdd         []string
	capDrop        []string
	cdiConfigDir   string
	contextDir     string
	devices        []string
	env            []string
	hostname       string
	isolation      string
	mounts         []string
	runtime        string
	runtimeFlag    []string
	noHostname     bool
	noHosts        bool
	noPivot        bool
	terminal       bool
	validExitCodes []int32
	volumes        []string
	workingDir     string
	*buildahcli.NameSpaceResults
}

func runInit() {
	var (
		runDescription = "\n  Runs a specified command using the container's root filesystem as a root\n  filesystem, using configuration settings inherited from the container's\n  image or as specified using previous calls to the config command."
		opts           runInputOptions
	)

	namespaceResults := buildahcli.NameSpaceResults{}

	runCommand := &cobra.Command{
		Use:   "run",
		Short: "Run a command inside of a working container",
		Long:  runDescription,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.NameSpaceResults = &namespaceResults
			return runCmd(cmd, args, opts)
		},
		Example: `buildah run containerID -- ps -auxw
  buildah run --terminal containerID /bin/bash
  buildah run --volume /path/on/host:/path/in/container:ro,z containerID /bin/sh`,
		GroupID: groupContainers,
	}
	runCommand.SetUsageTemplate(UsageTemplate())

	flags := runCommand.Flags()
	flags.SetInterspersed(false)
	flags.BoolVar(&opts.addHistory, "add-history", false, "add an entry for this operation to the image's history.  Use BUILDAH_HISTORY environment variable to override. (default false)")
	flags.StringSliceVar(&opts.capAdd, "cap-add", []string{}, "add the specified capability (default [])")
	flags.StringSliceVar(&opts.capDrop, "cap-drop", []string{}, "drop the specified capability (default [])")
	flags.StringVar(&opts.cdiConfigDir, "cdi-config-dir", "", "`directory` of CDI configuration files")
	_ = flags.MarkHidden("cdi-config-dir")
	flags.StringVar(&opts.contextDir, "contextdir", "", "context directory path")
	flags.StringArrayVar(&opts.devices, "device", []string{}, "additional devices to provide")
	flags.StringArrayVarP(&opts.env, "env", "e", []string{}, "add environment variable to be set temporarily when running command (default [])")
	flags.StringVar(&opts.hostname, "hostname", "", "set the hostname inside of the container")
	flags.StringVar(&opts.isolation, "isolation", "", "`type` of process isolation to use. Use BUILDAH_ISOLATION environment variable to override.")
	// Do not set a default runtime here, we'll do that later in the processing.
	flags.StringVar(&opts.runtime, "runtime", util.Runtime(), "`path` to an alternate OCI runtime")
	flags.StringSliceVar(&opts.runtimeFlag, "runtime-flag", []string{}, "add global flags for the container runtime")
	flags.BoolVar(&opts.noHostname, "no-hostname", false, "do not override the /etc/hostname file within the container")
	flags.BoolVar(&opts.noHosts, "no-hosts", false, "do not override the /etc/hosts file within the container")
	flags.BoolVar(&opts.noPivot, "no-pivot", false, "do not use pivot root to jail process inside rootfs")
	flags.BoolVarP(&opts.terminal, "terminal", "t", false, "allocate a pseudo-TTY in the container")
	flags.Int32SliceVar(&opts.validExitCodes, "valid-exit-codes", []int32{0}, "list of exit codes to consider successful (default [0])")
	flags.StringArrayVarP(&opts.volumes, "volume", "v", []string{}, "bind mount a host location into the container while running the command")
	flags.StringArrayVar(&opts.mounts, "mount", []string{}, "attach a filesystem mount to the container (default [])")
	flags.StringVar(&opts.workingDir, "workingdir", "", "temporarily set working directory for command (default to container's workingdir)")

	userFlags := getUserFlags()
	namespaceFlags := buildahcli.GetNameSpaceFlags(&namespaceResults)

	flags.AddFlagSet(&userFlags)
	flags.AddFlagSet(&namespaceFlags)
	flags.SetNormalizeFunc(buildahcli.AliasFlags)

	rootCmd.AddCommand(runCommand)
}

func runCmd(c *cobra.Command, args []string, iopts runInputOptions) error {
	if len(args) == 0 {
		return errors.New("container ID must be specified")
	}
	name := args[0]
	args = Tail(args)
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}

	if len(args) == 0 {
		return errors.New("command must be specified")
	}

	tmpDir, err := os.MkdirTemp(tmpdir.GetTempDir(), "buildahvolume")
	if err != nil {
		return fmt.Errorf("creating temporary directory: %w", err)
	}
	defer func() {
		if err := os.Remove(tmpDir); err != nil {
			logrus.Debugf("removing should-be-empty temporary directory %q: %v", tmpDir, err)
		}
	}()

	store, err := getStore(c)
	if err != nil {
		return err
	}

	builder, err := openBuilder(getContext(), store, name)
	if err != nil {
		return fmt.Errorf("reading build container %q: %w", name, err)
	}

	isolation, err := parse.IsolationOption(c.Flag("isolation").Value.String())
	if err != nil {
		return err
	}

	runtimeFlags := []string{}
	for _, arg := range iopts.runtimeFlag {
		runtimeFlags = append(runtimeFlags, "--"+arg)
	}

	noPivot := iopts.noPivot || (os.Getenv("BUILDAH_NOPIVOT") != "")

	namespaceOptions, networkPolicy, err := parse.NamespaceOptions(c)
	if err != nil {
		return err
	}
	if c.Flag("network").Changed && c.Flag("isolation").Changed {
		if isolation == buildah.IsolationChroot {
			if ns := namespaceOptions.Find(string(specs.NetworkNamespace)); ns != nil {
				if !ns.Host {
					return fmt.Errorf("cannot set --network other than host with --isolation %s", c.Flag("isolation").Value.String())
				}
			}
		}
	}

	options := buildah.RunOptions{
		Hostname:         iopts.hostname,
		Runtime:          iopts.runtime,
		Args:             runtimeFlags,
		NoHostname:       iopts.noHostname,
		NoHosts:          iopts.noHosts,
		NoPivot:          noPivot,
		User:             c.Flag("user").Value.String(),
		Isolation:        isolation,
		NamespaceOptions: namespaceOptions,
		ConfigureNetwork: networkPolicy,
		ContextDir:       iopts.contextDir,
		AddCapabilities:  iopts.capAdd,
		DropCapabilities: iopts.capDrop,
		WorkingDir:       iopts.workingDir,
		DeviceSpecs:      iopts.devices,
		CDIConfigDir:     iopts.cdiConfigDir,
		ValidExitCodes:   iopts.validExitCodes,
	}

	if c.Flag("terminal").Changed {
		if iopts.terminal {
			options.Terminal = buildah.WithTerminal
		} else {
			options.Terminal = buildah.WithoutTerminal
		}
	}

	options.Env = buildahcli.LookupEnvVarReferences(iopts.env, os.Environ())

	systemContext, err := parse.SystemContextFromOptions(c)
	if err != nil {
		return fmt.Errorf("building system context: %w", err)
	}

	var excludes []string
	if iopts.contextDir != "" {
		excludes, _, err = parse.ContainerIgnoreFile(iopts.contextDir, "", nil)
		if err != nil {
			return fmt.Errorf("parsing ignore file under context directory %s: %w", iopts.contextDir, err)
		}
	}

	mounts, mountedImages, intermediateMounts, _, targetLocks, err := volumes.GetVolumes(systemContext, store, builder.MountLabel, iopts.volumes, iopts.mounts, iopts.contextDir, builder.IDMappingOptions, iopts.workingDir, tmpDir, excludes)
	if err != nil {
		return err
	}
	defer func() {
		if err := overlay.CleanupContent(tmpDir); err != nil {
			logrus.Debugf("unmounting overlay mounts under %q: %v", tmpDir, err)
		}
		for _, intermediateMount := range intermediateMounts {
			if err := mount.Unmount(intermediateMount); err != nil {
				logrus.Debugf("unmounting mount %q: %v", intermediateMount, err)
			}
			if err := os.Remove(intermediateMount); err != nil {
				logrus.Debugf("removing should-be-empty mount directory %q: %v", intermediateMount, err)
			}
		}
		for _, mountedImage := range mountedImages {
			if _, err := store.UnmountImage(mountedImage, false); err != nil {
				logrus.Debugf("unmounting image %q: %v", mountedImage, err)
			}
		}
		volumes.UnlockLockArray(targetLocks)
	}()
	options.Mounts = mounts
	options.CgroupManager = globalFlagResults.CgroupManager

	runerr := builder.Run(args, options)

	if runerr != nil {
		logrus.Debugf("error running %v in container %q: %v", args, builder.Container, runerr)
	}
	if runerr == nil {
		shell := "/bin/sh -c"
		if len(builder.Shell()) > 0 {
			shell = strings.Join(builder.Shell(), " ")
		}
		conditionallyAddHistory(builder, c, "%s %s", shell, strings.Join(args, " "))
		return builder.Save()
	}
	return runerr
}
