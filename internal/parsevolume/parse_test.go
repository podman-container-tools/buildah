package parsevolume

import (
	"reflect"
	"testing"
)

func TestSplitStringWithColonEscape(t *testing.T) {
	tests := []struct {
		volume   string
		expected []string
	}{
		{"/a:/b", []string{"/a", "/b"}},
		{"/a:/b:ro", []string{"/a", "/b", "ro"}},
		// escaped colon inside a path
		{`/a\:b:/c`, []string{"/a:b", "/c"}},
		{`/a:/b\:c`, []string{"/a", "/b:c"}},
		// escaped colon as the second character used to be split wrong
		// because the guard read idx-1 > 0 instead of idx > 0
		{`\:a:/b`, []string{":a", "/b"}},
		// unescaped leading colon is a separator, first field is empty
		{":/a:/b", []string{"", "/a", "/b"}},
		{"/a", []string{"/a"}},
	}
	for _, test := range tests {
		got := SplitStringWithColonEscape(test.volume)
		if !reflect.DeepEqual(got, test.expected) {
			t.Errorf("SplitStringWithColonEscape(%q) = %v, expected %v", test.volume, got, test.expected)
		}
	}
}

func TestVolume(t *testing.T) {
	hostDir := t.TempDir()

	tests := []struct {
		volume      string
		expectedErr bool
		options     []string
	}{
		{hostDir + ":/ctr", false, nil},
		{hostDir + ":/ctr:ro", false, []string{"ro"}},
		{hostDir + ":/ctr:ro,Z", false, []string{"ro", "Z"}},
		// cached and delegated are documented as silently dropped
		{hostDir + ":/ctr:cached", false, nil},
		{hostDir + ":/ctr:ro,delegated", false, []string{"ro"}},
		{hostDir, true, nil},
		{hostDir + ":/ctr:ro,rw", true, nil},
		{"relative/dir:/ctr", true, nil},
	}
	for _, test := range tests {
		mount, err := Volume(test.volume)
		if test.expectedErr {
			if err == nil {
				t.Errorf("Volume(%q) expected an error, got %+v", test.volume, mount)
			}
			continue
		}
		if err != nil {
			t.Errorf("Volume(%q) unexpected error: %v", test.volume, err)
			continue
		}
		if len(mount.Options) != len(test.options) || !reflect.DeepEqual(append([]string{}, mount.Options...), append([]string{}, test.options...)) {
			t.Errorf("Volume(%q) options = %v, expected %v", test.volume, mount.Options, test.options)
		}
		if mount.Source != hostDir || mount.Destination != "/ctr" {
			t.Errorf("Volume(%q) source/dest = %q %q", test.volume, mount.Source, mount.Destination)
		}
	}
}
