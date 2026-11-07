package control

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	controlapi "github.com/moby/buildkit/api/services/control"
	rspec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/buildah"
	"go.podman.io/common/libimage"
	imageStorage "go.podman.io/image/v5/storage"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
	storageTypes "go.podman.io/storage/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// These tests can't be parallelized: they use buildah.NewBuilder(), which
// touches a global in go-selinux (see containers/buildah#5967)

var testSystemContext = types.SystemContext{
	SignaturePolicyPath:      "../../../tests/policy.json",
	SystemRegistriesConfPath: "../../../tests/registries.conf",
}

// imageCounter keeps image names and layer contents unique, so storage never
// reuses a layer between two test images
var imageCounter atomic.Int64

func TestMain(m *testing.M) {
	if buildah.InitReexec() {
		return
	}
	os.Exit(m.Run())
}

// faultyStore injects errors into the store calls usageRecords makes
type faultyStore struct {
	storage.Store
	containersErr error
	diffSizeErr   map[string]error // keyed by layer ID
	onDiffSize    func()
	onImage       func(id string)
	deleteImage   func(id string) error // runs before the real DeleteImage; an error is returned instead
}

func (s *faultyStore) Containers() ([]storage.Container, error) {
	if s.containersErr != nil {
		return nil, s.containersErr
	}
	return s.Store.Containers()
}

func (s *faultyStore) DiffSize(from, to string) (int64, error) {
	if s.onDiffSize != nil {
		s.onDiffSize()
	}
	if err, ok := s.diffSizeErr[to]; ok {
		return -1, err
	}
	return s.Store.DiffSize(from, to)
}

func newTestStore(t *testing.T) storage.Store {
	t.Helper()
	driver := os.Getenv("STORAGE_DRIVER")
	if driver == "" {
		driver = "vfs"
	}
	store, err := storage.GetStore(storageTypes.StoreOptions{
		RunRoot:         t.TempDir(),
		GraphRoot:       t.TempDir(),
		GraphDriverName: driver,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		if _, err := store.Shutdown(true); err != nil {
			t.Logf("shutting down temporary store: %v", err)
		}
	})
	return store
}

func newTestServer(t *testing.T, store storage.Store) *controlServer {
	t.Helper()
	srv, err := NewServer(store, &libimage.RuntimeOptions{SystemContext: &testSystemContext})
	require.NoError(t, err)
	return srv.(*controlServer)
}

// commitImage builds a tagged image on top of from ("scratch" for none) and
// returns its ID. With emptyLayer set, the commit only changes config, so the
// image should reuse its parent's top layer
func commitImage(t *testing.T, store storage.Store, from string, emptyLayer bool) string {
	t.Helper()
	ctx := t.Context()
	n := imageCounter.Add(1)

	b, err := buildah.NewBuilder(ctx, store, buildah.BuilderOptions{
		FromImage:     from,
		SystemContext: &testSystemContext,
		NamespaceOptions: []buildah.NamespaceOption{{
			Name: string(rspec.NetworkNamespace),
			Host: true,
		}},
	})
	require.NoError(t, err)
	// NewBuilder leaves a working container behind, which would make the
	// image look in use
	defer func() { assert.NoError(t, b.Delete()) }()

	if emptyLayer {
		b.SetEnv("STEP", fmt.Sprint(n))
	} else {
		file := filepath.Join(t.TempDir(), fmt.Sprintf("file%d", n))
		require.NoError(t, os.WriteFile(file, fmt.Appendf(nil, "layer %d", n), 0o644))
		require.NoError(t, b.AddContext(ctx, "/", false, buildah.AddAndCopyOptions{}, file))
	}
	b.SetCreatedBy(fmt.Sprintf("step %d", n))

	ref, err := imageStorage.Transport.ParseStoreReference(store, fmt.Sprintf("localhost/control-test:%d", n))
	require.NoError(t, err)
	id, _, _, err := b.Commit(ctx, ref, buildah.CommitOptions{
		SystemContext: &testSystemContext,
		EmptyLayer:    emptyLayer,
	})
	require.NoError(t, err)
	return id
}

