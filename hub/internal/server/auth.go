package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/adefesioso/homedash/hub/internal/store"
)

type ctxKey int

const userKey ctxKey = 1

const sessionCookie = "homedash_session"

// currentUser is who is asking, from the session cookie or a bearer;
// nil when nobody is. A bearer is an API token (hd_…) for a script, or
// a session token, which is how the CLI signs as the person who
// approved it.
func (s *Server) currentUser(r *http.Request) *store.User {
	if u, ok := r.Context().Value(userKey).(*store.User); ok {
		return u
	}
	if tok := s.sessionToken(r); tok != "" {
		if u, err := s.Store.SessionUser(r.Context(), tok); err == nil {
			return u
		}
	}
	if tok := bearer(r); strings.HasPrefix(tok, "hd_") {
		if name, role, err := s.Store.TokenRole(r.Context(), tok); err == nil {
			return &store.User{Name: "token:" + name, Role: role}
		}
	}
	return nil
}

func bearer(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return ""
}

// sessionToken is the session this request carries, from the cookie or
// as a bearer; empty when it carries none.
func (s *Server) sessionToken(r *http.Request) string {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		return c.Value
	}
	if tok := bearer(r); tok != "" && !strings.HasPrefix(tok, "hd_") {
		return tok
	}
	return ""
}

// guard is the sign-in gate for the API: no account, no answer, except
// health and the sign-in endpoints themselves. A viewer reads; every
// write is an admin's. A link front (/~peer/service/) is behind the
// same sign-in, and its app takes a viewer's POSTs.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		open := p == "/api/health" || strings.HasPrefix(p, "/api/auth/") || !strings.HasPrefix(p, "/api/") && !strings.HasPrefix(p, "/v1/") && !strings.HasPrefix(p, "/~")
		// A hub with no account yet takes an export on its setup page:
		// the accounts come with it.
		if !open && p == "/api/backup/restore" {
			n, _ := s.Store.UserCount(r.Context())
			open = n == 0
		}
		if open {
			next.ServeHTTP(w, r)
			return
		}
		u := s.currentUser(r)
		if u == nil {
			http.Error(w, "sign in", http.StatusUnauthorized)
			return
		}
		if u.Role != "admin" && r.Method != http.MethodGet && r.Method != http.MethodHead && !viewerMayPost(p) {
			http.Error(w, "viewers read; this needs an admin", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	})
}

// viewerMayPost: the router is a read for a viewer (a prompt changes
// nothing about the lab), and so is signing out and using a fronted
// service, whose app has its own idea of who may write.
func viewerMayPost(p string) bool {
	return p == "/api/chat" || p == "/api/generate" || p == "/api/embed" || p == "/api/embeddings" || p == "/api/show" || strings.HasPrefix(p, "/v1/") || strings.HasPrefix(p, "/~")
}

func (s *Server) authState(w http.ResponseWriter, r *http.Request) {
	n, _ := s.Store.UserCount(r.Context())
	u := s.currentUser(r)
	out := map[string]any{"setupOpen": n == 0, "user": nil}
	if u != nil {
		out["user"] = map[string]any{"name": u.Name, "role": u.Role, "passkeys": u.Passkeys}
	}
	writeJSON(w, out)
}

func (s *Server) registerBegin(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name, Invite string }
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in)
	opts, id, err := s.Auth.BeginRegistration(r.Context(), in.Name, strings.TrimSpace(in.Invite), s.currentUser(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"ceremony": id, "options": opts})
}

func (s *Server) registerFinish(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("ceremony")
	u, err := s.Auth.FinishRegistration(r.Context(), id, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.startSession(w, r, u)
	s.Notify("auth.passkey", u.Name, "a passkey was registered for "+u.Name)
	writeJSON(w, map[string]any{"name": u.Name, "role": u.Role, "passkeys": u.Passkeys})
}

func (s *Server) loginBegin(w http.ResponseWriter, _ *http.Request) {
	opts, id, err := s.Auth.BeginLogin()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"ceremony": id, "options": opts})
}

