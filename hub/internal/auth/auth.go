// Package auth is sign-in: passkeys, two roles, single-use invites, API
// tokens. No identity provider and no password. The first passkey
// registered is the admin; after that registration is closed and an
// admin invites the rest, or a shell on the hub prints a one-time code.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/adefesioso/homedash/hub/internal/store"
)

// Auth holds the WebAuthn config, the in-flight ceremonies and the CLI
// grants waiting to be redeemed.
type Auth struct {
	Store    *store.Store
	wa       *webauthn.WebAuthn
	mu       sync.Mutex
	inflight map[string]ceremony
	grants   map[string]grant
}

// grant is a one-time code the panel hands to the CLI on the person's
// behalf: the account, and how long the CLI has to come back for it.
type grant struct {
	userID  []byte
	expires time.Time
}

type ceremony struct {
	data    webauthn.SessionData
	user    *user
	role    string
	invite  string
	expires time.Time
}

// New configures WebAuthn for the hub's origins. RPID is the host the
// panel is opened on: localhost for the launcher, or the name behind a
// reverse proxy.
func New(st *store.Store, rpID string, origins []string) (*Auth, error) {
	wa, err := webauthn.New(&webauthn.Config{
		RPID: rpID, RPDisplayName: "HomeDash", RPOrigins: origins,
		Timeouts: webauthn.TimeoutsConfig{
			Login:        webauthn.TimeoutConfig{Enforce: true, Timeout: 2 * time.Minute, TimeoutUVD: 2 * time.Minute},
			Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: 2 * time.Minute, TimeoutUVD: 2 * time.Minute},
		},
	})
	if err != nil {
		return nil, err
	}
	return &Auth{Store: st, wa: wa, inflight: map[string]ceremony{}, grants: map[string]grant{}}, nil
}

// user adapts a store user to WebAuthn.
type user struct{ u *store.User }

func (w user) WebAuthnID() []byte          { return w.u.ID }
func (w user) WebAuthnName() string        { return w.u.Name }
func (w user) WebAuthnDisplayName() string { return w.u.Name }
func (w user) WebAuthnCredentials() []webauthn.Credential {
	out := make([]webauthn.Credential, 0, len(w.u.Credentials))
	for _, c := range w.u.Credentials {
		var cred webauthn.Credential
		if json.Unmarshal([]byte(c), &cred) == nil {
			out = append(out, cred)
		}
	}
	return out
}

func (a *Auth) keep(c ceremony) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	for k, v := range a.inflight {
		if time.Now().After(v.expires) {
			delete(a.inflight, k)
		}
	}
	id := randomID()
	c.expires = time.Now().Add(3 * time.Minute)
	a.inflight[id] = c
	return id
}

func (a *Auth) take(id string) (ceremony, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, ok := a.inflight[id]
	delete(a.inflight, id)
	return c, ok && time.Now().Before(c.expires)
}

// BeginRegistration starts a passkey registration. With no users yet, the
// first registration is open and makes the admin. Otherwise an invite
// code is needed: one that names a user adds a passkey to that account
// (the second passkey the panel keeps asking for), one that does not
// makes a new account of the invite's role. A signed-in user adds a
// passkey to their own account without a code.
func (a *Auth) BeginRegistration(ctx context.Context, name, invite string, current *store.User) (any, string, error) {
	// An abandoned first attempt (browser closed or cancelled before
	// FinishRegistration) leaves an admin row with no passkey, which
	// reads as "registration closed" forever with no way to sign in.
	// Clear those out before deciding whether registration is open.
	_ = a.Store.PruneEmptyUsers(ctx)
	var u *store.User
	role := "admin"
	n, err := a.Store.UserCount(ctx)
	if err != nil {
		return nil, "", err
	}
	switch {
	case current != nil && invite == "":
		u = current
	case n == 0:
		// open
	case invite != "":
		// Checked, not spent: the code is only burned once a passkey is
		// actually saved, so a failed ceremony can be retried with it.
		r, uid, err := a.Store.PeekInvite(ctx, invite)
		if err != nil {
			return nil, "", err
		}
		role = r
		if uid != nil {
			if u, err = a.Store.UserByID(ctx, uid); err != nil {
				return nil, "", err
			}
		}
	default:
		return nil, "", errors.New("registration is closed; ask an admin for an invite")
	}
	if u == nil {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, "", errors.New("an account needs a name")
		}
		if _, err := a.Store.UserByName(ctx, name); err == nil {
			return nil, "", errors.New("that name is taken")
		}
		// The handle the passkey is bound to has to be the account's, so
		// the row exists before the ceremony; one that never gets a
		// passkey is pruned.
		if u, err = a.Store.CreateUser(ctx, name, role); err != nil {
			return nil, "", err
		}
	}
	wu := user{u}
	opts, sess, err := a.wa.BeginRegistration(wu,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{ResidentKey: protocol.ResidentKeyRequirementRequired, UserVerification: protocol.VerificationPreferred}))
	if err != nil {
		_ = a.Store.PruneEmptyUsers(ctx)
		return nil, "", err
	}
	id := a.keep(ceremony{data: *sess, user: &wu, role: role, invite: invite})
	return opts, id, nil
}

