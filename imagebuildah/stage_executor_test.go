package imagebuildah

import (
	"encoding/json"
	"strconv"
	"testing"

	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/openshift/imagebuilder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHistoryEntriesEqual(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		a, b  string
		equal bool
	}{
		{
			a:     `{}`,
			b:     `{}`,
			equal: true,
		},
		{
			a:     `{"created":"2020-06-17T00:22:25.47282687Z"}`,
			b:     `{"created":"2020-06-17T00:22:25.47282687Z"}`,
			equal: true,
		},
		{
			a:     `{"created":"2020-07-16T12:38:26.733333497-04:00"}`,
			b:     `{"created":"2020-07-16T12:38:26.733333497-04:00"}`,
			equal: true,
		},
		{
			a:     `{"created":"2020-07-16T12:38:26.733333497-04:00"}`,
			b:     `{"created":"2020-07-16T12:38:26.733333497Z"}`,
			equal: false,
		},
		{
			a:     `{"created":"2020-07-16T12:38:26.733333497Z"}`,
			b:     `{}`,
			equal: false,
		},
		{
			a:     `{}`,
			b:     `{"created":"2020-07-16T12:38:26.733333497Z"}`,
			equal: false,
		},
		{
			a:     `{"comment":"thing"}`,
			b:     `{"comment":"thing"}`,
			equal: true,
		},
		{
			a:     `{"comment":"thing","ignored-field-for-testing":"ignored"}`,
			b:     `{"comment":"thing"}`,
			equal: true,
		},
		{
			a:     `{"CoMmEnT":"thing"}`,
			b:     `{"comment":"thing"}`,
			equal: true,
		},
		{
			a:     `{"comment":"thing"}`,
			b:     `{"comment":"things"}`,
			equal: false,
		},
		{
			a:     `{"author":"respected"}`,
			b:     `{"author":"respected"}`,
			equal: true,
		},
		{
			a:     `{"author":"respected"}`,
			b:     `{"author":"discredited"}`,
			equal: false,
		},
		{
			a:     `{"created_by":"actions"}`,
			b:     `{"created_by":"actions"}`,
			equal: true,
		},
		{
			a:     `{"created_by":"jiggery"}`,
			b:     `{"created_by":"pokery"}`,
			equal: false,
		},
	}
	for i := range testCases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			var a, b v1.History
			err := json.Unmarshal([]byte(testCases[i].a), &a)
			require.Nil(t, err, "error unmarshalling history %q: %v", testCases[i].a, err)
			err = json.Unmarshal([]byte(testCases[i].b), &b)
			require.Nil(t, err, "error unmarshalling history %q: %v", testCases[i].b, err)
			equal := historyEntriesEqual(a, b)
			assert.Equal(t, testCases[i].equal, equal, "historyEntriesEqual(%q, %q) != %v", testCases[i].a, testCases[i].b, testCases[i].equal)
		})
	}
}

func TestQuoteLabelValue(t *testing.T) {
	t.Parallel()

	env := []string{"VERSION=1.2", "EMPTY="}
	testCases := []struct {
		name, input, expected string
	}{
		{name: "plain", input: "value", expected: "value"},
		{name: "undefined", input: "$something", expected: "$something"},
		{name: "undefinedBraced", input: "${something}", expected: "${something}"},
		{name: "trailingDollar", input: "cost$", expected: "cost$"},
		{name: "set", input: "$VERSION", expected: "1.2"},
		{name: "setBraced", input: "v${VERSION}-rc", expected: "v1.2-rc"},
		{name: "setEmpty", input: "a${EMPTY}b", expected: "ab"},
		{name: "mixed", input: "$VERSION-$something", expected: "1.2-$something"},
		{name: "modifier", input: "${something:-fallback}", expected: "fallback"},
		{name: "escapedDollar", input: `\$VERSION`, expected: `\1.2`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			resolved, err := imagebuilder.ProcessWord(quoteLabelValue(testCase.input, env), env)
			require.NoError(t, err)
			assert.Equal(t, testCase.expected, resolved)
		})
	}
}

func TestLabelInstruction(t *testing.T) {
	t.Parallel()
	assert.Equal(t, `LABEL "a"="\$b" "c"=""`, labelInstruction([]string{"a=$b", "c", "=ignored"}, nil))
}
