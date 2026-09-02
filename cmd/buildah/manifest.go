package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	digest "github.com/opencontainers/go-digest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"go.podman.io/buildah/pkg/cli"
	"go.podman.io/buildah/pkg/parse"
	"go.podman.io/buildah/util"
	"go.podman.io/common/libimage"
	"go.podman.io/common/libimage/manifests"
	"go.podman.io/common/pkg/auth"
	cp "go.podman.io/image/v5/copy"
	"go.podman.io/image/v5/image"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/pkg/compression"
	"go.podman.io/image/v5/transports"
	"go.podman.io/image/v5/transports/alltransports"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
)

type manifestCreateOpts struct {
	os, arch                                 string
	all, tlsVerify, insecure, amend, replace bool
	annotations                              []string
}

type manifestAddOpts struct {
	authfile, certDir, creds, os, arch, variant, osVersion string
	features, osFeatures, annotations, artifactAnnotations []string
	tlsVerify, insecure, all                               bool
	artifact, artifactExcludeTitles                        bool
	artifactType, artifactLayerType                        string
	artifactConfigType, artifactConfigFile                 string
	artifactSubject                                        string
}

type manifestRemoveOpts struct{}

type manifestAnnotateOpts struct {
	os, arch, variant, osVersion      string
	features, osFeatures, annotations []string
	index                             bool
	subject                           string
}

type manifestInspectOpts struct {
	authfile  string
	tlsVerify bool
}