func (s *Server) loginFinish(w http.ResponseWriter, r *http.Request) {
	u, err := s.Auth.FinishLogin(r.Context(), r.URL.Query().Get("ceremony"), r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	s.startSession(w, r, u)
	writeJSON(w, map[string]any{"name": u.Name, "role": u.Role, "passkeys": u.Passkeys})
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, u *store.User) {
	tok, err := s.Store.NewSession(r.Context(), u.ID)
	if err != nil {
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: tok, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https", Expires: time.Now().Add(30 * 24 * time.Hour)})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if tok := s.sessionToken(r); tok != "" {
		_ = s.Store.DeleteSession(r.Context(), tok)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

// cliGrant is the panel approving a CLI: the person is signed in here,
// and the code that comes back is what the browser carries to the CLI's
// loopback port. Under /api/auth/ so the guard leaves it open; it checks
// the session itself.
func (s *Server) cliGrant(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	if u == nil || u.ID == nil {
		http.Error(w, "sign in", http.StatusUnauthorized)
		return
	}
	code := s.Auth.GrantCLI(u)
	s.Notify("auth.cli", u.Name, u.Name+" signed in a command line")
	writeJSON(w, map[string]string{"code": code})
}

// cliRedeem is the CLI trading its code for a session.
func (s *Server) cliRedeem(w http.ResponseWriter, r *http.Request) {
	var in struct{ Code string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	tok, u, err := s.Auth.RedeemCLI(r.Context(), strings.TrimSpace(in.Code))
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	writeJSON(w, map[string]string{"token": tok, "name": u.Name, "role": u.Role})
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	us, err := s.Store.Users(r.Context())
	if err != nil {
		http.Error(w, "users unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, us)
}

func (s *Server) setRole(w http.ResponseWriter, r *http.Request) {
	var in struct{ Role string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&in); err != nil || (in.Role != "admin" && in.Role != "viewer") {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	if in.Role == "viewer" {
		// Same "leave an admin" guard as deleting a user: refuse a
		// demotion — another admin's or the caller's own — that would
		// leave zero admins (C-5).
		us, err := s.Store.Users(r.Context())
		if err != nil {
			http.Error(w, "users unavailable", http.StatusInternalServerError)
			return
		}
		admins := 0
		for _, u := range us {
			if u.Role == "admin" {
				admins++
			}
		}
		for _, u := range us {
			if u.Name == name && u.Role == "admin" && admins <= 1 {
				http.Error(w, "the last admin; leave one", http.StatusBadRequest)
				return
			}
		}
	}
	if err := s.Store.SetRole(r.Context(), name, in.Role); err != nil {
		http.Error(w, "users unavailable", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	if u := s.currentUser(r); u != nil && u.Name == r.PathValue("name") {
		http.Error(w, "not yourself; leave an admin", http.StatusBadRequest)
		return
	}
	if err := s.Store.DeleteUser(r.Context(), r.PathValue("name")); err != nil {
		http.Error(w, "users unavailable", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listInvites(w http.ResponseWriter, r *http.Request) {
	is, err := s.Store.Invites(r.Context())
	if err != nil {
		http.Error(w, "invites unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, is)
}

// newInvite mints a code: for a new account of a role, or — with "self"
// — a second passkey for the caller's own account.
func (s *Server) newInvite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Role string `json:"role"`
		Self bool   `json:"self"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&in)
	var uid []byte
	if in.Self {
		if u := s.currentUser(r); u != nil {
			uid = u.ID
			in.Role = u.Role
		}
	}
	if in.Role != "admin" && in.Role != "viewer" {
		in.Role = "viewer"
	}
	inv, err := s.Store.NewInvite(r.Context(), in.Role, uid)
	if err != nil {
		http.Error(w, "invites unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, inv)
}

// deleteInvite revokes a code before it is used.
func (s *Server) deleteInvite(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteInvite(r.Context(), r.PathValue("code")); err != nil {
		http.Error(w, "invites unavailable", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listTokens(w http.ResponseWriter, r *http.Request) {
	ts, err := s.Store.Tokens(r.Context())
	if err != nil {
		http.Error(w, "tokens unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, ts)
}

func (s *Server) newToken(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name, Role string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&in); err != nil || strings.TrimSpace(in.Name) == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if in.Role != "admin" {
		in.Role = "viewer"
	}
	name := strings.TrimSpace(in.Name)
	tok, err := s.Store.NewToken(r.Context(), name, in.Role)
	if errors.Is(err, store.ErrTokenExists) {
		http.Error(w, "a token named "+name+" exists; revoke it first", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"token": tok})
}

func (s *Server) deleteToken(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteToken(r.Context(), r.PathValue("name")); err != nil {
		http.Error(w, "tokens unavailable", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
