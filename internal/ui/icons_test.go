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
			data := recolorShield(src, tt.color, 1.0)
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

	connected := decodeNRGBA(t, recolorShield(srcImg, colorConnected, 1.0))
	connecting := decodeNRGBA(t, recolorShield(srcImg, colorConnecting, 1.0))
	disconnected := decodeNRGBA(t, recolorShield(srcImg, colorDisconnected, 1.0))

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
	assert.Nil(t, recolorShield(nil, colorConnected, 1.0))
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

	out := decodeNRGBA(t, recolorShield(src, colorConnected, 1.0))

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

// maxChannel returns the largest of a pixel's RGB channels (its HSV "value").
func maxChannel(p color.NRGBA) uint8 {
	m := p.R
	if p.G > m {
		m = p.G
	}
	if p.B > m {
		m = p.B
	}
	return m
}

// TestRecolorShield_ValueScale verifies valueScale darkens recolored pixels:
// halving the scale halves a body pixel's brightness, and the scale is clamped
// so it can never brighten past the source value.
func TestRecolorShield_ValueScale(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 30, G: 30, B: 200, A: 255}) // blue, brightness 200

	half := decodeNRGBA(t, recolorShield(src, colorConnected, 0.5))
	assert.InDelta(t, 100, maxChannel(half.NRGBAAt(0, 0)), 2, "scale 0.5 should halve brightness")

	clamped := decodeNRGBA(t, recolorShield(src, colorConnected, 2.0))
	assert.InDelta(t, 255, maxChannel(clamped.NRGBAAt(0, 0)), 1, "scale must clamp at full brightness")

	dark := decodeNRGBA(t, recolorShield(src, colorConnected, -1.0))
	assert.InDelta(t, 0, maxChannel(dark.NRGBAAt(0, 0)), 1, "negative scale must clamp to 0 (fully dark)")
}

// TestStateIcons_AreDistinguishable pins the fix: across the shield body, the
// disconnected icon must be clearly darker than the connected one, and the
// connected body must read as green. This is what keeps the two states apart at
// tray size — a regression here is the original "white vs pale-green" bug.
func TestStateIcons_AreDistinguishable(t *testing.T) {
	srcImg, err := png.Decode(bytes.NewReader(assets.ShieldIconPNG))
	require.NoError(t, err)
	b := srcImg.Bounds()
	src := image.NewNRGBA(b)
	draw.Draw(src, b, srcImg, b.Min, draw.Src)

	disconnected := decodeNRGBA(t, iconDisconnectedPNG)
	connected := decodeNRGBA(t, iconConnectedPNG)

	var bodyPixels, discSum, connSum, greenDominant int
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			p := src.NRGBAAt(x, y)
			if p.A == 0 || !isBlueDominant(p.R, p.G, p.B) {
				continue
			}
			bodyPixels++
			discSum += int(maxChannel(disconnected.NRGBAAt(x, y)))
			cp := connected.NRGBAAt(x, y)
			connSum += int(maxChannel(cp))
			if cp.G > cp.R && cp.G > cp.B {
				greenDominant++
			}
		}
	}

	require.Greater(t, bodyPixels, 100, "sanity: source must contain shield-body pixels")
	discMean := float64(discSum) / float64(bodyPixels)
	connMean := float64(connSum) / float64(bodyPixels)

	// Disconnected must be substantially dimmer than connected so the two states
	// don't both read as a light shield. Margin is generous to avoid brittleness.
	assert.Less(t, discMean, connMean*0.75, "disconnected body should be clearly darker than connected (disc=%.1f conn=%.1f)", discMean, connMean)
	assert.GreaterOrEqual(t, greenDominant, bodyPixels*9/10, "connected body should read as green")
}

