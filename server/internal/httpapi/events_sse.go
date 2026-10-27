package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

var sseHeartbeat = 15 * time.Second

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	rc.SetWriteDeadline(time.Time{})

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ": connected\n\n")
	if err := rc.Flush(); err != nil {
		return
	}

	ch, cancel := s.Bus.Subscribe(userID(r))
	defer cancel()
	tick := time.NewTicker(sseHeartbeat)
	defer tick.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			fmt.Fprint(w, ": ping\n\n")
		case e, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(e.Data)
			if err != nil {
				s.Log.Error("encode event", "type", e.Type, "err", err)
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, data)
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}
