// Package icon holds jam's program icon. Images returns the sizes Windows
// picks from for the title bar and taskbar; jam.ico (16-256 px) is the same
// art for the exe resource. make_icon.py regenerates every file here.
package icon

import (
	"bytes"
	"embed"
	"fmt"
	"image"
	"image/png"
)

//go:embed icon-16.png icon-24.png icon-32.png icon-48.png icon-64.png icon-128.png icon-256.png
var files embed.FS

var sizes = []int{16, 24, 32, 48, 64, 128, 256}

// Images decodes every embedded size, smallest first.
func Images() ([]image.Image, error) {
	out := make([]image.Image, 0, len(sizes))
	for _, n := range sizes {
		b, err := files.ReadFile(fmt.Sprintf("icon-%d.png", n))
		if err != nil {
			return nil, err
		}
		img, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			return nil, fmt.Errorf("icon-%d.png: %w", n, err)
		}
		out = append(out, img)
	}
	return out, nil
}
