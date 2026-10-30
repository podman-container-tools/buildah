package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.podman.io/buildah"
)

// TestProcessAnnotationSpec verifies
// processing of the "--annotation" flags.
func TestProcessAnnotationSpec(t *testing.T) {
	tests := []struct {
		name                        string
		beforeImageAnnotations      map[string]string
		beforeTopLayerAnnotations   map[string]string
		spec                        string
		expectedImageAnnotations    map[string]string
		expectedTopLayerAnnotations map[string]string
	}{
		{
			name:                     "set key-value image annotation",
			spec:                     "key=value",
			expectedImageAnnotations: map[string]string{"key": "value"},
		},
		{
			name:                     "set only key image annotation",
			spec:                     "key",
			expectedImageAnnotations: map[string]string{"key": ""},
		},
		{
			name:                     "unset image annotation",
			beforeImageAnnotations:   map[string]string{"k1": "v1", "k2": "v2"},
			spec:                     "k1-",
			expectedImageAnnotations: map[string]string{"k2": "v2"},
		},
		{
			name:                        "clear all image annotations",
			beforeImageAnnotations:      map[string]string{"k1": "v1", "k2": "v2"},
			beforeTopLayerAnnotations:   map[string]string{"lk1": "lv1"},
			spec:                        "-",
			expectedTopLayerAnnotations: map[string]string{"lk1": "lv1"},
		},
		{
			name:                        "set layer annotation",
			spec:                        "layer:key=value",
			expectedTopLayerAnnotations: map[string]string{"key": "value"},
		},
		{
			name:                        "set layer annotation with empty value",
			spec:                        "layer:key",
			expectedTopLayerAnnotations: map[string]string{"key": ""},
		},
		{
			name:                        "unset layer annotation",
			beforeTopLayerAnnotations:   map[string]string{"k1": "v1", "k2": "v2"},
			spec:                        "layer:k1-",
			expectedTopLayerAnnotations: map[string]string{"k2": "v2"},
		},
		{
			name:                      "clear all layer annotations",
			beforeImageAnnotations:    map[string]string{"ik1": "iv1"},
			beforeTopLayerAnnotations: map[string]string{"k1": "v1", "k2": "v2"},
			spec:                      "layer:-",
			expectedImageAnnotations:  map[string]string{"ik1": "iv1"},
		},
		{
			name:                     "empty string sets empty key",
			spec:                     "",
			expectedImageAnnotations: map[string]string{"": ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := &buildah.Builder{
				ImageAnnotations:    tt.beforeImageAnnotations,
				TopLayerAnnotations: tt.beforeTopLayerAnnotations,
			}
			processAnnotationSpec(builder, tt.spec)
			assert.Equal(t, tt.expectedImageAnnotations, builder.ImageAnnotations)
			assert.Equal(t, tt.expectedTopLayerAnnotations, builder.TopLayerAnnotations)
		})
	}
}
