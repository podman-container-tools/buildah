package mkcw

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSlop(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		input  int64
		slop   string
		output int64
	}{
		{100, "", 125},
		{100, "10%", 110},
		{100, "100%", 200},
		{100, "10GB", 10*1024*1024*1024 + 100},
		{100, "10%+10GB", 10*1024*1024*1024 + 110},
		{100, "10% + 10GB", 10*1024*1024*1024 + 110},
	}
	for _, testCase := range testCases {
		t.Run(testCase.slop, func(t *testing.T) {
			assert.Equal(t, testCase.output, slop(testCase.input, testCase.slop))
		})
	}
}

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
		var registrationRequest RegistrationRequest
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

func TestArchive(t *testing.T) {
	t.Parallel()
	ociConfig := &v1.Image{
		Config: v1.ImageConfig{
			User:       "root",
			Env:        []string{"PATH=/usr/local/bin:/usr/local/sbin:/usr/bin:/usr/sbin:/usr/sbin:/sbin:/usr/sbin:/sbin"},
			Cmd:        []string{"/bin/bash"},
			WorkingDir: "/root",
			Labels: map[string]string{
				"label_a": "b",
				"label_c": "d",
			},
		},
	}
	for _, status := range []int{http.StatusOK, http.StatusInternalServerError} {
		for _, ignoreChainRetrievalErrors := range []bool{false, true} {
			for _, ignoreMeasurementErrors := range []bool{false, true} {
				for _, ignoreAttestationErrors := range []bool{false, true} {
					for _, requestIgnoreAttestationErrors := range []bool{false, true} {
						t.Run(fmt.Sprintf("status_%d+ignoreChainRetrievalErrors_%v+ignoreMeasurementErrors_%v+ignoreAttestationErrors_%v+requestIgnoreAttestationErrors_%v", status, ignoreChainRetrievalErrors, ignoreMeasurementErrors, ignoreAttestationErrors, requestIgnoreAttestationErrors), func(t *testing.T) {
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
							// generate the container rootfs using a temporary empty directory
							archiveOptions := ArchiveOptions{
								CPUs:                    4,
								Memory:                  256,
								TempDir:                 t.TempDir(),
								AttestationURL:          "http://" + addr.String(),
								IgnoreAttestationErrors: requestIgnoreAttestationErrors,
							}
							inputPath := t.TempDir()
							rc, workloadConfig, err := Archive(t.Context(), inputPath, ociConfig, archiveOptions)
							// bail now if we got an error we didn't expect
							if errors.As(err, &chainRetrievalError{}) {
								if !ignoreChainRetrievalErrors {
									if errors.Is(err, exec.ErrNotFound) {
										t.Skip("sevctl not found")
									}
									require.NoError(t, err)
								}
								return
							}
							if errors.As(err, &measurementError{}) {
								if !ignoreMeasurementErrors {
									if errors.Is(err, os.ErrNotExist) {
										t.Skip("firmware shared library not found")
									}
									require.NoError(t, err)
								}
								return
							}
							if errors.As(err, &attestationError{}) {
								if !ignoreAttestationErrors {
									if status != http.StatusInternalServerError {
										// not an intentionally-returned error
										require.NoError(t, err)
									}
								}
								return
							}
							require.NoError(t, err)
							defer rc.Close()
							// read each archive entry's contents into a map
							contents := make(map[string][]byte)
							tr := tar.NewReader(rc)
							hdr, err := tr.Next()
							for hdr != nil {
								contents[hdr.Name], err = io.ReadAll(tr)
								require.NoError(t, err)
								hdr, err = tr.Next()
							}
							if err != nil {
								require.ErrorIs(t, err, io.EOF)
							}
							// check that krun-sev.json is a JSON-encoded copy of the workload config
							var writtenWorkloadConfig WorkloadConfig
							err = json.Unmarshal(contents["krun-sev.json"], &writtenWorkloadConfig)
							require.NoError(t, err)
							assert.Equal(t, workloadConfig, writtenWorkloadConfig)
							// save the disk image to a file
							encryptedFile := filepath.Join(t.TempDir(), "encrypted.img")
							err = os.WriteFile(encryptedFile, contents["disk.img"], 0o600)
							require.NoError(t, err)
							// check that we have a configuration footer in there
							_, err = ReadWorkloadConfigFromImage(encryptedFile)
							require.NoError(t, err)
							// check that the attestation server got the encryption passphrase
							handler.passphrasesLock.Lock()
							passphrase := handler.passphrases[workloadConfig.WorkloadID]
							handler.passphrasesLock.Unlock()
							err = CheckLUKSPassphrase(encryptedFile, passphrase)
							require.NoError(t, err)
						})
					}
				}
			}
		}
	}
}
