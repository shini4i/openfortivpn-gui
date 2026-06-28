package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"math"

	"github.com/shini4i/openfortivpn-gui/assets"
)

// Status colors for the tray shield outline. With the icon reduced to a hollow
// outline, the color alone carries the connection state, so the three must stay
// well separated: a desaturated gray, a saturated orange, and a vivid green.
var (
	colorDisconnected = color.RGBA{120, 120, 120, 255} // neutral gray
	colorConnecting   = color.RGBA{255, 140, 0, 255}   // orange
	colorConnected    = color.RGBA{0, 200, 60, 255}    // vivid green
)

// Shield geometry and rendering parameters. The earlier tray icons recolored the
// detailed application artwork (a blue shield with a window grid and key); at
// tray size all that detail collapsed into an unreadable smudge. Instead we draw
// a clean hollow shield from scratch — a thick colored outline around an empty
// center — so the only thing the eye has to resolve is the outline's color.
const (
	iconSize      = 64   // render resolution; tray hosts downscale this to the panel size
	iconMargin    = 0.06 // transparent padding around the shield, as a fraction of the canvas
	innerScale    = 0.74 // inner shield as a fraction of the outer; the gap between them is the stroke
	shieldCenterY = 0.46 // the point the inner shield shrinks toward (the silhouette's rough centroid)
	supersample   = 4    // sub-samples per axis per pixel, for anti-aliased edges
)

// Pre-generated PNG icons for each connection state.
var (
	iconDisconnectedPNG []byte
	iconConnectingPNG   []byte
	iconConnectedPNG    []byte
)

func init() {
	iconDisconnectedPNG = drawShieldOutline(iconSize, colorDisconnected)
	iconConnectingPNG = drawShieldOutline(iconSize, colorConnecting)
	iconConnectedPNG = drawShieldOutline(iconSize, colorConnected)

	// Encoding an in-memory image effectively never fails, but if it somehow
	// does, fall back to the embedded application artwork so the tray never
	// receives a nil icon.
	for _, icon := range []*[]byte{&iconDisconnectedPNG, &iconConnectingPNG, &iconConnectedPNG} {
		if *icon == nil {
			*icon = assets.ShieldIconPNG
		}
	}
}

// drawShieldOutline renders a hollow shield — a thick colored outline around a
// transparent center — as a size×size PNG. The painted band is the region inside
// the shield silhouette but outside an inner shield shrunk by innerScale toward
// shieldCenterY; only that band is filled with col, so the connection state is
// conveyed by the outline color alone and stays legible at any tray size. Each
// pixel is supersampled so the curved edges are anti-aliased. Returns nil if PNG
// encoding fails, which lets init substitute a fallback icon.
func drawShieldOutline(size int, col color.RGBA) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	span := 1 - 2*iconMargin // the drawing area inside the transparent margin

	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var hits int
			for sy := 0; sy < supersample; sy++ {
				for sx := 0; sx < supersample; sx++ {
					// Sub-pixel sample center, in [0,1) canvas coordinates.
					cx := (float64(px) + (float64(sx)+0.5)/supersample) / float64(size)
					cy := (float64(py) + (float64(sy)+0.5)/supersample) / float64(size)
					// Map the drawing area (inside the margin) onto the unit shield.
					nx := (cx - iconMargin) / span
					ny := (cy - iconMargin) / span

					inner := inShield((nx-0.5)/innerScale+0.5, (ny-shieldCenterY)/innerScale+shieldCenterY)
					if inShield(nx, ny) && !inner {
						hits++
					}
				}
			}
			if hits == 0 {
				continue
			}
			// Coverage fraction becomes the pixel's alpha; the RGB is always the
			// state color, so every painted pixel reads as that single color.
			a := uint8(hits * 255 / (supersample * supersample))
			img.SetNRGBA(px, py, color.NRGBA{R: col.R, G: col.G, B: col.B, A: a})
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		slog.Error("failed to encode shield outline icon", "error", err)
		return nil
	}
	return buf.Bytes()
}

// shieldHalfWidth returns the shield silhouette's half-width at normalized height
// y (y=0 is the top edge, y=1 the bottom point), as a fraction of the canvas: 0
// at the point and up to 0.5 at the widest. It returns -1 for y outside [0,1].
// The profile is rounded top corners, straight vertical sides down to the
// shoulder, then a parabolic taper converging to a point.
func shieldHalfWidth(y float64) float64 {
	const (
		maxHalf   = 0.5  // widest half-width, spanning the full drawing area
		corner    = 0.14 // top-corner radius
		shoulderY = 0.50 // height at which the straight sides begin tapering
	)
	switch {
	case y < 0 || y > 1:
		return -1
	case y <= corner:
		// Quarter-circle top corners pulled in from the full width. The sqrt
		// argument is clamped to 0 as a guard against floating-point imprecision:
		// the compiler constant-folds corner*corner at extended precision while
		// dy*dy is a runtime float64 multiply, so the two can differ by a ULP near
		// the corner boundary and drive the argument slightly negative (observed
		// as NaN at y=0, where dy==corner).
		dy := corner - y
		return (maxHalf - corner) + math.Sqrt(math.Max(0, corner*corner-dy*dy))
	case y <= shoulderY:
		return maxHalf
	default:
		// Parabolic taper: flat at the shoulder, steepening to a point at y=1.
		t := (y - shoulderY) / (1 - shoulderY)
		return maxHalf * (1 - t*t)
	}
}

// inShield reports whether the normalized point (x,y) lies inside the shield
// silhouette, where x and y are in [0,1] across the drawing area.
func inShield(x, y float64) bool {
	hw := shieldHalfWidth(y)
	return hw >= 0 && math.Abs(x-0.5) <= hw
}
