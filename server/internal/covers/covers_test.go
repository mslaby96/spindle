package covers

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
)

func solid(w, h int, fill func(x, y int) color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, fill(x, y))
		}
	}
	return img
}

func encode(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDominantColorIgnoresWhiteBorder(t *testing.T) {
	img := solid(200, 200, func(x, y int) color.Color {
		if x < 30 || y < 30 || x > 170 || y > 170 {
			return color.White
		}
		return color.RGBA{200, 30, 30, 255}
	})
	if got := DominantColor(img); got != "#c81e1e" {
		t.Fatalf("got %s", got)
	}
}

func TestDominantColorPicksLargestArea(t *testing.T) {
	img := solid(100, 100, func(x, y int) color.Color {
		if x < 70 {
			return color.RGBA{20, 60, 160, 255}
		}
		return color.RGBA{240, 200, 0, 255}
	})
	if got := DominantColor(img); got != "#143ca0" {
		t.Fatalf("got %s", got)
	}
}

func TestDominantColorAllBlack(t *testing.T) {
	img := solid(10, 10, func(int, int) color.Color { return color.Black })
	if got := DominantColor(img); got != "#000000" {
		t.Fatalf("got %s", got)
	}
}

func TestSaveWritesFile(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	data := encode(t, solid(50, 50, func(int, int) color.Color { return color.RGBA{10, 120, 60, 255} }))
	name, hex, err := s.Save(7, data)
	if err != nil {
		t.Fatal(err)
	}
	if name != "7.png" || hex != "#0a783c" {
		t.Fatalf("name %s color %s", name, hex)
	}
	got, err := os.ReadFile(s.Path(name))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("stored file mismatch: %v", err)
	}
}

func TestSaveRejectsGarbageAndHugeImages(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	if _, _, err := s.Save(1, []byte("not an image")); err == nil {
		t.Error("garbage accepted")
	}
	huge := encode(t, image.NewGray(image.Rect(0, 0, maxSide+1, 1)))
	if _, _, err := s.Save(1, huge); !errors.Is(err, ErrTooLarge) {
		t.Errorf("huge image: %v", err)
	}
}

func TestPathCannotEscapeDir(t *testing.T) {
	s := &Store{Dir: "/data/covers"}
	if got := s.Path("../../etc/passwd"); got != "/data/covers/passwd" {
		t.Fatalf("got %s", got)
	}
}
