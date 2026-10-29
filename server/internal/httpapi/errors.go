package httpapi

import (
	"cmp"
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
	{devices.ErrNotFound, http.StatusNotFound, ""},
	{devices.ErrNoName, http.StatusBadRequest, ""},
	{devices.ErrInvalidCode, http.StatusForbidden, ""},
	{catalog.ErrNothingToAssign, http.StatusBadRequest, ""},
	{catalog.ErrUnknownListensChoice, http.StatusBadRequest, ""},
	{sessions.ErrNotFound, http.StatusNotFound, ""},
	{sessions.ErrNotPending, http.StatusConflict, ""},
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

// noContent finishes a request that returns nothing on success.
func (s *Server) noContent(w http.ResponseWriter, err error) {
	if err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
