package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"go.podman.io/buildah"
	"go.podman.io/buildah/define"
	"go.podman.io/buildah/pkg/cli"
	"go.podman.io/buildah/pkg/parse"
	util "go.podman.io/buildah/util"
	"go.podman.io/common/pkg/auth"
	"go.podman.io/common/pkg/config"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/pkg/compression"
	"go.podman.io/image/v5/transports"
	"go.podman.io/image/v5/transports/alltransports"
	"go.podman.io/storage"
)

type pushOptions struct {
	all                    bool
	authfile               string
	blobCache              string
	blobInfoCache          string
	certDir                string
	creds                  string
	digestfile             string
	disableCompression     bool
	format                 string
	compressionFormat      string
	compressionLevel       int
	forceCompressionFormat bool
	retry                  int
	retryDelay             string
	rm                     bool
	quiet                  bool
	removeSignatures       bool
	signaturePolicy        string
	signBy                 string
	tlsVerify              bool
	encryptionKeys         []string
	encryptLayers          []int
	insecure               bool
	addCompression         []string
	destTimestamp          int64
	timestampSet           bool
}

func pushInit() {
	var (
		opts            pushOptions
		pushDescription = fmt.Sprintf(`
  Pushes an image to a specified location.

  The Image "DESTINATION" uses a "transport":"details" format. If not specified, will reuse source IMAGE as DESTINATION.

  Supported transports:
  %s

  See buildah-push(1) section "DESTINATION" for the expected format
`, getListOfTransports())
	)

	pushCommand := &cobra.Command{
		Use:   "push",
		Short: "Push an image to a specified destination",
		Long:  pushDescription,
		RunE: func(cmd *cobra.Command, args []string) error {
			return pushCmd(cmd, args, opts)
		},
		Example: `buildah push imageID docker://registry.example.com/repository:tag
  buildah push imageID docker-daemon:image:tagi
  buildah push imageID oci:/path/to/layout:image:tag`,
		GroupID: groupImages,
	}
	pushCommand.SetUsageTemplate(UsageTemplate())

	flags := pushCommand.Flags()
	flags.SetInterspersed(false)
	flags.BoolVar(&opts.all, "all", false, "push all of the images referenced by the manifest list")
	flags.StringVar(&opts.authfile, "authfile", auth.GetDefaultAuthFile(), "path of the authentication file. Use REGISTRY_AUTH_FILE environment variable to override")
	flags.StringVar(&opts.blobCache, "blob-cache", "", "assume image blobs in the specified directory will be available for pushing")
	flags.StringVar(&opts.blobInfoCache, "blob-info-cache-dir", "", "cache information about blobs in the specified directory instead of the default location")
	if err := flags.MarkHidden("blob-info-cache-dir"); err != nil {
		panic(fmt.Sprintf("error marking blob-info-cache-dir as hidden: %v", err))
	}
	flags.StringVar(&opts.certDir, "cert-dir", "", "use certificates at the specified path to access the registry")
	flags.StringVar(&opts.creds, "creds", "", "use `[username[:password]]` for accessing the registry")
	flags.StringVar(&opts.digestfile, "digestfile", "", "after copying the image, write the digest of the resulting image to the file")
	flags.BoolVarP(&opts.disableCompression, "disable-compression", "D", false, "don't compress layers")
	flags.BoolVarP(&opts.forceCompressionFormat, "force-compression", "", false, "use the specified compression algorithm if the destination contains a differently-compressed variant already")
	flags.StringVarP(&opts.format, "format", "f", "", "manifest type (oci, v2s1, or v2s2) to use in the destination (default is manifest type of source, with fallbacks)")
	flags.StringVar(&opts.compressionFormat, "compression-format", "", "compression format to use")
	flags.IntVar(&opts.compressionLevel, "compression-level", 0, "compression level to use")
	flags.BoolVarP(&opts.quiet, "quiet", "q", false, "don't output progress information when pushing images")
	flags.IntVar(&opts.retry, "retry", int(defaultContainerConfig.Engine.Retry), "number of times to retry in case of failure when performing push")
	flags.StringVar(&opts.retryDelay, "retry-delay", defaultContainerConfig.Engine.RetryDelay, "delay between retries in case of push failures")
	flags.BoolVar(&opts.rm, "rm", false, "remove the manifest list if push succeeds")
	flags.BoolVarP(&opts.removeSignatures, "remove-signatures", "", false, "don't copy signatures when pushing image")
	flags.StringVar(&opts.signBy, "sign-by", "", "sign the image using a GPG key with the specified `FINGERPRINT`")
	flags.StringVar(&opts.signaturePolicy, "signature-policy", "", "`pathname` of signature policy file (not usually used)")
	flags.StringSliceVar(&opts.encryptionKeys, "encryption-key", nil, "key with the encryption protocol to use needed to encrypt the image (e.g. jwe:/path/to/key.pem)")
	flags.IntSliceVar(&opts.encryptLayers, "encrypt-layer", nil, "layers to encrypt, 0-indexed layer indices with support for negative indexing (e.g. 0 is the first layer, -1 is the last layer). If not defined, will encrypt all layers if encryption-key flag is specified")

	if err := flags.MarkHidden("signature-policy"); err != nil {
		panic(fmt.Sprintf("error marking signature-policy as hidden: %v", err))
	}
	flags.BoolVar(&opts.tlsVerify, "tls-verify", true, "require HTTPS and verify certificates when accessing the registry. TLS verification cannot be used when talking to an insecure registry.")
	if err := flags.MarkHidden("blob-cache"); err != nil {
		panic(fmt.Sprintf("error marking blob-cache as hidden: %v", err))
	}

	rootCmd.AddCommand(pushCommand)
}

