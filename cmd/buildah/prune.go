package main

import (
	"fmt"

	"github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	buildahcli "go.podman.io/buildah/pkg/cli"
	"go.podman.io/buildah/pkg/parse"
	"go.podman.io/buildah/pkg/volumes"
	"go.podman.io/common/libimage"
)

type pruneOptions struct {
	force bool
	all   bool
}

func pruneInit() {
	var (
		pruneDescription = `
Cleanup intermediate images as well as build and mount cache.`
		opts pruneOptions
	)
	pruneCommand := &cobra.Command{
		Use:   "prune",
		Short: "Cleanup intermediate images as well as build and mount cache",
		Long:  pruneDescription,
		RunE: func(cmd *cobra.Command, args []string) error {
			return pruneCmd(cmd, args, opts)
		},
		Example: `buildah prune
  buildah prune --force`,
		GroupID: groupSystem,
	}
	pruneCommand.SetUsageTemplate(UsageTemplate())

	flags := pruneCommand.Flags()
	flags.SetInterspersed(false)

	flags.BoolVarP(&opts.all, "all", "a", false, "remove all unused images")
	flags.BoolVarP(&opts.force, "force", "f", false, "force removal of the image and any containers using the image")

	rootCmd.AddCommand(pruneCommand)
}

func pruneCmd(c *cobra.Command, args []string, iopts pruneOptions) error {
	if err := buildahcli.VerifyFlagsArgsOrder(args); err != nil {
		return err
	}

	store, err := getStore(c)
	if err != nil {
		return err
	}

	systemContext, err := parse.SystemContextFromOptions(c)
	if err != nil {
		return err
	}
	runtime, err := libimage.RuntimeFromStore(store, &libimage.RuntimeOptions{SystemContext: systemContext})
	if err != nil {
		return err
	}

	err = volumes.CleanCacheMount()
	if err != nil {
		return err
	}

	options := &libimage.RemoveImagesOptions{
		Filters: []string{"readonly=false"},
	}
	if !iopts.all {
		options.Filters = append(options.Filters, "dangling=true")
		options.Filters = append(options.Filters, "intermediate=true")
	}
	options.Force = iopts.force

	rmiReports, rmiErrors := runtime.RemoveImages(getContext(), args, options)
	for _, r := range rmiReports {
		for _, u := range r.Untagged {
			fmt.Printf("untagged: %s\n", u)
		}
	}
	for _, r := range rmiReports {
		if r.Removed {
			fmt.Printf("%s\n", r.ID)
		}
	}

	var multiE *multierror.Error
	multiE = multierror.Append(multiE, rmiErrors...)
	return multiE.ErrorOrNil()
}
