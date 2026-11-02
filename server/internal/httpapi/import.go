package httpapi

import "net/http"

func (s *Server) importStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Importer.Status(userID(r)))
}

func (s *Server) importPreview(w http.ResponseWriter, r *http.Request) {
	sum, err := s.Importer.Preview(r.Context(), userID(r))
	s.reply(w, http.StatusOK, sum, err)
}

// startImport answers at once; the import runs on and reports through the event stream.
func (s *Server) startImport(w http.ResponseWriter, r *http.Request) {
	if err := s.Importer.Start(userID(r)); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, s.Importer.Status(userID(r)))
}
