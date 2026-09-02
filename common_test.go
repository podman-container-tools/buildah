package buildah

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	digest "github.com/opencontainers/go-digest"
	ispec "github.com/opencontainers/image-spec/specs-go"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cp "go.podman.io/image/v5/copy"
	"go.podman.io/image/v5/signature"
	imageStorage "go.podman.io/image/v5/storage"
	"go.podman.io/image/v5/transports/alltransports"
	"go.podman.io/storage"
	storageTypes "go.podman.io/storage/types"
)

type testRetryCopyImageWrappedStore struct {
	phantomImageID string
	storage.Store
}

func (ts *testRetryCopyImageWrappedStore) CreateImage(id string, names []string, layer, metadata string, options *storage.ImageOptions) (*storage.Image, error) {
	if id == ts.phantomImageID {
		if img, err := ts.Store.Image(id); img != nil && err == nil {
			// i'm another thread somewhere
			if _, err := ts.Store.DeleteImage(id, true); err != nil {
				return nil, err
			}
		}
	}
	return ts.Store.CreateImage(id, names, layer, metadata, options)
}

func TestRetryCopyImage(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	graphDriverName := os.Getenv("STORAGE_DRIVER")
	if graphDriverName == "" {
		graphDriverName = "vfs"
	}
	store, err := storage.GetStore(storageTypes.StoreOptions{
		RunRoot:         t.TempDir(),
		GraphRoot:       t.TempDir(),
		GraphDriverName: graphDriverName,
	})
	require.NoError(t, err, "initializing storage")
	t.Cleanup(func() { _, err := store.Shutdown(true); assert.NoError(t, err) })

	// construct an "image" that can be pulled into local storage
	var layerBuffer bytes.Buffer
	tw := tar.NewWriter(&layerBuffer)
	err = tw.WriteHeader(&tar.Header{
		Name:     "rootfile",
		Typeflag: tar.TypeReg,
		Size:     1234,
	})
	require.NoError(t, err, "writing header for archive")
	_, err = tw.Write(make([]byte, 1234))
	require.NoError(t, err, "writing empty file to archive")
	require.NoError(t, tw.Close(), "finishing layer")
	layerDigest := digest.Canonical.FromBytes(layerBuffer.Bytes())
	imageConfig := v1.Image{
		RootFS: v1.RootFS{
			Type:    "layers",
			DiffIDs: []digest.Digest{layerDigest},
		},
	}
	imageConfigBytes, err := json.Marshal(&imageConfig)
	require.NoError(t, err, "marshalling image configuration blob")
	imageConfigDigest := digest.Canonical.FromBytes(imageConfigBytes)
	imageManifest := v1.Manifest{
		Versioned: ispec.Versioned{
			SchemaVersion: 2,
		},
		MediaType: v1.MediaTypeImageManifest,
		Config: v1.Descriptor{
			MediaType: v1.MediaTypeImageConfig,
			Size:      int64(len(imageConfigBytes)),
			Digest:    digest.FromBytes(imageConfigBytes),
		},
		Layers: []v1.Descriptor{
			{
				MediaType: v1.MediaTypeImageLayer,
				Size:      int64(layerBuffer.Len()),
				Digest:    layerDigest,
			},
		},
	}
	imageManifestBytes, err := json.Marshal(&imageManifest)
	require.NoError(t, err, "marshalling image manifest")
	imageManifestDigest := digest.Canonical.FromBytes(imageManifestBytes)

	// write it to an oci layout
	ociDir := t.TempDir()
	blobbyDir := filepath.Join(ociDir, "blobs")
	require.NoError(t, os.Mkdir(blobbyDir, 0o700))
	blobDir := filepath.Join(blobbyDir, layerDigest.Algorithm().String())
	require.NoError(t, os.Mkdir(blobDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(blobDir, layerDigest.Encoded()), layerBuffer.Bytes(), 0o600), "writing layer")
	require.NoError(t, os.WriteFile(filepath.Join(blobDir, imageConfigDigest.Encoded()), imageConfigBytes, 0o600), "writing image config")
	require.NoError(t, os.WriteFile(filepath.Join(blobDir, imageManifestDigest.Encoded()), imageManifestBytes, 0o600), "writing manifest")
	imageIndex := v1.Index{
		Versioned: ispec.Versioned{
			SchemaVersion: 2,
		},
		MediaType: v1.MediaTypeImageIndex,
		Manifests: []v1.Descriptor{
			{
				MediaType: v1.MediaTypeImageManifest,
				Digest:    imageManifestDigest,
				Size:      int64(len(imageManifestBytes)),
			},
		},
	}
	imageIndexBytes, err := json.Marshal(&imageIndex)
	require.NoError(t, err, "marshalling image index")
	require.NoError(t, os.WriteFile(filepath.Join(ociDir, v1.ImageIndexFile), imageIndexBytes, 0o600), "writing image index")
	imageLayout := v1.ImageLayout{
		Version: v1.ImageLayoutVersion,
	}
	imageLayoutBytes, err := json.Marshal(&imageLayout)
	require.NoError(t, err, "marshalling image layout")
	require.NoError(t, os.WriteFile(filepath.Join(ociDir, v1.ImageLayoutFile), imageLayoutBytes, 0o600), "writing image layout")

	// pull the image, twice, just to make sure nothing weird happens
	srcRef, err := alltransports.ParseImageName("oci:" + ociDir)
	require.NoError(t, err, "building reference to image layout")
	destRef, err := imageStorage.Transport.NewStoreReference(store, nil, imageConfigDigest.Encoded())
	require.NoError(t, err, "building reference to image in store")
	policy, err := signature.NewPolicyFromFile("tests/policy.json")
	require.NoError(t, err, "reading signature policy")
	policyContext, err := signature.NewPolicyContext(policy)
	require.NoError(t, err, "building policy context")
	t.Cleanup(func() {
		require.NoError(t, policyContext.Destroy(), "destroying policy context")
	})
	_, err = retryCopyImage(ctx, policyContext, destRef, srcRef, destRef, &cp.Options{}, 3, 1*time.Second)
	require.NoError(t, err, "copying image")
	_, err = retryCopyImage(ctx, policyContext, destRef, srcRef, destRef, &cp.Options{}, 3, 1*time.Second)
	require.NoError(t, err, "copying image")

	// now make something weird happen
	wrappedStore := &testRetryCopyImageWrappedStore{
		phantomImageID: imageConfigDigest.Encoded(),
		Store:          store,
	}
	wrappedDestRef, err := imageStorage.Transport.NewStoreReference(wrappedStore, nil, imageConfigDigest.Encoded())
	require.NoError(t, err, "building wrapped reference")

	// copy with retry-on-storage-layer-unknown = false: expect an error
	// (if it succeeds, either the test is broken, or we can remove this
	// case from the retry function)
	_, err = retryCopyImageWithOptions(ctx, policyContext, wrappedDestRef, srcRef, wrappedDestRef, &cp.Options{}, 3, 1*time.Second, false)
	require.ErrorIs(t, err, storage.ErrLayerUnknown, "copying image")

	// copy with retry-on-storage-layer-unknown = true: expect no error
	_, err = retryCopyImageWithOptions(ctx, policyContext, wrappedDestRef, srcRef, wrappedDestRef, &cp.Options{}, 3, 1*time.Second, true)
	require.NoError(t, err, "copying image")
}
