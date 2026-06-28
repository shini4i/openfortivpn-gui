package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decodeNRGBA decodes PNG bytes into a straight-alpha NRGBA image for pixel
// inspection in tests.
func decodeNRGBA(t *testing.T, data []byte) *image.NRGBA {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	require.NoError(t, err, "should be valid PNG")
	b := img.Bounds()
	out := image.NewNRGBA(b)
	draw.Draw(out, b, img, b.Min, draw.Src)
	return out
}

// opaquePixels returns the number of fully or partially opaque pixels in img.
func opaquePixels(img *image.NRGBA) int {
	b := img.Bounds()
	var n int
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.NRGBAAt(x, y).A > 0 {
				n++
			}
		}
	}
	return n
}

func TestDrawShieldOutline_ReturnsValidPNG(t *testing.T) {
	tests := []struct {
		name  string
		color color.RGBA
	}{
		{"gray", colorDisconnected},
		{"orange", colorConnecting},
		{"green", colorConnected},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := drawShieldOutline(iconSize, tt.color)
			require.NotNil(t, data, "icon data should not be nil")
			assert.NotEmpty(t, data, "icon data should not be empty")

			img, err := png.Decode(bytes.NewReader(data))
			require.NoError(t, err, "should be valid PNG")
			assert.Equal(t, image.Rect(0, 0, iconSize, iconSize), img.Bounds(), "icon must be iconSize square")
		})
	}
}

// TestShieldOutline_IsHollow pins the defining property of the new icon: the
// center is empty (transparent), so the shield reads as an outline rather than a
// solid badge.
func TestShieldOutline_IsHollow(t *testing.T) {
	img := decodeNRGBA(t, drawShieldOutline(iconSize, colorConnected))
	b := img.Bounds()
	center := img.NRGBAAt(b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2)
	assert.Equal(t, uint8(0), center.A, "shield center must be transparent (hollow)")
}

// TestShieldOutline_CornersTransparent verifies the silhouette is clipped: the
// canvas corners lie outside the shield and must stay transparent, so the icon
// never paints a square block.
func TestShieldOutline_CornersTransparent(t *testing.T) {
	img := decodeNRGBA(t, drawShieldOutline(iconSize, colorConnected))
	b := img.Bounds()
	corners := []image.Point{
		{b.Min.X, b.Min.Y},
		{b.Max.X - 1, b.Min.Y},
		{b.Min.X, b.Max.Y - 1},
		{b.Max.X - 1, b.Max.Y - 1},
	}
	for _, p := range corners {
		assert.Equal(t, uint8(0), img.NRGBAAt(p.X, p.Y).A, "corner (%d,%d) must be transparent", p.X, p.Y)
	}
}

// TestShieldOutline_PaintsOnlyTheStateColor verifies every painted pixel carries
// exactly the state color (only its alpha varies for anti-aliasing). This is what
// makes the outline color the sole, unambiguous state signal.
func TestShieldOutline_PaintsOnlyTheStateColor(t *testing.T) {
	tests := []struct {
		name  string
		color color.RGBA
	}{
		{"gray", colorDisconnected},
		{"orange", colorConnecting},
		{"green", colorConnected},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			img := decodeNRGBA(t, drawShieldOutline(iconSize, tt.color))
			b := img.Bounds()
			var painted int
			for y := b.Min.Y; y < b.Max.Y; y++ {
				for x := b.Min.X; x < b.Max.X; x++ {
					p := img.NRGBAAt(x, y)
					if p.A == 0 {
						continue
					}
					painted++
					assert.Equal(t, tt.color.R, p.R, "painted pixel must use the state red")
					assert.Equal(t, tt.color.G, p.G, "painted pixel must use the state green")
					assert.Equal(t, tt.color.B, p.B, "painted pixel must use the state blue")
				}
			}
			// The real outline paints ~1400 pixels; a floor of 500 fails on a
			// degenerate render (collapsed stroke) while tolerating anti-aliasing drift.
			assert.Greater(t, painted, 500, "outline must paint a substantial number of pixels")
		})
	}
}

