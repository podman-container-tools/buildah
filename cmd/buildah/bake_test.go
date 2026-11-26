package main

import (
	"path/filepath"
	"reflect"
	"testing"
)

// This is tested at the unit level because ulimits are not exposed in the
// resulting image or build output, so a Bats integration test cannot reliably
// verify that inherited and child values are merged correctly.
func TestResolveBakeTargetMergesUlimits(t *testing.T) {
	config := bakeConfig{
		Target: map[string]bakeTarget{
			"base": {
				Ulimits: []string{"cpu=300"},
			},
			"child": {
				Inherits: []string{"base"},
				Ulimits:  []string{"cpu=600"},
			},
		},
	}

	got, err := resolveBakeTarget(config, "child", make(map[string]bool))
	if err != nil {
		t.Fatalf("resolveBakeTarget() error = %v", err)
	}

	want := []string{"cpu=300", "cpu=600"}
	if !reflect.DeepEqual(got.Ulimits, want) {
		t.Errorf("Ulimits = %v, want %v", got.Ulimits, want)
	}
}

// This is a unit test because platform replacement is an internal target-
// resolution detail. Testing it through Bats would require invoking a build
// and would introduce unnecessary dependencies on platform availability.
func TestResolveBakeTargetReplacesPlatforms(t *testing.T) {
	config := bakeConfig{
		Target: map[string]bakeTarget{
			"base": {
				Platforms: []string{"linux/amd64"},
			},
			"child": {
				Inherits:  []string{"base"},
				Platforms: []string{"linux/arm64"},
			},
		},
	}

	got, err := resolveBakeTarget(config, "child", make(map[string]bool))
	if err != nil {
		t.Fatalf("resolveBakeTarget() error = %v", err)
	}

	want := []string{"linux/arm64"}
	if !reflect.DeepEqual(got.Platforms, want) {
		t.Errorf("Platforms = %v, want %v", got.Platforms, want)
	}
}

func TestIsRemoteBakeContext(t *testing.T) {
	for _, test := range []struct {
		context string
		want    bool
	}{
		{context: ".", want: false},
		{context: "./context", want: false},
		{context: "/tmp/context", want: false},
		{context: "https://example.com/context.tar.gz", want: true},
		{context: "http://example.com/context.tar.gz", want: true},
		{context: "https://github.com/example/project.git#main", want: true},
		{context: "git://example.com/example/project.git", want: true},
		{context: "github.com/example/project", want: true},
		{context: "github.com/example/project.git#main", want: true},
		{context: "ssh://git@example.com/example/project.git", want: false},
		{context: "git@example.com:example/project.git", want: false},
		{context: "file:///tmp/context", want: false},
		{context: "oci://example.com/context", want: false},
		{context: "container-image://alpine", want: false},
	} {
		t.Run(test.context, func(t *testing.T) {
			if got := isRemoteBakeContext(test.context); got != test.want {
				t.Errorf("isRemoteBakeContext(%q) = %t, want %t", test.context, got, test.want)
			}
		})
	}
}

func TestBakeDockerfilePath(t *testing.T) {
	for _, test := range []struct {
		name       string
		context    string
		dockerfile string
		want       string
	}{
		{name: "local context", context: "context", dockerfile: "nested/Dockerfile", want: filepath.Join("context", "nested/Dockerfile")},
		{name: "absolute Dockerfile", context: "context", dockerfile: "/tmp/Dockerfile", want: "/tmp/Dockerfile"},
		{name: "remote HTTP context", context: "https://example.com/context.tar.gz", dockerfile: "nested/Dockerfile", want: "nested/Dockerfile"},
		{name: "remote Git context", context: "git://example.com/example/project.git#main", dockerfile: "Dockerfile", want: "Dockerfile"},
		{name: "remote GitHub shorthand context", context: "github.com/example/project", dockerfile: "nested/Dockerfile", want: "nested/Dockerfile"},
		{name: "remote tarball context", context: "https://example.com/context.tar.gz", dockerfile: "nested/Dockerfile", want: "nested/Dockerfile"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := bakeDockerfilePath(test.context, test.dockerfile); got != test.want {
				t.Errorf("bakeDockerfilePath(%q, %q) = %q, want %q", test.context, test.dockerfile, got, test.want)
			}
		})
	}
}

func TestResolveBakeTargetOverridesNoCache(t *testing.T) {
	noCache := true
	childNoCache := false

	config := bakeConfig{
		Target: map[string]bakeTarget{
			"base": {
				NoCache: &noCache,
			},
			"child": {
				Inherits: []string{"base"},
				NoCache:  &childNoCache,
			},
		},
	}

	got, err := resolveBakeTarget(config, "child", make(map[string]bool))
	if err != nil {
		t.Fatalf("resolveBakeTarget() error = %v", err)
	}

	if got.NoCache == nil {
		t.Fatal("NoCache = nil, want false")
	}
	if *got.NoCache {
		t.Error("NoCache = true, want false")
	}
}