func pushCmd(c *cobra.Command, args []string, iopts pushOptions) error {
	var src, destSpec string

	if err := cli.VerifyFlagsArgsOrder(args); err != nil {
		return err
	}
	if err := auth.CheckAuthFile(iopts.authfile); err != nil {
		return err
	}

	switch len(args) {
	case 0:
		return errors.New("at least a source image ID must be specified")
	case 1:
		src = args[0]
		destSpec = src
		logrus.Debugf("Destination argument not specified, assuming the same as the source: %s", destSpec)
	case 2:
		src = args[0]
		destSpec = args[1]
		if src == "" {
			return fmt.Errorf(`invalid image name "%s"`, args[0])
		}
	default:
		return errors.New("only two arguments are necessary to push: source and destination")
	}

	compress := define.Gzip
	if iopts.disableCompression {
		compress = define.Uncompressed
	}

	store, err := getStore(c)
	if err != nil {
		return err
	}

	dest, err := alltransports.ParseImageName(destSpec)
	// add the docker:// transport to see if they neglected it.
	if err != nil {
		destTransport, _, destHasSeparator := strings.Cut(destSpec, ":")
		if destHasSeparator {
			if t := transports.Get(destTransport); t != nil {
				return err
			}
		}

		if strings.Contains(destSpec, "://") {
			return err
		}

		destSpec = "docker://" + destSpec
		dest2, err2 := alltransports.ParseImageName(destSpec)
		if err2 != nil {
			return err
		}
		dest = dest2
		logrus.Debugf("Assuming docker:// as the transport method for DESTINATION: %s", destSpec)
	}

	systemContext, err := parse.SystemContextFromOptions(c)
	if err != nil {
		return fmt.Errorf("building system context: %w", err)
	}

	var manifestType string
	if iopts.format != "" {
		switch iopts.format {
		case "oci":
			manifestType = imgspecv1.MediaTypeImageManifest
		case "v2s1":
			manifestType = manifest.DockerV2Schema1SignedMediaType
		case "v2s2", "docker":
			manifestType = manifest.DockerV2Schema2MediaType
		default:
			return fmt.Errorf("unknown format %q. Choose on of the supported formats: 'oci', 'v2s1', or 'v2s2'", iopts.format)
		}
	}

	encConfig, encLayers, err := cli.EncryptConfig(iopts.encryptionKeys, iopts.encryptLayers)
	if err != nil {
		return fmt.Errorf("unable to obtain encryption config: %w", err)
	}

	options := buildah.PushOptions{
		Compression:            compress,
		ManifestType:           manifestType,
		SignaturePolicyPath:    iopts.signaturePolicy,
		Store:                  store,
		SystemContext:          systemContext,
		BlobDirectory:          iopts.blobCache,
		RemoveSignatures:       iopts.removeSignatures,
		SignBy:                 iopts.signBy,
		MaxRetries:             iopts.retry,
		OciEncryptConfig:       encConfig,
		OciEncryptLayers:       encLayers,
		ForceCompressionFormat: iopts.forceCompressionFormat,
	}
	if iopts.retryDelay != "" {
		options.RetryDelay, err = time.ParseDuration(iopts.retryDelay)
		if err != nil {
			return fmt.Errorf("unable to parse value provided %q as --retry-delay: %w", iopts.retryDelay, err)
		}
	}
	if !iopts.quiet {
		options.ReportWriter = os.Stderr
	}
	defaultContainerConfig, err := config.Default()
	if err != nil {
		return fmt.Errorf("failed to get container config: %w", err)
	}
	if iopts.compressionFormat != "" {
		algo, err := compression.AlgorithmByName(iopts.compressionFormat)
		if err != nil {
			return err
		}
		options.CompressionFormat = &algo
		if !c.Flag("force-compression").Changed {
			options.ForceCompressionFormat = true
		}
	} else if defaultContainerConfig.Engine.CompressionFormat != "" && defaultContainerConfig.Engine.CompressionFormat != "gzip" {
		algo, err := compression.AlgorithmByName(defaultContainerConfig.Engine.CompressionFormat)
		if err != nil {
			return fmt.Errorf("parsing compression_format from containers.conf: %w", err)
		}
		options.CompressionFormat = &algo
		if !c.Flag("force-compression").Changed {
			options.ForceCompressionFormat = true
		}
	}
	if c.Flag("compression-level").Changed {
		options.CompressionLevel = &iopts.compressionLevel
	} else {
		options.CompressionLevel = defaultContainerConfig.Engine.CompressionLevel
	}

	ref, digest, err := buildah.Push(getContext(), src, dest, options)
	if err != nil {
		if !errors.Is(err, storage.ErrImageUnknown) {
			// Image might be a manifest so attempt a manifest push
			if manifestsErr := manifestPush(systemContext, store, src, destSpec, iopts); manifestsErr == nil {
				return nil
			}
		}
		return util.GetFailureCause(err, fmt.Errorf("pushing image %q to %q: %w", src, destSpec, err))
	}
	if ref != nil {
		logrus.Debugf("pushed image %q with digest %s", ref, digest.String())
	} else {
		logrus.Debugf("pushed image with digest %s", digest.String())
	}

	logrus.Debugf("Successfully pushed %s with digest %s", transports.ImageName(dest), digest.String())

	if iopts.digestfile != "" {
		if err = os.WriteFile(iopts.digestfile, []byte(digest.String()), 0o644); err != nil {
			return util.GetFailureCause(err, fmt.Errorf("failed to write digest to file %q: %w", iopts.digestfile, err))
		}
	}

	return nil
}

// getListOfTransports gets the transports supported from the image library
// and strips of the "tarball" transport from the string of transports returned
func getListOfTransports() string {
	allTransports := strings.Join(transports.ListNames(), ",")
	return strings.Replace(allTransports, ",tarball", "", 1)
}
