package sanitize

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	imgspec "github.com/opencontainers/image-spec/specs-go"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	imageCopy "go.podman.io/image/v5/copy"
	"go.podman.io/image/v5/pkg/compression"
	"go.podman.io/image/v5/signature"
	"go.podman.io/image/v5/transports"
	"go.podman.io/image/v5/transports/alltransports"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/reexec"
)

func TestMain(m *testing.M) {
	if reexec.Init() {
		return
	}

	result := m.Run()
	os.Exit(result)
}

func TestSanitizeImageName(t *testing.T) {
	storageRoot, storageRunRoot := t.TempDir(), t.TempDir()
	store, err := storage.GetStore(storage.StoreOptions{
		GraphRoot:       storageRoot,
		RunRoot:         storageRunRoot,
		GraphDriverName: "vfs",
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := store.Shutdown(false)
		assert.NoError(t, err)
	})

	const badLinkTarget = "../../../../../../../../../../../etc/passwd"

	// prepare to copy from the layout to other types of destinations
	ctx := context.Background()
	// sys := &types.SystemContext{}
	policy, err := signature.NewPolicyFromBytes([]byte(`{"default":[{"type":"insecureAcceptAnything"}]}`))
	require.NoErrorf(t, err, "creating a policy")
	policyContext, err := signature.NewPolicyContext(policy)
	require.NoErrorf(t, err, "creating a policy context")
	t.Cleanup(func() {
		if err := policyContext.Destroy(); err != nil {
			t.Logf("destroying policy context: %v", err)
		}
	})

	scanDir := func(dir string) (bool, error) {
		// look for anything that isn't a plain directory or file
		foundSuspiciousStuff := false
		err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.Type().IsRegular() && !d.Type().IsDir() {
				foundSuspiciousStuff = true
			}
			return nil
		})
		return foundSuspiciousStuff, err
	}
	scanArchive := func(file string) (bool, error) {
		// look for anything that isn't a plain directory, file, or hard link
		foundSuspiciousStuff := false
		f, err := os.Open(file)
		if err != nil {
			return foundSuspiciousStuff, err
		}
		dc, _, err := compression.AutoDecompress(f)
		if err != nil {
			return foundSuspiciousStuff, err
		}
		tr := tar.NewReader(dc)
		hdr, err := tr.Next()
		for hdr != nil {
			if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeDir && hdr.Typeflag != tar.TypeLink {
				foundSuspiciousStuff = true
				break
			}
			if hdr.Typeflag == tar.TypeLink {
				if strings.TrimPrefix(path.Clean("/"+hdr.Linkname), "/") != hdr.Linkname {
					foundSuspiciousStuff = true
					break
				}
			}
			hdr, err = tr.Next()
		}
		if err := dc.Close(); err != nil {
			if err2 := f.Close(); err2 != nil {
				err = errors.Join(err, err2)
			}
			return foundSuspiciousStuff, err
		}
		if err := f.Close(); err != nil {
			return foundSuspiciousStuff, err
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return foundSuspiciousStuff, err
		}
		return foundSuspiciousStuff, nil
	}
	generateLayout := func(t *testing.T, layoutDir string) (uncompressed digest.Digest, compressed digest.Digest) {
		t.Helper()
		var layer bytes.Buffer
		layerDigester := digest.Canonical.Digester()
		wc, err := compression.CompressStream(io.MultiWriter(&layer, layerDigester.Hash()), compression.Gzip, nil)
		require.NoErrorf(t, err, "compressing empty layer")
		diffID := digest.Canonical.Digester()
		tw := tar.NewWriter(io.MultiWriter(wc, diffID.Hash()))
		require.NoErrorf(t, tw.Close(), "flushing empty layer")
		require.NoErrorf(t, wc.Close(), "flushing compressor")
		diffDigest := diffID.Digest()
		layerBytes := layer.Bytes()
		layerDigest := layerDigester.Digest()
		ociConfig := v1.Image{
			RootFS: v1.RootFS{
				Type:    "layers",
				DiffIDs: []digest.Digest{diffDigest},
			},
		}
		configBytes, err := json.Marshal(&ociConfig)
		require.NoError(t, err, "encoding oci config")
		configDigest := digest.Canonical.FromBytes(configBytes)
		ociManifest := v1.Manifest{
			Versioned: imgspec.Versioned{
				SchemaVersion: 2,
			},
			MediaType: v1.MediaTypeImageManifest,
			Config: v1.Descriptor{
				MediaType: v1.MediaTypeImageConfig,
				Digest:    configDigest,
				Size:      int64(len(configBytes)),
			},
			Layers: []v1.Descriptor{{
				MediaType: v1.MediaTypeImageLayerGzip,
				Digest:    layerDigest,
				Size:      int64(len(layerBytes)),
			}},
		}
		manifestBytes, err := json.Marshal(&ociManifest)
		require.NoError(t, err, "encoding oci manifest")
		manifestDigest := digest.Canonical.FromBytes(manifestBytes)
		index := v1.Index{
			Versioned: imgspec.Versioned{
				SchemaVersion: 2,
			},
			MediaType: v1.MediaTypeImageIndex,
			Manifests: []v1.Descriptor{{
				MediaType: v1.MediaTypeImageManifest,
				Digest:    manifestDigest,
				Size:      int64(len(manifestBytes)),
				Annotations: map[string]string{
					v1.AnnotationRefName: "latest",
				},
			}},
		}
		indexBytes, err := json.Marshal(&index)
		require.NoError(t, err, "encoding oci index")
		layoutBytes, err := json.Marshal(&v1.ImageLayout{Version: v1.ImageLayoutVersion})
		require.NoError(t, err, "encoding oci layout")
		blobsDir := filepath.Join(layoutDir, v1.ImageBlobsDir)
		require.NoError(t, os.MkdirAll(blobsDir, 0o700), "creating blobs subdirectory")
		blobsDir = filepath.Join(blobsDir, digest.Canonical.String())
		require.NoErrorf(t, os.MkdirAll(blobsDir, 0o700), "creating %q/%q subdirectory", v1.ImageBlobsDir, digest.Canonical.String())
		require.NoError(t, os.WriteFile(filepath.Join(blobsDir, layerDigest.Encoded()), layerBytes, 0o600), "writing layer")
		sus, err := scanArchive(filepath.Join(blobsDir, layerDigest.Encoded()))
		require.NoError(t, err, "unexpected error scanning empty layer")
		require.False(t, sus, "empty layer should pass scan")
		require.NoError(t, os.WriteFile(filepath.Join(blobsDir, configDigest.Encoded()), configBytes, 0o600), "writing config")
		require.NoError(t, os.WriteFile(filepath.Join(blobsDir, manifestDigest.Encoded()), manifestBytes, 0o600), "writing manifest")
		require.NoError(t, os.WriteFile(filepath.Join(layoutDir, v1.ImageIndexFile), indexBytes, 0o600), "writing index")
		require.NoError(t, os.WriteFile(filepath.Join(layoutDir, v1.ImageLayoutFile), layoutBytes, 0o600), "writing layout")
		sus, err = scanDir(layoutDir)
		require.NoError(t, err, "scanning layout directory")
		require.False(t, sus, "check on layout directory")
		return diffDigest, layerDigest
	}
	mutateDirectory := func(t *testing.T, parentdir, input, replace string) string {
		t.Helper()
		tmpdir, err := os.MkdirTemp(parentdir, "directory") //nolint:usetesting
		require.NoError(t, err, "creating mutated directory")
		found := false
		err = filepath.Walk(input, func(path string, info fs.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(input, path)
			if err != nil {
				return err
			}
			if rel == replace {
				found = true
				return os.Symlink(badLinkTarget, filepath.Join(tmpdir, rel))
			}
			if info.IsDir() {
				if rel == "." {
					return nil
				}
				return os.Mkdir(filepath.Join(tmpdir, rel), info.Mode())
			}
			switch info.Mode() & os.ModeType {
			case os.ModeSymlink:
				target, err := os.Readlink(path)
				require.NoErrorf(t, err, "reading link target from %q", path)
				return os.Symlink(target, filepath.Join(tmpdir, rel))
			case os.ModeCharDevice | os.ModeDevice:
				t.Fatalf("unexpected character device %q", path)
			case os.ModeDevice:
				t.Fatalf("unexpected block device %q", path)
			case os.ModeNamedPipe:
				t.Fatalf("unexpected named pipe %q", path)
			case os.ModeSocket:
				t.Fatalf("unexpected socket %q", path)
			case os.ModeIrregular:
				t.Fatalf("unexpected irregularity %q", path)
			case os.ModeDir:
				t.Fatalf("unexpected directory %q after we should have created it", path)
			default:
				inf, err := os.Open(path)
				require.NoErrorf(t, err, "opening %q to read it", path)
				outpath := filepath.Join(tmpdir, rel)
				outf, err := os.OpenFile(outpath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
				require.NoErrorf(t, err, "opening %q to write it", outpath)
				n, err := io.Copy(outf, inf)
				require.NoErrorf(t, err, "copying contents of %q", path)
				require.Equalf(t, info.Size(), n, "unexpected write length to %q (%d != %d)", outpath, n, info.Size())
				require.NoErrorf(t, outf.Close(), "closing new file %q", outpath)
				require.NoErrorf(t, inf.Close(), "closing old file %q", path)
			}
			return nil
		})
		require.NoError(t, err, "mutating directory")
		require.Truef(t, found, "did not replace %q", replace)
		return tmpdir
	}
	mutateArchive := func(t *testing.T, parentdir, input, replace string) string {
		t.Helper()
		f, err := os.Open(input)
		require.NoError(t, err, "opening archive for mutating")
		tmpfile, err := os.CreateTemp(parentdir, "archive")
		require.NoError(t, err, "creating mutated archive")
		tr := tar.NewReader(f)
		tw := tar.NewWriter(tmpfile)
		found := false
		hdr, err := tr.Next()
		for hdr != nil {
			if hdr.Name == replace {
				err := tw.WriteHeader(&tar.Header{
					Name:     hdr.Name,
					Typeflag: tar.TypeSymlink,
					Linkname: badLinkTarget,
				})
				require.NoErrorf(t, err, "copying contents of %q", hdr.Name)
				found = true
			} else {
				err := tw.WriteHeader(hdr)
				require.NoErrorf(t, err, "copying contents of %q", hdr.Name)
				_, err = io.Copy(tw, tr)
				require.NoErrorf(t, err, "copying contents of %q", hdr.Name)
			}
			if err != nil {
				break
			}
			hdr, err = tr.Next()
		}
		if err != nil && !errors.Is(err, io.EOF) {
			require.NoError(t, err, "when finished reading archive for mutating")
		}
		require.NoError(t, f.Close(), "closing archive for mutating")
		require.NoError(t, tw.Close(), "finishing up writing mutated archive")
		tmpfileName := tmpfile.Name()
		require.NoError(t, tmpfile.Close(), "closing mutated archive")
		require.Truef(t, found, "did not replace %q", replace)
		return tmpfileName
	}
	requireImageReadable := func(t *testing.T, imageName string) {
		t.Helper()
		tmpdir := t.TempDir()
		ref, err := alltransports.ParseImageName(imageName)
		require.NoErrorf(t, err, "parsing image reference %q", imageName)
		dir, err := alltransports.ParseImageName("dir:" + tmpdir)
		require.NoErrorf(t, err, "parsing image reference %q", "dir:"+tmpdir)
		_, err = imageCopy.Image(ctx, policyContext, dir, ref, nil)
		require.NoErrorf(t, err, "copying image %q, which should have been successful", transports.ImageName(ref))
	}

	contextDir := t.TempDir()
	subdirsDir := filepath.Join(contextDir, "subdirs")
	require.NoError(t, os.Mkdir(subdirsDir, 0o700), "creating subdirectory directory")
	archiveDir := filepath.Join(contextDir, "archives")
	require.NoError(t, os.Mkdir(archiveDir, 0o700), "creating archives directory")

	// create a normal layout somewhere under contextDir
	goodLayout, err := os.MkdirTemp(subdirsDir, "goodlayout") //nolint:usetesting
	require.NoErrorf(t, err, "creating a known-good OCI layout")
	diffDigest, blobDigest := generateLayout(t, goodLayout)
	goodLayoutRef, err := alltransports.ParseImageName("oci:" + goodLayout)
	require.NoErrorf(t, err, "parsing image reference to known-good OCI layout")
	sus, err := scanDir(goodLayout)
	require.NoError(t, err, "scanning known-good OCI layout")
	assert.False(t, sus, "check on known-good OCI layout")

	// copy to a directory
	goodDir, err := os.MkdirTemp(subdirsDir, "gooddir") //nolint:usetesting
	require.NoErrorf(t, err, "creating a temporary directory to store a good directory")
	goodDirRef, err := alltransports.ParseImageName("dir:" + goodDir)
	require.NoErrorf(t, err, "parsing image reference to good directory")
	_, err = imageCopy.Image(ctx, policyContext, goodDirRef, goodLayoutRef, nil)
	require.NoError(t, err, "copying an acceptable OCI layout to a directory")

	// scan the directory
	sus, err = scanDir(goodDir)
	require.NoError(t, err, "scanning good directory")
	assert.False(t, sus, "check on good directory")

	// copy to a docker-archive
	goodDockerArchiveFile, err := os.CreateTemp(archiveDir, "gooddockerarchive")
	require.NoErrorf(t, err, "creating a temporary file to store a good docker archive")
	goodDockerArchive := goodDockerArchiveFile.Name()
	goodDockerArchiveRef, err := alltransports.ParseImageName("docker-archive:" + goodDockerArchive)
	require.NoErrorf(t, err, "parsing image reference to good docker archive")
	require.NoError(t, err, goodDockerArchiveFile.Close())
	require.NoError(t, err, os.Remove(goodDockerArchive))
	_, err = imageCopy.Image(ctx, policyContext, goodDockerArchiveRef, goodLayoutRef, nil)
	require.NoError(t, err, "copying an acceptable OCI layout to a docker archive")

	// scan the docker-archive
	sus, err = scanArchive(goodDockerArchive)
	require.NoError(t, err, "scanning good docker archive")
	assert.True(t, sus, "check on good docker archive") // there are symlinks in there

	// copy to an oci-archive
	goodOCIArchiveFile, err := os.CreateTemp(archiveDir, "goodociarchive")
	require.NoErrorf(t, err, "creating a temporary file to store a good oci archive")
	goodOCIArchive := goodOCIArchiveFile.Name()
	goodOCIArchiveRef, err := alltransports.ParseImageName("oci-archive:" + goodOCIArchive)
	require.NoErrorf(t, err, "parsing image reference to good oci archive")
	require.NoError(t, err, goodOCIArchiveFile.Close())
	require.NoError(t, err, os.Remove(goodOCIArchive))
	_, err = imageCopy.Image(ctx, policyContext, goodOCIArchiveRef, goodLayoutRef, nil)
	require.NoError(t, err, "copying an acceptable OCI layout to an OCI archive")

	// scan the oci-archive
	sus, err = scanArchive(goodOCIArchive)
	require.NoError(t, err, "scanning good OCI archive")
	assert.False(t, sus, "check on good OCI archive")

	// make sure the original versions can all be read without error
	requireImageReadable(t, transports.ImageName(goodLayoutRef))
	requireImageReadable(t, transports.ImageName(goodDirRef))
	requireImageReadable(t, transports.ImageName(goodOCIArchiveRef))
	requireImageReadable(t, transports.ImageName(goodDockerArchiveRef))

	// sanitize them all
	goodLayoutRel, err := filepath.Rel(contextDir, goodLayout)
	require.NoErrorf(t, err, "converting absolute path %q to a relative one", goodLayout)
	newGoodLayout, err := ImageName(store, "oci", goodLayoutRel, contextDir, t.TempDir(), false)
	require.NoError(t, err, "sanitizing good OCI layout")
	goodDirRel, err := filepath.Rel(contextDir, goodDir)
	require.NoErrorf(t, err, "converting absolute path %q to a relative one", goodDir)
	newGoodDir, err := ImageName(store, "dir", goodDirRel, contextDir, t.TempDir(), false)
	require.NoError(t, err, "sanitizing good directory")
	goodOCIArchiveRel, err := filepath.Rel(contextDir, goodOCIArchive)
	require.NoErrorf(t, err, "converting absolute path %q to a relative one", goodOCIArchive)
	newGoodOCIArchive, err := ImageName(store, "oci-archive", goodOCIArchiveRel, contextDir, t.TempDir(), false)
	require.NoError(t, err, "sanitizing good OCI archive")
	goodDockerArchiveRel, err := filepath.Rel(contextDir, goodDockerArchive)
	require.NoErrorf(t, err, "converting absolute path %q to a relative one", goodDockerArchive)
	newGoodDockerArchive, err := ImageName(store, "docker-archive", goodDockerArchiveRel, contextDir, t.TempDir(), false)
	require.NoError(t, err, "sanitizing good docker archive")

	// make sure the sanitized versions can all be read without error
	requireImageReadable(t, newGoodLayout)
	requireImageReadable(t, newGoodDir)
	requireImageReadable(t, newGoodOCIArchive)
	requireImageReadable(t, newGoodDockerArchive)

	// mutate them all
	badLayout := mutateDirectory(t, contextDir, goodLayout, filepath.Join(v1.ImageBlobsDir, blobDigest.Algorithm().String(), blobDigest.Encoded()))
	badLayoutRel, err := filepath.Rel(contextDir, badLayout)
	require.NoErrorf(t, err, "converting absolute path %q to a relative one", badLayout)
	_, err = ImageName(store, "oci", badLayoutRel, contextDir, t.TempDir(), false)
	require.ErrorIs(t, err, os.ErrNotExist, "sanitizing bad OCI layout")

	badDir := mutateDirectory(t, contextDir, goodDir, blobDigest.Encoded())
	badDirRel, err := filepath.Rel(contextDir, badDir)
	require.NoErrorf(t, err, "converting absolute path %q to a relative one", badDir)
	_, err = ImageName(store, "dir", badDirRel, contextDir, t.TempDir(), false)
	require.ErrorIs(t, err, os.ErrNotExist, "sanitizing bad directory")

	badOCIArchive := mutateArchive(t, contextDir, goodOCIArchive, filepath.Join(v1.ImageBlobsDir, blobDigest.Algorithm().String(), blobDigest.Encoded()))
	badOCIArchiveRel, err := filepath.Rel(contextDir, badOCIArchive)
	require.NoErrorf(t, err, "converting absolute path %q to a relative one", badOCIArchive)
	_, err = ImageName(store, "oci-archive", badOCIArchiveRel, contextDir, t.TempDir(), false)
	require.ErrorContains(t, err, "invalid symbolic link", "sanitizing bad oci archive")

	badDockerArchive := mutateArchive(t, contextDir, goodDockerArchive, diffDigest.Encoded()+".tar")
	badDockerArchiveRel, err := filepath.Rel(contextDir, badDockerArchive)
	require.NoErrorf(t, err, "converting absolute path %q to a relative one", badDockerArchive)
	_, err = ImageName(store, "docker-archive", badDockerArchiveRel, contextDir, t.TempDir(), false)
	require.ErrorContains(t, err, "invalid symbolic link", "sanitizing bad docker archive")

	// sanitize with absolute paths (simulating --from CLI flag)
	// For archives, the file is read directly and still sanitized.
	// For directories, the absolute path is returned as-is.
	newAbsOCIArchive, err := ImageName(store, "oci-archive", goodOCIArchive, contextDir, t.TempDir(), true)
	require.NoError(t, err, "sanitizing absolute OCI archive path")
	requireImageReadable(t, newAbsOCIArchive)

	newAbsDockerArchive, err := ImageName(store, "docker-archive", goodDockerArchive, contextDir, t.TempDir(), true)
	require.NoError(t, err, "sanitizing absolute docker archive path")
	requireImageReadable(t, newAbsDockerArchive)

	newAbsLayout, err := ImageName(store, "oci", goodLayout, contextDir, t.TempDir(), true)
	require.NoError(t, err, "sanitizing absolute OCI layout path")
	assert.Equal(t, "oci:"+goodLayout, newAbsLayout)
	requireImageReadable(t, newAbsLayout)

	newAbsDir, err := ImageName(store, "dir", goodDir, contextDir, t.TempDir(), true)
	require.NoError(t, err, "sanitizing absolute directory path")
	assert.Equal(t, "dir:"+goodDir, newAbsDir)
	requireImageReadable(t, newAbsDir)
}
