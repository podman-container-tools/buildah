package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"go.podman.io/buildah"
	"go.podman.io/buildah/define"
	"go.podman.io/buildah/internal"
	"go.podman.io/buildah/pkg/cli"
	"go.podman.io/buildah/pkg/parse"
	"go.podman.io/buildah/util"
	"go.podman.io/common/pkg/auth"
	"go.podman.io/common/pkg/completion"
	"go.podman.io/common/pkg/config"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/pkg/compression"
	"go.podman.io/image/v5/pkg/shortnames"
	storageTransport "go.podman.io/image/v5/storage"
	"go.podman.io/image/v5/transports/alltransports"
	"go.podman.io/image/v5/types"
)

type commitInputOptions struct {
	authfile               string
	omitHistory            bool
	blobCache              string
	blobInfoCache          string
	certDir                string
	changes                []string
	compressionFormat      string
	compressionLevel       int
	configFile             string
	creds                  string
	cwOptions              string
	disableCompression     bool
	forceCompressionFormat bool
	format                 string
	iidfile                string
	manifest               string
	omitTimestamp          bool
	timestamp              int64
	sourceDateEpoch        string
	rewriteTimestamp       bool
	quiet                  bool
	referenceTime          string
	rm                     bool
	pull                   string
	pullAlways             bool
	pullNever              bool
	sbomImgOutput          string
	sbomImgPurlOutput      string
	sbomMergeStrategy      string
	sbomOutput             string
	sbomPreset             string
	sbomPurlOutput         string
	sbomScannerCommand     []string
	sbomScannerImage       string
	signaturePolicy        string
	signBy                 string
	squash                 bool
	tlsVerify              bool
	identityLabel          bool
	encryptionKeys         []string
	encryptLayers          []int
	unsetenvs              []string
	addFile                []string
	unsetAnnotation        []string
	annotation             []string
	createdAnnotation      bool
	metadataFile           string
}

func commitInit() {
	var (
		opts              commitInputOptions
		commitDescription = "\n  Writes a new image using the container's read-write layer and, if it is based\n  on an image, the layers of that image."
	)
	commitCommand := &cobra.Command{
		Use:   "commit",
		Short: "Create an image from a working container",
		Long:  commitDescription,
		RunE: func(cmd *cobra.Command, args []string) error {
			return commitCmd(cmd, args, opts)
		},
		Example: `buildah commit containerID
  buildah commit containerID newImageName
  buildah commit containerID docker://localhost:5000/imageId`,
		GroupID: groupContainers,
	}
	commitCommand.SetUsageTemplate(UsageTemplate())
	commitListFlagSet(commitCommand, &opts)
	rootCmd.AddCommand(commitCommand)
}