// TestDrawDisabledSlash verifies the disabled slash is drawn across the icon's
// diagonal and clipped to the shield silhouette: a pixel on the diagonal inside
// the shield becomes the dark slash color, while a shield pixel well off the
// diagonal is left untouched.
func TestDrawDisabledSlash(t *testing.T) {
	src, err := png.Decode(bytes.NewReader(assets.ShieldIconPNG))
	require.NoError(t, err)

	base := recolorShield(src, colorDisconnected, scaleDisconnected)
	require.NotNil(t, base)

	out := drawDisabledSlash(base)
	require.NotNil(t, out)

	img := decodeNRGBA(t, out)
	baseImg := decodeNRGBA(t, base)
	b := img.Bounds()
	assert.Equal(t, baseImg.Bounds(), b, "slash must preserve dimensions")

	// The center sits on the top-right -> bottom-left diagonal and inside the
	// shield, so it must carry the dark, neutral-gray slash color.
	cx := b.Min.X + b.Dx()/2
	cy := b.Min.Y + b.Dy()/2
	center := img.NRGBAAt(cx, cy)
	assert.Equal(t, uint8(255), center.A, "slash pixel must be opaque")
	assert.Less(t, int(maxChannel(center)), 90, "diagonal pixel should be darkened by the slash")
	assert.LessOrEqual(t, int(absDiff(center.R, center.G)), 4, "slash should be neutral gray")
	assert.LessOrEqual(t, int(absDiff(center.G, center.B)), 4, "slash should be neutral gray")

	// A shield pixel well off the diagonal must be identical to the un-slashed base.
	offX, offY := b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/4 // top-center: far from the line
	assert.Equal(t, baseImg.NRGBAAt(offX, offY), img.NRGBAAt(offX, offY), "pixels off the slash must be unchanged")

	// Pin the two load-bearing invariants directly: every opaque pixel in the
	// halo band is painted white, and the slash never paints over a transparent
	// pixel on the diagonal (so it stays clipped to the shield silhouette).
	w := float64(b.Dx())
	half := w * slashHalfFrac
	halo := half + w*slashHaloFrac
	var haloChecked, clipChecked int
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dist := math.Abs(float64(x-b.Min.X)+float64(y-b.Min.Y)-w) / math.Sqrt2
			base := baseImg.NRGBAAt(x, y)
			switch {
			case base.A == 0 && dist <= half:
				assert.Equal(t, uint8(0), img.NRGBAAt(x, y).A, "slash must not streak across transparent corner at (%d,%d)", x, y)
				clipChecked++
			case base.A != 0 && dist > half && dist <= halo:
				assert.Equal(t, slashHalo, img.NRGBAAt(x, y), "halo band pixel must be white at (%d,%d)", x, y)
				haloChecked++
			}
		}
	}
	require.Greater(t, haloChecked, 0, "sanity: the halo band must be exercised")
	require.Greater(t, clipChecked, 0, "sanity: a transparent on-diagonal pixel must be exercised")
}

// TestDrawDisabledSlash_NilAndInvalid pins the init-fallback contract: nil in
// yields nil out (so the existing nil-guard substitutes the raw artwork), and
// undecodable input is returned unchanged rather than dropped.
func TestDrawDisabledSlash_NilAndInvalid(t *testing.T) {
	assert.Nil(t, drawDisabledSlash(nil), "nil in -> nil out for the init fallback")
	garbage := []byte("not a png")
	assert.Equal(t, garbage, drawDisabledSlash(garbage), "undecodable input returned unchanged")
}

// TestDisconnectedIcon_HasSlash pins that the pre-generated disconnected icon
// carries the disabled slash — the shape cue that distinguishes it from the
// connected icon independent of color (and survives grayscale).
func TestDisconnectedIcon_HasSlash(t *testing.T) {
	img := decodeNRGBA(t, iconDisconnectedPNG)
	b := img.Bounds()
	center := img.NRGBAAt(b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2)
	assert.Less(t, int(maxChannel(center)), 90, "disconnected icon center should sit under the dark slash")
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