// FinishRegistration takes the browser's answer and stores the passkey,
// creating the account if this is its first. The invite, if any, is
// spent only now that a passkey actually exists to show for it.
func (a *Auth) FinishRegistration(ctx context.Context, id string, r *http.Request) (*store.User, error) {
	c, ok := a.take(id)
	if !ok {
		return nil, errors.New("the registration timed out; start again")
	}
	cred, err := a.wa.FinishRegistration(*c.user, c.data, r)
	if err != nil {
		_ = a.Store.PruneEmptyUsers(ctx)
		return nil, err
	}
	u := c.user.u
	b, _ := json.Marshal(cred)
	if err := a.Store.AddCredential(ctx, u.ID, cred.ID, string(b)); err != nil {
		return nil, err
	}
	if c.invite != "" {
		if _, _, err := a.Store.UseInvite(ctx, c.invite); err != nil {
			return nil, err
		}
	}
	return a.Store.UserByID(ctx, u.ID)
}

// BeginLogin starts a discoverable login: no username asked.
func (a *Auth) BeginLogin() (any, string, error) {
	opts, sess, err := a.wa.BeginDiscoverableLogin()
	if err != nil {
		return nil, "", err
	}
	return opts, a.keep(ceremony{data: *sess}), nil
}

// FinishLogin resolves the passkey the browser used to its account.
func (a *Auth) FinishLogin(ctx context.Context, id string, r *http.Request) (*store.User, error) {
	c, ok := a.take(id)
	if !ok {
		return nil, errors.New("the sign-in timed out; try again")
	}
	var found *store.User
	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		u, err := a.Store.UserByCredential(ctx, rawID)
		if err != nil {
			return nil, err
		}
		found = u
		return user{u}, nil
	}
	cred, err := a.wa.FinishDiscoverableLogin(handler, c.data, r)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(cred)
	_ = a.Store.UpdateCredential(ctx, cred.ID, string(b))
	return found, nil
}

// GrantCLI is the panel, signed in, approving a CLI on the person's own
// computer: a one-time code good for two minutes that stands for the
// account. The code is what crosses the redirect to the CLI's loopback
// port, so the session itself never appears in a URL.
func (a *Auth) GrantCLI(u *store.User) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	for k, g := range a.grants {
		if time.Now().After(g.expires) {
			delete(a.grants, k)
		}
	}
	code := randomID()
	a.grants[code] = grant{userID: u.ID, expires: time.Now().Add(2 * time.Minute)}
	return code
}

// RedeemCLI spends a grant for a session of its account: the same
// thirty-day row the panel gets, sent by the CLI as a bearer.
func (a *Auth) RedeemCLI(ctx context.Context, code string) (string, *store.User, error) {
	a.mu.Lock()
	g, ok := a.grants[code]
	delete(a.grants, code)
	a.mu.Unlock()
	if !ok || time.Now().After(g.expires) {
		return "", nil, errors.New("the code is not live; run login again")
	}
	u, err := a.Store.UserByID(ctx, g.userID)
	if err != nil {
		return "", nil, err
	}
	tok, err := a.Store.NewSession(ctx, u.ID)
	if err != nil {
		return "", nil, err
	}
	return tok, u, nil
}

func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
