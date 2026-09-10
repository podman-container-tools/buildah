package imagebuildah

import (
	"os"
	"testing"

	"go.podman.io/storage/pkg/reexec"
)

// TestMain lets the helpers that the build path re-executes, such as the copier, run as
// themselves instead of re-entering the test binary.
func TestMain(m *testing.M) {
	if reexec.Init() {
		return
	}
	os.Exit(m.Run())
}
