// Package qrcode draws text as a QR code, for a phone's camera to read off a
// screen: as an SVG image for a page, and as lines of block characters for a
// terminal.
package qrcode

import (
	"fmt"
	"strings"

	"rsc.io/qr"
)

// quietModules is the blank border the QR specification asks for around a
// code, in modules, so a camera can find its edges against anything.
const quietModules = 4

// SVG is text as a QR code in an SVG image, black on white whatever the page's
// theme, because a camera reads dark on light.
func SVG(text string) (string, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", fmt.Errorf("qrcode: %w", err)
	}
	side := code.Size + 2*quietModules
	var drawn strings.Builder
	fmt.Fprintf(&drawn, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges">`, side, side)
	fmt.Fprintf(&drawn, `<rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="`, side, side)
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			if code.Black(x, y) {
				fmt.Fprintf(&drawn, "M%d %dh1v1h-1z", x+quietModules, y+quietModules)
			}
		}
	}
	drawn.WriteString(`"/></svg>`)
	return drawn.String(), nil
}

// TerminalLines is text as a QR code in lines of block characters, two rows
// of modules to a line so it keeps its shape. It is drawn for a dark
// terminal: the code's light modules are blocks in the text color and its
// dark ones are spaces. In a light terminal it comes out inverted, which not
// every camera reads, so the command prints the link beside it.
func TerminalLines(text string) ([]string, error) {
	code, err := qr.Encode(text, qr.L)
	if err != nil {
		return nil, fmt.Errorf("qrcode: %w", err)
	}
	isDark := func(x, y int) bool {
		x, y = x-quietModules, y-quietModules
		return x >= 0 && y >= 0 && x < code.Size && y < code.Size && code.Black(x, y)
	}
	side := code.Size + 2*quietModules
	lines := make([]string, 0, (side+1)/2)
	for y := 0; y < side; y += 2 {
		var line strings.Builder
		for x := 0; x < side; x++ {
			top, bottom := !isDark(x, y), !isDark(x, y+1)
			switch {
			case top && bottom:
				line.WriteRune('█')
			case top:
				line.WriteRune('▀')
			case bottom:
				line.WriteRune('▄')
			default:
				line.WriteRune(' ')
			}
		}
		lines = append(lines, line.String())
	}
	return lines, nil
}
