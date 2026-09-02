package buildah

import (
	"archive/tar"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	digest "github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	rspec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/image/v5/manifest"
	ociLayout "go.podman.io/image/v5/oci/layout"
	imageStorage "go.podman.io/image/v5/storage"
	"go.podman.io/image/v5/transports"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/archive"
	storageTypes "go.podman.io/storage/types"
)

func makeFile(t *testing.T, base string, size int64) string {
	t.Helper()
	fn := filepath.Join(t.TempDir(), base)
	f, err := os.Create(fn)
	require.NoError(t, err)
	defer f.Close()
	if size == 0 {
		size = 512
	}
	_, err = io.CopyN(f, rand.Reader, size)
	require.NoErrorf(t, err, "writing payload file %d", base)
	return f.Name()
}

func TestCommitLinkedLayers(t *testing.T) {
	// This test cannot be parallelized as this uses NewBuilder()
	// which eventually and indirectly accesses a global variable
	// defined in `go-selinux`, this must be fixed at `go-selinux`
	// or builder must enable sometime of locking mechanism i.e if
	// routine is creating Builder other's must wait for it.
	// Tracked here: https://github.com/containers/buildah/issues/5967
	ctx := t.Context()
	now := time.Now()

	graphDriverName := os.Getenv("STORAGE_DRIVER")
	if graphDriverName == "" {
		graphDriverName = "vfs"
	}
	t.Logf("using storage driver %q", graphDriverName)
	store, err := storage.GetStore(storageTypes.StoreOptions{
		RunRoot:         t.TempDir(),
		GraphRoot:       t.TempDir(),
		GraphDriverName: graphDriverName,
	})
	require.NoError(t, err, "initializing storage")
	t.Cleanup(func() { _, err := store.Shutdown(true); assert.NoError(t, err) })

	imageName := func(i int) string { return fmt.Sprintf("image%d", i) }
	makeFile := func(base string, size int64) string {
		return makeFile(t, base, size)
	}
	makeArchive := func(base string, size int64) string {
		t.Helper()
		file := makeFile(base, size)
		archiveDir := t.TempDir()
		st, err := os.Stat(file)
		require.NoError(t, err)
		archiveName := filepath.Join(archiveDir, filepath.Base(file))
		f, err := os.Create(archiveName)
		require.NoError(t, err)
		defer f.Close()
		tw := tar.NewWriter(f)
		defer tw.Close()
		hdr, err := tar.FileInfoHeader(st, "")
		require.NoErrorf(t, err, "building tar header for %s", file)
		err = tw.WriteHeader(hdr)
		require.NoErrorf(t, err, "writing tar header for %s", file)
		f, err = os.Open(file)
		require.NoError(t, err)
		defer f.Close()
		_, err = io.Copy(tw, f)
		require.NoErrorf(t, err, "writing tar payload for %s", file)
		return archiveName
	}
	layerNumber := 0

	// Build a from-scratch image with one layer.
	builderOptions := BuilderOptions{
		FromImage: "scratch",
		NamespaceOptions: []NamespaceOption{{
			Name: string(rspec.NetworkNamespace),
			Host: true,
		}},
		SystemContext: &testSystemContext,
	}
	b, err := NewBuilder(ctx, store, builderOptions)
	require.NoError(t, err, "creating builder")
	b.SetCreatedBy(imageName(layerNumber))
	firstFile := makeFile("file0", 0)
	err = b.AddContext(t.Context(), "/", false, AddAndCopyOptions{}, firstFile)
	require.NoError(t, err, "adding", firstFile)
	commitOptions := CommitOptions{
		SystemContext: &testSystemContext,
	}
	ref, err := imageStorage.Transport.ParseStoreReference(store, imageName(layerNumber))
	require.NoError(t, err, "parsing reference for to-be-committed image", imageName(layerNumber))
	_, _, _, err = b.Commit(ctx, ref, commitOptions)
	require.NoError(t, err, "committing", imageName(layerNumber))

	// Build another image based on the first with not much in its layer.
	builderOptions.FromImage = imageName(layerNumber)
	layerNumber++
	b, err = NewBuilder(ctx, store, builderOptions)
	require.NoError(t, err, "creating builder")
	b.SetCreatedBy(imageName(layerNumber))
	secondFile := makeFile("file1", 0)
	err = b.AddContext(t.Context(), "/", false, AddAndCopyOptions{}, secondFile)
	require.NoError(t, err, "adding", secondFile)
	commitOptions = CommitOptions{
		SystemContext: &testSystemContext,
	}
	ref, err = imageStorage.Transport.ParseStoreReference(store, imageName(layerNumber))
	require.NoError(t, err, "parsing reference for to-be-committed image", imageName(layerNumber))
	_, _, _, err = b.Commit(ctx, ref, commitOptions)
	require.NoError(t, err, "committing", imageName(layerNumber))

	// Build a third image with two layers on either side of its read-write layer.
	builderOptions.FromImage = imageName(layerNumber)
	layerNumber++
	b, err = NewBuilder(ctx, store, builderOptions)
	require.NoError(t, err, "creating builder")
	thirdFile := makeFile("file2", 0)
	fourthArchiveFile := makeArchive("file3", 0)
	fifthFile := makeFile("file4", 0)
	sixthFile := makeFile("file5", 0)
	seventhArchiveFile := makeArchive("file6", 0)
	eighthFile := makeFile("file7", 0)
	ninthArchiveFile := makeArchive("file8", 0)
	err = b.AddContext(t.Context(), "/", false, AddAndCopyOptions{}, sixthFile)
	require.NoError(t, err, "adding", sixthFile)
	b.SetCreatedBy(imageName(layerNumber + 3))
	b.AddPrependedLinkedLayer(nil, imageName(layerNumber), "", "", filepath.Dir(thirdFile))
	commitOptions = CommitOptions{
		PrependedLinkedLayers: []LinkedLayer{
			{
				BlobPath: fourthArchiveFile,
				History: v1.History{
					Created:   &now,
					CreatedBy: imageName(layerNumber + 1),
				},
			},
			{
				BlobPath: filepath.Dir(fifthFile),
				History: v1.History{
					Created:   &now,
					CreatedBy: imageName(layerNumber + 2),
				},
			},
		},
		AppendedLinkedLayers: []LinkedLayer{
			{
				BlobPath: seventhArchiveFile,
				History: v1.History{
					Created:   &now,
					CreatedBy: imageName(layerNumber + 4),
				},
			},
			{
				BlobPath: filepath.Dir(eighthFile),
				History: v1.History{
					Created:   &now,
					CreatedBy: imageName(layerNumber + 5),
				},
			},
		},
		SystemContext: &testSystemContext,
	}
	b.AddAppendedLinkedLayer(nil, imageName(layerNumber+6), "", "", ninthArchiveFile)
	ref, err = imageStorage.Transport.ParseStoreReference(store, imageName(layerNumber))
	require.NoErrorf(t, err, "parsing reference for to-be-committed image %q", imageName(layerNumber))
	_, _, _, err = b.Commit(ctx, ref, commitOptions)
	require.NoErrorf(t, err, "committing %q", imageName(layerNumber))

	// Build one last image based on the previous one.
	builderOptions.FromImage = imageName(layerNumber)
	layerNumber += 7
	b, err = NewBuilder(ctx, store, builderOptions)
	require.NoError(t, err, "creating builder")
	b.SetCreatedBy(imageName(layerNumber))
	tenthFile := makeFile("file9", 0)
	err = b.AddContext(t.Context(), "/", false, AddAndCopyOptions{}, tenthFile)
	require.NoError(t, err, "adding", tenthFile)
	commitOptions = CommitOptions{
		SystemContext: &testSystemContext,
	}
	ref, err = imageStorage.Transport.ParseStoreReference(store, imageName(layerNumber))
	require.NoError(t, err, "parsing reference for to-be-committed image", imageName(layerNumber))
	_, _, _, err = b.Commit(ctx, ref, commitOptions)
	require.NoError(t, err, "committing", imageName(layerNumber))

	// Get set to examine this image.  At this point, each history entry
	// should just have "image%d" as its CreatedBy field, and each layer
	// should have the corresponding file (and nothing else) in it.
	src, err := ref.NewImageSource(ctx, &testSystemContext)
	require.NoError(t, err, "opening image source")
	defer src.Close()
	img, err := ref.NewImage(ctx, &testSystemContext)
	require.NoError(t, err, "opening image")
	defer img.Close()
	config, err := img.OCIConfig(ctx)
	require.NoError(t, err, "reading config in OCI format")
	require.Len(t, config.History, 10, "history length")
	for i := range config.History {
		require.Equal(t, fmt.Sprintf("image%d", i), config.History[i].CreatedBy, "history createdBy is off")
	}
	require.Len(t, config.RootFS.DiffIDs, 10, "diffID list")

	layerContents := func(archive io.ReadCloser) []string {
		var contents []string
		defer archive.Close()
		tr := tar.NewReader(archive)
		entry, err := tr.Next()
		for entry != nil {
			contents = append(contents, entry.Name)
			if err != nil {
				break
			}
			entry, err = tr.Next()
		}
		require.ErrorIs(t, err, io.EOF)
		return contents
	}
	infos, err := img.LayerInfosForCopy(ctx)
	require.NoError(t, err, "getting layer infos")
	require.Len(t, infos, 10)
	for i, blobInfo := range infos {
		func() {
			t.Helper()
			rc, _, err := src.GetBlob(ctx, blobInfo, nil)
			require.NoError(t, err, "getting blob", i)
			defer rc.Close()
			contents := layerContents(rc)
			require.Len(t, contents, 1)
			require.Equal(t, fmt.Sprintf("file%d", i), contents[0])
		}()
	}
}