func commitListFlagSet(cmd *cobra.Command, opts *commitInputOptions) {
	flags := cmd.Flags()
	flags.SetInterspersed(false)

	flags.StringArrayVar(&opts.addFile, "add-file", nil, "add contents of a file to the image at a specified path (`source:destination`)")
	flags.StringVar(&opts.authfile, "authfile", auth.GetDefaultAuthFile(), "path of the authentication file. Use REGISTRY_AUTH_FILE environment variable to override")
	_ = cmd.RegisterFlagCompletionFunc("authfile", completion.AutocompleteDefault)
	flags.StringVar(&opts.blobCache, "blob-cache", "", "assume image blobs in the specified directory will be available for pushing")
	if err := flags.MarkHidden("blob-cache"); err != nil {
		panic(fmt.Sprintf("error marking blob-cache as hidden: %v", err))
	}
	flags.StringVar(&opts.blobInfoCache, "blob-info-cache-dir", "", "cache information about blobs in the specified directory instead of the default location")
	if err := flags.MarkHidden("blob-info-cache-dir"); err != nil {
		panic(fmt.Sprintf("error marking blob-info-cache-dir as hidden: %v", err))
	}
	flags.StringSliceVar(&opts.encryptionKeys, "encryption-key", nil, "key with the encryption protocol to use needed to encrypt the image (e.g. jwe:/path/to/key.pem)")
	_ = cmd.RegisterFlagCompletionFunc("encryption-key", completion.AutocompleteDefault)
	flags.IntSliceVar(&opts.encryptLayers, "encrypt-layer", nil, "layers to encrypt, 0-indexed layer indices with support for negative indexing (e.g. 0 is the first layer, -1 is the last layer). If not defined, will encrypt all layers if encryption-key flag is specified")
	_ = cmd.RegisterFlagCompletionFunc("encryption-key", completion.AutocompleteNone)

	flags.StringArrayVarP(&opts.changes, "change", "c", nil, "apply containerfile `instruction`s to the committed image")
	flags.StringVar(&opts.certDir, "cert-dir", "", "use certificates at the specified path to access the registry")
	_ = cmd.RegisterFlagCompletionFunc("cert-dir", completion.AutocompleteDefault)
	flags.StringVar(&opts.configFile, "config", "", "apply configuration JSON `file` to the committed image")
	_ = cmd.RegisterFlagCompletionFunc("config", completion.AutocompleteDefault)
	flags.StringVar(&opts.creds, "creds", "", "use `[username[:password]]` for accessing the registry")
	_ = cmd.RegisterFlagCompletionFunc("creds", completion.AutocompleteNone)
	flags.StringVar(&opts.compressionFormat, "compression-format", "", "compression format to use")
	_ = cmd.RegisterFlagCompletionFunc("compression-format", completion.AutocompleteNone)
	flags.IntVar(&opts.compressionLevel, "compression-level", 0, "compression level to use")
	_ = cmd.RegisterFlagCompletionFunc("compression-level", completion.AutocompleteNone)
	flags.StringVar(&opts.cwOptions, "cw", "", "confidential workload `options`")
	flags.BoolVarP(&opts.disableCompression, "disable-compression", "D", true, "don't compress layers")
	flags.BoolVar(&opts.forceCompressionFormat, "force-compression", false, "use the specified compression algorithm if the destination contains a differently-compressed variant already")
	flags.StringVarP(&opts.format, "format", "f", defaultFormat(), "`format` of the image manifest and metadata")
	_ = cmd.RegisterFlagCompletionFunc("format", completion.AutocompleteNone)
	flags.StringVar(&opts.manifest, "manifest", "", "adds created image to the specified manifest list. Creates manifest list if it does not exist")
	_ = cmd.RegisterFlagCompletionFunc("manifest", completion.AutocompleteNone)
	flags.StringVar(&opts.iidfile, "iidfile", "", "write the image ID to the file")
	_ = cmd.RegisterFlagCompletionFunc("iidfile", completion.AutocompleteDefault)
	flags.StringVar(&opts.metadataFile, "metadata-file", "", "`file` to write metadata about the image to")
	_ = cmd.RegisterFlagCompletionFunc("metadata-file", completion.AutocompleteDefault)

	flags.BoolVar(&opts.omitTimestamp, "omit-timestamp", false, "set created timestamp to epoch 0 to allow for deterministic builds")
	sourceDateEpochUsageDefault := "current time"
	if v := os.Getenv(internal.SourceDateEpochName); v != "" {
		sourceDateEpochUsageDefault = fmt.Sprintf("%q", v)
	}
	flags.StringVar(&opts.sourceDateEpoch, "source-date-epoch", os.Getenv(internal.SourceDateEpochName), "set new timestamps in image info to `seconds` after the epoch, defaults to "+sourceDateEpochUsageDefault)
	_ = cmd.RegisterFlagCompletionFunc("source-date-epoch", completion.AutocompleteNone)
	flags.BoolVar(&opts.rewriteTimestamp, "rewrite-timestamp", false, "set timestamps in layer to no later than the value for --source-date-epoch")
	flags.Int64Var(&opts.timestamp, "timestamp", 0, "set new timestamps in image info and layer to `seconds` after the epoch, defaults to current times")
	_ = cmd.RegisterFlagCompletionFunc("timestamp", completion.AutocompleteNone)
	flags.BoolVarP(&opts.quiet, "quiet", "q", false, "don't output progress information when writing images")
	flags.StringVar(&opts.referenceTime, "reference-time", "", "set the timestamp on the image to match the named `file`")
	_ = cmd.RegisterFlagCompletionFunc("reference-time", completion.AutocompleteNone)

	flags.StringVar(&opts.pull, "pull", "true", "pull SBOM scanner images from the registry if newer or not present in store, if false, only pull SBOM scanner images if not present, if always, pull SBOM scanner images even if the named images are present in store, if never, only use images present in store if available")
	flags.Lookup("pull").NoOptDefVal = "true" // allow `--pull ` to be set to `true` as expected.

	flags.BoolVar(&opts.pullAlways, "pull-always", false, "pull the image even if the named image is present in store")
	if err := flags.MarkHidden("pull-always"); err != nil {
		panic(fmt.Sprintf("error marking the pull-always flag as hidden: %v", err))
	}
	flags.BoolVar(&opts.pullNever, "pull-never", false, "do not pull the image, use the image present in store if available")
	if err := flags.MarkHidden("pull-never"); err != nil {
		panic(fmt.Sprintf("error marking the pull-never flag as hidden: %v", err))
	}

	flags.StringVar(&opts.sbomPreset, "sbom", "", "scan working container using `preset` configuration")
	_ = cmd.RegisterFlagCompletionFunc("sbom", completion.AutocompleteNone)
	flags.StringVar(&opts.sbomScannerImage, "sbom-scanner-image", "", "scan working container using scanner command from `image`")
	_ = cmd.RegisterFlagCompletionFunc("sbom-scanner-image", completion.AutocompleteNone)
	flags.StringArrayVar(&opts.sbomScannerCommand, "sbom-scanner-command", nil, "scan working container using `command` in scanner image")
	_ = cmd.RegisterFlagCompletionFunc("sbom-scanner-command", completion.AutocompleteNone)
	flags.StringVar(&opts.sbomMergeStrategy, "sbom-merge-strategy", "", "merge scan results using `strategy`")
	_ = cmd.RegisterFlagCompletionFunc("sbom-merge-strategy", completion.AutocompleteNone)
	flags.StringVar(&opts.sbomOutput, "sbom-output", "", "save scan results to `file`")
	_ = cmd.RegisterFlagCompletionFunc("sbom-output", completion.AutocompleteDefault)
	flags.StringVar(&opts.sbomImgOutput, "sbom-image-output", "", "add scan results to image as `path`")
	_ = cmd.RegisterFlagCompletionFunc("sbom-image-output", completion.AutocompleteNone)
	flags.StringVar(&opts.sbomPurlOutput, "sbom-purl-output", "", "save scan results to `file``")
	_ = cmd.RegisterFlagCompletionFunc("sbom-purl-output", completion.AutocompleteDefault)
	flags.StringVar(&opts.sbomImgPurlOutput, "sbom-image-purl-output", "", "add scan results to image as `path`")
	_ = cmd.RegisterFlagCompletionFunc("sbom-image-purl-output", completion.AutocompleteNone)

	flags.StringVar(&opts.signBy, "sign-by", "", "sign the image using a GPG key with the specified `FINGERPRINT`")
	_ = cmd.RegisterFlagCompletionFunc("sign-by", completion.AutocompleteNone)
	if err := flags.MarkHidden("omit-timestamp"); err != nil {
		panic(fmt.Sprintf("error marking omit-timestamp as hidden: %v", err))
	}
	if err := flags.MarkHidden("reference-time"); err != nil {
		panic(fmt.Sprintf("error marking reference-time as hidden: %v", err))
	}

	flags.BoolVar(&opts.omitHistory, "omit-history", false, "omit build history information from the built image (default false)")
	flags.BoolVar(&opts.identityLabel, "identity-label", true, "add default builder label (default true)")
	flags.BoolVar(&opts.rm, "rm", false, "remove the container and its content after committing it to an image. Default leaves the container and its content in place.")
	flags.StringVar(&opts.signaturePolicy, "signature-policy", "", "`pathname` of signature policy file (not usually used)")
	_ = cmd.RegisterFlagCompletionFunc("signature-policy", completion.AutocompleteDefault)

	if err := flags.MarkHidden("signature-policy"); err != nil {
		panic(fmt.Sprintf("error marking signature-policy as hidden: %v", err))
	}

	flags.BoolVar(&opts.squash, "squash", false, "produce an image with only one layer")
	flags.BoolVar(&opts.tlsVerify, "tls-verify", true, "require HTTPS and verify certificates when accessing the registry. TLS verification cannot be used when talking to an insecure registry.")

	flags.StringSliceVar(&opts.unsetenvs, "unsetenv", nil, "unset env from final image")
	_ = cmd.RegisterFlagCompletionFunc("unsetenv", completion.AutocompleteNone)
	flags.StringSliceVar(&opts.unsetAnnotation, "unsetannotation", nil, "unset annotation when inheriting annotations from base image")
	_ = cmd.RegisterFlagCompletionFunc("unsetannotation", completion.AutocompleteNone)
	flags.StringArrayVar(&opts.annotation, "annotation", []string{}, "set metadata for an image (default [])")
	_ = cmd.RegisterFlagCompletionFunc("annotation", completion.AutocompleteNone)
	flags.BoolVar(&opts.createdAnnotation, "created-annotation", true, `set an "org.opencontainers.image.created" annotation in the image`)
}

