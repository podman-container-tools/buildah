// Package buildoutput parses custom build output options specified via the build --output flag.
package buildoutput

import (
	"fmt"
	"strings"
)

// Type identifies the destination format for a custom build output.
type Type int

const (
	// TypeInvalid indicates an unset or invalid build output type.
	TypeInvalid Type = 0
	// TypeStdout streams the build output tar archive to stdout.
	TypeStdout Type = 1
	// TypeLocalDir exports the build output to a local directory.
	TypeLocalDir Type = 2
	// TypeTar exports the build output to a tar archive file.
	TypeTar Type = 3
)

// String returns a human-readable representation of the build output type.
func (t Type) String() string {
	switch t {
	case TypeInvalid:
		return "invalid (unset)!"
	case TypeStdout:
		return "stdout"
	case TypeLocalDir:
		return "local"
	case TypeTar:
		return "tar"
	}
	return fmt.Sprintf("unknown %d!", int(t))
}

// Option contains the outcome of parsing the value of a build --output flag.
type Option struct {
	Type Type
	Path string // Only valid if Type is local dir or tar
}

// GetBuildOutput is responsible for parsing custom build output argument i.e `build --output` flag.
// Takes `buildOutput` as string and returns Option.
func GetBuildOutput(buildOutput string) (Option, error) {
	// Support simple values, in the form --output ./mydir
	if !strings.Contains(buildOutput, ",") && !strings.Contains(buildOutput, "=") {
		if buildOutput == "-" {
			// Feature parity with buildkit, output tar to stdout
			// Read more here: https://docs.docker.com/engine/reference/commandline/build/#custom-build-outputs
			return Option{
				Type: TypeStdout,
				Path: "",
			}, nil
		}

		return Option{
			Type: TypeLocalDir,
			Path: buildOutput,
		}, nil
	}

	// Support complex values, in the form --output type=local,dest=./mydir
	typeSelected := TypeInvalid
	pathSelected := ""
	for option := range strings.SplitSeq(buildOutput, ",") {
		key, value, found := strings.Cut(option, "=")
		if !found {
			return Option{}, fmt.Errorf("invalid build output options %q, expected format key=value", buildOutput)
		}
		switch key {
		case "type":
			if typeSelected != TypeInvalid {
				return Option{}, fmt.Errorf("duplicate %q not supported", key)
			}
			switch value {
			case "local":
				typeSelected = TypeLocalDir
			case "tar":
				typeSelected = TypeTar
			default:
				return Option{}, fmt.Errorf("invalid type %q selected for build output options %q", value, buildOutput)
			}
		case "dest":
			if pathSelected != "" {
				return Option{}, fmt.Errorf("duplicate %q not supported", key)
			}
			pathSelected = value
		default:
			return Option{}, fmt.Errorf("unrecognized key %q in build output option: %q", key, buildOutput)
		}
	}

	// Validate there is a type
	if typeSelected == TypeInvalid {
		return Option{}, fmt.Errorf("missing required key %q in build output option: %q", "type", buildOutput)
	}

	// Validate path
	if typeSelected == TypeLocalDir || typeSelected == TypeTar {
		if pathSelected == "" {
			return Option{}, fmt.Errorf("missing required key %q in build output option: %q", "dest", buildOutput)
		}
	} else {
		// Clear path when not needed by type
		pathSelected = ""
	}

	// Handle redirecting stdout for tar output
	if pathSelected == "-" {
		if typeSelected == TypeTar {
			typeSelected = TypeStdout
			pathSelected = ""
		} else {
			return Option{}, fmt.Errorf(`invalid build output option %q, only "type=tar" can be used with "dest=-"`, buildOutput)
		}
	}

	return Option{
		Type: typeSelected,
		Path: pathSelected,
	}, nil
}