// chain builds n tagged images, each on top of the previous one, and returns
// their IDs base first
func chain(t *testing.T, store storage.Store, n int) []string {
	t.Helper()
	ids := make([]string, 0, n)
	from := "scratch"
	for range n {
		id := commitImage(t, store, from, false)
		ids = append(ids, id)
		from = id
	}
	return ids
}

func untag(t *testing.T, store storage.Store, ids ...string) {
	t.Helper()
	for _, id := range ids {
		img, err := store.Image(id)
		require.NoError(t, err)
		require.NoError(t, store.RemoveNames(id, img.Names))
	}
}

func topLayer(t *testing.T, store storage.Store, id string) string {
	t.Helper()
	img, err := store.Image(id)
	require.NoError(t, err)
	return img.TopLayer
}

func addToManifestList(t *testing.T, srv *controlServer, id string) {
	t.Helper()
	list, err := srv.runtime.CreateManifestList(fmt.Sprintf("localhost/control-test-list-%d", imageCounter.Add(1)))
	require.NoError(t, err)
	_, err = list.Add(t.Context(), id, &libimage.ManifestListAddOptions{})
	require.NoError(t, err)
}

func diskUsage(t *testing.T, srv *controlServer) map[string]*controlapi.UsageRecord {
	t.Helper()
	resp, err := srv.DiskUsage(t.Context(), &controlapi.DiskUsageRequest{})
	require.NoError(t, err)
	byID := make(map[string]*controlapi.UsageRecord, len(resp.GetRecord()))
	for _, rec := range resp.GetRecord() {
		byID[rec.GetID()] = rec
	}
	return byID
}

func TestDiskUsageRequestValidation(t *testing.T) {
	srv := newTestServer(t, newTestStore(t))
	for _, tc := range []struct {
		name string
		req  *controlapi.DiskUsageRequest
		code codes.Code
	}{
		{"nil request", nil, codes.InvalidArgument},
		{"filter", &controlapi.DiskUsageRequest{Filter: []string{"type==regular"}}, codes.Unimplemented},
		{"age limit", &controlapi.DiskUsageRequest{AgeLimit: int64(time.Hour)}, codes.Unimplemented},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := srv.DiskUsage(t.Context(), tc.req)
			assert.Equal(t, tc.code, status.Code(err))
		})
	}
}

// expected is what a scenario expects for one record
type expected struct {
	inUse  bool
	shared bool
	parent string
}

