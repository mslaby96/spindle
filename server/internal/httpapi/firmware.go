package httpapi

import "net/http"

// firmwareFile gives a stand the manifest of the firmware this server has, or one of its files.
func (s *Server) firmwareFile(w http.ResponseWriter, r *http.Request) {
	p := r.PathValue("path")
	if p == "" {
		writeJSON(w, http.StatusOK, s.Firmware)
		return
	}
	data, ok := s.Firmware.File(p)
	if !ok {
		writeError(w, http.StatusNotFound, "no such firmware file")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(data)
}