func manifestInit() {
	var (
		manifestDescription         = "\n  Creates, modifies, and pushes manifest lists and image indexes."
		manifestCreateDescription   = "\n  Creates manifest lists and image indexes."
		manifestAddDescription      = "\n  Adds an image or artifact to a manifest list or image index."
		manifestRemoveDescription   = "\n  Removes an image or artifact from a manifest list or image index."
		manifestAnnotateDescription = "\n  Adds or updates information about an image index or an entry in a manifest list or image index."
		manifestInspectDescription  = "\n  Display the contents of a manifest list or image index."
		manifestPushDescription     = "\n  Pushes manifest lists and image indexes to registries."
		manifestRmDescription       = "\n  Remove one or more manifest lists from local storage."
		manifestExistsDescription   = "\n  Check if a manifest list exists in local storage."
		manifestCreateOpts          manifestCreateOpts
		manifestAddOpts             manifestAddOpts
		manifestRemoveOpts          manifestRemoveOpts
		manifestAnnotateOpts        manifestAnnotateOpts
		manifestInspectOpts         manifestInspectOpts
		manifestPushOpts            pushOptions
	)
	manifestCommand := &cobra.Command{
		Use:   "manifest",
		Short: "Manipulate manifest lists and image indexes",
		Long:  manifestDescription,
		Example: `buildah manifest create localhost/list
  buildah manifest add localhost/list localhost/image
  buildah manifest annotate --annotation A=B localhost/list localhost/image
  buildah manifest annotate --annotation A=B localhost/list sha256:entryManifestDigest
  buildah manifest inspect localhost/list
  buildah manifest push localhost/list transport:destination
  buildah manifest remove localhost/list sha256:entryManifestDigest
  buildah manifest rm localhost/list`,
	}
	manifestCommand.SetUsageTemplate(UsageTemplate())
	rootCmd.AddCommand(manifestCommand)

	manifestCreateCommand := &cobra.Command{
		Use:   "create",
		Short: "Create manifest list or image index",
		Long:  manifestCreateDescription,
		RunE: func(cmd *cobra.Command, args []string) error {
			return manifestCreateCmd(cmd, args, manifestCreateOpts)
		},
		Example: `buildah manifest create mylist:v1.11
  buildah manifest create mylist:v1.11 arch-specific-image-to-add
  buildah manifest create --all mylist:v1.11 transport:tagged-image-to-add`,
		Args: cobra.MinimumNArgs(1),
	}
	manifestCreateCommand.SetUsageTemplate(UsageTemplate())
	flags := manifestCreateCommand.Flags()
	flags.BoolVar(&manifestCreateOpts.all, "all", false, "add all of the lists' images if the images to add are lists")
	flags.BoolVar(&manifestCreateOpts.amend, "amend", false, "modify an existing list if one with the desired name already exists")
	flags.BoolVar(&manifestCreateOpts.replace, "replace", false, "replace an existing image with the desired name")
	flags.StringSliceVar(&manifestCreateOpts.annotations, "annotation", nil, "set an `annotation` for the image index")
	flags.StringVar(&manifestCreateOpts.os, "os", "", "if any of the specified images is a list, choose the one for `os`")
	if err := flags.MarkHidden("os"); err != nil {
		panic(fmt.Sprintf("error marking --os as hidden: %v", err))
	}
	flags.StringVar(&manifestCreateOpts.arch, "arch", "", "if any of the specified images is a list, choose the one for `arch`")
	if err := flags.MarkHidden("arch"); err != nil {
		panic(fmt.Sprintf("error marking --arch as hidden: %v", err))
	}
	flags.BoolVar(&manifestCreateOpts.insecure, "insecure", false, "neither require HTTPS nor verify certificates when accessing the registry. TLS verification cannot be used when talking to an insecure registry.")
	if err := flags.MarkHidden("insecure"); err != nil {
		panic(fmt.Sprintf("error marking insecure as hidden: %v", err))
	}
	flags.BoolVar(&manifestCreateOpts.tlsVerify, "tls-verify", true, "require HTTPS and verify certificates when accessing the registry. TLS verification cannot be used when talking to an insecure registry.")
	flags.SetNormalizeFunc(cli.AliasFlags)
	manifestCommand.AddCommand(manifestCreateCommand)

	manifestAddCommand := &cobra.Command{
		Use:   "add",
		Short: "Add an image or artifact to a manifest list or image index",
		Long:  manifestAddDescription,
		RunE: func(cmd *cobra.Command, args []string) error {
			return manifestAddCmd(cmd, args, manifestAddOpts)
		},
		Example: `buildah manifest add mylist:v1.11 image:v1.11-amd64
  buildah manifest add mylist:v1.11 transport:imageName
  buildah manifest add --artifact --artifact-type text/plain mylist:v1.11 ./somefile.txt ./somefile.png`,
		Args: cobra.MinimumNArgs(2),
	}
	manifestAddCommand.SetUsageTemplate(UsageTemplate())
	flags = manifestAddCommand.Flags()
	flags.BoolVar(&manifestAddOpts.artifact, "artifact", false, "treat the argument as a filename and add it as an artifact")
	flags.StringVar(&manifestAddOpts.artifactType, "artifact-type", "", "artifact manifest media type")
	flags.StringVar(&manifestAddOpts.artifactConfigType, "artifact-config-type", imgspecv1.DescriptorEmptyJSON.MediaType, "artifact config media type")
	flags.StringVar(&manifestAddOpts.artifactConfigFile, "artifact-config", "", "artifact config file")
	flags.StringVar(&manifestAddOpts.artifactLayerType, "artifact-layer-type", "", "artifact layer media type")
	flags.BoolVar(&manifestAddOpts.artifactExcludeTitles, "artifact-exclude-titles", false, fmt.Sprintf(`refrain from setting %q annotations on "layers"`, imgspecv1.AnnotationTitle))
	flags.StringVar(&manifestAddOpts.artifactSubject, "artifact-subject", "", "artifact subject reference")
	flags.StringSliceVar(&manifestAddOpts.artifactAnnotations, "artifact-annotation", nil, "artifact annotation")
	flags.StringVar(&manifestAddOpts.authfile, "authfile", auth.GetDefaultAuthFile(), "path of the authentication file. Use REGISTRY_AUTH_FILE environment variable to override")
	flags.StringVar(&manifestAddOpts.certDir, "cert-dir", "", "use certificates at the specified path to access the registry")
	flags.StringVar(&manifestAddOpts.creds, "creds", "", "use `[username[:password]]` for accessing the registry")
	flags.StringVar(&manifestAddOpts.os, "os", "", "override the `OS` of the specified image")
	flags.StringVar(&manifestAddOpts.arch, "arch", "", "override the `architecture` of the specified image")
	flags.StringVar(&manifestAddOpts.variant, "variant", "", "override the `variant` of the specified image")
	flags.StringVar(&manifestAddOpts.osVersion, "os-version", "", "override the OS `version` of the specified image")
	flags.StringSliceVar(&manifestAddOpts.features, "features", nil, "override the `features` of the specified image")
	flags.StringSliceVar(&manifestAddOpts.osFeatures, "os-features", nil, "override the OS `features` of the specified image")
	flags.StringSliceVar(&manifestAddOpts.annotations, "annotation", nil, "set an `annotation` for the specified image or artifact")
	flags.BoolVar(&manifestAddOpts.insecure, "insecure", false, "neither require HTTPS nor verify certificates when accessing the registry. TLS verification cannot be used when talking to an insecure registry.")
	if err := flags.MarkHidden("insecure"); err != nil {
		panic(fmt.Sprintf("error marking insecure as hidden: %v", err))
	}
	flags.BoolVar(&manifestAddOpts.tlsVerify, "tls-verify", true, "require HTTPS and verify certificates when accessing the registry. TLS verification cannot be used when talking to an insecure registry.")
	flags.BoolVar(&manifestAddOpts.all, "all", false, "add all of the list's images if the image is a list")
	flags.SetNormalizeFunc(cli.AliasFlags)
	manifestCommand.AddCommand(manifestAddCommand)

	manifestRemoveCommand := &cobra.Command{
		Use:   "remove",
		Short: "Remove an entry from a manifest list or image index",
		Long:  manifestRemoveDescription,
		RunE: func(cmd *cobra.Command, args []string) error {
			return manifestRemoveCmd(cmd, args, manifestRemoveOpts)
		},
		Example: `buildah manifest remove mylist:v1.11 sha256:15352d97781ffdf357bf3459c037be3efac4133dc9070c2dce7eca7c05c3e736`,
		Args:    cobra.MinimumNArgs(2),
	}
	manifestRemoveCommand.SetUsageTemplate(UsageTemplate())
	manifestCommand.AddCommand(manifestRemoveCommand)

	manifestExistsCommand := &cobra.Command{
		Use:   "exists",
		Short: "Check if a manifest list exists in local storage",
		Long:  manifestExistsDescription,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return manifestExistsCmd(cmd, args)
		},
		Example: "buildah manifest exists mylist",
	}
	manifestExistsCommand.SetUsageTemplate(UsageTemplate())
	manifestCommand.AddCommand(manifestExistsCommand)

	manifestAnnotateCommand := &cobra.Command{
		Use:   "annotate",
		Short: "Add or update information about an entry in a manifest list or image index",
		Long:  manifestAnnotateDescription,
		RunE: func(cmd *cobra.Command, args []string) error {
			return manifestAnnotateCmd(cmd, args, manifestAnnotateOpts)
		},
		Example: `buildah manifest annotate --annotation left=right mylist:v1.11 image:v1.11-amd64`,
		Args:    cobra.RangeArgs(1, 2),
	}
	flags = manifestAnnotateCommand.Flags()
	flags.StringVar(&manifestAnnotateOpts.os, "os", "", "override the `OS` of the specified image")
	flags.StringVar(&manifestAnnotateOpts.arch, "arch", "", "override the `Architecture` of the specified image")
	flags.BoolVar(&manifestAnnotateOpts.index, "index", false, "set annotations or artifact type for the index itself instead of for an entry in the index")
	flags.StringVar(&manifestAnnotateOpts.variant, "variant", "", "override the `Variant` of the specified image")
	flags.StringVar(&manifestAnnotateOpts.osVersion, "os-version", "", "override the os `version` of the specified image")
	flags.StringSliceVar(&manifestAnnotateOpts.features, "features", nil, "override the `features` of the specified image")
	flags.StringSliceVar(&manifestAnnotateOpts.osFeatures, "os-features", nil, "override the os `features` of the specified image")
	flags.StringSliceVar(&manifestAnnotateOpts.annotations, "annotation", nil, "set an `annotation` for the specified image")
	flags.StringVar(&manifestAnnotateOpts.subject, "subject", "", "set a subject for the image index")
	manifestAnnotateCommand.SetUsageTemplate(UsageTemplate())
	manifestCommand.AddCommand(manifestAnnotateCommand)

	manifestInspectCommand := &cobra.Command{
		Use:   "inspect",
		Short: "Display the contents of a manifest list or image index",
		Long:  manifestInspectDescription,
		RunE: func(cmd *cobra.Command, args []string) error {
			return manifestInspectCmd(cmd, args, manifestInspectOpts)
		},
		Example: `buildah manifest inspect mylist:v1.11`,
		Args:    cobra.MinimumNArgs(1),
	}
	flags = manifestInspectCommand.Flags()
	flags.StringVar(&manifestInspectOpts.authfile, "authfile", auth.GetDefaultAuthFile(), "path of the authentication file. Use REGISTRY_AUTH_FILE environment variable to override")
	flags.BoolVar(&manifestInspectOpts.tlsVerify, "tls-verify", true, "require HTTPS and verify certificates when accessing the registry. TLS verification cannot be used when talking to an insecure registry.")
	manifestInspectCommand.SetUsageTemplate(UsageTemplate())
	manifestCommand.AddCommand(manifestInspectCommand)

	manifestPushCommand := &cobra.Command{
		Use:   "push",
		Short: "Push a manifest list or image index to a registry",
		Long:  manifestPushDescription,
		RunE: func(cmd *cobra.Command, args []string) error {
			return manifestPushCmd(cmd, args, manifestPushOpts)
		},
		Example: `buildah manifest push mylist:v1.11 transport:imageName`,
		Args:    cobra.MinimumNArgs(1),
	}
	manifestPushCommand.SetUsageTemplate(UsageTemplate())
	flags = manifestPushCommand.Flags()
	flags.BoolVar(&manifestPushOpts.rm, "rm", false, "remove the manifest list if push succeeds")
	flags.BoolVar(&manifestPushOpts.all, "all", true, "also push the images in the list")
	flags.StringVar(&manifestPushOpts.authfile, "authfile", auth.GetDefaultAuthFile(), "path of the authentication file. Use REGISTRY_AUTH_FILE environment variable to override")
	flags.StringVar(&manifestPushOpts.certDir, "cert-dir", "", "use certificates at the specified path to access the registry")
	flags.StringVar(&manifestPushOpts.creds, "creds", "", "use `[username[:password]]` for accessing the registry")
	flags.StringVar(&manifestPushOpts.digestfile, "digestfile", "", "after copying the image, write the digest of the resulting digest to the file")
	flags.BoolVarP(&manifestPushOpts.forceCompressionFormat, "force-compression", "", false, "use the specified compression algorithm if the destination contains a differently-compressed variant already")
	flags.StringVar(&manifestPushOpts.compressionFormat, "compression-format", "", "compression format to use")
	flags.IntVar(&manifestPushOpts.compressionLevel, "compression-level", 0, "compression level to use")
	flags.StringVarP(&manifestPushOpts.format, "format", "f", "", "manifest type (oci or v2s2) to attempt to use when pushing the manifest list (default is manifest type of source)")
	flags.StringArrayVar(&manifestPushOpts.addCompression, "add-compression", defaultContainerConfig.Engine.AddCompression.Get(), "add instances with selected compression while pushing")
	flags.BoolVarP(&manifestPushOpts.removeSignatures, "remove-signatures", "", false, "don't copy signatures when pushing images")
	flags.StringVar(&manifestPushOpts.signBy, "sign-by", "", "sign the image using a GPG key with the specified `FINGERPRINT`")
	flags.StringVar(&manifestPushOpts.signaturePolicy, "signature-policy", "", "`pathname` of signature policy file (not usually used)")
	if err := flags.MarkHidden("signature-policy"); err != nil {
		panic(fmt.Sprintf("error marking signature-policy as hidden: %v", err))
	}
	flags.BoolVar(&manifestPushOpts.insecure, "insecure", false, "neither require HTTPS nor verify certificates when accessing the registry. TLS verification cannot be used when talking to an insecure registry.")
	if err := flags.MarkHidden("insecure"); err != nil {
		panic(fmt.Sprintf("error marking insecure as hidden: %v", err))
	}
	flags.Int64Var(&manifestPushOpts.destTimestamp, "timestamp", 0, "set timestamps on new content to `seconds` after the epoch")
	flags.BoolVar(&manifestPushOpts.tlsVerify, "tls-verify", true, "require HTTPS and verify certificates when accessing the registry. TLS verification cannot be used when talking to an insecure registry.")
	flags.BoolVarP(&manifestPushOpts.quiet, "quiet", "q", false, "don't output progress information when pushing lists")
	flags.IntVar(&manifestPushOpts.retry, "retry", int(defaultContainerConfig.Engine.Retry), "number of times to retry in case of failure when performing push")
	flags.StringVar(&manifestPushOpts.retryDelay, "retry-delay", defaultContainerConfig.Engine.RetryDelay, "delay between retries in case of push failures")
	flags.SetNormalizeFunc(cli.AliasFlags)
	manifestCommand.AddCommand(manifestPushCommand)

	manifestRmCommand := &cobra.Command{
		Use:   "rm",
		Short: "Remove manifest list or image index",
		Long:  manifestRmDescription,
		RunE: func(cmd *cobra.Command, args []string) error {
			return manifestRmCmd(cmd, args)
		},
		Example: `buildah manifest rm mylist:v1.11`,
		Args:    cobra.MinimumNArgs(1),
	}
	manifestRmCommand.SetUsageTemplate(UsageTemplate())
	manifestCommand.AddCommand(manifestRmCommand)
}