// Each scenario returns the records DiskUsage should report, by image ID
func TestDiskUsageInUse(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, store storage.Store, srv *controlServer) map[string]expected
	}{
		{"empty store", func(*testing.T, storage.Store, *controlServer) map[string]expected {
			return map[string]expected{}
		}},
		{"chain under a tag", func(t *testing.T, store storage.Store, _ *controlServer) map[string]expected {
			c := chain(t, store, 3)
			untag(t, store, c[0], c[1])
			return map[string]expected{
				c[0]: {shared: true},
				c[1]: {shared: true, parent: c[0]},
				c[2]: {shared: true, parent: c[1]},
			}
		}},
		{"abandoned chain", func(t *testing.T, store storage.Store, _ *controlServer) map[string]expected {
			c := chain(t, store, 3)
			untag(t, store, c...)
			return map[string]expected{
				c[0]: {},
				c[1]: {parent: c[0]},
				c[2]: {parent: c[1]},
			}
		}},
		{"container on middle image", func(t *testing.T, store storage.Store, _ *controlServer) map[string]expected {
			c := chain(t, store, 3)
			untag(t, store, c...)
			_, err := store.CreateContainer("", nil, c[1], "", "", nil)
			require.NoError(t, err)
			return map[string]expected{
				c[0]: {inUse: true},
				c[1]: {inUse: true, parent: c[0]},
				c[2]: {parent: c[1]},
			}
		}},
		{"branch with one tagged side", func(t *testing.T, store storage.Store, _ *controlServer) map[string]expected {
			base := commitImage(t, store, "scratch", false)
			left := commitImage(t, store, base, false) // stays tagged
			right := commitImage(t, store, base, false)
			untag(t, store, base, right)
			return map[string]expected{
				base:  {shared: true},
				left:  {shared: true, parent: base},
				right: {parent: base},
			}
		}},
		// The tagged list itself isn't a record
		{"manifest list instance with child", func(t *testing.T, store storage.Store, srv *controlServer) map[string]expected {
			c := chain(t, store, 3)
			untag(t, store, c...)
			addToManifestList(t, srv, c[1])
			return map[string]expected{
				c[0]: {inUse: true},
				c[1]: {inUse: true, parent: c[0]},
				c[2]: {parent: c[1]},
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newTestStore(t)
			srv := newTestServer(t, store)
			want := tc.setup(t, store, srv)

			recs := diskUsage(t, srv)
			require.Len(t, recs, len(want))
			for id, w := range want {
				require.Contains(t, recs, id)
				rec := recs[id]
				assert.Equal(t, w.inUse, rec.GetInUse(), "InUse of %s", id)
				assert.Equal(t, w.shared, rec.GetShared(), "Shared of %s", id)
				if w.parent == "" {
					assert.Empty(t, rec.GetParents(), "Parents of %s", id)
				} else {
					assert.Equal(t, []string{w.parent}, rec.GetParents(), "Parents of %s", id)
				}
				assert.Equal(t, recordTypeRegular, rec.GetRecordType())
				assert.NotNil(t, rec.GetCreatedAt())
				assert.Nil(t, rec.GetLastUsedAt(), "not tracked by buildah")
			}
		})
	}
}

// a config-only step reuses its parent's top layer and must not count it again
func TestDiskUsageSize(t *testing.T) {
	store := newTestStore(t)
	srv := newTestServer(t, store)
	base := commitImage(t, store, "scratch", false)
	meta := commitImage(t, store, base, true)
	untag(t, store, base, meta)
	require.Equal(t, topLayer(t, store, base), topLayer(t, store, meta), "harness assumption: EmptyLayer reuses the parent's top layer")

	recs := diskUsage(t, srv)
	require.Len(t, recs, 2)
	assert.Positive(t, recs[base].GetSize())
	assert.Zero(t, recs[meta].GetSize())
}

// Each case runs against two untagged images, base <- child, with one store
// call failing
func TestDiskUsageStoreErrors(t *testing.T) {
	errBoom := errors.New("boom")
	for _, tc := range []struct {
		name    string
		wrap    func(t *testing.T, store storage.Store, ids []string, cancel context.CancelFunc) *faultyStore
		wantErr error
		wantLen int
	}{
		{
			// removed between ListImages and DiffSize: drop that record only
			name: "layer removed",
			wrap: func(t *testing.T, store storage.Store, ids []string, _ context.CancelFunc) *faultyStore {
				return &faultyStore{Store: store, diffSizeErr: map[string]error{topLayer(t, store, ids[1]): storage.ErrLayerUnknown}}
			},
			wantLen: 1,
		},
		{
			// any other DiffSize error fails the call rather than reporting 0
			name: "size error",
			wrap: func(t *testing.T, store storage.Store, ids []string, _ context.CancelFunc) *faultyStore {
				return &faultyStore{Store: store, diffSizeErr: map[string]error{topLayer(t, store, ids[0]): errBoom}}
			},
			wantErr: errBoom,
		},
		{
			name: "containers error",
			wrap: func(_ *testing.T, store storage.Store, _ []string, _ context.CancelFunc) *faultyStore {
				return &faultyStore{Store: store, containersErr: errBoom}
			},
			wantErr: errBoom,
		},
		{
			// cancelling during the first DiffSize must stop the loop before
			// the second record
			name: "cancelled mid-loop",
			wrap: func(_ *testing.T, store storage.Store, _ []string, cancel context.CancelFunc) *faultyStore {
				return &faultyStore{Store: store, onDiffSize: cancel}
			},
			wantErr: context.Canceled,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newTestStore(t)
			ids := chain(t, store, 2)
			untag(t, store, ids...)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			srv := newTestServer(t, tc.wrap(t, store, ids, cancel))

			resp, err := srv.DiskUsage(ctx, &controlapi.DiskUsageRequest{})
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Len(t, resp.GetRecord(), tc.wantLen)
		})
	}
}

