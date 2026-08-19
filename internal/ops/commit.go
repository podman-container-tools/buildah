package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"time"

	_ "github.com/moby/docker-image-spec/specs-go/v1" // docker+oci format
	digest "github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/identity"
	imgspec "github.com/opencontainers/image-spec/specs-go"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"go.podman.io/buildah/copier"
	"go.podman.io/buildah/define"
	"go.podman.io/buildah/docker"
	"go.podman.io/buildah/internal/tmpdir"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/ioutils"
)

// configDigestToImageID converts a config digest to an image ID.
// FIXME: for algorithms other than SHA256, the method used here is just a guess, and this function
// should be replaced, and callers to this function updated, once we figure out how we're going to
// handle other digest algorithms.
func configDigestToImageID(configDigest digest.Digest) string {
	if configDigest.Algorithm() == digest.SHA256 {
		return configDigest.Encoded()
	}
	return digest.SHA256.FromBytes([]byte(configDigest.String())).Encoded()
}

// CommitOptions provides options for the Commit() function.  Most of what might have been
// provided by Commit() before should now be handled by the Export() function.
type CommitOptions struct {
	Names            []string
	ImageAnnotations map[string]string // annotations to set in the image's manifest
	OCIMediaTypes    bool              // use OCI MediaType values instead of v2s2 MediaType values
	DigestAlgorithm  digest.Algorithm  // use when computing diffIDs for the config and layer digests for the manifest
}

// buildImageInfoWithLayerSpecs() builds the configuration and manifest blobs for the image
// described by the passed-in arguments, using the passed-in configBlob as a starting point, but
// overwriting its rootfs field.
// Returns the digest of the configuration blob and an ImageOptions structure
// which includes the configuration and manifest blobs as big data items.
// The configuration and manifest will describe the layers in layerSpecs.
func buildImageInfoWithLayerSpecs(configBlob []byte, layerSpecs LayerSpecs, layerType, configType, manifestType string, options CommitOptions) (imageID string, configDigest digest.Digest, imageOptions *storage.ImageOptions, err error) {
	// Start with the passed-in image configuration, whatever its format.
	if len(configBlob) == 0 {
		configBlob = []byte("{}")
	}
	config := make(map[string]any)
	if err := json.Unmarshal(configBlob, &config); err != nil {
		return "", "", nil, fmt.Errorf("decoding original configuration blob: %w", err)
	}

	// Build the config blob with this layer chain's diffIDs as a RootFS.
	var configDiffIDs []digest.Digest
	var configDiffSizes []int64
	for i, layerSpec := range layerSpecs {
		switch layerSpec.Type {
		case LayerSourceTypeLayer:
			layerID := layerSpec.LayerID
			diffID := layerSpec.LayerDigest
			if err := diffID.Validate(); err != nil {
				return "", "", nil, fmt.Errorf("diff ID for layer %q (layer chain %d) not known: %w", layerID, i, err)
			}
			var diffSize int64
			if layerSpec.LayerDiffSize != nil {
				diffSize = *layerSpec.LayerDiffSize
			} else {
				return "", "", nil, fmt.Errorf("diff size for layer %q (layer chain %d) not known", layerID, i)
			}
			configDiffIDs = append(configDiffIDs, diffID)
			configDiffSizes = append(configDiffSizes, diffSize)
		case LayerSourceTypeDirectory:
			directory := layerSpec.Directory
			diffID := layerSpec.DirectoryDigest
			if err := diffID.Validate(); err != nil {
				return "", "", nil, fmt.Errorf("diff ID for directory %q (layer chain %d) not known: %w", directory, i, err)
			}
			var diffSize int64
			if layerSpec.DirectoryDiffSize != nil {
				diffSize = *layerSpec.DirectoryDiffSize
			} else {
				return "", "", nil, fmt.Errorf("diff size for directory %q (layer chain %d) not known", directory, i)
			}
			configDiffIDs = append(configDiffIDs, diffID)
			configDiffSizes = append(configDiffSizes, diffSize)
		default:
			return "", "", nil, fmt.Errorf("unhandled layer source type %s", layerSpec.Type.String())
		}
	}
	config["rootfs"] = imgspecv1.RootFS{
		Type:    docker.TypeLayers,
		DiffIDs: configDiffIDs,
	}
	config[define.Package] = define.Version // a little something non-standard that'll be filtered out if we try to "export"
	configForStorage, err := json.Marshal(config)
	if err != nil {
		return "", "", nil, fmt.Errorf("re-encoding configuration blob with updated layer information: %w", err)
	}
	configDigest = options.DigestAlgorithm.FromBytes(configForStorage)

	// Build a manifest using the just-marshaled config.
	rawManifest := imgspecv1.Manifest{
		Versioned: imgspec.Versioned{
			SchemaVersion: 2,
		},
		MediaType: manifestType,
		Config: imgspecv1.Descriptor{
			Digest:    configDigest,
			Size:      int64(len(configForStorage)),
			MediaType: configType,
		},
		Annotations: maps.Clone(options.ImageAnnotations),
	}
	for i, diffID := range configDiffIDs {
		rawManifest.Layers = append(rawManifest.Layers, imgspecv1.Descriptor{
			Digest:      diffID,
			Size:        configDiffSizes[i],
			MediaType:   layerType,
			Annotations: layerSpecs[i].Annotations,
		})
	}
	manifestForStorage, err := json.Marshal(rawManifest)
	if err != nil {
		return "", "", nil, fmt.Errorf("encoding manifest for image: %w", err)
	}
	manifestForStorageDigest := options.DigestAlgorithm.FromBytes(manifestForStorage)

	// Set up the big data items we're going to want to save along with the new image record.
	imageOptions = &storage.ImageOptions{
		CreationDate: time.Now(),
		BigData: []storage.ImageBigDataOption{
			{
				Key:    configDigest.String(),
				Data:   configForStorage,
				Digest: configDigest,
			},
			{
				Key:    storage.ImageDigestManifestBigDataNamePrefix + "-" + manifestForStorageDigest.String(),
				Data:   manifestForStorage,
				Digest: manifestForStorageDigest,
			},
			{
				Key:    storage.ImageDigestManifestBigDataNamePrefix,
				Data:   manifestForStorage,
				Digest: manifestForStorageDigest,
			},
		},
	}

	return configDigestToImageID(configDigest), configDigest, imageOptions, nil
}

