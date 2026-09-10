package main

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"go.podman.io/buildah"
	"go.podman.io/buildah/define"
	"go.podman.io/buildah/pkg/cli"
	"go.podman.io/buildah/pkg/parse"
	"go.podman.io/common/pkg/auth"
)

type pullOptions struct {
	allTags          bool
	authfile         string
	blobCache        string
	blobInfoCache    string
	certDir          string
	creds            string
	signaturePolicy  string
	quiet            bool
	removeSignatures bool
	tlsVerify        bool
	decryptionKeys   []string
	pullPolicy       string
	retry            int
	retryDelay       string
}

func pullInit() {
	var (
		opts pullOptions

		pullDescription = `  Pulls an image from a registry and stores it locally.
  An image can be pulled using its tag or digest. If a tag is not
  specified, the image with the 'latest' tag (if it exists) is pulled.`
	)

	pullCommand := &cobra.Command{
		Use:   "pull",
		Short: "Pull an image from the specified location",
		Long:  pullDescription,
		RunE: func(cmd *cobra.Command, args []string) error {
			return pullCmd(cmd, args, opts)
		},
		Example: `buildah pull imagename
  buildah pull docker-daemon:imagename:imagetag
  buildah pull myregistry/myrepository/imagename:imagetag`,
		GroupID: groupImages,
	}
	pullCommand.SetUsageTemplate(UsageTemplate())

	flags := pullCommand.Flags()
	flags.SetInterspersed(false)
	flags.BoolVarP(&opts.allTags, "all-tags", "a", false, "download all tagged images in the repository")
	flags.StringVar(&opts.authfile, "authfile", auth.GetDefaultAuthFile(), "path of the authentication file. Use REGISTRY_AUTH_FILE environment variable to override")
	flags.StringVar(&opts.blobCache, "blob-cache", "", "store copies of pulled image blobs in the specified directory")
	flags.StringVar(&opts.blobInfoCache, "blob-info-cache-dir", "", "cache information about blobs in the specified directory instead of the default location")
	if err := flags.MarkHidden("blob-info-cache-dir"); err != nil {
		panic(fmt.Sprintf("error marking blob-info-cache-dir as hidden: %v", err))
	}
	flags.StringVar(&opts.certDir, "cert-dir", "", "use certificates at the specified path to access the registry")
	flags.StringVar(&opts.creds, "creds", "", "use `[username[:password]]` for accessing the registry")
	flags.StringVar(&opts.pullPolicy, "policy", "missing", "missing, always, ifnewer, or never.")
	flags.BoolVarP(&opts.removeSignatures, "remove-signatures", "", false, "don't copy signatures when pulling image")
	flags.StringVar(&opts.signaturePolicy, "signature-policy", "", "`pathname` of signature policy file (not usually used)")
	flags.StringSliceVar(&opts.decryptionKeys, "decryption-key", nil, "key needed to decrypt the image")
	if err := flags.MarkHidden("signature-policy"); err != nil {
		panic(fmt.Sprintf("error marking signature-policy as hidden: %v", err))
	}
	flags.BoolVarP(&opts.quiet, "quiet", "q", false, "don't output progress information when pulling images")
	flags.String("os", runtime.GOOS, "prefer `OS` instead of the running OS for choosing images")
	flags.String("arch", runtime.GOARCH, "prefer `ARCH` instead of the architecture of the machine for choosing images")
	flags.StringSlice("platform", []string{parse.DefaultPlatform()}, "prefer OS/ARCH instead of the current operating system and architecture for choosing images")
	flags.String("variant", "", "override the `variant` of the specified image")
	flags.BoolVar(&opts.tlsVerify, "tls-verify", true, "require HTTPS and verify certificates when accessing the registry. TLS verification cannot be used when talking to an insecure registry.")
	flags.IntVar(&opts.retry, "retry", int(defaultContainerConfig.Engine.Retry), "number of times to retry in case of failure when performing pull")
	flags.StringVar(&opts.retryDelay, "retry-delay", defaultContainerConfig.Engine.RetryDelay, "delay between retries in case of pull failures")
	if err := flags.MarkHidden("blob-cache"); err != nil {
		panic(fmt.Sprintf("error marking blob-cache as hidden: %v", err))
	}

	rootCmd.AddCommand(pullCommand)
}

func pullCmd(c *cobra.Command, args []string, iopts pullOptions) error {
	if len(args) == 0 {
		return errors.New("an image name must be specified")
	}
	if err := cli.VerifyFlagsArgsOrder(args); err != nil {
		return err
	}
	if len(args) > 1 {
		return errors.New("too many arguments specified")
	}
	if err := auth.CheckAuthFile(iopts.authfile); err != nil {
		return err
	}

	systemContext, err := parse.SystemContextFromOptions(c)
	if err != nil {
		return fmt.Errorf("building system context: %w", err)
	}
	platforms, err := parse.PlatformsFromOptions(c)
	if err != nil {
		return err
	}
	if len(platforms) > 1 {
		logrus.Warnf("ignoring platforms other than %+v: %+v", platforms[0], platforms[1:])
	}

	store, err := getStore(c)
	if err != nil {
		return err
	}

	decConfig, err := cli.DecryptConfig(iopts.decryptionKeys)
	if err != nil {
		return fmt.Errorf("unable to obtain decryption config: %w", err)
	}

	policy, ok := define.PolicyMap[iopts.pullPolicy]
	if !ok {
		return fmt.Errorf("unsupported pull policy %q", iopts.pullPolicy)
	}
	options := buildah.PullOptions{
		SignaturePolicyPath: iopts.signaturePolicy,
		Store:               store,
		SystemContext:       systemContext,
		BlobDirectory:       iopts.blobCache,
		AllTags:             iopts.allTags,
		ReportWriter:        os.Stderr,
		RemoveSignatures:    iopts.removeSignatures,
		MaxRetries:          iopts.retry,
		OciDecryptConfig:    decConfig,
		PullPolicy:          policy,
	}

	if iopts.retryDelay != "" {
		options.RetryDelay, err = time.ParseDuration(iopts.retryDelay)
		if err != nil {
			return fmt.Errorf("unable to parse value provided %q as --retry-delay: %w", iopts.retryDelay, err)
		}
	}

	if iopts.quiet {
		options.ReportWriter = nil // Turns off logging output
	}

	id, err := buildah.Pull(getContext(), args[0], options)
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", id)
	return nil
}
