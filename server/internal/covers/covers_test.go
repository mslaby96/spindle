package covers

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
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

func TestDominantColor(t *testing.T) {
	blue, yellow := color.RGBA{20, 60, 160, 255}, color.RGBA{240, 200, 0, 255}
	pale, red := color.RGBA{216, 217, 232, 255}, color.RGBA{200, 30, 30, 255}
	cases := []struct {
		name string
		img  image.Image
		want string
	}{
		{"larger blue on the left", split(100, 70, blue, yellow), "#143ca0"},
		{"larger yellow on the right", split(100, 30, blue, yellow), "#f0c800"},
		{"all black", solid(10, 10, func(int, int) color.Color { return color.Black }), "#000000"},
		{"all white", solid(10, 10, func(int, int) color.Color { return color.White }), "#ffffff"},
		{"a colourful area over a larger pale one", split(100, 60, pale, color.RGBA{130, 40, 50, 255}), "#822832"},
		{"colourful specks don't count", split(100, 96, pale, red), "#d8d9e8"},
		{"colourful area just under 5%", split(64, 3, red, pale), "#d8d9e8"},
		{"colourful area just over 5%", split(64, 4, red, pale), "#c81e1e"},
		{"channel spread just under vivid", split(100, 60, pale, color.RGBA{139, 100, 100, 255}), "#d8d9e8"},
		{"channel spread just vivid", split(100, 60, pale, color.RGBA{140, 100, 100, 255}), "#8c6464"},
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
		img := split(100, 70, color.RGBA{grey, grey, grey, 255}, color.White)
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

func TestRemoveReportsOtherFailures(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	busy := filepath.Join(s.Dir, "busy")
	os.MkdirAll(filepath.Join(busy, "inside"), 0o755)
	if err := s.Remove("busy"); err == nil {
		t.Fatal("a cover that could not be deleted was reported as gone")
	}
}

func TestColorReadsAStoredCover(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	name, _, err := s.Save(3, encode(t, solid(20, 20, func(int, int) color.Color { return color.RGBA{10, 120, 60, 255} })))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.Color(name); err != nil || got != "#0a783c" {
		t.Fatalf("got %s: %v", got, err)
	}
	if _, err := s.Color("nope.png"); err == nil {
		t.Fatal("missing file accepted")
	}
	os.WriteFile(s.Path("junk.png"), []byte("junk"), 0o640)
	if _, err := s.Color("junk.png"); err == nil {
		t.Fatal("junk accepted")
	}
}

func TestSaveRejectsImagesWithTooManyPixels(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	// both sides are within maxSide, but decoding it would take far more memory than any cover needs
	big := encode(t, image.NewGray(image.Rect(0, 0, 3500, 3500)))
	if _, _, err := s.Save(1, big); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("%v", err)
	}
}
