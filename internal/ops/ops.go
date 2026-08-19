package ops

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"syscall"

	digest "github.com/opencontainers/go-digest"
	"go.podman.io/storage"
)

// buildahExportInfoLayer holds information about a layer and its contents
type buildahExportInfoLayer struct {
	LayerID string                             `json:"layer"`   // the ID of the layer described here
	Digests map[digest.Algorithm]digest.Digest `json:"digests"` // uncompressed digest of the layer named by LayerID
	Size    int64                              `json:"size"`    // uncompressed size of the diff for the layer named by LayerID
}

const (
	buildahExportInfoBigDataKey = "buildah-export-info-v1"
)

// readLayerInfoCache: a helper to read a cached record of a layer's digest and size
// from its big data.
func readLayerInfoCacheRaw(store storage.Store, layerID string) (*buildahExportInfoLayer, error) {
	rc, err := store.LayerBigData(layerID, buildahExportInfoBigDataKey)
	if err != nil && !errors.Is(err, syscall.ENOENT) {
		return nil, err
	}
	var exportInfoLayer buildahExportInfoLayer
	buf, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("reading cached info about layer %q: %w", layerID, errors.Join(err, rc.Close()))
	}
	err = json.Unmarshal(buf, &exportInfoLayer)
	if err != nil {
		return nil, fmt.Errorf("parsing cached info about layer %q: %w", layerID, errors.Join(err, rc.Close()))
	}
	if exportInfoLayer.LayerID != layerID {
		return nil, fmt.Errorf("validating cached info about layer %q: %w", layerID, errors.Join(err, rc.Close()))
	}
	return &exportInfoLayer, rc.Close()
}

// readLayerInfoCache: a helper to read a cached record of a layer's digest and size
// from its big data.
func readLayerInfoCache(store storage.Store, layerID string, diffIDsByLayerID map[string]digest.Digest, diffSizesByLayerID map[string]int64, diffSizesByDiffID map[digest.Digest]int64, digests map[digest.Algorithm]digest.Digest, mustHaveDigestAlgorithm digest.Algorithm) error {
	exportInfoLayer, err := readLayerInfoCacheRaw(store, layerID)
	if err != nil {
		return err
	}
	if mustHaveDigestAlgorithm.String() != "" {
		if _, ok := exportInfoLayer.Digests[mustHaveDigestAlgorithm]; !ok {
			return fmt.Errorf("cached info about layer %q did not include %q digest: %w", layerID, mustHaveDigestAlgorithm.String(), syscall.ENOENT)
		}
		diffIDsByLayerID[layerID] = exportInfoLayer.Digests[mustHaveDigestAlgorithm]
	} else {
		for _, v := range exportInfoLayer.Digests {
			diffIDsByLayerID[layerID] = v
			if v.Algorithm() == digest.Canonical {
				break
			}
		}
	}
	diffSizesByLayerID[layerID] = exportInfoLayer.Size
	diffSizesByDiffID[exportInfoLayer.Digests[mustHaveDigestAlgorithm]] = exportInfoLayer.Size
	if digests != nil {
		maps.Copy(digests, exportInfoLayer.Digests)
	}
	return nil
}

// encodeLayerInfoCache: a helper to encode a cache of a layer's digest and
// size for storing in its big data.
func encodeLayerInfoCache(layerID string, layerDigests map[digest.Algorithm]digest.Digest, layerSize int64) (storage.LayerBigDataOption, error) {
	exportInfoLayer := buildahExportInfoLayer{
		LayerID: layerID,
		Digests: layerDigests,
		Size:    layerSize,
	}
	var opt storage.LayerBigDataOption
	buf, err := json.Marshal(&exportInfoLayer)
	if err != nil {
		return opt, fmt.Errorf("encoding info about layer for cache: %w", err)
	}
	opt.Key = buildahExportInfoBigDataKey
	opt.Data = bytes.NewReader(buf)
	return opt, nil
}

// writeLayerInfoCache: a helper to write a cache of a layer's digest and size to
// its big data.
func writeLayerInfoCache(store storage.Store, layerID string, layerDigests map[digest.Algorithm]digest.Digest, layerSize int64) error {
	exportInfoLayer := buildahExportInfoLayer{
		LayerID: layerID,
		Digests: layerDigests,
		Size:    layerSize,
	}
	buf, err := json.Marshal(&exportInfoLayer)
	if err != nil {
		return fmt.Errorf("encoding info about layer for cache: %w", err)
	}
	if err := store.SetLayerBigData(layerID, buildahExportInfoBigDataKey, bytes.NewReader(buf)); err != nil {
		return fmt.Errorf("saving %q for layer with ID or name %q: %w", buildahExportInfoBigDataKey, layerID, err)
	}
	return err
}