// TestShieldOutline_IsAntiAliased verifies the edges are smoothed rather than
// hard-cut: a hollow-shield outline with no partial-alpha pixels would be jagged.
func TestShieldOutline_IsAntiAliased(t *testing.T) {
	img := decodeNRGBA(t, drawShieldOutline(iconSize, colorConnected))
	b := img.Bounds()
	var partial int
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if a := img.NRGBAAt(x, y).A; a > 0 && a < 255 {
				partial++
			}
		}
	}
	assert.Greater(t, partial, 0, "outline edges should have anti-aliased (partial-alpha) pixels")
}

// TestStateColors_AreDistinct pins the palette intent: connected reads green
// (G dominant), connecting reads orange (R>G>B), and disconnected reads neutral
// gray (R==G==B). With color as the only state cue, these must stay separated.
func TestStateColors_AreDistinct(t *testing.T) {
	assert.True(t, colorConnected.G > colorConnected.R && colorConnected.G > colorConnected.B, "connected must be green-dominant")
	assert.True(t, colorConnecting.R > colorConnecting.G && colorConnecting.G > colorConnecting.B, "connecting must be orange (R>G>B)")
	assert.True(t, colorDisconnected.R == colorDisconnected.G && colorDisconnected.G == colorDisconnected.B, "disconnected must be neutral gray")
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

			img := decodeNRGBA(t, data)
			assert.Greater(t, opaquePixels(img), 500, "pre-generated icon should have a visible outline")
		})
	}
}

// TestShieldHalfWidth pins the silhouette profile across its piecewise regions:
// out-of-range heights return -1, the top corner is pulled in, the straight
// section is full width, and the taper reaches a point at the bottom.
func TestShieldHalfWidth(t *testing.T) {
	assert.Equal(t, -1.0, shieldHalfWidth(-0.01), "above the top edge is outside")
	assert.Equal(t, -1.0, shieldHalfWidth(1.01), "below the bottom point is outside")

	// The top edge (y=0) must stay finite: the math.Max clamp guards against the
	// sqrt argument going slightly negative there (it would otherwise be NaN and
	// silently corrupt the silhouette). Pinned directly so the guard can't be
	// removed as dead code even though the render grid never samples y=0 exactly.
	assert.False(t, math.IsNaN(shieldHalfWidth(0)), "top edge must not be NaN")
	assert.InDelta(t, 0.36, shieldHalfWidth(0), 0.001, "top edge half-width is maxHalf-corner")

	assert.True(t, shieldHalfWidth(0) < 0.5, "top corner is rounded in from full width")
	assert.InDelta(t, 0.5, shieldHalfWidth(0.45), 0.001, "straight section is full half-width")
	assert.InDelta(t, 0.0, shieldHalfWidth(1.0), 0.001, "bottom tapers to a point")

	// Continuity at the piecewise seams: the corner arc reaches full width at
	// y=corner, and the straight-to-taper junction at the shoulder is continuous.
	assert.InDelta(t, 0.5, shieldHalfWidth(0.14), 0.001, "corner arc meets full width at y=corner")
	assert.InDelta(t, 0.5, shieldHalfWidth(0.49), 0.001, "just above the shoulder is full width")
	assert.InDelta(t, 0.5, shieldHalfWidth(0.51), 0.005, "just below the shoulder stays continuous")

	// The taper is monotonically narrowing from the shoulder to the point.
	assert.True(t, shieldHalfWidth(0.6) > shieldHalfWidth(0.8), "shield narrows toward the bottom")
}

func TestInShield(t *testing.T) {
	assert.True(t, inShield(0.5, 0.4), "center of the body is inside")
	assert.False(t, inShield(0.02, 0.02), "top-left corner is outside the silhouette")
	assert.False(t, inShield(0.5, 1.2), "below the point is outside")
	assert.False(t, inShield(0.95, 0.9), "wide near the bottom point is outside")
}