func TestCommitCompression(t *testing.T) {
	// This test cannot be parallelized as this uses NewBuilder()
	// which eventually and indirectly accesses a global variable
	// defined in `go-selinux`, this must be fixed at `go-selinux`
	// or builder must enable sometime of locking mechanism i.e if
	// routine is creating Builder other's must wait for it.
	// Tracked here: https://github.com/containers/buildah/issues/5967
	ctx := t.Context()

	graphDriverName := os.Getenv("STORAGE_DRIVER")
	if graphDriverName == "" {
		graphDriverName = "vfs"
	}
	t.Logf("using storage driver %q", graphDriverName)
	store, err := storage.GetStore(storageTypes.StoreOptions{
		RunRoot:         t.TempDir(),
		GraphRoot:       t.TempDir(),
		GraphDriverName: graphDriverName,
	})
	require.NoError(t, err, "initializing storage")
	t.Cleanup(func() { _, err := store.Shutdown(true); assert.NoError(t, err) })

	builderOptions := BuilderOptions{
		FromImage: "scratch",
		NamespaceOptions: []NamespaceOption{{
			Name: string(rspec.NetworkNamespace),
			Host: true,
		}},
		SystemContext: &testSystemContext,
	}
	b, err := NewBuilder(ctx, store, builderOptions)
	require.NoError(t, err, "creating builder")
	payload := makeFile(t, "file0", 0)
	b.SetCreatedBy("ADD file0 in /")
	err = b.AddContext(t.Context(), "/", false, AddAndCopyOptions{}, payload)
	require.NoError(t, err, "adding", payload)
	for _, compressor := range []struct {
		compression    archive.Compression
		name           string
		expectError    bool
		layerMediaType string
		os             string
	}{
		{archive.Uncompressed, "uncompressed", false, v1.MediaTypeImageLayer, ""},
		{archive.Uncompressed, "uncompressed-win", false, v1.MediaTypeImageLayer, "windows"},
		{archive.Gzip, "gzip", false, v1.MediaTypeImageLayerGzip, ""},
		{archive.Gzip, "gzip-win", false, v1.MediaTypeImageLayerGzip, "windows"},
		{archive.Bzip2, "bz2", true, "", ""},
		{archive.Bzip2, "bz2-win", true, "", "windows"},
		{archive.Xz, "xz", true, "", ""},
		{archive.Xz, "xz-win", true, "", "windows"},
		{archive.Zstd, "zstd", false, v1.MediaTypeImageLayerZstd, ""},
		{archive.Zstd, "zstd-win", false, v1.MediaTypeImageLayerZstd, "windows"},
	} {
		t.Run(compressor.name, func(t *testing.T) {
			var ref types.ImageReference
			commitOptions := CommitOptions{
				PreferredManifestType: v1.MediaTypeImageManifest,
				SystemContext:         &testSystemContext,
				Compression:           compressor.compression,
			}
			b.OCIv1.OS = compressor.os
			imageName := compressor.name
			ref, err := imageStorage.Transport.ParseStoreReference(store, imageName)
			require.NoErrorf(t, err, "parsing reference for to-be-committed local image %q", imageName)
			_, _, _, err = b.Commit(ctx, ref, commitOptions)
			if compressor.expectError {
				require.Errorf(t, err, "committing local image %q", imageName)
			} else {
				require.NoErrorf(t, err, "committing local image %q", imageName)
			}
			imageName = t.TempDir()
			ref, err = ociLayout.Transport.ParseReference(imageName)
			require.NoErrorf(t, err, "parsing reference for to-be-committed oci layout %q", imageName)
			_, _, _, err = b.Commit(ctx, ref, commitOptions)
			if compressor.expectError {
				require.Errorf(t, err, "committing oci layout %q", imageName)
				return
			}
			require.NoErrorf(t, err, "committing oci layout %q", imageName)
			src, err := ref.NewImageSource(ctx, &testSystemContext)
			require.NoErrorf(t, err, "reading oci layout %q", imageName)
			defer src.Close()
			manifestBytes, manifestType, err := src.GetManifest(ctx, nil)
			require.NoErrorf(t, err, "reading manifest from oci layout %q", imageName)
			require.Equalf(t, v1.MediaTypeImageManifest, manifestType, "manifest type from oci layout %q looked wrong", imageName)
			parsedManifest, err := manifest.OCI1FromManifest(manifestBytes)
			require.NoErrorf(t, err, "parsing manifest from oci layout %q", imageName)
			require.Lenf(t, parsedManifest.Layers, 1, "expected exactly one layer in oci layout %q", imageName)
			require.Equalf(t, compressor.layerMediaType, parsedManifest.Layers[0].MediaType, "expected the layer media type to reflect compression in oci layout %q", imageName)
			blobReadCloser, _, err := src.GetBlob(ctx, types.BlobInfo{
				Digest:    parsedManifest.Layers[0].Digest,
				MediaType: parsedManifest.Layers[0].MediaType,
			}, nil)
			require.NoErrorf(t, err, "reading the first layer from oci layout %q", imageName)
			defer blobReadCloser.Close()
			blob, err := io.ReadAll(blobReadCloser)
			require.NoErrorf(t, err, "consuming the first layer from oci layout %q", imageName)
			require.Equalf(t, compressor.compression, archive.DetectCompression(blob), "detected compression looks wrong for layer in oci layout %q")
		})
	}
}