// validateMediaTypes supplies a corresponding layer, config, or manifest
// MediaType when we know at least one such value, or a default if we know none
// of them
func validateMediaTypes(layerType, configType, manifestType string) (string, string, string, error) {
	if configType == "" && manifestType == "" && layerType == "" {
		layerType, configType, manifestType = imgspecv1.MediaTypeImageLayer, imgspecv1.MediaTypeImageConfig, imgspecv1.MediaTypeImageManifest
	}
	if layerType == "" {
		switch manifestType {
		case imgspecv1.MediaTypeImageManifest:
			layerType = imgspecv1.MediaTypeImageLayer
		case manifest.DockerV2Schema2MediaType:
			layerType = manifest.DockerV2SchemaLayerMediaTypeUncompressed
		case "":
			switch configType {
			case imgspecv1.MediaTypeImageConfig:
				layerType = imgspecv1.MediaTypeImageLayer
			case manifest.DockerV2Schema2ConfigMediaType:
				layerType = manifest.DockerV2SchemaLayerMediaTypeUncompressed
			default:
				return "", "", "", fmt.Errorf("no layerType specified, and could not guess based on no manifest type and config type %q", configType)
			}
		default:
			return "", "", "", fmt.Errorf("no layerType specified, and could not guess based on manifest type %q", manifestType)
		}
	}
	if configType == "" {
		switch manifestType {
		case imgspecv1.MediaTypeImageManifest:
			configType = imgspecv1.MediaTypeImageConfig
		case manifest.DockerV2Schema2MediaType:
			configType = manifest.DockerV2Schema2ConfigMediaType
		case "":
			switch layerType {
			case imgspecv1.MediaTypeImageLayer, imgspecv1.MediaTypeImageLayerGzip, imgspecv1.MediaTypeImageLayerZstd:
				configType = imgspecv1.MediaTypeImageConfig
			case manifest.DockerV2Schema2LayerMediaType, manifest.DockerV2SchemaLayerMediaTypeUncompressed, manifest.DockerV2SchemaLayerMediaTypeZstd:
				configType = manifest.DockerV2Schema2ConfigMediaType
			default:
				return "", "", "", fmt.Errorf("no configType specified, and could not guess based on no manifest type and layer type %q", layerType)
			}
		default:
			return "", "", "", fmt.Errorf("no configType specified, and could not guess based on manifest type %q", manifestType)
		}
	}
	if manifestType == "" {
		switch configType {
		case imgspecv1.MediaTypeImageConfig:
			manifestType = imgspecv1.MediaTypeImageManifest
		case manifest.DockerV2Schema2ConfigMediaType:
			manifestType = manifest.DockerV2Schema2MediaType
		case "":
			switch layerType {
			case imgspecv1.MediaTypeImageLayer, imgspecv1.MediaTypeImageLayerGzip, imgspecv1.MediaTypeImageLayerZstd:
				manifestType = imgspecv1.MediaTypeImageManifest
			case manifest.DockerV2Schema2LayerMediaType, manifest.DockerV2SchemaLayerMediaTypeUncompressed, manifest.DockerV2SchemaLayerMediaTypeZstd:
				manifestType = manifest.DockerV2Schema2MediaType
			default:
				return "", "", "", fmt.Errorf("no manifestType specified, and could not guess based on no config type and layer type %q", layerType)
			}
		default:
			return "", "", "", fmt.Errorf("no manifestType specified, and could not guess based on config type %q", configType)
		}
	}
	return layerType, configType, manifestType, nil
}

