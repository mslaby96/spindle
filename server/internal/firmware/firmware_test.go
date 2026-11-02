package firmware

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"testing/fstest"
)

var built = fstest.MapFS{
	"main.py":                                {Data: []byte("import ota\n")},
	"ota.py":                                 {Data: []byte("# ota\n")},
	"app/version.py":                         {Data: []byte("VERSION = \"0.6.0\"\n")},
	"app/runner.py":                          {Data: []byte("def run(): pass\n")},
	"app/__pycache__/runner.cpython-314.pyc": {Data: []byte("x")},
	"lib/mfrc522.py":                         {Data: []byte("class MFRC522: pass\n")},
	"config.json":                            {Data: []byte("{}")},
	"tests/test_ota.py":                      {Data: []byte("x")},
	".gitkeep":                               {Data: nil},
	"oled.py":                                {Data: []byte("x")},
	"tests/main.py":                          {Data: []byte("x")},
}

func TestLoadTakesTheStandsCode(t *testing.T) {
	b, err := Load(built)
	if err != nil {
		t.Fatal(err)
	}
	if b.Version != "0.6.0" {
		t.Fatalf("version %q", b.Version)
	}
	var paths []string
	for _, f := range b.Files {
		paths = append(paths, f.Path)
	}
	want := []string{"app/runner.py", "app/version.py", "lib/mfrc522.py"}
	if len(paths) != len(want) {
		t.Fatalf("files %v", paths)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("files %v", paths)
		}
	}
	sum := sha256.Sum256([]byte("class MFRC522: pass\n"))
	driver := b.Files[2]
	if driver.Size != 20 || driver.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("lib/mfrc522.py %+v", driver)
	}
	if data, ok := b.File("app/runner.py"); !ok || string(data) != "def run(): pass\n" {
		t.Fatalf("first file %q %v", data, ok)
	}
	// main.py and ota.py roll a bad update back, so they are never handed out
	for _, p := range []string{"main.py", "ota.py", "oled.py", "tests/main.py", "config.json", "tests/test_ota.py", "nope.py", ""} {
		if _, ok := b.File(p); ok {
			t.Errorf("%q is served", p)
		}
	}
}

func TestNothingBundled(t *testing.T) {
	b, err := Load(fstest.MapFS{".gitkeep": {}})
	if err != nil || b.Version != "" || len(b.Files) != 0 {
		t.Fatalf("%+v %v", b, err)
	}
}

func TestBundleWithoutAVersionIsAnError(t *testing.T) {
	if _, err := Load(fstest.MapFS{"app/runner.py": {Data: []byte("x")}}); err == nil {
		t.Fatal("loaded firmware without a version")
	}
}

func TestEmbeddedLoads(t *testing.T) {
	if _, err := Embedded(); err != nil {
		t.Fatal(err)
	}
}