func TestCommitEmpty(t *testing.T) {
	// This test cannot be parallelized as this uses NewBuilder()
	// which eventually and indirectly accesses a global variable
	// defined in `go-selinux`, this must be fixed at `go-selinux`
	// or builder must enable sometime of locking mechanism i.e if
	// routine is creating Builder other's must wait for it.
	// Tracked here: https://github.com/containers/buildah/issues/5967
	ctx := t.Context()

	graphDriverName := os.Getenv("STORAGE_DRIVER")
	if graphDriverName == "" {
		graphDriverName = "vfs"
	}
	t.Logf("using storage driver %q", graphDriverName)
	store, err := storage.GetStore(storageTypes.StoreOptions{
		RunRoot:         t.TempDir(),
		GraphRoot:       t.TempDir(),
		GraphDriverName: graphDriverName,
	})
	require.NoError(t, err, "initializing storage")
	t.Cleanup(func() { _, err := store.Shutdown(true); assert.NoError(t, err) })

	builderOptions := BuilderOptions{
		FromImage: "scratch",
		NamespaceOptions: []NamespaceOption{{
			Name: string(rspec.NetworkNamespace),
			Host: true,
		}},
		SystemContext: &testSystemContext,
	}
	b, err := NewBuilder(ctx, store, builderOptions)
	require.NoError(t, err, "creating builder")

	committedLayoutDir := t.TempDir()
	committedRef, err := ociLayout.ParseReference(committedLayoutDir)
	require.NoError(t, err, "parsing reference to where we're committing a basic image")
	_, _, _, err = b.Commit(ctx, committedRef, CommitOptions{})
	require.NoError(t, err, "committing with default settings")

	committedImg, err := committedRef.NewImageSource(ctx, &testSystemContext)
	require.NoError(t, err, "preparing to read committed image")
	defer committedImg.Close()
	committedManifestBytes, committedManifestType, err := committedImg.GetManifest(ctx, nil)
	require.NoError(t, err, "reading manifest from committed image")
	require.Equalf(t, v1.MediaTypeImageManifest, committedManifestType, "unexpected manifest type")
	committedManifest, err := manifest.FromBlob(committedManifestBytes, committedManifestType)
	require.NoError(t, err, "parsing manifest from committed image")
	require.Equalf(t, 1, len(committedManifest.LayerInfos()), "expected one layer in manifest")
	configReadCloser, _, err := committedImg.GetBlob(ctx, committedManifest.ConfigInfo(), nil)
	require.NoError(t, err, "reading config blob from committed image")
	defer configReadCloser.Close()
	var committedImage v1.Image
	err = json.NewDecoder(configReadCloser).Decode(&committedImage)
	require.NoError(t, err, "parsing config blob from committed image")
	require.Equalf(t, 1, len(committedImage.History), "expected one history entry")
	require.Falsef(t, committedImage.History[0].EmptyLayer, "expected lone history entry to not be marked as an empty layer")
	require.Equalf(t, 1, len(committedImage.RootFS.DiffIDs), "expected one rootfs layer")

	t.Run("emptylayer", func(t *testing.T) {
		options := CommitOptions{
			EmptyLayer: true,
		}
		layoutDir := t.TempDir()
		ref, err := ociLayout.ParseReference(layoutDir)
		require.NoError(t, err, "parsing reference to image we're going to commit with EmptyLayer")
		_, _, _, err = b.Commit(ctx, ref, options)
		require.NoError(t, err, "committing with EmptyLayer = true")
		img, err := ref.NewImageSource(ctx, &testSystemContext)
		require.NoError(t, err, "preparing to read committed image")
		defer img.Close()
		manifestBytes, manifestType, err := img.GetManifest(ctx, nil)
		require.NoError(t, err, "reading manifest from committed image")
		require.Equalf(t, v1.MediaTypeImageManifest, manifestType, "unexpected manifest type")
		parsedManifest, err := manifest.FromBlob(manifestBytes, manifestType)
		require.NoError(t, err, "parsing manifest from committed image")
		require.Zerof(t, len(parsedManifest.LayerInfos()), "expected no layers in manifest")
		configReadCloser, _, err := img.GetBlob(ctx, parsedManifest.ConfigInfo(), nil)
		require.NoError(t, err, "reading config blob from committed image")
		defer configReadCloser.Close()
		var image v1.Image
		err = json.NewDecoder(configReadCloser).Decode(&image)
		require.NoError(t, err, "parsing config blob from committed image")
		require.Equalf(t, 1, len(image.History), "expected one history entry")
		require.Truef(t, image.History[0].EmptyLayer, "expected lone history entry to be marked as an empty layer")
	})

	t.Run("omitlayerhistoryentry", func(t *testing.T) {
		options := CommitOptions{
			OmitLayerHistoryEntry: true,
		}
		layoutDir := t.TempDir()
		ref, err := ociLayout.ParseReference(layoutDir)
		require.NoError(t, err, "parsing reference to image we're going to commit with OmitLayerHistoryEntry")
		_, _, _, err = b.Commit(ctx, ref, options)
		require.NoError(t, err, "committing with OmitLayerHistoryEntry = true")
		img, err := ref.NewImageSource(ctx, &testSystemContext)
		require.NoError(t, err, "preparing to read committed image")
		defer img.Close()
		manifestBytes, manifestType, err := img.GetManifest(ctx, nil)
		require.NoError(t, err, "reading manifest from committed image")
		require.Equalf(t, v1.MediaTypeImageManifest, manifestType, "unexpected manifest type")
		parsedManifest, err := manifest.FromBlob(manifestBytes, manifestType)
		require.NoError(t, err, "parsing manifest from committed image")
		require.Equalf(t, 0, len(parsedManifest.LayerInfos()), "expected no layers in manifest")
		configReadCloser, _, err := img.GetBlob(ctx, parsedManifest.ConfigInfo(), nil)
		require.NoError(t, err, "reading config blob from committed image")
		defer configReadCloser.Close()
		var image v1.Image
		err = json.NewDecoder(configReadCloser).Decode(&image)
		require.NoError(t, err, "parsing config blob from committed image")
		require.Equalf(t, 0, len(image.History), "expected no history entries")
		require.Equalf(t, 0, len(image.RootFS.DiffIDs), "expected no diff IDs")
	})

	builderOptions.FromImage = transports.ImageName(committedRef)
	b, err = NewBuilder(ctx, store, builderOptions)
	require.NoError(t, err, "creating builder from committed base image")

	t.Run("derived-emptylayer", func(t *testing.T) {
		options := CommitOptions{
			EmptyLayer: true,
		}
		layoutDir := t.TempDir()
		ref, err := ociLayout.ParseReference(layoutDir)
		require.NoError(t, err, "parsing reference to image we're going to commit with EmptyLayer")
		_, _, _, err = b.Commit(ctx, ref, options)
		require.NoError(t, err, "committing with EmptyLayer = true")
		img, err := ref.NewImageSource(ctx, &testSystemContext)
		require.NoError(t, err, "preparing to read committed image")
		defer img.Close()
		manifestBytes, manifestType, err := img.GetManifest(ctx, nil)
		require.NoError(t, err, "reading manifest from committed image")
		require.Equalf(t, v1.MediaTypeImageManifest, manifestType, "unexpected manifest type")
		parsedManifest, err := manifest.FromBlob(manifestBytes, manifestType)
		require.NoError(t, err, "parsing manifest from committed image")
		require.Equalf(t, len(committedManifest.LayerInfos()), len(parsedManifest.LayerInfos()), "expected no new layers in manifest")
		configReadCloser, _, err := img.GetBlob(ctx, parsedManifest.ConfigInfo(), nil)
		require.NoError(t, err, "reading config blob from committed image")
		defer configReadCloser.Close()
		var image v1.Image
		err = json.NewDecoder(configReadCloser).Decode(&image)
		require.NoError(t, err, "parsing config blob from committed image")
		require.Equalf(t, len(committedImage.History)+1, len(image.History), "expected one new history entry")
		require.Equalf(t, len(committedImage.RootFS.DiffIDs), len(image.RootFS.DiffIDs), "expected no new diff IDs")
		require.Truef(t, image.History[1].EmptyLayer, "expected new history entry to be marked as an empty layer")
	})

	t.Run("derived-omitlayerhistoryentry", func(t *testing.T) {
		options := CommitOptions{
			OmitLayerHistoryEntry: true,
		}
		layoutDir := t.TempDir()
		ref, err := ociLayout.ParseReference(layoutDir)
		require.NoError(t, err, "parsing reference to image we're going to commit with OmitLayerHistoryEntry")
		_, _, _, err = b.Commit(ctx, ref, options)
		require.NoError(t, err, "committing with OmitLayerHistoryEntry = true")
		img, err := ref.NewImageSource(ctx, &testSystemContext)
		require.NoError(t, err, "preparing to read committed image")
		defer img.Close()
		manifestBytes, manifestType, err := img.GetManifest(ctx, nil)
		require.NoError(t, err, "reading manifest from committed image")
		require.Equalf(t, v1.MediaTypeImageManifest, manifestType, "unexpected manifest type")
		parsedManifest, err := manifest.FromBlob(manifestBytes, manifestType)
		require.NoError(t, err, "parsing manifest from committed image")
		require.Equalf(t, len(committedManifest.LayerInfos()), len(parsedManifest.LayerInfos()), "expected no new layers in manifest")
		configReadCloser, _, err := img.GetBlob(ctx, parsedManifest.ConfigInfo(), nil)
		require.NoError(t, err, "reading config blob from committed image")
		defer configReadCloser.Close()
		var image v1.Image
		err = json.NewDecoder(configReadCloser).Decode(&image)
		require.NoError(t, err, "parsing config blob from committed image")
		require.Equalf(t, len(committedImage.History), len(image.History), "expected no new history entry")
		require.Equalf(t, len(committedImage.RootFS.DiffIDs), len(image.RootFS.DiffIDs), "expected no new diff IDs")
	})

	t.Run("derived-synthetic", func(t *testing.T) {
		randomDir := t.TempDir()
		randomFile, err := os.CreateTemp(randomDir, "file")
		require.NoError(t, err, "creating a temporary file")
		layerDigest := digest.Canonical.Digester()
		_, err = io.CopyN(io.MultiWriter(layerDigest.Hash(), randomFile), rand.Reader, 512)
		require.NoError(t, err, "writing a temporary file")
		require.NoError(t, randomFile.Close(), "closing temporary file")
		options := CommitOptions{
			OmitLayerHistoryEntry: true,
			AppendedLinkedLayers: []LinkedLayer{{
				History: v1.History{
					CreatedBy: "yolo",
				}, // history entry to add
				BlobPath: randomFile.Name(),
			}},
		}
		layoutDir := t.TempDir()
		ref, err := ociLayout.ParseReference(layoutDir)
		require.NoErrorf(t, err, "parsing reference for to-be-committed image with externally-controlled changes")
		_, _, _, err = b.Commit(ctx, ref, options)
		require.NoError(t, err, "committing with OmitLayerHistoryEntry = true")
		img, err := ref.NewImageSource(ctx, &testSystemContext)
		require.NoError(t, err, "preparing to read committed image")
		defer img.Close()
		manifestBytes, manifestType, err := img.GetManifest(ctx, nil)
		require.NoError(t, err, "reading manifest from committed image")
		require.Equalf(t, v1.MediaTypeImageManifest, manifestType, "unexpected manifest type")
		parsedManifest, err := manifest.FromBlob(manifestBytes, manifestType)
		require.NoError(t, err, "parsing manifest from committed image")
		require.Equalf(t, len(committedManifest.LayerInfos())+1, len(parsedManifest.LayerInfos()), "expected one new layer in manifest")
		configReadCloser, _, err := img.GetBlob(ctx, parsedManifest.ConfigInfo(), nil)
		require.NoError(t, err, "reading config blob from committed image")
		defer configReadCloser.Close()
		var image v1.Image
		err = json.NewDecoder(configReadCloser).Decode(&image)
		require.NoError(t, err, "decoding image config")
		require.Equalf(t, len(committedImage.History)+1, len(image.History), "expected one new history entry")
		require.Equalf(t, len(committedImage.RootFS.DiffIDs)+1, len(image.RootFS.DiffIDs), "expected one new diff ID")
		require.Equalf(t, layerDigest.Digest(), image.RootFS.DiffIDs[len(image.RootFS.DiffIDs)-1], "expected new diff ID to match the randomly-generated layer")
	})
}