func manifestExistsCmd(c *cobra.Command, args []string) error {
	if len(args) == 0 {
		return errors.New("at least a name must be specified for the list")
	}
	name := args[0]

	store, err := getStore(c)
	if err != nil {
		return err
	}

	systemContext, err := parse.SystemContextFromOptions(c)
	if err != nil {
		return fmt.Errorf("building system context: %w", err)
	}
	runtime, err := libimage.RuntimeFromStore(store, &libimage.RuntimeOptions{SystemContext: systemContext})
	if err != nil {
		return err
	}

	_, err = runtime.LookupManifestList(name)
	if err != nil {
		if errors.Is(err, storage.ErrImageUnknown) {
			exitCode = 1
		} else {
			return err
		}
	}
	return nil
}

func manifestCreateCmd(c *cobra.Command, args []string, opts manifestCreateOpts) error {
	if len(args) == 0 {
		return errors.New("at least a name must be specified for the list")
	}
	if opts.amend && opts.replace {
		return errors.New("amend and replace are mutually exclusive")
	}
	listImageSpec := args[0]
	imageSpecs := args[1:]

	store, err := getStore(c)
	if err != nil {
		return err
	}

	systemContext, err := parse.SystemContextFromOptions(c)
	if err != nil {
		return fmt.Errorf("building system context: %w", err)
	}
	runtime, err := libimage.RuntimeFromStore(store, &libimage.RuntimeOptions{SystemContext: systemContext})
	if err != nil {
		return err
	}

	list := manifests.Create()
	var manifestListID string

	names, err := util.ExpandNames([]string{listImageSpec}, systemContext, store)
	if err != nil {
		return fmt.Errorf("encountered while expanding image name %q: %w", listImageSpec, err)
	}
	if manifestListID, err = list.SaveToImage(store, "", names, ""); err != nil {
		if !errors.Is(err, storage.ErrDuplicateName) {
			return err
		}
		if opts.amend {
			img, _, lookupErr := runtime.LookupImage(listImageSpec, &libimage.LookupImageOptions{ManifestList: true})
			if lookupErr != nil {
				return fmt.Errorf("looking up image to amend %q: %w", listImageSpec, lookupErr)
			}
			if manifestList, err := img.ToManifestList(); err == nil {
				manifestListID = manifestList.ID()
			} else {
				converted, convertErr := img.ConvertToManifestList(getContext())
				if convertErr != nil {
					return fmt.Errorf("converting image %q to manifest list: %w", listImageSpec, convertErr)
				}
				manifestListID = converted.ID()
			}
		} else if opts.replace {
			img, _, lookupErr := runtime.LookupImage(listImageSpec, &libimage.LookupImageOptions{ManifestList: true})
			if lookupErr != nil {
				return fmt.Errorf("looking up image to replace %q: %w", listImageSpec, lookupErr)
			}
			if err := store.RemoveNames(img.ID(), names); err != nil {
				return fmt.Errorf("removing name from image %s: %w", img.ID(), err)
			}
			if manifestListID, err = list.SaveToImage(store, "", names, ""); err != nil {
				return err
			}
		} else {
			return err
		}
	}

	locker, err := manifests.LockerForImage(store, manifestListID)
	if err != nil {
		return err
	}
	locker.Lock()
	defer locker.Unlock()

	if _, list, err = manifests.LoadFromImage(store, manifestListID); err != nil {
		return err
	}

	if len(opts.annotations) != 0 {
		annotations := make(map[string]string)
		for _, annotationSpec := range opts.annotations {
			k, v, ok := strings.Cut(annotationSpec, "=")
			if !ok {
				return fmt.Errorf(`no "=" found in annotation %q`, annotationSpec)
			}
			annotations[k] = v
		}
		if err := list.SetAnnotations(nil, annotations); err != nil {
			return err
		}
	}

	for _, imageSpec := range imageSpecs {
		ref, err := alltransports.ParseImageName(imageSpec)
		if err != nil {
			if ref, err = alltransports.ParseImageName(util.DefaultTransport + imageSpec); err != nil {
				// check if the local image exists
				if ref, _, err = util.FindImage(store, "", systemContext, imageSpec); err != nil {
					return err
				}
			}
		}
		refLocal, _, err := util.FindImage(store, "", systemContext, imageSpec)
		if err == nil {
			// Found local image so use that.
			ref = refLocal
		}
		if _, err = list.Add(getContext(), systemContext, ref, opts.all); err != nil {
			return err
		}
	}

	imageID, err := list.SaveToImage(store, manifestListID, names, "")
	if err == nil {
		fmt.Printf("%s\n", imageID)
	}
	return err
}

