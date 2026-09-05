package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.podman.io/buildah"
	"go.podman.io/buildah/pkg/cli"
	"go.podman.io/buildah/pkg/parse"
	"go.podman.io/common/pkg/auth"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
)

type addCopyResults struct {
	addHistory         bool
	chmod              string
	chown              string
	checksum           string
	quiet              bool
	ignoreFile         string
	contextdir         string
	from               string
	blobCache          string
	blobInfoCache      string
	decryptionKeys     []string
	removeSignatures   bool
	signaturePolicy    string
	authfile           string
	creds              string
	tlsVerify          bool
	certDir            string
	retry              int
	retryDelay         string
	excludes           []string
	parents            bool
	timestamp          string
	link               bool
	allowWildcard      bool
	allowEmptyWildcard bool
	noFollowSymlinks   bool
}

func createCommand(addCopy string, desc string, short string, opts *addCopyResults) *cobra.Command {
	return &cobra.Command{
		Use:   addCopy,
		Short: short,
		Long:  desc,
		RunE: func(cmd *cobra.Command, args []string) error {
			return addAndCopyCmd(cmd, args, strings.ToUpper(addCopy), *opts)
		},
		Example: `buildah ` + addCopy + ` containerID '/myapp/app.conf'
  buildah ` + addCopy + ` containerID 'app.conf' '/myapp/app.conf'
  buildah ` + addCopy + ` containerID 'app.conf' 'drop-in.conf' '/myapp/app.conf.d/'`,
		Args:    cobra.MinimumNArgs(1),
		GroupID: groupContainers,
	}
}

func applyFlagVars(flags *pflag.FlagSet, opts *addCopyResults) {
	flags.SetInterspersed(false)
	flags.BoolVar(&opts.addHistory, "add-history", false, "add an entry for this operation to the image's history.  Use BUILDAH_HISTORY environment variable to override. (default false)")
	flags.StringVar(&opts.authfile, "authfile", auth.GetDefaultAuthFile(), "path of the authentication file. Use REGISTRY_AUTH_FILE environment variable to override")
	if err := flags.MarkHidden("authfile"); err != nil {
		panic(fmt.Sprintf("error marking authfile as hidden: %v", err))
	}
	flags.StringVar(&opts.blobCache, "blob-cache", "", "store copies of pulled image blobs in the specified directory")
	if err := flags.MarkHidden("blob-cache"); err != nil {
		panic(fmt.Sprintf("error marking blob-cache as hidden: %v", err))
	}
	flags.StringVar(&opts.blobInfoCache, "blob-info-cache-dir", "", "cache information about blobs in the specified directory instead of the default location")
	if err := flags.MarkHidden("blob-info-cache-dir"); err != nil {
		panic(fmt.Sprintf("error marking blob-info-cache-dir as hidden: %v", err))
	}
	flags.StringVar(&opts.certDir, "cert-dir", "", "use certificates at the specified path to access registries and sources in HTTPS locations")
	flags.StringVar(&opts.checksum, "checksum", "", "checksum the HTTP source content")
	flags.StringVar(&opts.chown, "chown", "", "set the user and group ownership of the destination content")
	flags.StringVar(&opts.chmod, "chmod", "", "set the access permissions of the destination content")
	flags.StringVar(&opts.creds, "creds", "", "use `[username[:password]]` for accessing registries when pulling images")
	flags.BoolVar(&opts.link, "link", false, "enable layer caching for this operation (creates an independent layer)")
	flags.BoolVar(&opts.noFollowSymlinks, "no-follow-symlinks", false, "do not follow symlinks when copying content (copy the symlink itself)")
	if err := flags.MarkHidden("creds"); err != nil {
		panic(fmt.Sprintf("error marking creds as hidden: %v", err))
	}
	flags.StringVar(&opts.from, "from", "", "use the specified container's or image's root directory as the source root directory")
	flags.StringSliceVar(&opts.decryptionKeys, "decryption-key", nil, "key needed to decrypt a pulled image")
	if err := flags.MarkHidden("decryption-key"); err != nil {
		panic(fmt.Sprintf("error marking decryption-key as hidden: %v", err))
	}
	flags.StringSliceVar(&opts.excludes, "exclude", nil, "exclude pattern when copying files")
	flags.StringVar(&opts.ignoreFile, "ignorefile", "", "path to .containerignore file")
	flags.StringVar(&opts.contextdir, "contextdir", "", "context directory path")
	flags.IntVar(&opts.retry, "retry", cli.MaxPullPushRetries, "number of times to retry in case of failure when performing pull")
	flags.StringVar(&opts.retryDelay, "retry-delay", cli.PullPushRetryDelay.String(), "delay between retries in case of pull failures")
	flags.BoolVarP(&opts.quiet, "quiet", "q", false, "don't output a digest of the newly-added/copied content")
	flags.BoolVar(&opts.tlsVerify, "tls-verify", true, "require HTTPS and verify certificates when accessing registries when pulling images, and when retrieving sources from HTTPS URLs. TLS verification cannot be used when talking to an insecure registry.")
	flags.BoolVarP(&opts.removeSignatures, "remove-signatures", "", false, "don't copy signatures when pulling image")
	if err := flags.MarkHidden("remove-signatures"); err != nil {
		panic(fmt.Sprintf("error marking remove-signatures as hidden: %v", err))
	}
	flags.StringVar(&opts.signaturePolicy, "signature-policy", "", "`pathname` of signature policy file (not usually used)")
	if err := flags.MarkHidden("signature-policy"); err != nil {
		panic(fmt.Sprintf("error marking signature-policy as hidden: %v", err))
	}
	flags.StringVar(&opts.timestamp, "timestamp", "", "set timestamps on new content to `seconds` after the epoch")
	flags.BoolVar(&opts.allowWildcard, "allow-wildcard", true, "allow glob patterns in source paths")
	flags.BoolVar(&opts.allowEmptyWildcard, "allow-empty-wildcard", false, "don't error when glob patterns match nothing")
}

