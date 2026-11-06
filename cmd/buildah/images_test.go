package main

import (
	"testing"
)

func TestSizeFormatting(t *testing.T) {
	t.Parallel()
	size := formattedSize(0)
	if size != "0 B" {
		t.Errorf("Error formatting size: expected '%s' got '%s'", "0 B", size)
	}

	size = formattedSize(1000)
	if size != "1 KB" {
		t.Errorf("Error formatting size: expected '%s' got '%s'", "1 KB", size)
	}

	size = formattedSize(1000 * 1000 * 1000 * 1000)
	if size != "1 TB" {
		t.Errorf("Error formatting size: expected '%s' got '%s'", "1 TB", size)
	}
}

func TestMatchWithTag(t *testing.T) {
	t.Parallel()
	isMatch := matchesReference("gcr.io/pause:latest", "pause:latest")
	if !isMatch {
		t.Error("expected match, got not match")
	}

	isMatch = matchesReference("gcr.io/pause:latest", "kubernetes/pause:latest")
	if isMatch {
		t.Error("expected not match, got match")
	}
}

func TestNoMatchesReferenceWithTag(t *testing.T) {
	t.Parallel()
	isMatch := matchesReference("gcr.io/pause:latest", "redis:latest")
	if isMatch {
		t.Error("expected no match, got match")
	}

	isMatch = matchesReference("gcr.io/pause:latest", "kubernetes/redis:latest")
	if isMatch {
		t.Error("expected no match, got match")
	}
}

func TestMatchesReferenceWithoutTag(t *testing.T) {
	t.Parallel()
	isMatch := matchesReference("gcr.io/pause:latest", "pause")
	if !isMatch {
		t.Error("expected match, got not match")
	}

	isMatch = matchesReference("gcr.io/pause:latest", "kubernetes/pause")
	if isMatch {
		t.Error("expected not match, got match")
	}
}

func TestNoMatchesReferenceWithoutTag(t *testing.T) {
	t.Parallel()
	isMatch := matchesReference("gcr.io/pause:latest", "redis")
	if isMatch {
		t.Error("expected no match, got match")
	}

	isMatch = matchesReference("gcr.io/pause:latest", "kubernetes/redis")
	if isMatch {
		t.Error("expected no match, got match")
	}
}

// bugfix to #7201: matchesReference used to panic on a name with no colon and an arg with a colon,
// if the name ended with the part of the arg before that colon.
func TestMatchesReferenceNameWithoutTag(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		args  []string
		match bool
	}{
		// A container with no base image.
		{"scratch", []string{
			"scratch:latest", "scratch:", "cratch:x",
			"ratch:x", "atch:x", "tch:x", "ch:x", "h:x",
			":x", ":latest", ":",
		}, false},
		{"scratch", []string{"scratch"}, true},
		// A container from an image with no name.
		{"", []string{":x", ":latest", ":"}, false},
		// A name with a registry port and a tag.
		{"localhost:5000/foo:v2", []string{"localhost:5000/foo:v2", "localhost:5000/foo"}, true},
	} {
		for _, arg := range tc.args {
			t.Run("name="+tc.name+"/arg="+arg, func(t *testing.T) {
				t.Parallel()
				// Recover so that a panic fails this case instead of the whole test binary.
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("matchesReference(%q, %q) panicked: %v", tc.name, arg, r)
					}
				}()
				if got := matchesReference(tc.name, arg); got != tc.match {
					t.Errorf("matchesReference(%q, %q) = %v, want %v", tc.name, arg, got, tc.match)
				}
			})
		}
	}
}
