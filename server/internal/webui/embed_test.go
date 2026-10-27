package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func serve(t *testing.T, files fstest.MapFS, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	handler(files).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

var built = fstest.MapFS{
	"index.html":        {Data: []byte("<html>app</html>")},
	"assets/app-123.js": {Data: []byte("console.log(1)")},
}

func TestClientRoutesGetIndex(t *testing.T) {
	for _, p := range []string{"/", "/devices", "/settings/account", "/index.html"} {
		rec := serve(t, built, p)
		if rec.Code != http.StatusOK || rec.Body.String() != "<html>app</html>" {
			t.Errorf("%s: %d %q", p, rec.Code, rec.Body.String())
		}
	}
}

func TestAssetsServedWithCaching(t *testing.T) {
	rec := serve(t, built, "/assets/app-123.js")
	if rec.Code != http.StatusOK || rec.Body.String() != "console.log(1)" {
		t.Fatalf("%d %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("cache header %q", rec.Header().Get("Cache-Control"))
	}
}

func TestUnknownAPIRouteIs404(t *testing.T) {
	rec := serve(t, built, "/api/nope")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Header().Get("Content-Type"), "json") {
		t.Fatalf("%d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func TestPlaceholderWhenNotBuilt(t *testing.T) {
	rec := serve(t, fstest.MapFS{".gitkeep": {}}, "/")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "npm run build") {
		t.Fatalf("%d %q", rec.Code, rec.Body.String())
	}
}