func (s *faultyStore) Image(id string) (*storage.Image, error) {
	if s.onImage != nil {
		s.onImage(id)
	}
	return s.Store.Image(id)
}

func (s *faultyStore) DeleteImage(id string, commit bool) ([]string, error) {
	if s.deleteImage != nil {
		if err := s.deleteImage(id); err != nil {
			return nil, err
		}
	}
	return s.Store.DeleteImage(id, commit)
}

// pruneStream collects what Prune sends, or fails every Send with err
type pruneStream struct {
	grpc.ServerStream
	ctx  context.Context
	err  error
	sent []*controlapi.UsageRecord
}

func (s *pruneStream) Context() context.Context { return s.ctx }

func (s *pruneStream) Send(rec *controlapi.UsageRecord) error {
	if s.err != nil {
		return s.err
	}
	s.sent = append(s.sent, rec)
	return nil
}

func TestPruneRequestValidation(t *testing.T) {
	srv := newTestServer(t, newTestStore(t))
	for _, tc := range []struct {
		name string
		req  *controlapi.PruneRequest
		code codes.Code
	}{
		{"nil request", nil, codes.InvalidArgument},
		{"filter", &controlapi.PruneRequest{Filter: []string{"type==regular"}}, codes.Unimplemented},
		{"keep duration", &controlapi.PruneRequest{KeepDuration: int64(time.Hour)}, codes.Unimplemented},
		{"reserved space", &controlapi.PruneRequest{ReservedSpace: 1}, codes.Unimplemented},
		{"max used space", &controlapi.PruneRequest{MaxUsedSpace: 1}, codes.Unimplemented},
		{"min free space", &controlapi.PruneRequest{MinFreeSpace: 1}, codes.Unimplemented},
		{"all is accepted", &controlapi.PruneRequest{All: true}, codes.OK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := srv.Prune(tc.req, &pruneStream{ctx: t.Context()})
			assert.Equal(t, tc.code, status.Code(err))
		})
	}
}

// Prune removes exactly the records DiskUsage reports as neither in use nor
// shared, across every way an image can be protected
func TestPrune(t *testing.T) {
	store := newTestStore(t)
	srv := newTestServer(t, store)

	tagged := chain(t, store, 3)
	untag(t, store, tagged[0], tagged[1])
	abandoned := chain(t, store, 2)
	untag(t, store, abandoned...)
	withCtr := commitImage(t, store, "scratch", false)
	untag(t, store, withCtr)
	_, err := store.CreateContainer("", nil, withCtr, "", "", nil)
	require.NoError(t, err)
	// libimage sees withCtr as dangling once aboveCtr is gone, so without
	// NoPrune it tries to remove withCtr too and the error hides aboveCtr's record
	aboveCtr := commitImage(t, store, withCtr, false)
	untag(t, store, aboveCtr)
	listed := chain(t, store, 3)
	untag(t, store, listed...)
	addToManifestList(t, srv, listed[1])

	removed := []string{abandoned[0], abandoned[1], listed[2], aboveCtr}
	kept := []string{tagged[0], tagged[1], tagged[2], withCtr, listed[0], listed[1]}

	stream := &pruneStream{ctx: t.Context()}
	require.NoError(t, srv.Prune(&controlapi.PruneRequest{}, stream))

	var sent []string
	for _, rec := range stream.sent {
		sent = append(sent, rec.GetID())
		assert.False(t, rec.GetInUse())
		assert.Positive(t, rec.GetSize())
	}
	assert.ElementsMatch(t, removed, sent)
	for _, id := range removed {
		_, err := store.Image(id)
		assert.ErrorIs(t, err, storage.ErrImageUnknown, "%s should be removed", id)
	}
	for _, id := range kept {
		_, err := store.Image(id)
		assert.NoError(t, err, "%s should be kept", id)
	}

	// nothing is left to remove the second time
	stream = &pruneStream{ctx: t.Context()}
	require.NoError(t, srv.Prune(&controlapi.PruneRequest{}, stream))
	assert.Empty(t, stream.sent)

	// All removes shared images too, but never a tagged one
	stream = &pruneStream{ctx: t.Context()}
	require.NoError(t, srv.Prune(&controlapi.PruneRequest{All: true}, stream))
	sent = sent[:0]
	for _, rec := range stream.sent {
		sent = append(sent, rec.GetID())
	}
	assert.ElementsMatch(t, []string{tagged[0], tagged[1]}, sent)
	_, err = store.Image(tagged[2])
	assert.NoError(t, err, "a tagged image is never pruned")
}

