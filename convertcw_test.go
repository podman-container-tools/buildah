package buildah

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/buildah/internal/mkcw"
	mkcwtypes "go.podman.io/buildah/internal/mkcw/types"
	"go.podman.io/storage"
)

// dummyAttestationHandler replies with a fixed response code to requests to
// the right path, and caches passphrases indexed by workload ID
type dummyAttestationHandler struct {
	t               *testing.T
	status          int
	passphrases     map[string]string
	passphrasesLock sync.Mutex
}

func (d *dummyAttestationHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	var body bytes.Buffer
	if req.Body != nil {
		if _, err := io.Copy(&body, req.Body); err != nil {
			d.t.Logf("reading request body: %v", err)
			return
		}
		req.Body.Close()
	}
	if req.URL != nil && req.URL.Path == "/kbs/v0/register_workload" {
		var registrationRequest mkcwtypes.RegistrationRequest
		// if we can't decode the client request, bail
		if err := json.Unmarshal(body.Bytes(), &registrationRequest); err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			return
		}
		// cache the passphrase
		d.passphrasesLock.Lock()
		if d.passphrases == nil {
			d.passphrases = make(map[string]string)
		}
		d.passphrases[registrationRequest.WorkloadID] = registrationRequest.Passphrase
		d.passphrasesLock.Unlock()
		// return the predetermined status
		status := d.status
		if status == 0 {
			status = http.StatusOK
		}
		rw.WriteHeader(status)
		return
	}
	// no such handler
	rw.WriteHeader(http.StatusInternalServerError)
}

func TestCWConvertImage(t *testing.T) {
	// This test cannot be parallelized as this uses NewBuilder()
	// which eventually and indirectly accesses a global variable
	// defined in `go-selinux`, this must be fixed at `go-selinux`
	// or builder must enable sometime of locking mechanism i.e if
	// routine is creating Builder other's must wait for it.
	// Tracked here: https://github.com/containers/buildah/issues/5967
	ctx := t.Context()
	for _, status := range []int{http.StatusOK, http.StatusInternalServerError} {
		for _, ignoreChainRetrievalErrors := range []bool{false, true} {
			for _, ignoreAttestationErrors := range []bool{false, true} {
				t.Run(fmt.Sprintf("status~%d~ignoreChainRetrievalErrors~%v~ignoreAttestationErrors~%v", status, ignoreChainRetrievalErrors, ignoreAttestationErrors), func(t *testing.T) {
					// create a per-test Store object
					storeOptions := storage.StoreOptions{
						GraphRoot:       t.TempDir(),
						RunRoot:         t.TempDir(),
						GraphDriverName: "vfs",
					}
					store, err := storage.GetStore(storeOptions)
					require.NoError(t, err)
					t.Cleanup(func() {
						if _, err := store.Shutdown(true); err != nil {
							t.Logf("store.Shutdown(%q): %v", t.Name(), err)
						}
					})
					// listen on a system-assigned port
					listener, err := net.Listen("tcp", ":0")
					require.NoError(t, err)
					// keep track of our listener address
					addr := listener.Addr()
					// serve requests on that listener
					handler := &dummyAttestationHandler{t: t, status: status}
					server := http.Server{
						Handler: handler,
					}
					go func() {
						if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
							t.Logf("serve: %v", err)
						}
					}()
					// clean up at the end of this test
					t.Cleanup(func() { assert.NoError(t, server.Close()) })
					// convert an image
					options := CWConvertImageOptions{
						InputImage:              "docker.io/library/busybox",
						Tag:                     "localhost/busybox:encrypted",
						AttestationURL:          "http://" + addr.String(),
						IgnoreAttestationErrors: ignoreAttestationErrors,
						Slop:                    "16MB",
						SignaturePolicyPath:     testSystemContext.SignaturePolicyPath,
					}
					id, _, _, err := CWConvertImage(ctx, &testSystemContext, store, options)
					if status != http.StatusOK && !ignoreAttestationErrors {
						assert.Error(t, err)
						return
					}
					if ignoreChainRetrievalErrors && ignoreAttestationErrors {
						assert.NoError(t, err)
					}
					if err != nil {
						t.Skipf("%s: %v", t.Name(), err)
						return
					}
					// mount the image
					path, err := store.MountImage(id, nil, "")
					require.NoError(t, err)
					t.Cleanup(func() {
						if _, err := store.UnmountImage(id, true); err != nil {
							t.Logf("store.UnmountImage(%q): %v", t.Name(), err)
						}
					})
					// check that the image's contents look like what we expect: disk
					disk := filepath.Join(path, "disk.img")
					require.FileExists(t, disk)
					workloadConfig, err := mkcw.ReadWorkloadConfigFromImage(disk)
					require.NoError(t, err)
					handler.passphrasesLock.Lock()
					decryptionPassphrase := handler.passphrases[workloadConfig.WorkloadID]
					handler.passphrasesLock.Unlock()
					err = mkcw.CheckLUKSPassphrase(disk, decryptionPassphrase)
					assert.NoError(t, err)
					// check that the image's contents look like what we expect: config file
					config := filepath.Join(path, "krun-sev.json")
					require.FileExists(t, config)
					workloadConfigBytes, err := os.ReadFile(config)
					require.NoError(t, err)
					var workloadConfigTwo mkcwtypes.WorkloadConfig
					err = json.Unmarshal(workloadConfigBytes, &workloadConfigTwo)
					require.NoError(t, err)
					assert.Equal(t, workloadConfig, workloadConfigTwo)
					// check that the image's contents look like what we expect: an executable entry point
					entrypoint := filepath.Join(path, "entrypoint")
					require.FileExists(t, entrypoint)
					st, err := os.Stat(entrypoint)
					require.NoError(t, err)
					assert.Equal(t, st.Mode().Type(), os.FileMode(0)) // regular file
				})
			}
		}
	}
}
