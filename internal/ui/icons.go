package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

// Icon dimensions for system tray.
const iconSize = 22

// Pre-generated PNG icons for different connection states.
var (
	iconDisconnectedPNG []byte
	iconConnectingPNG   []byte
	iconConnectedPNG    []byte
)

func init() {
	iconDisconnectedPNG = generateLockIcon(color.RGBA{128, 128, 128, 255}) // Gray
	iconConnectingPNG = generateLockIcon(color.RGBA{255, 140, 0, 255})     // Orange
	iconConnectedPNG = generateLockIcon(color.RGBA{76, 175, 80, 255})      // Green
}

// generateLockIcon creates a simple padlock icon with the specified color.
func generateLockIcon(c color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, iconSize, iconSize))

	// Lock body dimensions (rectangle)
	bodyLeft := 4
	bodyRight := 17
	bodyTop := 10
	bodyBottom := 20

	// Shackle dimensions (the arch on top)
	shackleLeft := 7
	shackleRight := 14
	shackleTop := 3
	shackleThickness := 2

	// Draw the lock body (filled rectangle with rounded corners)
	for y := bodyTop; y <= bodyBottom; y++ {
		for x := bodyLeft; x <= bodyRight; x++ {
			img.Set(x, y, c)
		}
	}

	// Draw the shackle (U-shape arch)
	// Left vertical part
	for y := shackleTop; y <= bodyTop; y++ {
		for x := shackleLeft; x < shackleLeft+shackleThickness; x++ {
			img.Set(x, y, c)
		}
	}
	// Right vertical part
	for y := shackleTop; y <= bodyTop; y++ {
		for x := shackleRight - shackleThickness; x < shackleRight; x++ {
			img.Set(x, y, c)
		}
	}
	// Top horizontal part (connecting the two verticals)
	for y := shackleTop; y < shackleTop+shackleThickness; y++ {
		for x := shackleLeft; x < shackleRight; x++ {
			img.Set(x, y, c)
		}
	}

	// Draw keyhole (small dark circle and rectangle)
	keyhole := color.RGBA{40, 40, 40, 255}
	keyholeX := 10
	keyholeY := 14

	// Keyhole circle (top)
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx*dx+dy*dy <= 1 {
				img.Set(keyholeX+dx, keyholeY+dy, keyhole)
			}
		}
	}
	// Keyhole rectangle (bottom)
	for y := keyholeY + 1; y <= keyholeY+3; y++ {
		img.Set(keyholeX, y, keyhole)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}
