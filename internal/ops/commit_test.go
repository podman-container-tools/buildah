package ops

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	digest "github.com/opencontainers/go-digest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/system"
	"go.podman.io/storage/types"
)

func testCreateStorage(t *testing.T) storage.Store {
	topdir := filepath.Join(t.TempDir(), "storage")
	root := filepath.Join(topdir, "root")
	run := filepath.Join(topdir, "runroot")
	store, err := storage.GetStore(types.StoreOptions{
		RunRoot:         run,
		GraphRoot:       root,
		GraphDriverName: "vfs",
	})
	require.NoError(t, err, "initializing test storage")
	t.Cleanup(func() {
		t.Log("shutting down storage")
		mounted, err := store.Shutdown(true)
		assert.NoError(t, err, "error shutting down test storage")
		assert.Empty(t, mounted, "list of still-mounted layers")
		assert.NoError(t, system.EnsureRemoveAll(topdir), "clearing storage")
		assert.Empty(t, mounted, "expected to not have mounted layers left over in test storage")
		t.Log("shut down storage")
	})
	t.Log("initialized storage")
	return store
}

func testCreateSomeLayers(t *testing.T, store storage.Store) LayerSpecs {
	// create some storage layers in sequence, but without copying their contents to generate diff and size metadata
	var layerSpecs LayerSpecs
	var layerList []string
	for i := range 10 {
		parentLayer := ""
		if len(layerList) > 2 && len(layerList)%2 == 1 {
			parentLayer = layerList[len(layerList)-2]
		}
		layer, err := store.CreateLayer("", parentLayer, nil, "", true, &storage.LayerOptions{})
		require.NoErrorf(t, err, "creating layer %d", i)
		mountPoint, err := store.Mount(layer.ID, "")
		require.NoErrorf(t, err, "mounting layer %d", i)
		err = os.WriteFile(filepath.Join(mountPoint, fmt.Sprintf("layerfile%d.txt", i)), fmt.Appendf(nil, "this is file %d", i), 0o600)
		require.NoErrorf(t, err, "writing a file to layer %d", i)
		err = os.WriteFile(filepath.Join(mountPoint, "binary"), fmt.Appendf(nil, "this is file %d", i), 0o755)
		require.NoErrorf(t, err, `writing a "binary" file to layer %d`, i)
		_, err = store.Unmount(layer.ID, true)
		require.NoErrorf(t, err, "unmounting layer %d", i)
		if parentLayer != "" {
			t.Logf("created layer %q based on %q", layer.ID, parentLayer)
		} else {
			t.Logf("created layer %q", layer.ID)
		}
		layerSpecs = append(layerSpecs, LayerSpec{
			LayerSource: LayerSource{
				Type:    LayerSourceTypeLayer,
				LayerID: layer.ID,
			},
		})
		layerList = append(layerList, layer.ID)
		if i%3 == 0 {
			tempDir := t.TempDir()
			tempFile, err := os.Create(filepath.Join(tempDir, fmt.Sprintf("directoryfile%d", i/3)))
			require.NoError(t, err)
			_, err = io.Copy(tempFile, strings.NewReader(fmt.Sprintf("this is another file %d", i)))
			require.NoErrorf(t, err, "writing contents to %q", tempFile)
			require.NoError(t, tempFile.Close())
			layerSpecs = append(layerSpecs, LayerSpec{
				LayerSource: LayerSource{
					Type:      LayerSourceTypeDirectory,
					Directory: tempDir,
				},
			})
			t.Logf("created layer in %q", tempDir)
		}
	}
	t.Log("created test layers")
	return layerSpecs
}

func testRunTrashImage(t *testing.T, store storage.Store, imageID string) {
	// don't expect that listing this image (these images?) will work, since there's no data about the size or content of its layers
	cmd := podmanCommand(t, store, "images")
	output, err := cmd.CombinedOutput()
	if errors.Is(err, exec.ErrNotFound) {
		t.Skip("podman not found")
	}
	t.Log("`podman images`:\n" + string(output))
	require.NoErrorf(t, err, "expected no fatal errors trying to list images with podman:\n%q", string(output))

	// expect that they don't immediately get pruned by a system check
	cmd = podmanCommand(t, store, "system", "check", "--max=0", "--repair")
	output, err = cmd.CombinedOutput()
	if errors.Is(err, exec.ErrNotFound) {
		t.Skip("podman not found")
	}
	t.Log("`podman system check --repair`:\n" + string(output))
	require.NoErrorf(t, err, "expected no fatal errors repairing storage with podman:\n%q", string(output))

	cmd = podmanCommand(t, store, "images")
	output, err = cmd.CombinedOutput()
	if errors.Is(err, exec.ErrNotFound) {
		t.Skip("podman not found")
	}
	t.Log("`podman images`:\n" + string(output))
	require.NoErrorf(t, err, "expected no fatal errors trying to list images with podman:\n%q", string(output))

	// hope that we can try to run the image, at least as far as attempting to run the entry point
	cmd = podmanCommand(t, store, "run", "--rm", "--network=host", imageID, "/binary")
	output, err = cmd.CombinedOutput()
	t.Log("`podman run` on the new image with a bogus binary:\n" + string(output))
	assert.Error(t, err, "expected an error trying to run a minimal-but-broken image")
	assert.Regexp(t, "[Ee]xec format error", err.Error()+"\n"+string(output))
}