func manifestAddCmd(c *cobra.Command, args []string, opts manifestAddOpts) error {
	if err := auth.CheckAuthFile(opts.authfile); err != nil {
		return err
	}

	listImageSpec := ""
	imageSpec := ""
	artifactSpec := []string{}
	switch len(args) {
	case 0, 1:
		return errors.New("at least a list image and an image or artifact to add must be specified")
	default:
		listImageSpec = args[0]
		if listImageSpec == "" {
			return fmt.Errorf("invalid image name %q", args[0])
		}
		if opts.artifact {
			artifactSpec = args[1:]
		} else {
			if len(args) > 2 {
				return errors.New("too many arguments: expected list and image add to list")
			}
			imageSpec = args[1]
			if imageSpec == "" {
				return fmt.Errorf("invalid image name %q", args[1])
			}
		}
	}

	store, err := getStore(c)
	if err != nil {
		return err
	}

	systemContext, err := parse.SystemContextFromOptions(c)
	if err != nil {
		return fmt.Errorf("building system context: %w", err)
	}
	runtime, err := libimage.RuntimeFromStore(store, &libimage.RuntimeOptions{SystemContext: systemContext})
	if err != nil {
		return err
	}

	manifestList, err := runtime.LookupManifestList(listImageSpec)
	if err != nil {
		return err
	}

	locker, err := manifests.LockerForImage(store, manifestList.ID())
	if err != nil {
		return err
	}
	locker.Lock()
	defer locker.Unlock()

	_, list, err := manifests.LoadFromImage(store, manifestList.ID())
	if err != nil {
		return err
	}

	var instanceDigest digest.Digest
	if opts.artifact {
		var subjectRef types.ImageReference
		if opts.artifactSubject != "" {
			if subjectRef, err = alltransports.ParseImageName(opts.artifactSubject); err != nil {
				if subjectRef, err = alltransports.ParseImageName(util.DefaultTransport + opts.artifactSubject); err != nil {
					if subjectRef, _, err = util.FindImage(store, "", systemContext, opts.artifactSubject); err != nil {
						logrus.Errorf("Error while trying to parse artifact subject %q: %v", opts.artifactSubject, err)
						return err
					}
				}
			}
		}
		var artifactType *string
		if c.Flags().Changed("artifact-type") {
			artifactType = &opts.artifactType
		}
		var artifactLayerType *string
		if c.Flags().Changed("artifact-layer-type") {
			artifactLayerType = &opts.artifactLayerType
		}
		options := manifests.AddArtifactOptions{
			ManifestArtifactType: artifactType,
			LayerMediaType:       artifactLayerType,
			SubjectReference:     subjectRef,
		}
		if opts.artifactConfigType != "" {
			tmp := imgspecv1.DescriptorEmptyJSON
			tmp.MediaType = opts.artifactConfigType
			options.ConfigDescriptor = &tmp
		}
		if opts.artifactConfigFile != "" {
			if options.ConfigDescriptor == nil {
				tmp := imgspecv1.DescriptorEmptyJSON
				if opts.artifactConfigType == "" {
					tmp.MediaType = imgspecv1.MediaTypeImageConfig
				}
				options.ConfigDescriptor = &tmp
			}
			options.ConfigDescriptor.Size = -1
			options.ConfigFile = opts.artifactConfigFile
		}
		if len(opts.artifactAnnotations) > 0 {
			options.Annotations = make(map[string]string, len(opts.artifactAnnotations))
			for _, annotation := range opts.artifactAnnotations {
				k, v, _ := strings.Cut(annotation, "=")
				options.Annotations[k] = v
			}
		}
		options.ExcludeTitles = opts.artifactExcludeTitles
		instanceDigest, err = list.AddArtifact(getContext(), systemContext, options, artifactSpec...)
		if err != nil {
			logrus.Errorf("Error while trying to add artifact %q to image index: %v", artifactSpec, err)
			return err
		}
	} else {
		var changedArtifactFlags []string
		for _, artifactOption := range []string{"artifact-type", "artifact-config", "artifact-config-type", "artifact-layer-type", "artifact-subject", "artifact-exclude-titles", "artifact-annotation"} {
			if c.Flags().Changed(artifactOption) {
				changedArtifactFlags = append(changedArtifactFlags, "--"+artifactOption)
			}
		}
		switch {
		case len(changedArtifactFlags) == 1:
			return fmt.Errorf("%s requires --artifact", changedArtifactFlags[0])
		case len(changedArtifactFlags) > 1:
			return fmt.Errorf("%s require --artifact", strings.Join(changedArtifactFlags, "/"))
		}
		var ref types.ImageReference
		if ref, err = alltransports.ParseImageName(imageSpec); err != nil {
			if ref, err = alltransports.ParseImageName(util.DefaultTransport + imageSpec); err != nil {
				if ref, _, err = util.FindImage(store, "", systemContext, imageSpec); err != nil {
					return err
				}
			}
		}

		instanceDigest, err = list.Add(getContext(), systemContext, ref, opts.all)
		if err != nil {
			var storeErr error
			// Retry without a custom system context.  A user may want to add
			// a custom platform (see #3511).
			if ref, _, storeErr = util.FindImage(store, "", nil, imageSpec); storeErr != nil {
				logrus.Errorf("Error while trying to find image on local storage: %v", storeErr)
				return err
			}
			instanceDigest, storeErr = list.Add(getContext(), systemContext, ref, opts.all)
			if storeErr != nil {
				logrus.Errorf("Error while trying to add on manifest list: %v", storeErr)
				return err
			}
		}
	}

	if opts.os != "" {
		if err := list.SetOS(instanceDigest, opts.os); err != nil {
			return err
		}
	}
	if opts.osVersion != "" {
		if err := list.SetOSVersion(instanceDigest, opts.osVersion); err != nil {
			return err
		}
	}
	if len(opts.osFeatures) != 0 {
		if err := list.SetOSFeatures(instanceDigest, opts.osFeatures); err != nil {
			return err
		}
	}
	if opts.arch != "" {
		if err := list.SetArchitecture(instanceDigest, opts.arch); err != nil {
			return err
		}
	}
	if opts.variant != "" {
		if err := list.SetVariant(instanceDigest, opts.variant); err != nil {
			return err
		}
	}
	if len(opts.features) != 0 {
		if err := list.SetFeatures(instanceDigest, opts.features); err != nil {
			return err
		}
	}
	if len(opts.annotations) != 0 {
		annotations := make(map[string]string)
		for _, annotationSpec := range opts.annotations {
			k, v, ok := strings.Cut(annotationSpec, "=")
			if !ok {
				return fmt.Errorf(`no "=" found in annotation %q`, annotationSpec)
			}
			annotations[k] = v
		}
		if err := list.SetAnnotations(&instanceDigest, annotations); err != nil {
			return err
		}
	}

	updatedListID, err := list.SaveToImage(store, manifestList.ID(), nil, "")
	if err == nil {
		fmt.Printf("%s: %s\n", updatedListID, instanceDigest.String())
	}

	return err
}

