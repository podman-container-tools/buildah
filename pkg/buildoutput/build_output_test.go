package buildoutput

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetBuildOutput(t *testing.T) {
	testCases := []struct {
		description string
		input       string
		output      Option
		expectErr   bool
	}{
		{
			description: "hyphen",
			input:       "-",
			output: Option{
				Type: TypeStdout,
			},
		},
		{
			description: "just-a-path",
			input:       "/tmp",
			output: Option{
				Type: TypeLocalDir,
				Path: "/tmp",
			},
		},
		{
			description: "normal-path",
			input:       "type=local,dest=/tmp",
			output: Option{
				Type: TypeLocalDir,
				Path: "/tmp",
			},
		},
		{
			description: "tar-file",
			input:       "type=tar,dest=/tmp/out.tar",
			output: Option{
				Type: TypeTar,
				Path: "/tmp/out.tar",
			},
		},
		{
			description: "tar-stdout",
			input:       "type=tar,dest=-",
			output: Option{
				Type: TypeStdout,
			},
		},
		{
			description: "tar-missing-dest",
			input:       "type=tar",
			expectErr:   true,
		},
		{
			description: "local-missing-dest",
			input:       "type=local",
			expectErr:   true,
		},
		{
			description: "empty-dest",
			input:       "type=tar,dest=",
			expectErr:   true,
		},
		{
			description: "missing-equals",
			input:       "type=tar,",
			expectErr:   true,
		},
		{
			description: "local-stdout",
			input:       "type=local,dest=-",
			expectErr:   true,
		},
		{
			description: "invalid-type",
			input:       "type=wrong,dest=/tmp",
			expectErr:   true,
		},
		{
			description: "missing-type",
			input:       "dest=/tmp",
			expectErr:   true,
		},
		{
			description: "unrecognized-key",
			input:       "foo=bar",
			expectErr:   true,
		},
		{
			description: "duplicate-type",
			input:       "type=local,type=tar,dest=/tmp",
			expectErr:   true,
		},
		{
			description: "duplicate-dest",
			input:       "type=local,dest=/a,dest=/b",
			expectErr:   true,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.description, func(t *testing.T) {
			result, err := GetBuildOutput(testCase.input)
			if testCase.expectErr {
				assert.Error(t, err, "expected error for %q", testCase.input)
				return
			}
			require.NoErrorf(t, err, "expected to be able to parse %q", testCase.input)
			assert.Equal(t, testCase.output, result)
		})
	}
}

func TestTypeString(t *testing.T) {
	testCases := []struct {
		input    Type
		expected string
	}{
		{input: TypeInvalid, expected: "invalid (unset)!"},
		{input: TypeStdout, expected: "stdout"},
		{input: TypeLocalDir, expected: "local"},
		{input: TypeTar, expected: "tar"},
		{input: Type(99), expected: "unknown 99!"},
	}
	for _, testCase := range testCases {
		assert.Equal(t, testCase.expected, testCase.input.String())
	}
}
