// Package firmware holds the stand's code that this server hands out, so a stand can update itself over the network.
package firmware

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"
)

// The Docker build copies firmware/ in here; a plain go build has no firmware to give out.
//
//go:embed all:bundle
var embedded embed.FS

type File struct {
	Path   string `json:"path"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
	data   []byte
}

// Bundle is one version of the firmware. Its zero Version means there is none.
type Bundle struct {
	Version string `json:"version"`
	Files   []File `json:"files"`
}

var versionLine = regexp.MustCompile(`(?m)^VERSION = "([^"]+)"`)

func Embedded() (*Bundle, error) {
	sub, err := fs.Sub(embedded, "bundle")
	if err != nil {
		return nil, err
	}
	return Load(sub)
}

// Load takes the files a stand runs: main.py and ota.py, and the Python files in app/ and lib/.
func Load(fsys fs.FS) (*Bundle, error) {
	b := &Bundle{Files: []File{}}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !standFile(p) {
			return err
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		b.Files = append(b.Files, File{Path: p, Size: len(data), SHA256: hex.EncodeToString(sum[:]), data: data})
		return nil
	})
	if err != nil || len(b.Files) == 0 {
		return b, err
	}
	slices.SortFunc(b.Files, func(x, y File) int { return strings.Compare(x.Path, y.Path) })
	if b.Version, err = b.version(); err != nil {
		return nil, err
	}
	return b, nil
}

func (b *Bundle) version() (string, error) {
	data, _ := b.File("app/version.py")
	m := versionLine.FindSubmatch(data)
	if m == nil {
		return "", errors.New("firmware has no app/version.py with a VERSION")
	}
	return string(m[1]), nil
}

func standFile(p string) bool {
	dir, name := path.Split(p)
	if path.Ext(name) != ".py" {
		return false
	}
	return dir == "app/" || dir == "lib/" || (dir == "" && (name == "main.py" || name == "ota.py"))
}

func (b *Bundle) File(p string) ([]byte, bool) {
	i := slices.IndexFunc(b.Files, func(f File) bool { return f.Path == p })
	if i < 0 {
		return nil, false
	}
	return b.Files[i].data, true
}
