package httpapi

import (
	"cmp"
	"context"
	"errors"
	"net/http"

	"github.com/mslaby96/spindle/server/internal/auth"
	"github.com/mslaby96/spindle/server/internal/catalog"
	"github.com/mslaby96/spindle/server/internal/devices"
	"github.com/mslaby96/spindle/server/internal/inbox"
	"github.com/mslaby96/spindle/server/internal/metadata/discogs"
	"github.com/mslaby96/spindle/server/internal/sessions"
	"github.com/mslaby96/spindle/server/internal/stats"
)

var errAlbumNotFound = errors.New("album not found")

// knownErrors are failures the user can understand or fix. Without a message of its own the error's text is shown.
var knownErrors = []struct {
	err     error
	status  int
	message string
}{
	{auth.ErrSetupDone, http.StatusNotFound, ""},
	{auth.ErrInvalidSetupCode, http.StatusForbidden, ""},
	{auth.ErrWeakPassword, http.StatusBadRequest, ""},
	{auth.ErrInvalidCredentials, http.StatusUnauthorized, ""},
	{errAlbumNotFound, http.StatusNotFound, ""},
	{devices.ErrNotFound, http.StatusNotFound, ""},
	{devices.ErrNoName, http.StatusBadRequest, ""},
	{devices.ErrInvalidCode, http.StatusForbidden, ""},
	{devices.ErrNoFirmware, http.StatusConflict, ""},
	{devices.ErrUpToDate, http.StatusConflict, ""},
	{catalog.ErrNothingToAssign, http.StatusBadRequest, ""},
	{catalog.ErrUnknownListensChoice, http.StatusBadRequest, ""},
	{sessions.ErrNotFound, http.StatusNotFound, ""},
	{sessions.ErrNotPending, http.StatusConflict, ""},
	{sessions.ErrSleeveOnStand, http.StatusConflict, ""},
	{sessions.ErrUnknownResolution, http.StatusBadRequest, ""},
	{sessions.ErrInvalidTimes, http.StatusBadRequest, ""},
	{inbox.ErrNotFound, http.StatusNotFound, ""},
	{inbox.ErrInvalidRating, http.StatusBadRequest, ""},
	{stats.ErrBadQuery, http.StatusBadRequest, ""},
	{discogs.ErrNoToken, http.StatusServiceUnavailable, ""},
	{discogs.ErrRateLimited, http.StatusTooManyRequests, "Discogs is rate limiting requests, try again in a minute"},
	{discogs.ErrNotFound, http.StatusNotFound, "not found on Discogs"},
}

// fail answers with the status of a known failure and logs anything else as an internal error.
func (s *Server) fail(w http.ResponseWriter, err error) {
	for _, k := range knownErrors {
		if errors.Is(err, k.err) {
			writeError(w, k.status, cmp.Or(k.message, k.err.Error()))
			return
		}
	}
	var se *discogs.StatusError
	if errors.As(err, &se) {
		writeError(w, http.StatusBadGateway, "Discogs is not responding properly, try again later")
		return
	}
	s.internal(w, err)
}

// reply answers with v, or with the failure that kept the request from producing it.
func (s *Server) reply(w http.ResponseWriter, status int, v any, err error) {
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, status, v)
}

// fetch answers a GET with what load returns for the signed-in user.
func fetch[T any](s *Server, load func(ctx context.Context, userID int64) (T, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v, err := load(r.Context(), userID(r))
		s.reply(w, http.StatusOK, v, err)
	}
}

// act runs a change to the thing named by the id in the path and answers 204.
func (s *Server) act(w http.ResponseWriter, r *http.Request, change func(ctx context.Context, userID, id int64) error) {
	if id, ok := pathID(w, r); ok {
		s.noContent(w, change(r.Context(), userID(r), id))
	}
}

// noContent finishes a request that returns nothing on success.
func (s *Server) noContent(w http.ResponseWriter, err error) {
	if err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