func addcopyInit() {
	var (
		addDescription  = "\n  Adds the contents of a file, URL, or directory to a container's working\n  directory.  If a local file appears to be an archive, its contents are\n  extracted and added instead of the archive file itself."
		copyDescription = "\n  Copies the contents of a file, URL, or directory into a container's working\n  directory."
		shortAdd        = "Add content to a working container"
		shortCopy       = "Copy content into a working container"
		addOpts         addCopyResults
		copyOpts        addCopyResults
	)
	addCommand := createCommand("add", addDescription, shortAdd, &addOpts)
	addCommand.SetUsageTemplate(UsageTemplate())

	copyCommand := createCommand("copy", copyDescription, shortCopy, &copyOpts)
	copyCommand.SetUsageTemplate(UsageTemplate())

	addFlags := addCommand.Flags()
	applyFlagVars(addFlags, &addOpts)

	copyFlags := copyCommand.Flags()
	applyFlagVars(copyFlags, &copyOpts)
	copyFlags.BoolVar(&copyOpts.parents, "parents", false, "preserve leading directories in the paths of items being copied")

	rootCmd.AddCommand(addCommand)
	rootCmd.AddCommand(copyCommand)
}

func addAndCopyCmd(c *cobra.Command, args []string, verb string, iopts addCopyResults) error {
	if len(args) == 0 {
		return errors.New("container ID must be specified")
	}
	name := args[0]
	args = Tail(args)
	if len(args) == 0 {
		return errors.New("src must be specified")
	}

	if err := cli.VerifyFlagsArgsOrder(args); err != nil {
		return err
	}

	// If list is greater than one, the last item is the destination
	dest := ""
	size := len(args)
	if size > 1 {
		dest = args[size-1]
		args = args[:size-1]
	}

	store, err := getStore(c)
	if err != nil {
		return err
	}

	var from *buildah.Builder
	unmountFrom := false
	removeFrom := false
	var idMappingOptions *buildah.IDMappingOptions
	contextdir := iopts.contextdir
	if iopts.ignoreFile != "" && contextdir == "" {
		return errors.New("--ignorefile option requires that you specify a context dir using --contextdir")
	}

	systemContext, err := parse.SystemContextFromOptions(c)
	if err != nil {
		return fmt.Errorf("building system context: %w", err)
	}

	var preserveOwnership bool
	if iopts.from != "" {
		if from, err = openBuilder(getContext(), store, iopts.from); err != nil && errors.Is(err, storage.ErrContainerUnknown) {
			decryptConfig, err2 := cli.DecryptConfig(iopts.decryptionKeys)
			if err2 != nil {
				return fmt.Errorf("unable to obtain decrypt config: %w", err2)
			}
			options := buildah.BuilderOptions{
				FromImage:           iopts.from,
				BlobDirectory:       iopts.blobCache,
				SignaturePolicyPath: iopts.signaturePolicy,
				SystemContext:       systemContext,
				MaxPullRetries:      iopts.retry,
				OciDecryptConfig:    decryptConfig,
			}
			if iopts.retryDelay != "" {
				options.PullRetryDelay, err = time.ParseDuration(iopts.retryDelay)
				if err != nil {
					return fmt.Errorf("unable to parse value provided %q as --retry-delay: %w", iopts.retryDelay, err)
				}
			}
			if !iopts.quiet {
				options.ReportWriter = os.Stderr
			}
			if from, err = buildah.NewBuilder(getContext(), store, options); err != nil {
				return fmt.Errorf("no container named %q, error copying content from image %q: %w", iopts.from, iopts.from, err)
			}
			removeFrom = true
			defer func() {
				if !removeFrom {
					return
				}
				if err := from.Delete(); err != nil {
					logrus.Errorf("error deleting %q temporary working container %q", iopts.from, from.Container)
				}
			}()
		}
		if err != nil {
			return fmt.Errorf("reading --from container %q: %w", iopts.from, err)
		}

		fromcdir, err := store.ContainerDirectory(from.ContainerID)
		if err != nil {
			return fmt.Errorf("finding container state directory for %q: %w", iopts.from, err)
		}
		if err := flagServingSFTP(fromcdir); err != nil {
			return fmt.Errorf(`unable to use "buildah add" or "buildah copy" while container appears to be mounted: %v`, err)
		}

		fromMountPoint, err := from.Mount(from.MountLabel)
		if err != nil {
			return fmt.Errorf("mounting %q container %q: %w", iopts.from, from.Container, err)
		}
		unmountFrom = true
		defer func() {
			if !unmountFrom {
				return
			}
			if err := from.Unmount(); err != nil {
				logrus.Errorf("error unmounting %q container %q", iopts.from, from.Container)
			}
			if err := from.Save(); err != nil {
				logrus.Errorf("error saving information about %q container %q", iopts.from, from.Container)
			}
		}()
		idMappingOptions = &from.IDMappingOptions
		preserveOwnership = true
		contextdir = filepath.Join(fromMountPoint, iopts.contextdir)
		for i := range args {
			args[i] = filepath.Join(fromMountPoint, args[i])
		}
	}

	builder, err := openBuilder(getContext(), store, name)
	if err != nil {
		return fmt.Errorf("reading build container %q: %w", name, err)
	}

	cdir, err := store.ContainerDirectory(builder.ContainerID)
	if err != nil {
		return fmt.Errorf("finding container state directory for %q: %w", name, err)
	}
	if err := flagServingSFTP(cdir); err != nil {
		return fmt.Errorf(`unable to use "buildah add" or "buildah copy" while container appears to be busy: %v`, err)
	}

	builder.ContentDigester.Restart()

	var timestamp *time.Time
	if iopts.timestamp != "" {
		u, err := strconv.ParseInt(iopts.timestamp, 10, 64)
		if err != nil {
			return fmt.Errorf("parsing timestamp value %q: %w", iopts.timestamp, err)
		}
		t := time.Unix(u, 0).UTC()
		timestamp = &t
	}

	followSymlink := types.OptionalBoolUndefined
	if iopts.noFollowSymlinks {
		followSymlink = types.OptionalBoolFalse
	}

	options := buildah.AddAndCopyOptions{
		Chmod:             iopts.chmod,
		Chown:             iopts.chown,
		PreserveOwnership: preserveOwnership,
		Checksum:          iopts.checksum,
		ContextDir:        contextdir,
		Excludes:          iopts.excludes,
		IDMappingOptions:  idMappingOptions,
		// These next two fields are set based on command line flags
		// with more generic-sounding names.
		CertPath:              systemContext.DockerCertPath,
		InsecureSkipTLSVerify: systemContext.DockerInsecureSkipTLSVerify,
		MaxRetries:            iopts.retry,
		Parents:               iopts.parents,
		Timestamp:             timestamp,
		Link:                  iopts.link,
		FollowSymlink:         followSymlink,
	}
	if iopts.contextdir != "" {
		var excludes []string

		excludes, options.IgnoreFile, err = parse.ContainerIgnoreFile(options.ContextDir, iopts.ignoreFile, []string{})
		if err != nil {
			return err
		}
		options.Excludes = append(excludes, options.Excludes...)
	}
	if c.Flags().Changed("allow-wildcard") {
		options.AllowWildcard = types.NewOptionalBool(iopts.allowWildcard)
	}
	if c.Flags().Changed("allow-empty-wildcard") {
		options.AllowEmptyWildcard = types.NewOptionalBool(iopts.allowEmptyWildcard)
	}
	if iopts.retryDelay != "" {
		retryDelay, err := time.ParseDuration(iopts.retryDelay)
		if err != nil {
			return fmt.Errorf("unable to parse value provided %q as --retry-delay: %w", iopts.retryDelay, err)
		}
		options.RetryDelay = retryDelay
	}

	extractLocalArchives := verb == "ADD"
	err = builder.Add(dest, extractLocalArchives, options, args...)
	if err != nil {
		return fmt.Errorf("adding content to container %q: %w", builder.Container, err)
	}
	if unmountFrom {
		if err := from.Unmount(); err != nil {
			return fmt.Errorf("unmounting %q container %q: %w", iopts.from, from.Container, err)
		}
		if err := from.Save(); err != nil {
			return fmt.Errorf("saving information about %q container %q: %w", iopts.from, from.Container, err)
		}
		unmountFrom = false
	}
	if removeFrom {
		if err := from.Delete(); err != nil {
			return fmt.Errorf("deleting %q temporary working container %q: %w", iopts.from, from.Container, err)
		}
		removeFrom = false
	}

	contentType, digest := builder.ContentDigester.Digest()
	if digest == "" {
		logrus.Debug("no content copied, skipping digest and history")
		return nil
	}
	if !iopts.quiet {
		fmt.Printf("%s\n", digest.Hex())
	}
	if contentType != "" {
		contentType = contentType + ":"
	}
	conditionallyAddHistory(builder, c, "/bin/sh -c #(nop) %s %s%s", verb, contentType, digest.Hex())
	return builder.Save()
}
