package source

import (
	"context"
	"fmt"
	"time"

	spec "github.com/opencontainers/image-spec/specs-go"
	specV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"go.podman.io/storage/pkg/fileutils"
)

// CreateOptions includes data to alter certain knobs when creating a source
// image.
type CreateOptions struct {
	// Author is the author of the source image.
	Author string
	// TimeStamp controls whether a "created" timestamp is set or not.
	TimeStamp bool
}

// createdTime returns `time.Now()` if the options are configured to include a
// time stamp.
func (o *CreateOptions) createdTime() *time.Time {
	if !o.TimeStamp {
		return nil
	}
	now := time.Now()
	return &now
}

// Create creates an empty source image at the specified `sourcePath`.  Note
// that `sourcePath` must not exist.
func Create(ctx context.Context, sourcePath string, options CreateOptions) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if err := fileutils.Exists(sourcePath); err == nil {
		return fmt.Errorf("creating source image: %q already exists", sourcePath)
	}

	ociDest, err := openOrCreateSourceImage(ctx, sourcePath)
	if err != nil {
		return err
	}
	defer ociDest.Close()

	// Create and add a config.
	config := ImageConfig{
		Author:  options.Author,
		Created: options.createdTime(),
	}
	configBlob, err := addConfig(ctx, &config, ociDest)
	if err != nil {
		return err
	}

	// Create and write the manifest.
	manifest := specV1.Manifest{
		Versioned: spec.Versioned{SchemaVersion: 2},
		MediaType: specV1.MediaTypeImageManifest,
		Config: specV1.Descriptor{
			MediaType: MediaTypeSourceImageConfig,
			Digest:    configBlob.Digest,
			Size:      configBlob.Size,
		},
	}
	if _, _, err := writeManifest(ctx, &manifest, ociDest); err != nil {
		return err
	}

	return ociDest.Commit(ctx, nil)
}
