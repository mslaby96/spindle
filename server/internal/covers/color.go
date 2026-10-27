package covers

import (
	"fmt"
	"image"
)

const samples = 64

// DominantColor samples the image on a grid, groups pixels into coarse colour buckets and returns the average of
// the most common bucket. Near-white and near-black pixels are ignored so borders and backgrounds don't win.
func DominantColor(img image.Image) string {
	b := img.Bounds()
	type acc struct{ r, g, bl, n int }
	buckets := map[int]*acc{}
	var all acc

	for y := 0; y < samples; y++ {
		for x := 0; x < samples; x++ {
			px := b.Min.X + x*b.Dx()/samples
			py := b.Min.Y + y*b.Dy()/samples
			r32, g32, b32, _ := img.At(px, py).RGBA()
			r, g, bl := int(r32>>8), int(g32>>8), int(b32>>8)
			all.r, all.g, all.bl, all.n = all.r+r, all.g+g, all.bl+bl, all.n+1

			lum := (299*r + 587*g + 114*bl) / 1000
			if lum > 235 || lum < 20 {
				continue
			}
			key := (r>>4)<<8 | (g>>4)<<4 | bl>>4
			a := buckets[key]
			if a == nil {
				a = &acc{}
				buckets[key] = a
			}
			a.r, a.g, a.bl, a.n = a.r+r, a.g+g, a.bl+bl, a.n+1
		}
	}

	best := &all
	for _, a := range buckets {
		if best == &all || a.n > best.n {
			best = a
		}
	}
	if best.n == 0 {
		return "#000000"
	}
	return fmt.Sprintf("#%02x%02x%02x", best.r/best.n, best.g/best.n, best.bl/best.n)
}
