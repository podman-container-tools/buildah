package main

import (
	"reflect"
	"testing"
)

// This is tested at the unit level because ulimits are not exposed in the
// resulting image or build output, so a Bats integration test cannot reliably
// verify that inherited and child values are merged correctly
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
