package ui

import (
	"bytes"
	"image/color"
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateLockIcon_ReturnsValidPNG(t *testing.T) {
	tests := []struct {
		name  string
		color color.RGBA
	}{
		{"gray", color.RGBA{128, 128, 128, 255}},
		{"orange", color.RGBA{255, 140, 0, 255}},
		{"green", color.RGBA{76, 175, 80, 255}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := generateLockIcon(tt.color)

			require.NotNil(t, data, "icon data should not be nil")
			assert.NotEmpty(t, data, "icon data should not be empty")

			img, err := png.Decode(bytes.NewReader(data))
			require.NoError(t, err, "should be valid PNG")

			bounds := img.Bounds()
			assert.Equal(t, iconSize, bounds.Dx(), "width should match iconSize")
			assert.Equal(t, iconSize, bounds.Dy(), "height should match iconSize")
		})
	}
}

func TestPreGeneratedIcons_AreValid(t *testing.T) {
	icons := map[string][]byte{
		"disconnected": iconDisconnectedPNG,
		"connecting":   iconConnectingPNG,
		"connected":    iconConnectedPNG,
	}

	for name, data := range icons {
		t.Run(name, func(t *testing.T) {
			require.NotNil(t, data, "icon should not be nil")
			assert.NotEmpty(t, data, "icon should not be empty")

			_, err := png.Decode(bytes.NewReader(data))
			require.NoError(t, err, "should be valid PNG")
		})
	}
}