// BuildImageInfo creates the information that we'd need to commit a new image using the list of
// layers whose IDs or names or locations in the filesystem are in layerSpecs (in the order given),
// the passed-in configuration blob, and a manifest type.  The diffIDs for the layers will be
// calculated and implanted/replaced as we go, and represented in the manifest.
// Returns the top layer's ID, the digest of the config blob, image options which contain the config
// blob and manifest, and a list of layer IDs which can be removed on successful commit.
func buildImageInfo(ctx context.Context, sys *types.SystemContext, store storage.Store, layerSpecs LayerSpecs, configBlob []byte, layerType, configType, manifestType string, options CommitOptions) (imageID, leafLayer string, configDigest digest.Digest, imageOptions *storage.ImageOptions, disposableLayers []string, returnErr error) {
	var err error
	if configType == "" && manifestType == "" {
		if options.OCIMediaTypes {
			configType = imgspecv1.MediaTypeImageConfig
			manifestType = imgspecv1.MediaTypeImageManifest
		} else {
			configType = manifest.DockerV2Schema2ConfigMediaType
			manifestType = manifest.DockerV2Schema2MediaType
		}
	}
	if layerType, configType, manifestType, err = validateMediaTypes(layerType, configType, manifestType); err != nil {
		return "", "", "", nil, nil, err
	}
	if sys == nil {
		sys = &types.SystemContext{}
	}
	if options.DigestAlgorithm == "" {
		options.DigestAlgorithm = digest.Canonical
	}
	layerSpecs = slices.Clone(layerSpecs)
	// Walk the passed-in list of layer IDs.  They may all be parent-child links in a single
	// chain, but layers added for the sake of "COPY --link" and similar may just show up in
	// there anywhere.  This means we have to walk all of their chains to be sure we've found
	// them all.
	layers := make(map[string]*storage.Layer)
	for _, layerSpec := range layerSpecs {
		switch layerSpec.Type {
		case LayerSourceTypeLayer:
			thisLayerID := layerSpec.LayerID
			layerLineage := []string{thisLayerID}
			for thisLayerID != "" {
				// Only bother pulling up information about a given layer once.  If
				// we already visited it for a previous entry in the layerIDs list,
				// we don't need to do it again.
				if _, seen := layers[thisLayerID]; !seen {
					layer, err := store.Layer(thisLayerID)
					if err != nil {
						return "", "", "", nil, nil, fmt.Errorf("did not find layer with ID or name %q (lineage %v): %w", thisLayerID, layerLineage, err)
					}
					layers[thisLayerID] = layer
				}
				layer := layers[thisLayerID]
				// Walk up to the next link in this layer's parents chain.
				layerLineage = append([]string{thisLayerID}, layerLineage...)
				thisLayerID = layer.Parent
			}
		case LayerSourceTypeDirectory:
			// we're not caching any info about them, so nothing to do for directories
		default:
			return "", "", "", nil, nil, fmt.Errorf("unhandled layer source type %s", layerSpec.Type.String())
		}
	}
	// Construct the image info.
	var leafLayerID string
	if len(layerSpecs) == 0 {
		// "Image has no layers" is the simplest case.
		imageID, configDigest, imageOptions, err = buildImageInfoWithLayerSpecs(configBlob, layerSpecs, layerType, configType, manifestType, options)
		if err != nil {
			return "", "", "", nil, nil, fmt.Errorf("calculating info for image: %w", err)
		}
	} else {
		var temporaryDir string
		// setupTemporaryDir: a helper to create and clean up a temporary directory for
		// holding temporary copies of layer blobs.
		setupTemporaryDir := func() (string, error) {
			var setupTemporaryParent bool
			if temporaryDir == "" {
				temporaryDir = sys.BigFilesTemporaryDir
				setupTemporaryParent = true
			}
			if temporaryDir == "" {
				temporaryDir = tmpdir.GetTempDir()
				setupTemporaryParent = true
			}
			if temporaryDir == "" {
				return "", errors.New("unable to determine location for storing temporary files")
			}
			if setupTemporaryParent {
				temporaryDir, err = os.MkdirTemp(temporaryDir, "buildah-commit")
				if err != nil {
					return "", fmt.Errorf("creating temporary directory for layer blobs under %q: %w", sys.BigFilesTemporaryDir, err)
				}
			}
			return temporaryDir, nil
		}
		defer func() {
			if temporaryDir != "" {
				returnErr = errors.Join(returnErr, os.RemoveAll(temporaryDir))
			}
		}()
		// For each layer that should be a part of this image, make sure we have the
		// information required to build a manifest descriptor for it.
		diffSizesByLayerID := make(map[string]int64)
		diffIDsByLayerID := make(map[string]digest.Digest)
		diffSizesByDirectory := make(map[string]int64)
		diffIDsByDirectory := make(map[string]digest.Digest)
		diffSizesByDiffID := make(map[digest.Digest]int64)
		var layerChainDigests []digest.Digest
		var layerChain []string
		var chainParentID string
		for i := range layerSpecs {
			select {
			case <-ctx.Done():
				return "", "", "", nil, nil, ctx.Err()
			default:
			}
			var diffID digest.Digest
			var diffSize int64
			var getLayerFileName func() (string, error)

			switch layerSpecs[i].Type {
			case LayerSourceTypeLayer:
				layerID := layerSpecs[i].LayerID
				// saveLayerFile: a helper to save a layer's contents to a temporary
				// file and return the temporary file's name, the new file's digest,
				// and its size.
				saveLayerFile := func(layerID string) (string, digest.Digest, int64, error) {
					temporaryDir, err := setupTemporaryDir()
					if err != nil {
						return "", "", -1, err
					}
					f, err := os.CreateTemp(temporaryDir, "buildah-commit-")
					if err != nil {
						return "", "", -1, fmt.Errorf("creating temporary file to hold copy of diff for layer with ID or name %q: %w", layerID, err)
					}
					layerFileName := f.Name()
					var rc io.ReadCloser
					select {
					case <-ctx.Done():
						err = ctx.Err()
						return "", "", -1, errors.Join(fmt.Errorf("generating diff for layer with ID or name %q: %w", layerID, err), f.Close())
					case readCloserOrError, ok := <-diffContext(ctx, store, "", layerID, nil):
						if !ok {
							return "", "", -1, errors.Join(fmt.Errorf("generating diff for layer with ID or name %q: unknown error", layerID), f.Close())
						}
						if readCloserOrError.err != nil {
							return "", "", -1, errors.Join(fmt.Errorf("generating diff for layer with ID or name %q: %w", layerID, readCloserOrError.err), f.Close())
						}
						rc = readCloserOrError.readCloser
					}
					digester := options.DigestAlgorithm.Digester()
					counter := ioutils.NewWriteCounter(io.MultiWriter(digester.Hash(), f))
					n, copyErr := io.Copy(counter, rc)
					if n != counter.Count {
						panic("internal error: two byte counts that should be the same are not")
					}
					if copyErr != nil {
						copyErr = fmt.Errorf("digesting and counting size of diff for layer with ID or name %q: %w", layerID, copyErr)
					}
					return layerFileName, digester.Digest(), counter.Count, errors.Join(copyErr, rc.Close(), f.Close())
				}
				// a helper to save a layer's contents to a temporary file and
				// return the temporary file's name, cache the diff's digest and
				// size, and update the local maps that hold that information.
				getLayerFileName = func() (string, error) {
					tempLayerFileName, layerDigest, layerSize, err := saveLayerFile(layerID)
					if err != nil {
						return "", err
					}
					diffIDsByLayerID[layerID] = layerDigest
					diffSizesByLayerID[layerID] = layerSize
					diffSizesByDiffID[layerDigest] = layerSize
					getLayerFileName = func() (string, error) { return tempLayerFileName, nil } //nolint:unparam
					layerDigests := map[digest.Algorithm]digest.Digest{layerDigest.Algorithm(): layerDigest}
					return tempLayerFileName, writeLayerInfoCache(store, layerID, layerDigests, layerSize)
				}
				// Check if we already know the digest and size of the layer's
				// contents.
				layer := layers[layerID]
				var knowDiffID, knowDiffSize bool
				diffID, knowDiffID = diffIDsByLayerID[layer.ID]
				diffSize, knowDiffSize = diffSizesByLayerID[layer.ID]
				if knowDiffID && knowDiffSize {
					// Looks like we computed and cached these already for this
					// layer, probably a repeat of the layer?
					diffSizesByDiffID[diffID] = diffSize
				} else {
					// use prerecorded or cached values if we're not going to mess with the contents
					if layer.UncompressedDigest.Validate() == nil && layer.UncompressedSize != -1 {
						// These were already recorded for this layer by the storage
						// library, reused from another completed build?
						diffIDsByLayerID[layer.ID] = layer.UncompressedDigest
						diffSizesByLayerID[layer.ID] = layer.UncompressedSize
						diffSizesByDiffID[layer.UncompressedDigest] = layer.UncompressedSize
					} else {
						// If we previously cached these values for the layer
						// (either here or as part of another build), read them.
						if err := readLayerInfoCache(store, layer.ID, diffIDsByLayerID, diffSizesByLayerID, diffSizesByDiffID, nil, options.DigestAlgorithm); err != nil {
							// We didn't previously cache these values for the
							// layer, so we'll need to determine them for this
							// layer now instead of later.
							if _, err := getLayerFileName(); err != nil {
								return "", "", "", nil, nil, err
							}
						}
					}
				}
				var ok bool
				diffID, ok = diffIDsByLayerID[layerID]
				if !ok {
					return "", "", "", nil, nil, fmt.Errorf("should have a digest for layer %q, but don't?", layer.ID)
				}
				diffSize, ok = diffSizesByLayerID[layerID]
				if !ok {
					return "", "", "", nil, nil, fmt.Errorf("should have a diff size layer %q, but don't?", layer.ID)
				}
				layerSpecs[i].LayerDigest = diffID
				layerSpecs[i].LayerDiffSize = &diffSize
			case LayerSourceTypeDirectory:
				directory := layerSpecs[i].Directory
				// saveDirectory: a helper to save a directory's contents to a
				// temporary file and return the temporary file's name, the diff's
				// digest, and its size.
				saveDirectory := func() (string, digest.Digest, int64, error) {
					temporaryDir, err := setupTemporaryDir()
					if err != nil {
						return "", "", -1, err
					}
					f, err := os.CreateTemp(temporaryDir, "buildah-commit-")
					if err != nil {
						return "", "", -1, fmt.Errorf("creating temporary file to hold contents of %q: %w", directory, err)
					}
					archiveFileName := f.Name()
					digester := options.DigestAlgorithm.Digester()
					counter := ioutils.NewWriteCounter(io.MultiWriter(digester.Hash(), f))
					err = copier.Get(directory, directory, copier.GetOptions{}, []string{"."}, counter)
					if err != nil {
						return "", "", -1, errors.Join(fmt.Errorf("generating archive for directory at %q: %w", directory, err), f.Close())
					}
					return archiveFileName, digester.Digest(), counter.Count, f.Close()
				}
				// a helper to save a directory's contents to a temporary file and
				// return the temporary file's name and update the local maps that
				// hold that information.
				getLayerFileName = func() (string, error) {
					tempFileName, directoryDigest, directorySize, err := saveDirectory()
					if err != nil {
						return "", err
					}
					diffIDsByDirectory[directory] = directoryDigest
					diffSizesByDirectory[directory] = directorySize
					diffSizesByDiffID[diffID] = diffSize
					getLayerFileName = func() (string, error) { return tempFileName, nil } //nolint:unparam
					return tempFileName, nil
				}
				var knowDiffID, knowDiffSize bool
				diffID, knowDiffID = diffIDsByDirectory[directory]
				diffSize, knowDiffSize = diffSizesByDirectory[directory]
				if knowDiffID && knowDiffSize {
					// Looks like we computed and cached these already for this
					// directory, probably a repeat of its contents?
					diffSizesByDiffID[diffID] = diffSize
				} else {
					// We need to calculate these figures, so buffer the
					// directory's contents now instead of later.
					if _, err := getLayerFileName(); err != nil {
						return "", "", "", nil, nil, err
					}
				}
				var ok bool
				diffID, ok = diffIDsByDirectory[directory]
				if !ok {
					return "", "", "", nil, nil, fmt.Errorf("should have a digest for directory %q, but don't?", directory)
				}
				diffSize, ok = diffSizesByDirectory[directory]
				if !ok {
					return "", "", "", nil, nil, fmt.Errorf("should have a diff size directory %q, but don't?", directory)
				}
				layerSpecs[i].DirectoryDigest = diffID
				layerSpecs[i].DirectoryDiffSize = &diffSize
			}

			// Check if there is already a layer with the chain ID we'd expect to
			// generate from this layer's contents and the contents of its parents.
			layerChainDigests = append(layerChainDigests, diffID)
			chainID := identity.ChainID(layerChainDigests).Encoded()
			_, err = store.Layer(chainID)
			var layerFileName string
			if err != nil && errors.Is(err, storage.ErrLayerUnknown) {
				// There's not already a layer that has the right contents with the
				// right ID, so if we didn't already (re?)generate the layer diff,
				// we need to make that happen now.
				var saveErr error
				layerFileName, saveErr = getLayerFileName()
				if saveErr != nil {
					return "", "", "", nil, nil, saveErr
				}
				// Create a new layer with the right chain ID using the temporary
				// file with the layer's contents.
				layerOptions := &storage.LayerOptions{
					OriginalDigest:     diffID,
					OriginalSize:       &diffSize,
					UncompressedDigest: diffID,
				}
				f, openErr := os.Open(layerFileName)
				if openErr != nil {
					return "", "", "", nil, nil, fmt.Errorf("reading temporary copy of layer: %w", openErr)
				}
				layerDigests := map[digest.Algorithm]digest.Digest{diffID.Algorithm(): diffID}
				opt, optErr := encodeLayerInfoCache(chainID, layerDigests, diffSize)
				if optErr != nil {
					return "", "", "", nil, nil, fmt.Errorf("setting up to save cache info for layer %q: %w", chainID, errors.Join(optErr, f.Close()))
				}
				layerOptions.BigData = append(layerOptions.BigData, opt)
				select {
				case <-ctx.Done():
					err = errors.Join(ctx.Err(), f.Close())
				case layerAndSizeOrError, ok := <-putLayerContext(ctx, store, chainID, chainParentID, nil, "", false, layerOptions, f):
					if !ok {
						err = errors.Join(fmt.Errorf("writing new layer %q with contents from %q: unknown error", chainID, chainID), f.Close())
					} else if layerAndSizeOrError.err != nil {
						err = errors.Join(fmt.Errorf("writing new layer %q with contents from %q: %w", chainID, chainID, layerAndSizeOrError.err), f.Close())
					} else {
						newLayer := layerAndSizeOrError.layer
						err = errors.Join(readLayerInfoCache(store, newLayer.ID, diffIDsByLayerID, diffSizesByLayerID, diffSizesByDiffID, layerDigests, options.DigestAlgorithm), f.Close())
					}
				}
			} else if err == nil {
				layerDigests := map[digest.Algorithm]digest.Digest{diffID.Algorithm(): diffID}
				err = readLayerInfoCache(store, chainID, diffIDsByLayerID, diffSizesByLayerID, diffSizesByDiffID, layerDigests, options.DigestAlgorithm)
			}
			// If we buffered the diff contents to a file, we don't need them any more,
			// and not letting them pile up may be the difference between running out of
			// disk space, or not.
			if layerFileName != "" {
				if removeErr := os.Remove(layerFileName); removeErr != nil {
					return "", "", "", nil, nil, errors.Join(err, fmt.Errorf("cleaning up temporary copy of layer %q: %w", chainID, removeErr))
				}
			}
			if err != nil {
				return "", "", "", nil, nil, fmt.Errorf("ensuring layer with chain ID %q for layer: %w", chainID, err)
			}
			if layerSpecs[i].Type == LayerSourceTypeLayer && chainID != layerSpecs[i].LayerID {
				// we should clean up this temporary layer at some point
				disposableLayers = append(disposableLayers, layerSpecs[i].LayerID)
			}
			// Append this layer's ID to the layer list that we're reconstructing.
			layerChain = append(layerChain, chainID)
			chainParentID = chainID
		}
		imageID, configDigest, imageOptions, err = buildImageInfoWithLayerSpecs(configBlob, layerSpecs, layerType, configType, manifestType, options)
		if err != nil {
			return "", "", "", nil, nil, fmt.Errorf("calculating info for image with layer chain %v, diffIDsByLayerID %v, diffSizesByLayerID %v: %w", layerChain, diffIDsByLayerID, diffSizesByLayerID, err)
		}
		leafLayerID = layerChain[len(layerChain)-1]
	}
	return imageID, leafLayerID, configDigest, imageOptions, disposableLayers, nil
}

