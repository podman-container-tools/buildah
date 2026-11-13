package output

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetBuildOutput(t *testing.T) {
	testCases := []struct {
		description string
		input       string
		output      BuildOutputOption
	}{
		{
			description: "hyphen",
			input:       "-",
			output: BuildOutputOption{
				Type: BuildOutputStdout,
			},
		},
		{
			description: "just-a-path",
			input:       "/tmp",
			output: BuildOutputOption{
				Type: BuildOutputLocalDir,
				Path: "/tmp",
			},
		},
		{
			description: "normal-path",
			input:       "type=local,dest=/tmp",
			output: BuildOutputOption{
				Type: BuildOutputLocalDir,
				Path: "/tmp",
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.description, func(t *testing.T) {
			result, err := GetBuildOutput(testCase.input)
			require.NoErrorf(t, err, "expected to be able to parse %q", testCase.input)
			assert.Equal(t, testCase.output, result)
		})
	}
}
