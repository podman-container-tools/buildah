package imagebuildah

import (
	"context"
	"encoding/json"
	"errors"
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

func TestWaitForStage(t *testing.T) {
	t.Parallel()

	stages := imagebuilder.Stages{
		{Position: 0, Name: "stage0"},
		{Position: 1, Name: "stage1"},
	}

	testErr := errors.New("simulated stage error")

	testCases := []struct {
		name            string
		stageName       string
		terminatedStage map[int]error
		expectedIsStage bool
		expectedErr     error
	}{
		{
			name:            "non-existent stage",
			stageName:       "nonexistent",
			terminatedStage: map[int]error{},
			expectedIsStage: false,
			expectedErr:     nil,
		},
		{
			name:            "stage completed successfully",
			stageName:       "stage0",
			terminatedStage: map[int]error{0: nil},
			expectedIsStage: true,
			expectedErr:     nil,
		},
		{
			name:            "stage failed with error",
			stageName:       "stage0",
			terminatedStage: map[int]error{0: testErr},
			expectedIsStage: true,
			expectedErr:     testErr,
		},
		{
			name:            "stage referenced by index failed with error",
			stageName:       "0",
			terminatedStage: map[int]error{0: testErr},
			expectedIsStage: true,
			expectedErr:     testErr,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			b := &executor{
				terminatedStage: tc.terminatedStage,
				stages:          make(map[int]*stageExecutor),
			}
			isStage, err := b.waitForStage(context.Background(), tc.stageName, stages)
			assert.Equal(t, tc.expectedIsStage, isStage)
			if tc.expectedErr != nil {
				assert.ErrorIs(t, err, tc.expectedErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
