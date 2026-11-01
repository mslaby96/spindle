package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/mslaby96/spindle/server/internal/export"
)

func (s *Server) exportJSON(w http.ResponseWriter, r *http.Request) {
	e, ok := s.buildExport(w, r)
	if !ok {
		return
	}
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		s.internal(w, err)
		return
	}
	download(w, "application/json", e.ExportedAt, "json", b)
}

func (s *Server) exportCSV(w http.ResponseWriter, r *http.Request) {
	e, ok := s.buildExport(w, r)
	if !ok {
		return
	}
	var buf bytes.Buffer
	if err := export.WriteCSV(&buf, e, s.Config.Location); err != nil {
		s.internal(w, err)
		return
	}
	download(w, "text/csv; charset=utf-8", e.ExportedAt, "csv", buf.Bytes())
}

func (s *Server) buildExport(w http.ResponseWriter, r *http.Request) (export.Export, bool) {
	e, err := export.Build(r.Context(), s.DB, userID(r), time.Now().UTC())
	if err != nil {
		s.internal(w, err)
		return export.Export{}, false
	}
	return e, true
}

func download(w http.ResponseWriter, contentType string, at time.Time, ext string, body []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="spindle-export-%s.%s"`, at.Format("20060102"), ext))
	w.Header().Set("Cache-Control", "no-store")
	w.Write(body)
}
