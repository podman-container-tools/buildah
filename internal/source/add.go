package source

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	specV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage/pkg/archive"
	"go.podman.io/storage/pkg/fileutils"
)

// AddOptions include data to alter certain knobs when adding a source artifact
// to a source image.
type AddOptions struct {
	// Annotations for the source artifact.
	Annotations []string
}

// annotations parses the specified annotations and transforms them into a map.
// A given annotation can be specified only once.
func (o *AddOptions) annotations() (map[string]string, error) {
	annotations := make(map[string]string)

	for _, unparsed := range o.Annotations {
		key, value, hasValue := strings.Cut(unparsed, "=")
		if !hasValue {
			return nil, fmt.Errorf("invalid annotation %q (expected format is \"key=value\")", unparsed)
		}
		if _, exists := annotations[key]; exists {
			return nil, fmt.Errorf("annotation %q specified more than once", key)
		}
		annotations[key] = value
	}

	return annotations, nil
}

// Add adds the specified source artifact at `artifactPath` to the source image
// at `sourcePath`.  Note that the artifact will be added as a gzip-compressed
// tar ball.  Add attempts to auto-tar and auto-compress only if necessary.
func Add(ctx context.Context, sourcePath string, artifactPath string, options AddOptions) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Let's first make sure `sourcePath` exists and that we can access it.
	if err := fileutils.Exists(sourcePath); err != nil {
		return err
	}

	annotations, err := options.annotations()
	if err != nil {
		return err
	}

	ociDest, err := openOrCreateSourceImage(ctx, sourcePath)
	if err != nil {
		return err
	}
	defer ociDest.Close()

	tarStream, err := archive.TarWithOptions(artifactPath, &archive.TarOptions{Compression: archive.Gzip})
	if err != nil {
		return fmt.Errorf("creating compressed tar stream: %w", err)
	}

	info := types.BlobInfo{
		Size: -1, // "unknown": we'll get that information *after* adding
	}
	addedBlob, err := ociDest.PutBlob(ctx, tarStream, info, nil, false)
	if err != nil {
		return fmt.Errorf("adding source artifact: %w", err)
	}

	// Add the new layers to the source image's manifest.
	manifest, oldManifestDigest, _, err := readManifestFromOCIPath(ctx, sourcePath)
	if err != nil {
		return err
	}
	manifest.Layers = append(manifest.Layers,
		specV1.Descriptor{
			MediaType:   specV1.MediaTypeImageLayerGzip,
			Digest:      addedBlob.Digest,
			Size:        addedBlob.Size,
			Annotations: annotations,
		},
	)
	manifestDigest, manifestSize, err := writeManifest(ctx, manifest, ociDest)
	if err != nil {
		return err
	}

	// Now, as we've written the updated manifest, we can delete the
	// previous one.  `types.ImageDestination` doesn't expose a high-level
	// API to manage multi-manifest destination, so we need to do it
	// manually.  Not an issue, since paths are predictable for an OCI
	// layout.
	if err := removeBlob(oldManifestDigest, sourcePath); err != nil {
		return fmt.Errorf("removing old manifest: %w", err)
	}

	manifestDescriptor := specV1.Descriptor{
		MediaType: specV1.MediaTypeImageManifest,
		Digest:    *manifestDigest,
		Size:      manifestSize,
	}
	if err := updateIndexWithNewManifestDescriptor(&manifestDescriptor, sourcePath); err != nil {
		return err
	}

	return nil
}

func updateIndexWithNewManifestDescriptor(manifest *specV1.Descriptor, sourcePath string) error {
	index := specV1.Index{}
	indexPath := filepath.Join(sourcePath, "index.json")

	rawData, err := os.ReadFile(indexPath)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(rawData, &index); err != nil {
		return err
	}

	index.Manifests = []specV1.Descriptor{*manifest}
	rawData, err = json.Marshal(&index)
	if err != nil {
		return err
	}

	return os.WriteFile(indexPath, rawData, 0o644)
}
