package imagebuildah

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/openshift/imagebuilder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/image/v5/types"
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
		{name: "escapedSet", input: `\$VERSION`, expected: `$VERSION`},
		{name: "escapedUndefined", input: `\$something`, expected: `$something`},
		{name: "escapedBraced", input: `\${VERSION}`, expected: `${VERSION}`},
		{name: "doubleBackslashSet", input: `\\$VERSION`, expected: `\$VERSION`},
		{name: "doubleBackslashUndefined", input: `\\$something`, expected: `\$something`},
		{name: "tripleBackslash", input: `\\\$VERSION`, expected: `\\$VERSION`},
		{name: "backslashNotBeforeDollar", input: `a\b\\c`, expected: `a\b\\c`},
		{name: "escapedAndExpanded", input: `\$VERSION=$VERSION`, expected: `$VERSION=1.2`},
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

func TestCopyContextDir(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	src := t.TempDir()
	dest := t.TempDir()

	require.NoError(t, os.Mkdir(filepath.Join(src, "subdir"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "included.txt"), []byte("included"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "subdir", "nested.txt"), []byte("nested"), 0o644))
	require.NoError(t, os.Symlink("included.txt", filepath.Join(src, "link.txt")))

	require.NoError(t, copyContextDir(ctx, src, dest))

	content, err := os.ReadFile(filepath.Join(dest, "included.txt"))
	require.NoError(t, err)
	assert.Equal(t, "included", string(content))

	content, err = os.ReadFile(filepath.Join(dest, "subdir", "nested.txt"))
	require.NoError(t, err)
	assert.Equal(t, "nested", string(content))

	linkTarget, err := os.Readlink(filepath.Join(dest, "link.txt"))
	require.NoError(t, err)
	assert.Equal(t, "included.txt", linkTarget)

	require.NoError(t, os.WriteFile(filepath.Join(dest, "included.txt"), []byte("modified"), 0o644))

	content, err = os.ReadFile(filepath.Join(src, "included.txt"))
	require.NoError(t, err)
	assert.Equal(t, "included", string(content))
}

func TestRunStageMountPointsUsesFilteredContextCopy(t *testing.T) {
	tests := []struct {
		name                         string
		contextDir                   string
		contextDirRunBindMount       string
		contextDirWritesAreDiscarded bool
		expectedMountPoint           string
	}{
		{
			name:                         "original context",
			contextDir:                   "/build/context",
			contextDirWritesAreDiscarded: true,
			expectedMountPoint:           "/build/context",
		},
		{
			name:                         "filtered context copy",
			contextDir:                   "/build/context",
			contextDirRunBindMount:       "/tmp/filtered-context",
			contextDirWritesAreDiscarded: true,
			expectedMountPoint:           "/tmp/filtered-context",
		},
		{
			name:                         "temporary context copy without overlay",
			contextDir:                   "/build/context",
			contextDirRunBindMount:       "/tmp/filtered-context",
			contextDirWritesAreDiscarded: false,
			expectedMountPoint:           "/tmp/filtered-context",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &executor{
				contextDir:                   test.contextDir,
				contextDirRunBindMount:       test.contextDirRunBindMount,
				contextDirWritesAreDiscarded: test.contextDirWritesAreDiscarded,
			}
			stage := &stageExecutor{
				executor:      executor,
				systemContext: &types.SystemContext{},
			}

			mountPoints, err := stage.runStageMountPoints(nil)
			require.NoError(t, err)

			assert.Equal(t, test.expectedMountPoint, mountPoints[""].MountPoint)
			assert.Equal(t, test.contextDirWritesAreDiscarded, mountPoints[""].IsWritesDiscardedOverlay)
		})
	}
}

func TestFilterRunBindMountContextConcurrent(t *testing.T) {
	ctx := context.Background()
	contextDir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(contextDir, "included.txt"), []byte("included"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(contextDir, "ignored.txt"), []byte("ignored"), 0o644))

	b := &executor{
		contextDir: contextDir,
		excludes:   []string{"ignored.txt"},
	}

	const callers = 10
	paths := make([]string, callers)
	errs := make([]error, callers)

	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = b.filterRunBindMountContext(ctx)
			b.contextDirFilterLock.Lock()
			paths[i] = b.contextDirRunBindMount
			b.contextDirFilterLock.Unlock()
		}(i)
	}
	wg.Wait()

	for i := range callers {
		require.NoError(t, errs[i])
		assert.Equal(t, paths[0], paths[i])
	}

	require.NotEmpty(t, paths[0])

	_, err := os.Stat(filepath.Join(paths[0], "included.txt"))
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(paths[0], "ignored.txt"))
	assert.True(t, os.IsNotExist(err))

	// Filtering must not modify the original build context.
	_, err = os.Stat(filepath.Join(contextDir, "ignored.txt"))
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, os.RemoveAll(paths[0]))
	})
}
