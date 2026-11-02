package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
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

// intentHeader must accompany every state-changing request made with the session cookie. Browsers only send custom
// headers cross-origin after a CORS preflight, which Spindle never approves, so a foreign page can't forge one.
const intentHeader = "X-Spindle"

func hasIntent(r *http.Request) bool {
	return r.Method == http.MethodGet || r.Method == http.MethodHead || r.Header.Get(intentHeader) != ""
}

func (s *Server) user(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hasIntent(r) {
			writeError(w, http.StatusForbidden, "missing "+intentHeader+" header")
			return
		}
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "not logged in")
			return
		}
		sess, err := s.Auth.Lookup(r.Context(), c.Value)
		if errors.Is(err, auth.ErrNoSession) {
			s.clearCookie(w, r)
			writeError(w, http.StatusUnauthorized, "not logged in")
			return
		}
		if err != nil {
			s.internal(w, err)
			return
		}
		if sess.Persistent {
			s.setCookie(w, r, c.Value, sess)
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
	if !s.intended(w, r) || !decode(w, r, &req) || !s.allow(w, r, s.limiter, s.client(r).key) {
		return
	}
	id, err := s.Auth.CompleteSetup(r.Context(), req.Code, req.Username, req.Password)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.limiter.Done(s.client(r).key)
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
	if !s.intended(w, r) || !decode(w, r, &req) {
		return
	}
	account := "user:" + strings.ToLower(strings.TrimSpace(req.Username))
	if !s.allow(w, r, s.limiter, s.client(r).key) || !s.allow(w, r, s.accounts, account) {
		return
	}
	id, err := s.Auth.Authenticate(r.Context(), req.Username, req.Password)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !s.loggedIn(w, r, account) {
		return
	}
	if !s.startSession(w, r, id, req.Remember) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// intended refuses a request without the intent header. Login and setup need it too, so another site can't log a
// visitor into an account of its choosing.
func (s *Server) intended(w http.ResponseWriter, r *http.Request) bool {
	if !hasIntent(r) {
		writeError(w, http.StatusForbidden, "missing "+intentHeader+" header")
		return false
	}
	return true
}

// loggedIn settles the limiters and drops the session this browser had before, so it isn't left valid next to the new
// one. The account's own count is cleared, but the address only gets this attempt back: a success on one account must
// not buy more guesses at another.
func (s *Server) loggedIn(w http.ResponseWriter, r *http.Request, account string) bool {
	s.accounts.Reset(account)
	s.limiter.Done(s.client(r).key)
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return true
	}
	if err := s.Auth.DeleteSession(r.Context(), c.Value); err != nil {
		s.internal(w, err)
		return false
	}
	return true
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if !s.intended(w, r) {
		return
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err := s.Auth.DeleteSession(r.Context(), c.Value); err != nil {
			s.internal(w, err)
			return
		}
	}
	s.clearCookie(w, r)
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
	if errors.Is(err, auth.ErrInvalidCredentials) {
		writeError(w, http.StatusForbidden, "current password is incorrect")
		return
	}
	s.noContent(w, err)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID int64, persistent bool) bool {
	token, sess, err := s.Auth.CreateSession(r.Context(), userID, persistent)
	if err != nil {
		s.internal(w, err)
		return false
	}
	s.setCookie(w, r, token, sess)
	return true
}

func (s *Server) setCookie(w http.ResponseWriter, r *http.Request, token string, sess auth.Session) {
	c := &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure(r),
		SameSite: http.SameSiteLaxMode,
	}
	if sess.Persistent {
		c.Expires = sess.ExpiresAt
		c.MaxAge = int(time.Until(sess.ExpiresAt).Seconds())
	}
	http.SetCookie(w, c)
}

func (s *Server) clearCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.secure(r), SameSite: http.SameSiteLaxMode,
	})
}

// secure says whether the session cookie gets the Secure flag: always when configured, otherwise whenever the browser
// came over HTTPS, so a server behind TLS is safe without anyone remembering a setting.
func (s *Server) secure(r *http.Request) bool {
	return s.Config.SecureCookies || s.client(r).https
}

func (s *Server) client(r *http.Request) client {
	return clientOf(r, s.Config.TrustedProxies)
}

func (s *Server) allow(w http.ResponseWriter, r *http.Request, l *auth.Limiter, key string) bool {
	ok, wait := l.Begin(key)
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
