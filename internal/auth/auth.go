// Package auth provides the identity/authorization *mechanisms* for server mode:
// roles, an Actor, a pluggable Authenticator, coarse role authorization, and an
// append-only audit log. It is deliberately an ADAPTER concern — the core
// application.Service stays unaware of auth (Guardrail G2). Authorization is
// enforced by the HTTP layer (Phase 20 middleware).
//
// The default posture is TRUST-ALL (LAN): the default authenticator returns a
// single admin actor and does not enforce identity, so no request is ever
// rejected. This keeps a LAN deployment frictionless while providing a clean
// extension point — swap in a real Authenticator (API keys in Phase 22, or an
// OIDC/SSO provider later) without touching call sites. Untrusted exposure
// requires either enabling a real authenticator or fronting the server with an
// authenticating reverse proxy.
package auth

import (
	"errors"
	"net/http"
)

// Role is a coarse permission tier. Ordering: viewer < editor < admin.
type Role string

const (
	RoleViewer Role = "viewer" // search / read / graph
	RoleEditor Role = "editor" // + create / edit / delete / import / assets
	RoleAdmin  Role = "admin"  // + rebuild / workspace open / user management
)

func (r Role) rank() int {
	switch r {
	case RoleViewer:
		return 1
	case RoleEditor:
		return 2
	case RoleAdmin:
		return 3
	default:
		return 0
	}
}

// Actor is the resolved identity behind a request.
type Actor struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Role        Role   `json:"role"`
}

// Can reports whether the actor's role meets or exceeds required.
func (a Actor) Can(required Role) bool {
	return a.Role.rank() >= required.rank()
}

// LocalActor is the default identity used in trust-all mode: a single admin so
// every coarse role gate passes.
var LocalActor = Actor{ID: "local", DisplayName: "Local", Role: RoleAdmin}

// Sentinel errors the HTTP layer maps to 401 / 403.
var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
)

// Authenticator resolves the Actor behind an HTTP request. Implementations must
// be safe for concurrent use.
type Authenticator interface {
	// Authenticate returns the actor for r, or ErrUnauthorized if identity is
	// required but absent/invalid.
	Authenticate(r *http.Request) (Actor, error)
	// Enforced reports whether this authenticator actually checks identity.
	// Trust-all returns false; real providers return true.
	Enforced() bool
}

// TrustAll is the default authenticator: every request is the same configured
// actor (admin by default) and nothing is ever rejected.
type TrustAll struct {
	Actor Actor
}

// NewTrustAll returns a trust-all authenticator using LocalActor.
func NewTrustAll() TrustAll { return TrustAll{Actor: LocalActor} }

func (t TrustAll) Authenticate(*http.Request) (Actor, error) {
	if t.Actor.ID == "" {
		return LocalActor, nil
	}
	return t.Actor, nil
}

func (t TrustAll) Enforced() bool { return false }
