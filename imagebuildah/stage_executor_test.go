package imagebuildah

import (
	"encoding/json"
	"strconv"
	"testing"

	v1 "github.com/opencontainers/image-spec/specs-go/v1"
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

func TestStageMatchesNoCacheFilter(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		description   string
		noCacheFilter map[string]struct{}
		stageName     string
		stagePosition int
		matches       bool
	}{
		{
			description:   "empty filter never matches",
			noCacheFilter: map[string]struct{}{},
			stageName:     "build",
			stagePosition: 0,
			matches:       false,
		},
		{
			description:   "matches by stage name",
			noCacheFilter: map[string]struct{}{"build": {}},
			stageName:     "build",
			stagePosition: 0,
			matches:       true,
		},
		{
			description:   "matches by numeric position",
			noCacheFilter: map[string]struct{}{"1": {}},
			stageName:     "1",
			stagePosition: 1,
			matches:       true,
		},
		{
			description:   "unrelated name in filter does not match",
			noCacheFilter: map[string]struct{}{"other": {}},
			stageName:     "build",
			stagePosition: 0,
			matches:       false,
		},
		{
			description:   "named stage not matched by its numeric position alone",
			noCacheFilter: map[string]struct{}{"2": {}},
			stageName:     "build",
			stagePosition: 0,
			matches:       false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			matches := stageMatchesNoCacheFilter(tc.noCacheFilter, tc.stageName, tc.stagePosition)
			assert.Equal(t, tc.matches, matches, tc.description)
		})
	}
}
