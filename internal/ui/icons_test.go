package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/openfortivpn-gui/assets"
)

// absDiff returns the absolute difference between two bytes.
func absDiff(a, b uint8) uint8 {
	if a > b {
		return a - b
	}
	return b - a
}

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

func TestRecolorShield_ReturnsValidPNG(t *testing.T) {
	src, err := png.Decode(bytes.NewReader(assets.ShieldIconPNG))
	require.NoError(t, err)

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
			data := recolorShield(src, tt.color)
			require.NotNil(t, data, "icon data should not be nil")
			assert.NotEmpty(t, data, "icon data should not be empty")

			img, err := png.Decode(bytes.NewReader(data))
			require.NoError(t, err, "should be valid PNG")
			assert.Equal(t, src.Bounds(), img.Bounds(), "recolor must preserve dimensions")
		})
	}
}

// TestRecolorShield_ShiftsBluePixels is a coarse smoke check over the real
// artwork: pixels which were blue in the source are recolored toward the target
// hue, while non-blue pixels (the white window bars) are left untouched. The
// precise per-pixel guarantees live in TestRecolorShield_SyntheticImage, which
// is decoupled from the asset's pixel statistics.
func TestRecolorShield_ShiftsBluePixels(t *testing.T) {
	srcImg, err := png.Decode(bytes.NewReader(assets.ShieldIconPNG))
	require.NoError(t, err)
	b := srcImg.Bounds()
	src := image.NewNRGBA(b)
	draw.Draw(src, b, srcImg, b.Min, draw.Src)

	connected := decodeNRGBA(t, recolorShield(srcImg, colorConnected))
	connecting := decodeNRGBA(t, recolorShield(srcImg, colorConnecting))
	disconnected := decodeNRGBA(t, recolorShield(srcImg, colorDisconnected))

	var blueCount, greenOK, orangeOK, grayOK, whitePreserved, whiteCount int
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			p := src.NRGBAAt(x, y)
			if p.A == 0 {
				continue
			}
			if isBlueDominant(p.R, p.G, p.B) {
				blueCount++
				g := connected.NRGBAAt(x, y)
				if g.G >= g.B && g.G >= g.R {
					greenOK++
				}
				o := connecting.NRGBAAt(x, y)
				if o.R >= o.G && o.G >= o.B {
					orangeOK++
				}
				d := disconnected.NRGBAAt(x, y)
				if absDiff(d.R, d.G) <= 4 && absDiff(d.G, d.B) <= 4 {
					grayOK++
				}
			} else if p.R > 150 && p.G > 150 && p.B > 150 {
				whiteCount++
				w := connected.NRGBAAt(x, y)
				if w == p {
					whitePreserved++
				}
			}
		}
	}

	require.Greater(t, blueCount, 100, "sanity: source must contain blue pixels")
	require.Greater(t, whiteCount, 10, "sanity: source must contain white window bars")

	// Recolor should hold for the overwhelming majority of blue pixels.
	assert.GreaterOrEqual(t, greenOK, blueCount*9/10, "connected variant should be green")
	assert.GreaterOrEqual(t, orangeOK, blueCount*9/10, "connecting variant should be orange")
	assert.GreaterOrEqual(t, grayOK, blueCount*9/10, "disconnected variant should be gray")
	// White window bars must be left exactly as-is.
	assert.Equal(t, whiteCount, whitePreserved, "white pixels must be preserved")
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

func TestRgbToHSV(t *testing.T) {
	tests := []struct {
		name    string
		r, g, b uint8
		h, s, v float64
	}{
		{"red", 255, 0, 0, 0, 1, 1},
		{"green", 0, 255, 0, 120, 1, 1},
		{"blue", 0, 0, 255, 240, 1, 1},
		{"gray", 128, 128, 128, 0, 0, 128.0 / 255},
		{"black", 0, 0, 0, 0, 0, 0},
		{"hue-wraparound", 255, 0, 64, 344.94, 1, 1}, // forces the h<0 -> +360 branch
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, s, v := rgbToHSV(tt.r, tt.g, tt.b)
			assert.InDelta(t, tt.h, h, 0.5, "hue")
			assert.InDelta(t, tt.s, s, 0.01, "saturation")
			assert.InDelta(t, tt.v, v, 0.01, "value")
		})
	}
}