func commitCmd(c *cobra.Command, args []string, iopts commitInputOptions) error {
	var dest types.ImageReference
	if len(args) == 0 {
		return errors.New("container ID must be specified")
	}
	if err := cli.VerifyFlagsArgsOrder(args); err != nil {
		return err
	}
	if err := auth.CheckAuthFile(iopts.authfile); err != nil {
		return err
	}

	name := args[0]
	args = Tail(args)
	if len(args) > 1 {
		return errors.New("too many arguments specified")
	}
	image := ""
	if len(args) > 0 {
		image = args[0]
	}
	compress := define.Gzip
	if iopts.disableCompression {
		compress = define.Uncompressed
	}

	if c.Flag("disable-compression").Changed && iopts.disableCompression {
		if c.Flag("compression-format").Changed {
			return errors.New("--disable-compression and --compression-format cannot be used together")
		}
		if c.Flag("force-compression").Changed {
			return errors.New("--disable-compression and --force-compression cannot be used together")
		}
	}

	format, err := cli.GetFormat(iopts.format)
	if err != nil {
		return err
	}
	store, err := getStore(c)
	if err != nil {
		return err
	}

	ctx := getContext()

	builder, err := openBuilder(ctx, store, name)
	if err != nil {
		return fmt.Errorf("reading build container %q: %w", name, err)
	}

	systemContext, err := parse.SystemContextFromOptions(c)
	if err != nil {
		return fmt.Errorf("building system context: %w", err)
	}

	// If the user specified an image, we may need to massage it a bit if
	// no transport is specified.
	if image != "" {
		if dest, err = alltransports.ParseImageName(image); err != nil {
			candidates, err2 := shortnames.ResolveLocally(systemContext, image)
			if err2 != nil {
				return err2
			}
			if len(candidates) == 0 {
				return fmt.Errorf("parsing target image name %q", image)
			}
			dest2, err2 := storageTransport.Transport.ParseStoreReference(store, candidates[0].String())
			if err2 != nil {
				return fmt.Errorf("parsing target image name %q: %w", image, err)
			}
			dest = dest2
		}
	}

	encConfig, encLayers, err := cli.EncryptConfig(iopts.encryptionKeys, iopts.encryptLayers)
	if err != nil {
		return fmt.Errorf("unable to obtain encryption config: %w", err)
	}

	var overrideConfig *manifest.Schema2Config
	if c.Flag("config").Changed {
		configBytes, err := os.ReadFile(iopts.configFile)
		if err != nil {
			return fmt.Errorf("reading configuration blob from file: %w", err)
		}
		overrideConfig = &manifest.Schema2Config{}
		if err := json.Unmarshal(configBytes, &overrideConfig); err != nil {
			return fmt.Errorf("parsing configuration blob from %q: %w", iopts.configFile, err)
		}
	}

	var addFiles map[string]string
	if len(iopts.addFile) > 0 {
		addFiles = make(map[string]string)
		for _, spec := range iopts.addFile {
			specSlice := strings.SplitN(spec, ":", 2)
			if len(specSlice) == 1 {
				specSlice = []string{specSlice[0], specSlice[0]}
			}
			if len(specSlice) != 2 {
				return fmt.Errorf("parsing add-file argument %q: expected 1 or 2 parts, got %d", spec, len(strings.SplitN(spec, ":", 2)))
			}
			st, err := os.Stat(specSlice[0])
			if err != nil {
				return fmt.Errorf("parsing add-file argument %q: source %q: %w", spec, specSlice[0], err)
			}
			if st.IsDir() {
				return fmt.Errorf("parsing add-file argument %q: source %q is not a regular file", spec, specSlice[0])
			}
			addFiles[specSlice[1]] = specSlice[0]
		}
	}

	options := buildah.CommitOptions{
		PreferredManifestType:  format,
		Manifest:               iopts.manifest,
		Compression:            compress,
		ForceCompressionFormat: iopts.forceCompressionFormat,
		SignaturePolicyPath:    iopts.signaturePolicy,
		SystemContext:          systemContext,
		IIDFile:                iopts.iidfile,
		Squash:                 iopts.squash,
		BlobDirectory:          iopts.blobCache,
		OmitHistory:            iopts.omitHistory,
		SignBy:                 iopts.signBy,
		OciEncryptConfig:       encConfig,
		OciEncryptLayers:       encLayers,
		UnsetEnvs:              iopts.unsetenvs,
		OverrideChanges:        iopts.changes,
		OverrideConfig:         overrideConfig,
		ExtraImageContent:      addFiles,
		UnsetAnnotations:       iopts.unsetAnnotation,
		Annotations:            iopts.annotation,
		CreatedAnnotation:      types.NewOptionalBool(iopts.createdAnnotation),
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
	exclusiveFlags := 0
	if c.Flag("reference-time").Changed {
		exclusiveFlags++
		referenceFile := iopts.referenceTime
		finfo, err := os.Stat(referenceFile)
		if err != nil {
			return fmt.Errorf("reading timestamp of file %q: %w", referenceFile, err)
		}
		timestamp := finfo.ModTime().UTC()
		options.HistoryTimestamp = &timestamp
	}
	if iopts.sourceDateEpoch != "" {
		exclusiveFlags++
		sourceDateEpochVal, err := strconv.ParseInt(iopts.sourceDateEpoch, 10, 64)
		if err != nil {
			return fmt.Errorf("parsing source date epoch %q: %w", iopts.sourceDateEpoch, err)
		}
		sourceDateEpoch := time.Unix(sourceDateEpochVal, 0).UTC()
		options.SourceDateEpoch = &sourceDateEpoch
	}
	options.RewriteTimestamp = iopts.rewriteTimestamp
	if c.Flag("timestamp").Changed {
		exclusiveFlags++
		timestamp := time.Unix(iopts.timestamp, 0).UTC()
		options.HistoryTimestamp = &timestamp
	}
	if iopts.omitTimestamp {
		exclusiveFlags++
		timestamp := time.Unix(0, 0).UTC()
		options.HistoryTimestamp = &timestamp
	}
	if exclusiveFlags > 1 {
		return errors.New("cannot use more then one timestamp option at a time")
	}

	// Add builder identity information.
	var identityLabel types.OptionalBool
	if c.Flag("identity-label").Changed {
		identityLabel = types.NewOptionalBool(iopts.identityLabel)
	}
	switch identityLabel {
	case types.OptionalBoolTrue:
		builder.SetLabel(buildah.BuilderIdentityAnnotation, define.Version)
	case types.OptionalBoolFalse:
		// nothing - don't clear it if there's a value set in the base image
	default:
		if options.HistoryTimestamp == nil && options.SourceDateEpoch == nil {
			builder.SetLabel(buildah.BuilderIdentityAnnotation, define.Version)
		}
	}

	if iopts.cwOptions != "" {
		confidentialWorkloadOptions, err := parse.GetConfidentialWorkloadOptions(iopts.cwOptions)
		if err != nil {
			return fmt.Errorf("parsing --cw arguments: %w", err)
		}
		options.ConfidentialWorkloadOptions = confidentialWorkloadOptions
	}

	pullPolicy, err := parse.PullPolicyFromOptions(c)
	if err != nil {
		return err
	}

	if c.Flag("sbom").Changed || c.Flag("sbom-scanner-command").Changed || c.Flag("sbom-scanner-image").Changed || c.Flag("sbom-image-output").Changed || c.Flag("sbom-merge-strategy").Changed || c.Flag("sbom-output").Changed || c.Flag("sbom-image-output").Changed || c.Flag("sbom-purl-output").Changed || c.Flag("sbom-image-purl-output").Changed {
		var sbomOptions []define.SBOMScanOptions
		sbomOption, err := parse.SBOMScanOptions(c)
		if err != nil {
			return err
		}
		sbomOption.PullPolicy = pullPolicy
		sbomOptions = append(sbomOptions, *sbomOption)
		options.SBOMScanOptions = sbomOptions
	}

	if !iopts.quiet {
		options.ReportWriter = os.Stderr
	}
	results, err := builder.CommitResults(ctx, dest, options)
	if err != nil {
		return util.GetFailureCause(err, fmt.Errorf("committing container %q to %q: %w", builder.Container, image, err))
	}
	ref := results.Canonical
	id := results.ImageID
	if ref != nil && id != "" {
		logrus.Debugf("wrote image %s with ID %s", ref, id)
	} else if ref != nil {
		logrus.Debugf("wrote image %s", ref)
	} else if id != "" {
		logrus.Debugf("wrote image with ID %s", id)
	} else {
		logrus.Debugf("wrote image")
	}
	if options.IIDFile == "" && id != "" {
		fmt.Printf("%s\n", id)
	}
	if iopts.metadataFile != "" {
		metadataBytes, err := json.Marshal(results.Metadata)
		if err != nil {
			return fmt.Errorf("encoding contents for %q: %w", iopts.metadataFile, err)
		}
		if err := os.WriteFile(iopts.metadataFile, metadataBytes, 0o644); err != nil {
			return err
		}
	}

	if iopts.rm {
		return builder.Delete()
	}
	return nil
}