func digestDataItem(algorithmHint digest.Digest) func([]byte) (digest.Digest, error) {
	if algorithmHint.Validate() == nil {
		return func(item []byte) (digest.Digest, error) { return algorithmHint.Algorithm().FromBytes(item), nil }
	}
	return func(item []byte) (digest.Digest, error) { return digest.Canonical.FromBytes(item), nil }
}

// LayerSpecs are used to tell Commit() where to get layer contents.
type (
	LayerSpecs []LayerSpec
	LayerSpec  struct {
		LayerSource
		Annotations map[string]string // annotations for this layer's entry in the manifest
	}
)

// LayerSource describes where a single layer's contents should be found.
type LayerSource struct {
	Type              LayerSourceType // "LayerSourceTypeLayer" or "LayerSourceTypeDirectory"
	LayerID           string          // a layer in storage
	LayerDigest       digest.Digest   // the digest of a diff of that layer
	LayerDiffSize     *int64          // the size of the same diff of that layer
	Directory         string          // a directory out on the filesystem
	DirectoryDigest   digest.Digest   // the digest of an archive of that directory
	DirectoryDiffSize *int64          // the size of the same archive of that directory
}

// LayerSourceType is used to determine which fields in a LayerSource matter.
type LayerSourceType int

const (
	LayerSourceTypeLayer     = LayerSourceType(0) // a layer in storage
	LayerSourceTypeDirectory = LayerSourceType(1) // a directory on the filesystem
)

