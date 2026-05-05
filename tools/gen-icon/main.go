// gen-icon renders assets/mdm.svg shapes into a multi-resolution ICO file.
// Run from the repo root: go run ./tools/gen-icon/
package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"sort"
)

const oversample = 4

func main() {
	sizes := []int{16, 32, 48, 64, 128, 256}
	var pngs [][]byte
	for _, size := range sizes {
		var buf bytes.Buffer
		if err := png.Encode(&buf, renderIcon(size)); err != nil {
			panic(err)
		}
		pngs = append(pngs, buf.Bytes())
	}
	if err := writeICO("assets/mdm.ico", pngs, sizes); err != nil {
		panic(err)
	}
	println("wrote assets/mdm.ico")
}

// renderIcon produces a size×size RGBA image of the MDM icon.
func renderIcon(size int) image.Image {
	hi := size * oversample
	img := image.NewNRGBA(image.Rect(0, 0, hi, hi))
	s := float64(hi) / 256.0

	fillBackground(img, hi, 40*s)

	// Block-M (left portion, vertically centred)
	fillPolygon(img, scalePoints([][2]float64{
		{12, 180}, {12, 77}, {37, 77}, {79, 126},
		{120, 77}, {145, 77}, {145, 180}, {120, 180},
		{120, 105}, {79, 154}, {37, 105}, {37, 180},
	}, s))

	// Downward arrow stem
	fillRect(img, 191*s, 77*s, 215*s, 140*s)

	// Arrowhead (base overlaps stem by 7px at y=133)
	fillPolygon(img, scalePoints([][2]float64{
		{162, 133}, {244, 133}, {203, 180},
	}, s))

	return boxSample(img, size)
}

func scalePoints(pts [][2]float64, s float64) [][2]float64 {
	out := make([][2]float64, len(pts))
	for i, p := range pts {
		out[i] = [2]float64{p[0] * s, p[1] * s}
	}
	return out
}

// fillBackground paints white inside a rounded rectangle.
func fillBackground(img *image.NRGBA, hi int, r float64) {
	w := float64(hi)
	white := color.NRGBA{255, 255, 255, 255}
	for y := 0; y < hi; y++ {
		for x := 0; x < hi; x++ {
			if inRoundedRect(float64(x)+0.5, float64(y)+0.5, 0, 0, w, w, r) {
				img.SetNRGBA(x, y, white)
			}
		}
	}
}

func inRoundedRect(px, py, x0, y0, x1, y1, r float64) bool {
	if px < x0 || px > x1 || py < y0 || py > y1 {
		return false
	}
	cx, cy := -1.0, -1.0
	if px < x0+r {
		cx = x0 + r
	} else if px > x1-r {
		cx = x1 - r
	}
	if py < y0+r {
		cy = y0 + r
	} else if py > y1-r {
		cy = y1 - r
	}
	if cx < 0 || cy < 0 {
		return true
	}
	dx, dy := px-cx, py-cy
	return math.Sqrt(dx*dx+dy*dy) <= r
}

func fillRect(img *image.NRGBA, x0, y0, x1, y1 float64) {
	black := color.NRGBA{0, 0, 0, 255}
	for y := int(y0); y < int(y1); y++ {
		for x := int(x0); x < int(x1); x++ {
			img.SetNRGBA(x, y, black)
		}
	}
}

// fillPolygon uses a scanline even-odd fill.
func fillPolygon(img *image.NRGBA, pts [][2]float64) {
	black := color.NRGBA{0, 0, 0, 255}
	minY, maxY := int(pts[0][1]), int(pts[0][1])
	for _, p := range pts {
		if int(p[1]) < minY {
			minY = int(p[1])
		}
		if int(p[1]) > maxY {
			maxY = int(p[1])
		}
	}
	n := len(pts)
	for y := minY; y <= maxY; y++ {
		fy := float64(y) + 0.5
		var xs []float64
		for i := 0; i < n; i++ {
			j := (i + 1) % n
			a, b := pts[i], pts[j]
			if (a[1] <= fy && b[1] > fy) || (b[1] <= fy && a[1] > fy) {
				t := (fy - a[1]) / (b[1] - a[1])
				xs = append(xs, a[0]+t*(b[0]-a[0]))
			}
		}
		sort.Float64s(xs)
		for i := 0; i+1 < len(xs); i += 2 {
			for x := int(xs[i]); x < int(xs[i+1]); x++ {
				img.SetNRGBA(x, y, black)
			}
		}
	}
}

// boxSample averages oversample×oversample super-pixels down to size×size.
func boxSample(src *image.NRGBA, size int) image.Image {
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	ratio := src.Bounds().Max.X / size
	n := ratio * ratio
	for dy := 0; dy < size; dy++ {
		for dx := 0; dx < size; dx++ {
			var r, g, b, a int
			for sy := dy * ratio; sy < (dy+1)*ratio; sy++ {
				for sx := dx * ratio; sx < (dx+1)*ratio; sx++ {
					c := src.NRGBAAt(sx, sy)
					r += int(c.R)
					g += int(c.G)
					b += int(c.B)
					a += int(c.A)
				}
			}
			dst.SetNRGBA(dx, dy, color.NRGBA{uint8(r / n), uint8(g / n), uint8(b / n), uint8(a / n)})
		}
	}
	return dst
}

// writeICO writes a PNG-in-ICO container.
func writeICO(path string, pngs [][]byte, sizes []int) error {
	var buf bytes.Buffer
	n := len(pngs)

	// Header
	buf.Write([]byte{0, 0, 1, 0}) // reserved + ICO type
	writeU16(&buf, uint16(n))

	// Directory
	offset := uint32(6 + 16*n)
	for i, data := range pngs {
		dim := uint8(sizes[i])
		if sizes[i] == 256 {
			dim = 0
		}
		buf.WriteByte(dim)
		buf.WriteByte(dim)
		buf.WriteByte(0)
		buf.WriteByte(0)
		writeU16(&buf, 1)
		writeU16(&buf, 32)
		writeU32(&buf, uint32(len(data)))
		writeU32(&buf, offset)
		offset += uint32(len(data))
	}

	for _, data := range pngs {
		buf.Write(data)
	}

	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func writeU16(b *bytes.Buffer, v uint16) {
	b.WriteByte(byte(v))
	b.WriteByte(byte(v >> 8))
}

func writeU32(b *bytes.Buffer, v uint32) {
	b.WriteByte(byte(v))
	b.WriteByte(byte(v >> 8))
	b.WriteByte(byte(v >> 16))
	b.WriteByte(byte(v >> 24))
}
