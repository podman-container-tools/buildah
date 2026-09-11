//go:build linux || freebsd

package buildah

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/opencontainers/runtime-tools/generate"
)

func TestAddRlimits(t *testing.T) {
	t.Parallel()
	tt := []struct {
		name   string
		ulimit []string
		test   func(error, *generate.Generator) error
	}{
		{
			name:   "empty ulimit",
			ulimit: []string{},
			test: func(e error, _ *generate.Generator) error {
				return e
			},
		},
		{
			name:   "invalid ulimit argument",
			ulimit: []string{"bla"},
			test: func(e error, _ *generate.Generator) error {
				if e == nil {
					return errors.New("expected to receive an error but got nil")
				}
				errMsg := "invalid ulimit argument"
				if !strings.Contains(e.Error(), errMsg) {
					return fmt.Errorf("expected error message to include %#v in %#v", errMsg, e.Error())
				}
				return nil
			},
		},
		{
			name:   "invalid ulimit type",
			ulimit: []string{"bla=hard"},
			test: func(e error, _ *generate.Generator) error {
				if e == nil {
					return errors.New("expected to receive an error but got nil")
				}
				errMsg := "invalid ulimit type"
				if !strings.Contains(e.Error(), errMsg) {
					return fmt.Errorf("expected error message to include %#v in %#v", errMsg, e.Error())
				}
				return nil
			},
		},
		{
			name:   "valid ulimit",
			ulimit: []string{"fsize=1024:4096"},
			test: func(e error, g *generate.Generator) error {
				if e != nil {
					return e
				}
				rlimits := g.Config.Process.Rlimits
				for _, rlimit := range rlimits {
					if rlimit.Type == "RLIMIT_FSIZE" {
						if rlimit.Hard != 4096 {
							return fmt.Errorf("expected spec to have %#v hard limit set to %v but got %v", rlimit.Type, 4096, rlimit.Hard)
						}
						if rlimit.Soft != 1024 {
							return fmt.Errorf("expected spec to have %#v hard limit set to %v but got %v", rlimit.Type, 1024, rlimit.Soft)
						}
						return nil
					}
				}
				return errors.New("expected spec to have RLIMIT_FSIZE")
			},
		},
	}

	for _, tst := range tt {
		g, _ := generate.New("linux")
		err := addRlimits(tst.ulimit, &g, []string{})
		if testErr := tst.test(err, &g); testErr != nil {
			t.Errorf("test %#v failed: %v", tst.name, testErr)
		}
	}
}