func TestCommitChmod(t *testing.T) {
	ctx := t.Context()
	graphDriverName := os.Getenv("STORAGE_DRIVER")
	if graphDriverName == "" {
		graphDriverName = "vfs"
	}
	t.Logf("using storage driver %q", graphDriverName)
	store, err := storage.GetStore(storageTypes.StoreOptions{
		RunRoot:         t.TempDir(),
		GraphRoot:       t.TempDir(),
		GraphDriverName: graphDriverName,
	})
	require.NoError(t, err, "initializing storage")
	t.Cleanup(func() { _, err := store.Shutdown(true); assert.NoError(t, err) })

	// Build a from-scratch image with one layer.
	builderOptions := BuilderOptions{
		FromImage: "scratch",
		NamespaceOptions: []NamespaceOption{{
			Name: string(rspec.NetworkNamespace),
			Host: true,
		}},
		SystemContext: &testSystemContext,
	}
	b, err := NewBuilder(ctx, store, builderOptions)
	require.NoError(t, err, "creating builder")
	imgName := "image0"
	b.SetCreatedBy(imgName)

	type perms struct {
		chmod     string
		perm      int64
		startMode os.FileMode
	}

	filePerms := map[string]perms{
		"symbolic":                        {chmod: "u=rwX,go=rX", perm: 0o644},
		"symbolic adding mode bits":       {chmod: "u=rwX,go=rX", perm: 0o755, startMode: 0o111},
		"octal":                           {chmod: "750", perm: 0o750},
		"octal overwrite start mode":      {chmod: "644", perm: 0o644, startMode: 0o753},
		"clear group and other mode bits": {chmod: "go=", perm: 0o700, startMode: 0o777},
	}
	for name, v := range filePerms {
		f := makeFile(t, name, 0)
		if v.startMode != 0 {
			err = os.Chmod(f, v.startMode)
			require.NoError(t, err, "chmod", f)
		}
		err = b.AddContext(t.Context(), "/", false, AddAndCopyOptions{Chmod: v.chmod}, f)
		require.NoError(t, err, "adding", f)
	}

	commitOptions := CommitOptions{
		SystemContext: &testSystemContext,
	}
	ref, err := imageStorage.Transport.ParseStoreReference(store, imgName)
	require.NoError(t, err, "parsing reference for to-be-committed image", imgName)
	_, _, _, err = b.Commit(ctx, ref, commitOptions)
	require.NoError(t, err, "committing", imgName)

	src, err := ref.NewImageSource(ctx, &testSystemContext)
	require.NoError(t, err, "opening image source")
	defer src.Close()
	img, err := ref.NewImage(ctx, &testSystemContext)
	require.NoError(t, err, "opening image")
	defer img.Close()

	infos, err := img.LayerInfosForCopy(ctx)
	require.NoError(t, err, "getting layer infos")

	for i, blobInfo := range infos {
		rc, _, err := src.GetBlob(ctx, blobInfo, nil)
		require.NoError(t, err, "getting blob", i)
		defer rc.Close()
		tr := tar.NewReader(rc)
		entry, err := tr.Next()
		for entry != nil {
			expected := filePerms[entry.Name]
			require.Equal(t, expected.perm, entry.Mode)
			entry, err = tr.Next()
		}
		require.ErrorIs(t, err, io.EOF)
	}
}