func TestHsvToRGB(t *testing.T) {
	// Each case targets a distinct sector of the i%6 switch (h at 30,90,...,330)
	// plus the achromatic s==0 short-circuit.
	tests := []struct {
		name    string
		h, s, v float64
		r, g, b uint8
	}{
		{"sector0", 30, 1, 1, 255, 128, 0},
		{"sector1", 90, 1, 1, 128, 255, 0},
		{"sector2", 150, 1, 1, 0, 255, 128},
		{"sector3", 210, 1, 1, 0, 128, 255},
		{"sector4", 270, 1, 1, 128, 0, 255},
		{"sector5", 330, 1, 1, 255, 0, 128},
		{"achromatic", 0, 0, 0.5, 128, 128, 128},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, g, b := hsvToRGB(tt.h, tt.s, tt.v)
			assert.InDelta(t, tt.r, r, 1, "red")
			assert.InDelta(t, tt.g, g, 1, "green")
			assert.InDelta(t, tt.b, b, 1, "blue")
		})
	}
}

func TestHSVRoundTrip(t *testing.T) {
	colors := []color.RGBA{
		{200, 30, 40, 255},
		{12, 200, 180, 255},
		{77, 77, 200, 255},
		{255, 140, 0, 255},
		{76, 175, 80, 255},
	}
	for _, c := range colors {
		h, s, v := rgbToHSV(c.R, c.G, c.B)
		r, g, b := hsvToRGB(h, s, v)
		assert.InDelta(t, c.R, r, 1, "red")
		assert.InDelta(t, c.G, g, 1, "green")
		assert.InDelta(t, c.B, b, 1, "blue")
	}
}

func TestRecolorShield_NilSource(t *testing.T) {
	// The init() fallback relies on this nil contract.
	assert.Nil(t, recolorShield(nil, colorConnected))
}

// TestRecolorShield_SyntheticImage pins the recolor's defining properties on a
// hand-built image, decoupled from the real artwork's pixel statistics:
// transparent pixels stay transparent, blue pixels take the target hue while
// keeping their own brightness and alpha, and non-blue pixels are untouched.
func TestRecolorShield_SyntheticImage(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 3, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 0, G: 0, B: 0, A: 0})         // fully transparent
	src.SetNRGBA(1, 0, color.NRGBA{R: 30, G: 30, B: 200, A: 255})  // blue, brightness = 200/255
	src.SetNRGBA(2, 0, color.NRGBA{R: 240, G: 240, B: 240, A: 200}) // white bar, partial alpha

	out := decodeNRGBA(t, recolorShield(src, colorConnected))

	// Transparent source pixel remains transparent.
	assert.Equal(t, uint8(0), out.NRGBAAt(0, 0).A, "transparent pixel must stay transparent")

	// Blue pixel: green-dominant, alpha preserved, brightness (max channel) preserved.
	bp := out.NRGBAAt(1, 0)
	assert.Equal(t, uint8(255), bp.A, "alpha must be preserved")
	assert.GreaterOrEqual(t, bp.G, bp.R, "recolored pixel should be green-dominant")
	assert.GreaterOrEqual(t, bp.G, bp.B, "recolored pixel should be green-dominant")
	maxc := bp.R
	if bp.G > maxc {
		maxc = bp.G
	}
	if bp.B > maxc {
		maxc = bp.B
	}
	assert.InDelta(t, 200, maxc, 1, "source brightness (V) must be preserved")

	// White bar pixel (incl. partial alpha) must be byte-identical.
	assert.Equal(t, color.NRGBA{R: 240, G: 240, B: 240, A: 200}, out.NRGBAAt(2, 0), "non-blue pixel must be untouched")
}

func TestIsBlueDominant(t *testing.T) {
	tests := []struct {
		name    string
		r, g, b uint8
		want    bool
	}{
		{"below red margin", 100, 0, 112, false},
		{"above red margin", 100, 0, 113, true},
		{"below green margin", 0, 100, 108, false},
		{"above green margin", 0, 100, 109, true},
		{"white", 255, 255, 255, false},
		{"pure blue", 0, 0, 255, true},
		{"gray", 128, 128, 128, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isBlueDominant(tt.r, tt.g, tt.b))
		})
	}
}