// String() returns the name of the type, or a generic "unknown layer" string
func (t LayerSourceType) String() string {
	switch t {
	case LayerSourceTypeLayer:
		return "layer"
	case LayerSourceTypeDirectory:
		return "directory"
	default:
		return fmt.Sprintf("unknown layer source type %d", int(t))
	}
}

// Valid() returns true if the type is a recognized value
func (t LayerSourceType) Valid() bool {
	switch t {
	case LayerSourceTypeLayer, LayerSourceTypeDirectory:
		return true
	default:
		return false
	}
}

// Commit creates or updates a new image record using contents of the layers whose IDs or names, and
// directories whose locations, are in layerSpecs (in the order given), the passed-in configuration
// blob, and a manifest type.  If the right layers happen to be in place, they will be reused.  The
// diffIDs for the layers will be calculated and implanted into the configuration, and represented
// in the manifest.
func Commit(ctx context.Context, sys *types.SystemContext, store storage.Store, layerSpecs LayerSpecs, configBlob []byte, layerType, configType, manifestType string, options CommitOptions) (*storage.Image, error) {
	if sys == nil {
		sys = &types.SystemContext{}
	}
	desiredImageID, leafLayerID, configDigest, imageOptions, disposableLayers, err := buildImageInfo(ctx, sys, store, layerSpecs, configBlob, layerType, configType, manifestType, options)
	if err != nil {
		return nil, fmt.Errorf("calculating image info: %w", err)
	}
	img, err := store.CreateImage(desiredImageID, nil, leafLayerID, "", imageOptions)
	if err != nil {
		if !errors.Is(err, storage.ErrDuplicateID) {
			return nil, fmt.Errorf("creating image record: %w", err)
		}
		if imageOptions.Metadata != "" {
			if err := store.SetMetadata(configDigest.Encoded(), imageOptions.Metadata); err != nil {
				return nil, fmt.Errorf("updating image metadata for %q: %w", configDigest.Encoded(), err)
			}
		}
		for _, item := range imageOptions.BigData {
			if err := store.SetImageBigData(configDigest.Encoded(), item.Key, item.Data, digestDataItem(configDigest)); err != nil {
				return nil, fmt.Errorf("updating image metadata for %q: %w", configDigest.Encoded(), err)
			}
		}
	} else {
		if img.ID != desiredImageID {
			return nil, fmt.Errorf("requested image ID not preserved: requested %q, received %q?", desiredImageID, img.ID)
		}
	}
	for _, disposableLayer := range slices.Backward(disposableLayers) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		if err := store.DeleteLayer(disposableLayer); err != nil {
			logrus.Debugf("not deleting layer %q: %v", disposableLayer, err)
		}
	}
	if len(options.Names) > 0 {
		if err := store.AddNames(configDigest.Encoded(), options.Names); err != nil {
			return nil, fmt.Errorf("adding names to image %q: %w", configDigest.Encoded(), err)
		}
	}
	return store.Image(configDigestToImageID(configDigest))
}

