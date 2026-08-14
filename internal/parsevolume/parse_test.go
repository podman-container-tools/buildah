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
