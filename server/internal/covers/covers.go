package covers

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strconv"

	_ "golang.org/x/image/webp"
)

const maxSide = 4000

var ErrTooLarge = errors.New("cover image is too large")

type Store struct {
	Dir string
}

// Save validates and stores the image, returning its file name and dominant colour as #rrggbb.
func (s *Store) Save(albumID int64, data []byte) (string, string, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", "", fmt.Errorf("cover: %w", err)
	}
	if cfg.Width > maxSide || cfg.Height > maxSide {
		return "", "", ErrTooLarge
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", "", fmt.Errorf("cover: %w", err)
	}
	if err := os.MkdirAll(s.Dir, 0o750); err != nil {
		return "", "", err
	}
	name := strconv.FormatInt(albumID, 10) + "." + format
	tmp := filepath.Join(s.Dir, name+".tmp")
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return "", "", err
	}
	if err := os.Rename(tmp, filepath.Join(s.Dir, name)); err != nil {
		return "", "", err
	}
	return name, DominantColor(img), nil
}

// Remove deletes a stored cover; one that is already gone is not an error.
func (s *Store) Remove(name string) error {
	if err := os.Remove(s.Path(name)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Store) Path(name string) string {
	return filepath.Join(s.Dir, filepath.Base(name))
}