// Each case prunes three independent untagged images with one thing going
// wrong, so the outcome doesn't depend on removal order
func TestPruneFailures(t *testing.T) {
	errBoom := errors.New("boom")
	for _, tc := range []struct {
		name     string
		setup    func(t *testing.T, s *faultyStore, stream *pruneStream, ids []string, cancel context.CancelFunc)
		wantErr  error
		wantKept int
		wantSent int
	}{
		{
			name: "image tagged after listing is skipped",
			setup: func(t *testing.T, s *faultyStore, _ *pruneStream, ids []string, _ context.CancelFunc) {
				s.onImage = func(id string) {
					if id == ids[0] {
						s.onImage = nil
						assert.NoError(t, s.Store.AddNames(id, []string{"localhost/late:tag"}))
					}
				}
			},
			wantKept: 1,
			wantSent: 2,
		},
		{
			name: "image a container started using is skipped",
			setup: func(_ *testing.T, s *faultyStore, _ *pruneStream, ids []string, _ context.CancelFunc) {
				s.deleteImage = func(id string) error {
					if id == ids[0] {
						return storage.ErrImageUsedByContainer
					}
					return nil
				}
			},
			wantKept: 1,
			wantSent: 2,
		},
		{
			name: "removal error stops pruning",
			setup: func(_ *testing.T, s *faultyStore, _ *pruneStream, _ []string, _ context.CancelFunc) {
				s.deleteImage = func(string) error { return errBoom }
			},
			wantErr:  errBoom,
			wantKept: 3,
		},
		{
			name: "send failure doesn't stop pruning",
			setup: func(_ *testing.T, _ *faultyStore, stream *pruneStream, _ []string, _ context.CancelFunc) {
				stream.err = errBoom
			},
			wantErr: errBoom,
		},
		{
			name: "cancellation stops pruning",
			setup: func(_ *testing.T, s *faultyStore, _ *pruneStream, _ []string, cancel context.CancelFunc) {
				s.deleteImage = func(string) error {
					cancel()
					return nil
				}
			},
			wantErr:  context.Canceled,
			wantKept: 2,
			wantSent: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newTestStore(t)
			var ids []string
			for range 3 {
				ids = append(ids, commitImage(t, store, "scratch", false))
			}
			untag(t, store, ids...)

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			s := &faultyStore{Store: store}
			stream := &pruneStream{ctx: ctx}
			tc.setup(t, s, stream, ids, cancel)
			srv := newTestServer(t, s)

			err := srv.Prune(&controlapi.PruneRequest{}, stream)
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
			} else {
				assert.NoError(t, err)
			}
			kept := 0
			for _, id := range ids {
				if _, err := store.Image(id); err == nil {
					kept++
				}
			}
			assert.Equal(t, tc.wantKept, kept, "images left in storage")
			assert.Len(t, stream.sent, tc.wantSent, "records sent")
		})
	}
}
