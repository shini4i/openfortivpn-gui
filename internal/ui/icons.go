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
// brightness (then scaled — see below) so the shield retains its shading gradient.
var (
	colorDisconnected = color.RGBA{120, 120, 120, 255} // muted gray
	colorConnecting   = color.RGBA{255, 140, 0, 255}   // orange
	colorConnected    = color.RGBA{0, 200, 60, 255}    // vivid green
)

// Per-state brightness scale (<1 darkens). A straight hue swap left the gray
// "disconnected" and green "connected" shields at nearly the same luminance, and
// the eye resolves luminance — not hue — at tray size, so the two states read as
// the same shield in a different tint. Pulling disconnected far darker and
// keeping connected at full, vivid brightness opens a clear luminance gap. The
// disconnected icon additionally carries a disabled slash (see drawDisabledSlash)
// so the states differ in shape, not only color.
const (
	scaleDisconnected = 0.28 // push "off" dark so it can't be mistaken for "on"
	scaleConnecting   = 1.00 // keep the original brightness
	scaleConnected    = 1.00 // bright, vivid green
)

// Disabled-slash geometry and colors, as fractions of the icon width. The slash
// is a dark diagonal line with a thin white halo, clipped to the shield
// silhouette.
const (
	slashHalfFrac = 0.07  // slash half-thickness as a fraction of icon width
	slashHaloFrac = 0.045 // extra white halo width beyond the slash
)

var (
	slashColor = color.NRGBA{60, 60, 60, 255}    // near-black line
	slashHalo  = color.NRGBA{255, 255, 255, 255} // white outline so it reads on any tint
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

	iconDisconnectedPNG = drawDisabledSlash(recolorShield(src, colorDisconnected, scaleDisconnected))
	iconConnectingPNG = recolorShield(src, colorConnecting, scaleConnecting)
	iconConnectedPNG = recolorShield(src, colorConnected, scaleConnected)

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
// shape and shading stay intact. valueScale multiplies each recolored pixel's
// brightness (clamped to [0, 1]), letting a state darken the shield body so states
// stay distinguishable at tray size. Returns nil if src is nil or encoding fails.
func recolorShield(src image.Image, target color.RGBA, valueScale float64) []byte {
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
				v = math.Max(0, math.Min(1, v*valueScale)) // keep V within the HSV domain
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

// drawDisabledSlash overlays a diagonal "disabled" slash — a dark line with a
// thin white halo — across the icon's non-transparent pixels. Color alone is a
// weak cue at tray size, where green and gray shields read as nearly the same
// brightness, so the slash gives the disconnected state a shape-level marker
// that stays legible in grayscale and for color-blind users. The line runs from
// the top-right to the bottom-left and is clipped to the shield silhouette so it
// never streaks across the transparent corners. The line geometry assumes a
// square icon (the embedded shield is 48x48); for a non-square bounds the slash
// would run off-center. Returns pngData unchanged if it cannot be decoded, and
// nil if pngData is nil, so init's nil-guard still yields an icon.
func drawDisabledSlash(pngData []byte) []byte {
	if pngData == nil {
		return nil
	}

	src, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		slog.Error("failed to decode icon for disabled slash", "error", err)
		return pngData
	}

	b := src.Bounds()
	out := image.NewNRGBA(b)
	draw.Draw(out, b, src, b.Min, draw.Src)

	w := float64(b.Dx())
	half := w * slashHalfFrac
	halo := half + w*slashHaloFrac
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if out.NRGBAAt(x, y).A == 0 {
				continue // keep the slash inside the shield silhouette
			}
			// Distance from the pixel to the line (x-min)+(y-min) = w.
			dist := math.Abs(float64(x-b.Min.X)+float64(y-b.Min.Y)-w) / math.Sqrt2
			switch {
			case dist <= half:
				out.SetNRGBA(x, y, slashColor)
			case dist <= halo:
				out.SetNRGBA(x, y, slashHalo)
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		slog.Error("failed to encode slashed icon", "error", err)
		return pngData
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
