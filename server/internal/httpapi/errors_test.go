package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mslaby96/spindle/server/internal/metadata/discogs"
	"github.com/mslaby96/spindle/server/internal/sessions"
)

func TestFailAnswersWithTheStatusOfTheFailure(t *testing.T) {
	s := &Server{Deps: Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	cases := []struct {
		err     error
		status  int
		message string
	}{
		{fmt.Errorf("resolve 7: %w", sessions.ErrNotPending), http.StatusConflict, sessions.ErrNotPending.Error()},
		{discogs.ErrRateLimited, http.StatusTooManyRequests, "Discogs is rate limiting requests, try again in a minute"},
		{&discogs.StatusError{Code: 503}, http.StatusBadGateway, "Discogs is not responding properly, try again later"},
		{errors.New("disk on fire"), http.StatusInternalServerError, "internal error"},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		s.fail(w, tc.err)
		var body struct{ Error string }
		json.NewDecoder(w.Body).Decode(&body)
		if w.Code != tc.status || body.Error != tc.message {
			t.Errorf("%v: %d %q", tc.err, w.Code, body.Error)
		}
	}
}

func TestNoContent(t *testing.T) {
	s := &Server{Deps: Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	w := httptest.NewRecorder()
	s.noContent(w, nil)
	if w.Code != http.StatusNoContent {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	s.noContent(w, sessions.ErrNotFound)
	if w.Code != http.StatusNotFound {
		t.Fatal(w.Code)
	}
}
