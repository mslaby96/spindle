package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestExportDownloads(t *testing.T) {
	e := newEnv(t)
	c := e.loggedIn(t)
	e.catalogued(t, c, "04A1B2C3")

	resp := do(t, c, "GET", e.http.URL+"/api/export.json", nil)
	var body struct {
		Format string
		User   string
		Albums []struct{ Title string }
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if resp.StatusCode != http.StatusOK || body.Format != "spindle-export" || body.User != "maciej" || len(body.Albums) != 1 {
		t.Fatalf("json %d %+v", resp.StatusCode, body)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, `attachment; filename="spindle-export-`) || !strings.HasSuffix(cd, `.json"`) {
		t.Fatalf("disposition %q", cd)
	}

	resp = do(t, c, "GET", e.http.URL+"/api/export.csv", nil)
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/csv") ||
		!strings.HasPrefix(string(b), "date,artist,album") {
		t.Fatalf("csv %d %q %s", resp.StatusCode, resp.Header.Get("Content-Type"), b)
	}

	if resp := do(t, e.client(t), "GET", e.http.URL+"/api/export.json", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without signing in: %d", resp.StatusCode)
	}
}