func manifestRemoveCmd(c *cobra.Command, args []string, _ manifestRemoveOpts) error {
	listImageSpec := ""
	var instanceDigest digest.Digest
	var instanceSpec string
	switch len(args) {
	case 0, 1:
		return errors.New("at least a list image and one or more instance digests must be specified")
	case 2:
		listImageSpec = args[0]
		if listImageSpec == "" {
			return fmt.Errorf(`invalid image name "%s"`, args[0])
		}
		instanceSpec = args[1]
		if instanceSpec == "" {
			return fmt.Errorf(`invalid instance "%s"`, args[1])
		}
	default:
		return errors.New("at least two arguments are necessary: list and digest of instance to remove from list")
	}

	store, err := getStore(c)
	if err != nil {
		return err
	}

	systemContext, err := parse.SystemContextFromOptions(c)
	if err != nil {
		return fmt.Errorf("building system context: %w", err)
	}

	runtime, err := libimage.RuntimeFromStore(store, &libimage.RuntimeOptions{SystemContext: systemContext})
	if err != nil {
		return err
	}
	manifestList, err := runtime.LookupManifestList(listImageSpec)
	if err != nil {
		return err
	}
	_, list, err := manifests.LoadFromImage(store, manifestList.ID())
	if err != nil {
		return err
	}
	d, err := list.InstanceByFile(instanceSpec)
	if err != nil {
		instanceRef, err := alltransports.ParseImageName(instanceSpec)
		if err != nil {
			if instanceRef, err = alltransports.ParseImageName(util.DefaultTransport + instanceSpec); err != nil {
				if instanceRef, _, err = util.FindImage(store, "", systemContext, instanceSpec); err != nil {
					return fmt.Errorf(`invalid instance "%s": %v`, instanceSpec, err)
				}
			}
		}
		ctx := getContext()
		instanceImg, err := instanceRef.NewImageSource(ctx, systemContext)
		if err != nil {
			return fmt.Errorf("reading image instance: %w", err)
		}
		defer instanceImg.Close()
		manifestBytes, _, err := image.UnparsedInstance(instanceImg, nil).Manifest(ctx)
		if err != nil {
			return fmt.Errorf("reading image instance manifest: %w", err)
		}
		d, err = manifest.Digest(manifestBytes)
		if err != nil {
			return fmt.Errorf("digesting image instance manifest: %w", err)
		}
	}
	instanceDigest = d
	if err := manifestList.RemoveInstance(instanceDigest); err != nil {
		return err
	}

	fmt.Printf("%s: %s\n", manifestList.ID(), instanceDigest.String())

	return nil
}

