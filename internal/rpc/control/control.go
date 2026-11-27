package control

import (
	"context"
	"errors"
	"fmt"
	"slices"

	controlapi "github.com/moby/buildkit/api/services/control"
	digest "github.com/opencontainers/go-digest"
	"go.podman.io/common/libimage"
	"go.podman.io/storage"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// mirrors github.com/moby/buildkit/client.UsageRecordTypeRegular
const recordTypeRegular = "regular"

type controlServer struct {
	controlapi.UnimplementedControlServer
	store   storage.Store
	runtime *libimage.Runtime
}

// NewServer returns a ControlServer backed by store. The libimage runtime is
// derived from store so that the two can't disagree
func NewServer(store storage.Store, opts *libimage.RuntimeOptions) (controlapi.ControlServer, error) {
	runtime, err := libimage.RuntimeFromStore(store, opts)
	if err != nil {
		return nil, err
	}
	return &controlServer{store: store, runtime: runtime}, nil
}

func (c *controlServer) DiskUsage(ctx context.Context, req *controlapi.DiskUsageRequest) (*controlapi.DiskUsageResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if len(req.GetFilter()) != 0 || req.GetAgeLimit() != 0 {
		return nil, status.Error(codes.Unimplemented, "filters and age limits are not supported")
	}
	records, err := c.usageRecords(ctx)
	if err != nil {
		return nil, err
	}
	return &controlapi.DiskUsageResponse{Record: records}, nil
}

// usageRecords returns one record per image except manifest lists
func (c *controlServer) usageRecords(ctx context.Context) ([]*controlapi.UsageRecord, error) {
	// SetListData computes every image's parent from a single layer tree
	images, err := c.runtime.ListImages(ctx, &libimage.ListImagesOptions{SetListData: true})
	if err != nil {
		return nil, err
	}

	// One pass over storage containers instead of img.Containers() per image, covers
	// buildah working containers
	containers, err := c.store.Containers()
	if err != nil {
		return nil, err
	}
	hasContainer := make(map[string]bool, len(containers))
	for _, ctr := range containers {
		hasContainer[ctr.ImageID] = true
	}

	parentOf := make(map[string]string, len(images))
	for _, img := range images {
		if p := img.ListData.Parent; p != nil {
			parentOf[img.ID()] = p.ID()
		}
	}

	// Manifest lists and the images they reference, errors aren't skipped
	isList := make(map[string]bool)
	inManifestList := make(map[digest.Digest]bool)
	for _, img := range images {
		if img.TopLayer() != "" {
			continue // manifest lists have no layers
		}
		list, err := img.ToManifestList()
		if err != nil {
			if errors.Is(err, libimage.ErrNotAManifestList) || errors.Is(err, storage.ErrImageUnknown) {
				continue
			}
			return nil, err
		}
		data, err := list.Inspect()
		if err != nil {
			return nil, err
		}
		isList[img.ID()] = true
		for _, m := range data.Manifests {
			inManifestList[m.Digest] = true
		}
	}

	// Containers and manifest lists mark an image and its parents in use, tags
	// mark them shared
	inUse := make(map[string]bool, len(images))
	shared := make(map[string]bool, len(images))
	mark := func(m map[string]bool, id string) {
		for ; id != "" && !m[id]; id = parentOf[id] {
			m[id] = true
		}
	}
	for _, img := range images {
		referenced := slices.ContainsFunc(img.Digests(), func(d digest.Digest) bool {
			return inManifestList[d]
		})
		if hasContainer[img.ID()] || referenced {
			mark(inUse, img.ID())
		}
		if len(img.Names()) != 0 {
			mark(shared, img.ID())
		}
	}

	var records []*controlapi.UsageRecord
	for _, img := range images {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		id := img.ID()
		if isList[id] {
			continue
		}
		size, err := c.ownSize(img, img.ListData.Parent)
		if err != nil {
			if errors.Is(err, storage.ErrLayerUnknown) {
				continue // removed since ListImages
			}
			return nil, fmt.Errorf("computing size of image %s: %w", id, err)
		}
		rec := &controlapi.UsageRecord{
			ID:         id,
			CreatedAt:  timestamppb.New(img.Created()),
			RecordType: recordTypeRegular,
			InUse:      inUse[id],
			Shared:     shared[id],
			Size:       size,
			// Not tracked by buildah: LastUsedAt, UsageCount
		}
		if p := parentOf[id]; p != "" {
			rec.Parents = []string{p}
		}
		records = append(records, rec)
	}

	return records, nil
}

// ownSize returns the size of the layer img adds on top of its parent, or 0
// if it adds none
func (c *controlServer) ownSize(img, parent *libimage.Image) (int64, error) {
	top := img.TopLayer()
	if top == "" || (parent != nil && parent.TopLayer() == top) {
		return 0, nil
	}
	return c.store.DiffSize("", top)
}

// Prune removes records not in use or shared (shared too with All), never a
// tagged image, and streams each removed record
func (c *controlServer) Prune(req *controlapi.PruneRequest, stream controlapi.Control_PruneServer) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "nil request")
	}
	if len(req.GetFilter()) != 0 || req.GetKeepDuration() != 0 || req.GetReservedSpace() != 0 ||
		req.GetMaxUsedSpace() != 0 || req.GetMinFreeSpace() != 0 {
		return status.Error(codes.Unimplemented, "filters, keep duration and space limits are not supported")
	}

	ctx := stream.Context()
	records, err := c.usageRecords(ctx)
	if err != nil {
		return err
	}

	// After a send failure keep pruning without reporting; stop at the first removal failure
	var sendErr error
	for _, rec := range records {
		if rec.GetInUse() || (rec.GetShared() && !req.GetAll()) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return errors.Join(err, sendErr)
		}
		id := rec.GetID()
		// Never remove a tagged image
		img, err := c.store.Image(id)
		if err != nil {
			if errors.Is(err, storage.ErrImageUnknown) {
				continue
			}
			return errors.Join(fmt.Errorf("looking up image %s: %w", id, err), sendErr)
		}
		if len(img.Names) != 0 {
			continue
		}
		// NoPrune: remove only this image, not its parents
		_, rmErrs := c.runtime.RemoveImages(ctx, []string{id}, &libimage.RemoveImagesOptions{NoPrune: true})
		if err := errors.Join(rmErrs...); err != nil {
			if errors.Is(err, storage.ErrImageUnknown) || errors.Is(err, storage.ErrImageUsedByContainer) {
				continue
			}
			return errors.Join(fmt.Errorf("removing image %s: %w", id, err), sendErr)
		}
		if sendErr == nil {
			sendErr = stream.Send(rec)
		}
	}
	return sendErr
}
