package httpapi

import "net/http"

func (s *Server) nowPlaying(w http.ResponseWriter, r *http.Request) {
	list, err := s.Sessions.Current(r.Context(), userID(r), s.Catalog.Album)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}
