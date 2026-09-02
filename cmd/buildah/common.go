package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.podman.io/buildah"
	"go.podman.io/common/pkg/umask"
	is "go.podman.io/image/v5/storage"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/homedir"
	"go.podman.io/storage/pkg/unshare"
)

// configuration, including customizations made in containers.conf
var needToShutdownStore = false

func getStore(c *cobra.Command) (storage.Store, error) {
	if err := setXDGRuntimeDir(); err != nil {
		return nil, err
	}
	options, err := storage.DefaultStoreOptions()
	if err != nil {
		return nil, err
	}
	if c.Flag("root").Changed || c.Flag("runroot").Changed {
		options.GraphRoot = globalFlagResults.Root
		options.RunRoot = globalFlagResults.RunRoot
	}
	if c.Flag("imagestore").Changed {
		options.ImageStore = globalFlagResults.ImageStore
	}
	if c.Flag("transient-store").Changed {
		options.TransientStore = globalFlagResults.TransientStore
	}
	if c.Flag("storage-driver").Changed {
		options.GraphDriverName = globalFlagResults.StorageDriver
		// If any options setup in config, these should be dropped if user overrode the driver
		options.GraphDriverOptions = []string{}
	}
	if c.Flag("storage-opt").Changed {
		if len(globalFlagResults.StorageOpts) > 0 {
			options.GraphDriverOptions = globalFlagResults.StorageOpts
		}
	}

	// Do not allow to mount a graphdriver that is not vfs if we are creating the userns as part
	// of the mount command.
	// Differently, allow the mount if we are already in a userns, as the mount point will still
	// be accessible once "buildah mount" exits.
	if os.Geteuid() != 0 && options.GraphDriverName != "vfs" {
		return nil, fmt.Errorf("cannot mount using driver %s in rootless mode. You need to run it in a `buildah unshare` session", options.GraphDriverName)
	}

	if len(globalFlagResults.UserNSUID) > 0 {
		uopts := globalFlagResults.UserNSUID
		gopts := globalFlagResults.UserNSGID

		if len(gopts) == 0 {
			gopts = uopts
		}

		uidmap, gidmap, err := unshare.ParseIDMappings(uopts, gopts)
		if err != nil {
			return nil, err
		}
		options.UIDMap = uidmap
		options.GIDMap = gidmap
	} else {
		if len(globalFlagResults.UserNSGID) > 0 {
			return nil, errors.New("option --userns-gid-map can not be used without --userns-uid-map")
		}
	}

	// If a subcommand has the flags, check if they are set; if so, override the global values
	if c.Flags().Lookup("userns-uid-map").Changed {
		uopts, _ := c.Flags().GetStringSlice("userns-uid-map")
		gopts, _ := c.Flags().GetStringSlice("userns-gid-map")
		if len(gopts) == 0 {
			gopts = uopts
		}
		uidmap, gidmap, err := unshare.ParseIDMappings(uopts, gopts)
		if err != nil {
			return nil, err
		}
		options.UIDMap = uidmap
		options.GIDMap = gidmap
	} else {
		if c.Flags().Lookup("userns-gid-map").Changed {
			return nil, errors.New("option --userns-gid-map can not be used without --userns-uid-map")
		}
	}
	umask.Check()

	store, err := storage.GetStore(options)
	if store != nil {
		is.Transport.SetStore(store)
	}
	needToShutdownStore = true
	return store, err
}

// setXDGRuntimeDir sets XDG_RUNTIME_DIR when if it is unset under rootless
func setXDGRuntimeDir() error {
	if unshare.IsRootless() && os.Getenv("XDG_RUNTIME_DIR") == "" {
		runtimeDir, err := homedir.GetRuntimeDir()
		if err != nil {
			return err
		}
		if err := os.Setenv("XDG_RUNTIME_DIR", runtimeDir); err != nil {
			return errors.New("could not set XDG_RUNTIME_DIR")
		}
	}
	return nil
}

func openBuilder(ctx context.Context, store storage.Store, name string) (builder *buildah.Builder, err error) {
	if name != "" {
		builder, err = buildah.OpenBuilder(store, name)
		if errors.Is(err, os.ErrNotExist) {
			options := buildah.ImportOptions{
				Container: name,
			}
			builder, err = buildah.ImportBuilder(ctx, store, options)
		}
	}
	if err != nil {
		return nil, err
	}
	if builder == nil {
		return nil, errors.New("finding build container")
	}
	return builder, nil
}

func openBuilders(store storage.Store) (builders []*buildah.Builder, err error) {
	return buildah.OpenAllBuilders(store)
}

func openImage(ctx context.Context, sc *types.SystemContext, store storage.Store, name string) (builder *buildah.Builder, err error) {
	options := buildah.ImportFromImageOptions{
		Image:         name,
		SystemContext: sc,
	}
	builder, err = buildah.ImportBuilderFromImage(ctx, store, options)
	if err != nil {
		return nil, err
	}
	if builder == nil {
		return nil, errors.New("mocking up build configuration")
	}
	return builder, nil
}

// getContext returns a context that may have a timeout, and which cancels if
// it receives os.Interrupt
func getContext() context.Context {
	ctx, _ := getContextWithCancel()
	return ctx
}

func getContextCancel() context.CancelFunc {
	_, cancel := getContextWithCancel()
	return cancel
}

var getContextWithCancel = sync.OnceValues(func() (context.Context, context.CancelFunc) {
	var ctx context.Context
	var cancel1, cancel2 func()
	if rootCmd.PersistentFlags().Changed("experimental-timeout") {
		ctx, cancel1 = context.WithTimeout(context.Background(), globalFlagResults.ExperimentalTimeout)
	} else {
		ctx = context.Background()
		cancel1 = func() {}
	}
	ctx, cancel2 = signal.NotifyContext(ctx, os.Interrupt)
	return ctx, func() { cancel1(); cancel2() }
})

func getUserFlags() pflag.FlagSet {
	fs := pflag.FlagSet{}
	fs.String("user", "", "`user[:group]` to run the command as")
	return fs
}

func defaultFormat() string {
	format := os.Getenv("BUILDAH_FORMAT")
	if format != "" {
		return format
	}
	return buildah.OCI
}

// Tail returns a string slice after the first element unless there are
// not enough elements, then it returns an empty slice.  This is to replace
// the urfavecli Tail method for args
func Tail(a []string) []string {
	if len(a) >= 2 {
		return a[1:]
	}
	return []string{}
}

// UsageTemplate returns the usage template for buildah commands
// This blocks the displaying of the global options. The main buildah
// command should not use this.
func UsageTemplate() string {
	return `Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
  {{.Example}}{{end}}{{if .HasAvailableSubCommands}}

Available Commands:{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}
{{end}}
`
}