func manifestRmCmd(c *cobra.Command, args []string) error {
	store, err := getStore(c)
	if err != nil {
		return err
	}

	systemContext, err := parse.SystemContextFromOptions(c)
	if err != nil {
		return fmt.Errorf("building system context: %w", err)
	}

	runtime, err := libimage.RuntimeFromStore(store, &libimage.RuntimeOptions{SystemContext: systemContext})
	if err != nil {
		return err
	}

	options := &libimage.RemoveImagesOptions{
		Filters:        []string{"readonly=false"},
		LookupManifest: true,
	}
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

func manifestAnnotateCmd(c *cobra.Command, args []string, opts manifestAnnotateOpts) error {
	listImageSpec := ""
	instanceSpec := ""
	if opts.subject != "" {
		// this option is always only working at the index level
		opts.index = true
	}
	switch len(args) {
	case 0:
		return errors.New("at least a list image must be specified")
	case 1:
		listImageSpec = args[0]
		if listImageSpec == "" {
			return fmt.Errorf(`invalid image name "%s"`, args[0])
		}
		if !opts.index {
			return errors.New(`expected an instance digest, image name, or artifact name`)
		}
	case 2:
		listImageSpec = args[0]
		if listImageSpec == "" {
			return fmt.Errorf(`invalid image name "%s"`, args[0])
		}
		if opts.index {
			return fmt.Errorf(`did not expect image or artifact name "%s" when modifying the entire index`, args[1])
		}
		instanceSpec = args[1]
		if instanceSpec == "" {
			return fmt.Errorf(`invalid instance digest, image name, or artifact name "%s"`, instanceSpec)
		}
	default:
		return errors.New("expected either a list name and --index or a list name and an image digest or image name or artifact name")
	}

	store, err := getStore(c)
	if err != nil {
		return err
	}

	systemContext, err := parse.SystemContextFromOptions(c)
	if err != nil {
		return fmt.Errorf("building system context: %w", err)
	}
	runtime, err := libimage.RuntimeFromStore(store, &libimage.RuntimeOptions{SystemContext: systemContext})
	if err != nil {
		return err
	}

	manifestList, err := runtime.LookupManifestList(listImageSpec)
	if err != nil {
		return err
	}

	locker, err := manifests.LockerForImage(store, manifestList.ID())
	if err != nil {
		return err
	}
	locker.Lock()
	defer locker.Unlock()

	_, list, err := manifests.LoadFromImage(store, manifestList.ID())
	if err != nil {
		return err
	}

	var instance digest.Digest
	if !opts.index {
		d, err := list.InstanceByFile(instanceSpec)
		if err != nil {
			instanceRef, err := alltransports.ParseImageName(instanceSpec)
			if err != nil {
				if instanceRef, err = alltransports.ParseImageName(util.DefaultTransport + instanceSpec); err != nil {
					// check if the local image exists
					if instanceRef, _, err = util.FindImage(store, "", systemContext, instanceSpec); err != nil {
						return fmt.Errorf(`invalid instance "%s": %v`, instanceSpec, err)
					}
				}
			}
			ctx := getContext()
			instanceImg, err := instanceRef.NewImageSource(ctx, systemContext)
			if err != nil {
				return fmt.Errorf("reading image instance: %w", err)
			}
			defer instanceImg.Close()
			manifestBytes, _, err := image.UnparsedInstance(instanceImg, nil).Manifest(ctx)
			if err != nil {
				return fmt.Errorf("reading image instance manifest: %w", err)
			}
			d, err = manifest.Digest(manifestBytes)
			if err != nil {
				return fmt.Errorf("digesting image instance manifest: %w", err)
			}
		}
		instance = d
	}

	if opts.os != "" {
		if opts.index {
			return fmt.Errorf("--index is not compatible with --os")
		}
		if err := list.SetOS(instance, opts.os); err != nil {
			return err
		}
	}
	if opts.osVersion != "" {
		if opts.index {
			return fmt.Errorf("--index is not compatible with --os-version")
		}
		if err := list.SetOSVersion(instance, opts.osVersion); err != nil {
			return err
		}
	}
	if len(opts.osFeatures) != 0 {
		if opts.index {
			return fmt.Errorf("--index is not compatible with --os-features")
		}
		if err := list.SetOSFeatures(instance, opts.osFeatures); err != nil {
			return err
		}
	}
	if opts.arch != "" {
		if opts.index {
			return fmt.Errorf("--index is not compatible with --arch")
		}
		if err := list.SetArchitecture(instance, opts.arch); err != nil {
			return err
		}
	}
	if opts.variant != "" {
		if opts.index {
			return fmt.Errorf("--index is not compatible with --variant")
		}
		if err := list.SetVariant(instance, opts.variant); err != nil {
			return err
		}
	}
	if len(opts.features) != 0 {
		if opts.index {
			return fmt.Errorf("--index is not compatible with --features")
		}
		if err := list.SetFeatures(instance, opts.features); err != nil {
			return err
		}
	}
	if len(opts.annotations) != 0 {
		annotations := make(map[string]string)
		for _, annotationSpec := range opts.annotations {
			k, v, ok := strings.Cut(annotationSpec, "=")
			if !ok {
				return fmt.Errorf(`no "=" found in annotation %q`, annotationSpec)
			}
			annotations[k] = v
		}
		var instanceDigest *digest.Digest
		if !opts.index {
			instanceDigest = &instance
		}
		if err := list.SetAnnotations(instanceDigest, annotations); err != nil {
			return err
		}
	}
	if opts.subject != "" {
		subjectRef, err := alltransports.ParseImageName(opts.subject)
		if err != nil {
			if subjectRef, err = alltransports.ParseImageName(util.DefaultTransport + opts.subject); err != nil {
				// check if the local image exists
				if subjectRef, _, err = util.FindImage(store, "", systemContext, opts.subject); err != nil {
					logrus.Errorf("Error while trying to parse artifact subject: %v", err)
					return err
				}
			}
		}
		ctx := getContext()
		src, err := subjectRef.NewImageSource(ctx, systemContext)
		if err != nil {
			logrus.Errorf("Error while trying to read artifact subject: %v", err)
			return err
		}
		defer src.Close()

		manifestBytes, manifestType, err := image.UnparsedInstance(src, nil).Manifest(ctx)
		if err != nil {
			logrus.Errorf("Error while trying to read artifact subject manifest: %v", err)
			return err
		}
		manifestDigest, err := manifest.Digest(manifestBytes)
		if err != nil {
			logrus.Errorf("Error while trying to digest artifact subject manifest: %v", err)
			return err
		}
		descriptor := imgspecv1.Descriptor{
			MediaType: manifestType,
			Size:      int64(len(manifestBytes)),
			Digest:    manifestDigest,
		}
		if err := list.SetSubject(&descriptor); err != nil {
			return err
		}
	}

	updatedListID, err := list.SaveToImage(store, manifestList.ID(), nil, "")
	if err == nil {
		if instance == "" {
			fmt.Printf("%s\n", updatedListID)
		} else {
			fmt.Printf("%s: %s\n", updatedListID, instance.String())
		}
	}

	return nil
}

func manifestInspectCmd(c *cobra.Command, args []string, opts manifestInspectOpts) error {
	if c.Flag("authfile").Changed {
		if err := auth.CheckAuthFile(opts.authfile); err != nil {
			return err
		}
	}
	imageSpec := ""
	switch len(args) {
	case 0:
		return errors.New("at least a source list ID must be specified")
	case 1:
		imageSpec = args[0]
		if imageSpec == "" {
			return fmt.Errorf(`invalid image name "%s"`, imageSpec)
		}
	default:
		return errors.New("only one argument is necessary for inspect: an image name")
	}

	store, err := getStore(c)
	if err != nil {
		return err
	}

	systemContext, err := parse.SystemContextFromOptions(c)
	if err != nil {
		return fmt.Errorf("building system context: %w", err)
	}

	return manifestInspect(getContext(), store, systemContext, imageSpec)
}

func manifestInspect(ctx context.Context, store storage.Store, systemContext *types.SystemContext, imageSpec string) error {
	runtime, err := libimage.RuntimeFromStore(store, &libimage.RuntimeOptions{SystemContext: systemContext})
	if err != nil {
		return err
	}

	printManifest := func(manifest []byte) error {
		var b bytes.Buffer
		err = json.Indent(&b, manifest, "", "    ")
		if err != nil {
			return fmt.Errorf("rendering manifest for display: %w", err)
		}

		fmt.Printf("%s\n", b.String())
		return nil
	}

	// Before doing a remote lookup, attempt to resolve the manifest list
	// locally.
	manifestList, err := runtime.LookupManifestList(imageSpec)
	if err == nil {
		schema2List, err := manifestList.Inspect()
		if err != nil {
			return err
		}

		rawSchema2List, err := json.Marshal(schema2List)
		if err != nil {
			return err
		}

		return printManifest(rawSchema2List)
	}
	if !errors.Is(err, storage.ErrImageUnknown) && !errors.Is(err, libimage.ErrNotAManifestList) {
		return err
	}

	// TODO: at some point `libimage` should support resolving manifests
	// like that.  Similar to `libimage.Runtime.LookupImage` we could
	// implement a `*.LookupImageIndex`.
	refs, err := util.ResolveNameToReferences(store, systemContext, imageSpec)
	if err != nil {
		logrus.Debugf("error parsing reference to image %q: %v", imageSpec, err)
	}

	if ref, _, err := util.FindImage(store, "", systemContext, imageSpec); err == nil {
		refs = append(refs, ref)
	} else if ref, err := alltransports.ParseImageName(imageSpec); err == nil {
		refs = append(refs, ref)
	}
	if len(refs) == 0 {
		return fmt.Errorf("locating images with names %v", imageSpec)
	}

	var (
		latestErr error
		result    []byte
	)

	appendErr := func(e error) {
		if latestErr == nil {
			latestErr = e
		} else {
			latestErr = fmt.Errorf("tried %v: %w", e, latestErr)
		}
	}

	for _, ref := range refs {
		logrus.Debugf("Testing reference %q for possible manifest", transports.ImageName(ref))

		src, err := ref.NewImageSource(ctx, systemContext)
		if err != nil {
			appendErr(fmt.Errorf("reading image %q: %w", transports.ImageName(ref), err))
			continue
		}
		defer src.Close()

		manifestBytes, manifestType, err := image.UnparsedInstance(src, nil).Manifest(ctx)
		if err != nil {
			appendErr(fmt.Errorf("loading manifest %q: %w", transports.ImageName(ref), err))
			continue
		}

		if !manifest.MIMETypeIsMultiImage(manifestType) {
			appendErr(fmt.Errorf("manifest is of type %s (not a list type)", manifestType))
			continue
		}
		result = manifestBytes
		break
	}
	if len(result) == 0 && latestErr != nil {
		return latestErr
	}

	return printManifest(result)
}

func manifestPushCmd(c *cobra.Command, args []string, opts pushOptions) error {
	if err := auth.CheckAuthFile(opts.authfile); err != nil {
		return err
	}

	listImageSpec := ""
	destSpec := ""
	switch len(args) {
	case 0:
		return errors.New("at least a source list ID must be specified")
	case 1:
		listImageSpec = args[0]
		destSpec = "docker://" + listImageSpec
	case 2:
		listImageSpec = args[0]
		destSpec = args[1]
	default:
		return errors.New("only two arguments are necessary to push: source and destination")
	}
	if listImageSpec == "" {
		return fmt.Errorf(`invalid image name "%s"`, listImageSpec)
	}
	if destSpec == "" {
		return fmt.Errorf(`invalid image name "%s"`, destSpec)
	}
	store, err := getStore(c)
	if err != nil {
		return err
	}
	systemContext, err := parse.SystemContextFromOptions(c)
	if err != nil {
		return fmt.Errorf("building system context: %w", err)
	}
	if opts.compressionFormat != "" {
		algo, err := compression.AlgorithmByName(opts.compressionFormat)
		if err != nil {
			return err
		}
		systemContext.CompressionFormat = &algo
	}
	if c.Flag("compression-level").Changed {
		systemContext.CompressionLevel = &opts.compressionLevel
	}
	if c.Flag("compression-format").Changed {
		if !c.Flag("force-compression").Changed {
			// If `compression-format` is set and no value for `--force-compression`
			// is selected then defaults to `true`.
			opts.forceCompressionFormat = true
		}
	}
	if c.Flag("timestamp").Changed {
		opts.timestampSet = true
	}

	return manifestPush(systemContext, store, listImageSpec, destSpec, opts)
}

func manifestPush(systemContext *types.SystemContext, store storage.Store, listImageSpec, destSpec string, opts pushOptions) error {
	runtime, err := libimage.RuntimeFromStore(store, &libimage.RuntimeOptions{SystemContext: systemContext})
	if err != nil {
		return err
	}

	manifestList, err := runtime.LookupManifestList(listImageSpec)
	if err != nil {
		return err
	}

	locker, err := manifests.LockerForImage(store, manifestList.ID())
	if err != nil {
		return err
	}
	locker.Lock()
	defer locker.Unlock()

	_, list, err := manifests.LoadFromImage(store, manifestList.ID())
	if err != nil {
		return err
	}

	dest, err := alltransports.ParseImageName(destSpec)
	if err != nil {
		return err
	}

	var manifestType string
	if opts.format != "" {
		switch opts.format {
		case "oci":
			manifestType = imgspecv1.MediaTypeImageManifest
		case "v2s2", "docker":
			manifestType = manifest.DockerV2Schema2MediaType
		default:
			return fmt.Errorf("unknown format %q. Choose on of the supported formats: 'oci' or 'v2s2'", opts.format)
		}
	}

	retry := uint(opts.retry)

	options := manifests.PushOptions{
		Store:                  store,
		SystemContext:          systemContext,
		ImageListSelection:     cp.CopySpecificImages,
		Instances:              nil,
		RemoveSignatures:       opts.removeSignatures,
		SignBy:                 opts.signBy,
		ManifestType:           manifestType,
		AddCompression:         opts.addCompression,
		ForceCompressionFormat: opts.forceCompressionFormat,
		MaxRetries:             &retry,
	}
	if opts.retryDelay != "" {
		retryDelay, err := time.ParseDuration(opts.retryDelay)
		if err != nil {
			return fmt.Errorf("unable to parse retryDelay %q: %w", opts.retryDelay, err)
		}
		options.RetryDelay = &retryDelay
	}
	if opts.all {
		options.ImageListSelection = cp.CopyAllImages
	}
	if !opts.quiet {
		options.ReportWriter = os.Stderr
	}
	if opts.timestampSet {
		ts := time.Unix(opts.destTimestamp, 0).UTC()
		options.DestinationTimestamp = &ts
	}
	_, digest, err := list.Push(getContext(), dest, options)

	if err == nil && opts.rm {
		_, err = store.DeleteImage(manifestList.ID(), true)
	}

	if opts.digestfile != "" {
		if err = os.WriteFile(opts.digestfile, []byte(digest.String()), 0o644); err != nil {
			return util.GetFailureCause(err, fmt.Errorf("failed to write digest to file %q: %w", opts.digestfile, err))
		}
	}

	return err
}
