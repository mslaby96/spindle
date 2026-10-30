package covers

import (
	"fmt"
	"image"
)

const (
	samples = 64
	// A colourful area has to cover this percentage of the cover before it beats a larger pale one.
	minVividPercent = 5
	vividChroma     = 40
)

// DominantColor samples the image on a grid, groups pixels into coarse colour buckets and returns the average of
// the most common colourful bucket, falling back to the most common bucket when the cover has no colourful area of
// any size. Near-white and near-black pixels are ignored so borders and backgrounds don't win.
func DominantColor(img image.Image) string {
	var all mean
	buckets := map[int]*mean{}
	vivid := map[int]*mean{}
	pixels := sample(img)
	for _, c := range pixels {
		all.add(c)
		if c.dull() {
			continue
		}
		add(buckets, c)
		if c.vivid() {
			add(vivid, c)
		}
	}
	if best := largest(vivid); best != nil && best.n*100 >= len(pixels)*minVividPercent {
		return best.hex()
	}
	if best := largest(buckets); best != nil {
		return best.hex()
	}
	return all.hex()
}

func add(buckets map[int]*mean, c rgb) {
	if buckets[c.bucket()] == nil {
		buckets[c.bucket()] = &mean{}
	}
	buckets[c.bucket()].add(c)
}

// largest is the bucket with the most pixels, the lowest key on a tie so the answer doesn't depend on map order.
func largest(buckets map[int]*mean) *mean {
	var best *mean
	bestKey := 0
	for key, m := range buckets {
		if best == nil || m.n > best.n || (m.n == best.n && key < bestKey) {
			best, bestKey = m, key
		}
	}
	return best
}

type rgb struct{ r, g, b int }

// dull is near white or near black.
func (c rgb) dull() bool {
	lum := (299*c.r + 587*c.g + 114*c.b) / 1000
	return lum > 235 || lum < 20
}

// vivid has enough spread between its channels to read as a colour rather than a tinted grey.
func (c rgb) vivid() bool {
	return max(c.r, c.g, c.b)-min(c.r, c.g, c.b) >= vividChroma
}

func (c rgb) bucket() int {
	return (c.r>>4)<<8 | (c.g>>4)<<4 | c.b>>4
}

// sample reads a samples x samples grid of pixels spread over the image.
func sample(img image.Image) []rgb {
	bounds := img.Bounds()
	out := make([]rgb, 0, samples*samples)
	for y := range samples {
		for x := range samples {
			r, g, b, _ := img.At(bounds.Min.X+x*bounds.Dx()/samples, bounds.Min.Y+y*bounds.Dy()/samples).RGBA()
			out = append(out, rgb{int(r >> 8), int(g >> 8), int(b >> 8)})
		}
	}
	return out
}

type mean struct{ r, g, b, n int }

func (m *mean) add(c rgb) {
	m.r, m.g, m.b, m.n = m.r+c.r, m.g+c.g, m.b+c.b, m.n+1
}

func (m *mean) hex() string {
	return fmt.Sprintf("#%02x%02x%02x", m.r/m.n, m.g/m.n, m.b/m.n)
}
