package httpapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/mslaby96/spindle/server/internal/auth"
)

const sessionCookie = "spindle_session"

type ctxKey int

const (
	userKey ctxKey = iota
	tokenKey
)

func userID(r *http.Request) int64 { return r.Context().Value(userKey).(int64) }

func (s *Server) user(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "not logged in")
			return
		}
		sess, err := s.Auth.Lookup(r.Context(), c.Value)
		if errors.Is(err, auth.ErrNoSession) {
			s.clearCookie(w)
			writeError(w, http.StatusUnauthorized, "not logged in")
			return
		}
		if err != nil {
			s.internal(w, err)
			return
		}
		if sess.Persistent {
			s.setCookie(w, c.Value, sess)
		}
		ctx := context.WithValue(r.Context(), userKey, sess.UserID)
		ctx = context.WithValue(ctx, tokenKey, c.Value)
		h(w, r.WithContext(ctx))
	})
}

func (s *Server) setupStatus(w http.ResponseWriter, r *http.Request) {
	needed, err := s.Auth.SetupNeeded(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"needed": needed})
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code     string `json:"code"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) || !s.allow(w, r) {
		return
	}
	id, err := s.Auth.CompleteSetup(r.Context(), req.Code, req.Username, req.Password)
	switch {
	case errors.Is(err, auth.ErrSetupDone):
		writeError(w, http.StatusNotFound, "setup already completed")
		return
	case errors.Is(err, auth.ErrInvalidSetupCode):
		s.limiter.Fail(clientIP(r))
		writeError(w, http.StatusForbidden, "invalid setup code")
		return
	case errors.Is(err, auth.ErrWeakPassword):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		s.internal(w, err)
		return
	}
	s.limiter.Reset(clientIP(r))
	if !s.startSession(w, r, id, true) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"username": req.Username})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Remember bool   `json:"remember"`
	}
	if !decode(w, r, &req) || !s.allow(w, r) {
		return
	}
	id, err := s.Auth.Authenticate(r.Context(), req.Username, req.Password)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		s.limiter.Fail(clientIP(r))
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if err != nil {
		s.internal(w, err)
		return
	}
	s.limiter.Reset(clientIP(r))
	if !s.startSession(w, r, id, req.Remember) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err := s.Auth.DeleteSession(r.Context(), c.Value); err != nil {
			s.internal(w, err)
			return
		}
	}
	s.clearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	var username string
	err := s.DB.R.QueryRowContext(r.Context(), `SELECT username FROM users WHERE id = ?`, userID(r)).Scan(&username)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": userID(r), "username": username})
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !decode(w, r, &req) {
		return
	}
	token := r.Context().Value(tokenKey).(string)
	err := s.Auth.ChangePassword(r.Context(), userID(r), req.Current, req.New, token)
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusForbidden, "current password is incorrect")
	case errors.Is(err, auth.ErrWeakPassword):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		s.internal(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID int64, persistent bool) bool {
	token, sess, err := s.Auth.CreateSession(r.Context(), userID, persistent)
	if err != nil {
		s.internal(w, err)
		return false
	}
	s.setCookie(w, token, sess)
	return true
}

func (s *Server) setCookie(w http.ResponseWriter, token string, sess auth.Session) {
	c := &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.Config.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	}
	if sess.Persistent {
		c.Expires = sess.ExpiresAt
		c.MaxAge = int(time.Until(sess.ExpiresAt).Seconds())
	}
	http.SetCookie(w, c)
}

func (s *Server) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.Config.SecureCookies, SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) allow(w http.ResponseWriter, r *http.Request) bool {
	ok, wait := s.limiter.Allow(clientIP(r))
	if !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, "too many attempts, try again later")
	}
	return ok
}

func (s *Server) internal(w http.ResponseWriter, err error) {
	s.Log.Error("request failed", "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
