package covers

import (
	"bytes"
	"errors"
	"fmt"
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

// split fills the left part of a square image, up to cut, with one colour and the rest with another.
func split(size, cut int, left, right color.Color) image.Image {
	return solid(size, size, func(x, y int) color.Color {
		if x < cut {
			return left
		}
		return right
	})
}

func TestDominantColorIsTheLargestArea(t *testing.T) {
	blue, yellow := color.RGBA{20, 60, 160, 255}, color.RGBA{240, 200, 0, 255}
	cases := []struct {
		name string
		img  image.Image
		want string
	}{
		{"larger blue on the left", split(100, 70, blue, yellow), "#143ca0"},
		{"larger yellow on the right", split(100, 30, blue, yellow), "#f0c800"},
		{"all black", solid(10, 10, func(int, int) color.Color { return color.Black }), "#000000"},
		{"all white", solid(10, 10, func(int, int) color.Color { return color.White }), "#ffffff"},
	}
	for _, tc := range cases {
		if got := DominantColor(tc.img); got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestDominantColorTieIsStable(t *testing.T) {
	img := split(64, 32, color.RGBA{240, 200, 0, 255}, color.RGBA{20, 60, 160, 255})
	for range 50 {
		if got := DominantColor(img); got != "#143ca0" {
			t.Fatalf("got %s", got)
		}
	}
}

func TestDominantColorKeepsGreysAtTheDullThresholds(t *testing.T) {
	for _, grey := range []uint8{235, 20} {
		img := split(100, 70, color.RGBA{grey, grey, grey, 255}, color.RGBA{200, 30, 30, 255})
		want := fmt.Sprintf("#%02x%02x%02x", grey, grey, grey)
		if got := DominantColor(img); got != want {
			t.Errorf("grey %d: got %s", grey, got)
		}
	}
}

func TestDominantColorSingleColouredPixel(t *testing.T) {
	img := solid(64, 64, func(x, y int) color.Color {
		if x == 10 && y == 10 {
			return color.RGBA{200, 30, 30, 255}
		}
		return color.White
	})
	if got := DominantColor(img); got != "#c81e1e" {
		t.Fatalf("got %s", got)
	}
}

func TestSaveAcceptsImagesAtMaxSide(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	for _, r := range []image.Rectangle{image.Rect(0, 0, maxSide, 1), image.Rect(0, 0, 1, maxSide)} {
		if _, _, err := s.Save(1, encode(t, image.NewGray(r))); err != nil {
			t.Errorf("%v: %v", r, err)
		}
	}
}

func TestRemoveDeletesTheCoverAndToleratesAMissingOne(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	name, _, err := s.Save(1, encode(t, solid(40, 40, func(int, int) color.Color { return color.Black })))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(name); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Path(name)); !os.IsNotExist(err) {
		t.Fatalf("cover still there: %v", err)
	}
	if err := s.Remove(name); err != nil {
		t.Fatalf("removing twice: %v", err)
	}
}
