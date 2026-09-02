package source

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/opencontainers/go-digest"
	specV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"go.podman.io/image/v5/image"
	"go.podman.io/image/v5/oci/layout"
	"go.podman.io/image/v5/types"
)

// MediaTypeSourceImageConfig specifies the media type of a source-image config.
const MediaTypeSourceImageConfig = "application/vnd.oci.source.image.config.v1+json"

// ImageConfig specifies the config of a source image.
type ImageConfig struct {
	// Created is the combined date and time at which the layer was created, formatted as defined by RFC 3339, section 5.6.
	Created *time.Time `json:"created,omitempty"`

	// Author is the author of the source image.
	Author string `json:"author,omitempty"`
}

// writeManifest writes the specified OCI `manifest` to the source image at
// `ociDest`.
func writeManifest(ctx context.Context, manifest *specV1.Manifest, ociDest types.ImageDestination) (*digest.Digest, int64, error) {
	select {
	case <-ctx.Done():
		return nil, -1, ctx.Err()
	default:
	}

	rawData, err := json.Marshal(&manifest)
	if err != nil {
		return nil, -1, fmt.Errorf("marshalling manifest: %w", err)
	}

	if err := ociDest.PutManifest(ctx, rawData, nil); err != nil {
		return nil, -1, fmt.Errorf("writing manifest: %w", err)
	}

	manifestDigest := digest.FromBytes(rawData)
	return &manifestDigest, int64(len(rawData)), nil
}

// readManifestFromImageSource reads the manifest from the specified image
// source.  Note that the manifest is expected to be an OCI v1 manifest.
func readManifestFromImageSource(ctx context.Context, src types.ImageSource) (*specV1.Manifest, *digest.Digest, int64, error) {
	select {
	case <-ctx.Done():
		return nil, nil, -1, ctx.Err()
	default:
	}

	rawData, mimeType, err := image.UnparsedInstance(src, nil).Manifest(ctx)
	if err != nil {
		return nil, nil, -1, err
	}
	if mimeType != specV1.MediaTypeImageManifest {
		return nil, nil, -1, fmt.Errorf("image %q is of type %q (expected: %q)", strings.TrimPrefix(src.Reference().StringWithinTransport(), "//"), mimeType, specV1.MediaTypeImageManifest)
	}

	manifest := specV1.Manifest{}
	if err := json.Unmarshal(rawData, &manifest); err != nil {
		return nil, nil, -1, fmt.Errorf("reading manifest: %w", err)
	}

	manifestDigest := digest.FromBytes(rawData)
	return &manifest, &manifestDigest, int64(len(rawData)), nil
}

// readManifestFromOCIPath returns the manifest of the specified source image
// at `sourcePath` along with its digest.  The digest can later on be used to
// locate the manifest on the file system.
func readManifestFromOCIPath(ctx context.Context, sourcePath string) (*specV1.Manifest, *digest.Digest, int64, error) {
	select {
	case <-ctx.Done():
		return nil, nil, -1, ctx.Err()
	default:
	}

	ociRef, err := layout.ParseReference(sourcePath)
	if err != nil {
		return nil, nil, -1, err
	}

	ociSource, err := ociRef.NewImageSource(ctx, &types.SystemContext{})
	if err != nil {
		return nil, nil, -1, err
	}
	defer ociSource.Close()

	return readManifestFromImageSource(ctx, ociSource)
}

// openOrCreateSourceImage returns an OCI types.ImageDestination of the
// specified `sourcePath`.  Note that if the path doesn't exist, it'll be
// created along with the OCI directory layout.
func openOrCreateSourceImage(ctx context.Context, sourcePath string) (types.ImageDestination, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	ociRef, err := layout.ParseReference(sourcePath)
	if err != nil {
		return nil, err
	}

	// This will implicitly create an OCI directory layout at `path`.
	return ociRef.NewImageDestination(ctx, &types.SystemContext{})
}

// addConfig adds `config` to `ociDest` and returns the corresponding blob
// info.
func addConfig(ctx context.Context, config *ImageConfig, ociDest types.ImageDestination) (*types.BlobInfo, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	rawData, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("marshalling config: %w", err)
	}

	info := types.BlobInfo{
		Size: -1, // "unknown": we'll get that information *after* adding
	}
	addedBlob, err := ociDest.PutBlob(ctx, bytes.NewReader(rawData), info, nil, true)
	if err != nil {
		return nil, fmt.Errorf("adding config: %w", err)
	}

	return &addedBlob, nil
}

// removeBlob removes the specified `blob` from the source image at `sourcePath`.
func removeBlob(blob *digest.Digest, sourcePath string) error {
	blobPath := filepath.Join(filepath.Join(sourcePath, "blobs/sha256"), blob.Encoded())
	return os.Remove(blobPath)
}