func TestBuildImageInfo(t *testing.T) {
	for digestAlgorithmNickname, digestAlgorithm := range map[string]digest.Algorithm{
		"default":   "",
		"canonical": digest.Canonical,
		"sha256":    digest.SHA256,
		// "sha512":    digest.SHA512,
	} {
		t.Run(digestAlgorithmNickname, func(t *testing.T) {
			store := testCreateStorage(t)
			layerSpecs := testCreateSomeLayers(t, store)
			t.Logf("calculating image information (layerSpecs= %v)", layerSpecs)
			_, _, _, _, _, err := buildImageInfo(t.Context(), nil, store, layerSpecs, nil, "", imgspecv1.MediaTypeImageConfig, "", CommitOptions{DigestAlgorithm: digestAlgorithm})
			require.NoError(t, err, "calculating image info")
		})
	}
}

func TestBuildImageInfoNotFastEnough(t *testing.T) {
	for digestAlgorithmNickname, digestAlgorithm := range map[string]digest.Algorithm{
		"default":   "",
		"canonical": digest.Canonical,
		"sha256":    digest.SHA256,
		"sha512":    digest.SHA512,
	} {
		t.Run(digestAlgorithmNickname, func(t *testing.T) {
			store := testCreateStorage(t)
			layerSpecs := testCreateSomeLayers(t, store)
			t.Logf("calculating image information (layerSpecs= %v)", layerSpecs)
			subctx, cancel := context.WithTimeout(t.Context(), (1 * time.Nanosecond))
			defer cancel()
			time.Sleep(1 * time.Millisecond)
			_, _, _, _, _, err := buildImageInfo(subctx, nil, store, layerSpecs, nil, "", imgspecv1.MediaTypeImageConfig, "", CommitOptions{DigestAlgorithm: digestAlgorithm})
			require.ErrorIs(t, err, context.DeadlineExceeded, "calculating image info")
		})
	}
}

func TestCommitDumb(t *testing.T) {
	for digestAlgorithmNickname, digestAlgorithm := range map[string]digest.Algorithm{
		"default":   "",
		"canonical": digest.Canonical,
		"sha256":    digest.SHA256,
		// "sha512":    digest.SHA512,
	} {
		t.Run(digestAlgorithmNickname, func(t *testing.T) {
			store := testCreateStorage(t)
			layerSpecs := testCreateSomeLayers(t, store)
			imageID, topLayer, configDigest, imageOptions, disposableLayers, err := buildImageInfo(t.Context(), nil, store, layerSpecs, nil, "", imgspecv1.MediaTypeImageConfig, "", CommitOptions{DigestAlgorithm: digestAlgorithm})
			require.NoError(t, err, "calculating image info")
			t.Logf("could dispose of layers: %v", disposableLayers)

			t.Log("creating image record")
			img, err := store.CreateImage(imageID, nil, topLayer, "", imageOptions)
			require.NoError(t, err, "committing image directly with CreateImage()")
			t.Logf("created image %q with config digest %q", img.ID, configDigest.String())

			t.Log("recalculating image information")
			imageID2, topLayer, configDigest2, imageOptions, disposableLayers2, err := buildImageInfo(t.Context(), nil, store, layerSpecs, nil, "", imgspecv1.MediaTypeImageConfig, "", CommitOptions{DigestAlgorithm: digestAlgorithm})
			require.NoError(t, err, "recalculating image info")
			assert.Equal(t, imageID2, imageID, "recalculated image ID should have been the same")
			assert.Equal(t, configDigest, configDigest2, "recalculated config's digest should have been the same")
			assert.ElementsMatch(t, disposableLayers, disposableLayers2, "expected to derive the same set of disposable layers")
			t.Logf("could dispose of layers: %v", disposableLayers2)

			_, err = store.CreateImage(imageID2, nil, topLayer, "", imageOptions)
			require.ErrorIs(t, err, storage.ErrDuplicateID)

			for _, disposableLayer := range slices.Backward(disposableLayers) {
				if err := store.DeleteLayer(disposableLayer); err != nil {
					t.Logf("did not delete layer %q: %v", disposableLayer, err)
				} else {
					t.Logf("removed disposable layer %q", disposableLayer)
				}
			}

			testRunTrashImage(t, store, img.ID)
		})
	}
}

func TestCommitNotAsDumb(t *testing.T) {
	for digestAlgorithmNickname, digestAlgorithm := range map[string]digest.Algorithm{
		"default":   "",
		"canonical": digest.Canonical,
		"sha256":    digest.SHA256,
		// "sha512":    digest.SHA512,
	} {
		t.Run(digestAlgorithmNickname, func(t *testing.T) {
			store := testCreateStorage(t)
			layerSpecs := testCreateSomeLayers(t, store)
			img, err := Commit(t.Context(), nil, store, layerSpecs, nil, "", "", "", CommitOptions{DigestAlgorithm: digestAlgorithm})
			require.NoError(t, err, "committing image via Commit()")
			t.Logf("committed image %q", img.ID)

			testRunTrashImage(t, store, img.ID)
		})
	}
}

func TestCommitNotAsDumbButNotFastEnough(t *testing.T) {
	for digestAlgorithmNickname, digestAlgorithm := range map[string]digest.Algorithm{
		"default":   "",
		"canonical": digest.Canonical,
		"sha256":    digest.SHA256,
		"sha512":    digest.SHA512,
	} {
		t.Run(digestAlgorithmNickname, func(t *testing.T) {
			store := testCreateStorage(t)
			layerSpecs := testCreateSomeLayers(t, store)
			subctx, cancel := context.WithTimeout(t.Context(), (1 * time.Nanosecond))
			defer cancel()
			time.Sleep(1 * time.Millisecond)
			img, err := Commit(subctx, nil, store, layerSpecs, nil, "", "", "", CommitOptions{DigestAlgorithm: digestAlgorithm})
			require.ErrorIs(t, err, context.DeadlineExceeded, "committing image via Commit()")
			require.Nil(t, img, "committed image?")
		})
	}
}
