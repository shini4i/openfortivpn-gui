package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log/slog"
	"math"

	"github.com/shini4i/openfortivpn-gui/assets"
)

// Status colors applied to the shield body for each connection state. Only the
// hue and saturation of these colors are used; each source pixel keeps its own
// brightness so the shield retains its original shading gradient.
var (
	colorDisconnected = color.RGBA{128, 128, 128, 255} // gray
	colorConnecting   = color.RGBA{255, 140, 0, 255}   // orange
	colorConnected    = color.RGBA{76, 175, 80, 255}   // green
)

// Pre-generated PNG icons for different connection states, derived from the
// application shield artwork.
var (
	iconDisconnectedPNG []byte
	iconConnectingPNG   []byte
	iconConnectedPNG    []byte
)

func init() {
	src, err := png.Decode(bytes.NewReader(assets.ShieldIconPNG))
	if err != nil {
		// The icon is embedded at build time, so a decode failure is a
		// programming error rather than a runtime condition. Fall back to the
		// raw artwork for every state so the tray still shows something.
		slog.Error("failed to decode embedded shield icon", "error", err)
		iconDisconnectedPNG = assets.ShieldIconPNG
		iconConnectingPNG = assets.ShieldIconPNG
		iconConnectedPNG = assets.ShieldIconPNG
		return
	}

	iconDisconnectedPNG = recolorShield(src, colorDisconnected)
	iconConnectingPNG = recolorShield(src, colorConnecting)
	iconConnectedPNG = recolorShield(src, colorConnected)

	// Guard against an unexpected encode failure: fall back to the raw artwork
	// so the tray never receives a nil icon.
	for _, icon := range []*[]byte{&iconDisconnectedPNG, &iconConnectingPNG, &iconConnectedPNG} {
		if *icon == nil {
			*icon = assets.ShieldIconPNG
		}
	}
}

// recolorShield returns a PNG of the shield icon with its blue body shifted
// toward target. The white window bars (and any other non-blue pixel) and the
// per-pixel alpha are preserved, so only the shield's color changes while its
// shape and shading stay intact. Returns nil if src is nil or encoding fails.
func recolorShield(src image.Image, target color.RGBA) []byte {
	if src == nil {
		return nil
	}

	b := src.Bounds()
	// Normalize to straight-alpha NRGBA so anti-aliased edge pixels are read as
	// un-premultiplied colors (premultiplied values would darken recolored edges).
	in := image.NewNRGBA(b)
	draw.Draw(in, b, src, b.Min, draw.Src)

	th, ts, _ := rgbToHSV(target.R, target.G, target.B)

	out := image.NewNRGBA(b) // zero value is fully transparent
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			p := in.NRGBAAt(x, y)
			if p.A == 0 {
				continue
			}
			if isBlueDominant(p.R, p.G, p.B) {
				_, _, v := rgbToHSV(p.R, p.G, p.B)
				r, g, bl := hsvToRGB(th, ts, v)
				out.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: bl, A: p.A})
			} else {
				out.SetNRGBA(x, y, p)
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		slog.Error("failed to encode recolored shield icon", "error", err)
		return nil
	}
	return buf.Bytes()
}

// isBlueDominant reports whether a pixel belongs to the blue shield body rather
// than the white window bars. Blue pixels have a blue channel clearly above the
// red and green channels; white/gray bars have roughly equal channels and fail
// this test, so they are left untouched by the recolor. The margins (12 against
// red, 8 against green) are calibrated empirically against the shield artwork so
// the near-equal-channel window bars stay below the threshold.
func isBlueDominant(r, g, b uint8) bool {
	return int(b) > int(r)+12 && int(b) > int(g)+8
}

// rgbToHSV converts an RGB color to HSV. Hue is in degrees [0,360), while
// saturation and value are in [0,1].
func rgbToHSV(r, g, b uint8) (h, s, v float64) {
	rf := float64(r) / 255
	gf := float64(g) / 255
	bf := float64(b) / 255

	maxc := math.Max(rf, math.Max(gf, bf))
	minc := math.Min(rf, math.Min(gf, bf))
	v = maxc

	delta := maxc - minc
	if delta == 0 {
		return 0, 0, v // achromatic
	}
	s = delta / maxc

	switch maxc {
	case rf:
		h = (gf - bf) / delta
	case gf:
		h = 2 + (bf-rf)/delta
	default:
		h = 4 + (rf-gf)/delta
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return h, s, v
}

// hsvToRGB converts an HSV color (hue in degrees, saturation and value in
// [0,1]) back to an 8-bit RGB triple.
func hsvToRGB(h, s, v float64) (r, g, b uint8) {
	if s == 0 {
		c := uint8(math.Round(v * 255))
		return c, c, c // achromatic
	}

	h /= 60
	i := math.Floor(h)
	f := h - i
	p := v * (1 - s)
	q := v * (1 - s*f)
	t := v * (1 - s*(1-f))

	var rf, gf, bf float64
	switch int(i) % 6 {
	case 0:
		rf, gf, bf = v, t, p
	case 1:
		rf, gf, bf = q, v, p
	case 2:
		rf, gf, bf = p, v, t
	case 3:
		rf, gf, bf = p, q, v
	case 4:
		rf, gf, bf = t, p, v
	default:
		rf, gf, bf = v, p, q
	}

	return uint8(math.Round(rf * 255)), uint8(math.Round(gf * 255)), uint8(math.Round(bf * 255))
}