type layerAndSizeOrError struct {
	layer *storage.Layer
	n     int64
	err   error
}

type readCloserOrError struct {
	readCloser io.ReadCloser
	err        error
}

func putLayerContext(ctx context.Context, store storage.Store, chainID, chainParentID string, names []string, mountLabel string, writeable bool, options *storage.LayerOptions, diff io.Reader) <-chan layerAndSizeOrError {
	c := make(chan layerAndSizeOrError, 1)
	go func() {
		defer close(c)
		select {
		case <-ctx.Done():
			c <- layerAndSizeOrError{
				err: ctx.Err(),
			}
			return
		default:
		}
		newLayer, n, err := store.PutLayer(chainID, chainParentID, names, mountLabel, writeable, options, diff)
		if err != nil {
			c <- layerAndSizeOrError{
				err: err,
			}
			return
		}
		c <- layerAndSizeOrError{
			layer: newLayer,
			n:     n,
		}
	}()
	return c
}

func diffContext(ctx context.Context, store storage.Store, from, to string, diffOptions *storage.DiffOptions) <-chan readCloserOrError {
	c := make(chan readCloserOrError, 1)
	go func() {
		defer close(c)
		select {
		case <-ctx.Done():
			c <- readCloserOrError{
				err: ctx.Err(),
			}
			return
		default:
		}
		rc, err := store.Diff(from, to, diffOptions)
		if err != nil {
			c <- readCloserOrError{
				err: err,
			}
			return
		}
		c <- readCloserOrError{
			readCloser: rc,
		}
	}()
	return c
}
