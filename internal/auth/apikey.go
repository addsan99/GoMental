package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// APIKey is a stored, revocable credential issued to an agent or service
// account, tied to a role. The plaintext secret is shown once at creation; only
// its SHA-256 hash is persisted.
type APIKey struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Role      Role   `json:"role"`
	Hash      string `json:"hash"`
	CreatedAt string `json:"createdAt"`
	Revoked   bool   `json:"revoked"`
}

// APIKeyStore persists API keys as JSON under the server metadata dir (never in
// note content — Guardrail G4).
type APIKeyStore struct {
	mu   sync.Mutex
	path string
	keys map[string]*APIKey // by ID
}

// DefaultAPIKeyPath returns the key store path for a workspace root.
func DefaultAPIKeyPath(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, ".workspace", "server", "api-keys.json")
}

// OpenAPIKeyStore loads (or initializes) the key store at path.
func OpenAPIKeyStore(path string) (*APIKeyStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	s := &APIKeyStore{path: path, keys: map[string]*APIKey{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	var list []*APIKey
	if len(data) > 0 {
		if err := json.Unmarshal(data, &list); err != nil {
			return nil, err
		}
	}
	for _, k := range list {
		s.keys[k.ID] = k
	}
	return s, nil
}

func (s *APIKeyStore) saveLocked() error {
	list := make([]*APIKey, 0, len(s.keys))
	for _, k := range s.keys {
		list = append(list, k)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, append(data, '\n'), 0o600)
}

// Create issues a new key with the given name and role. It returns the plaintext
// token (shown only here) and the stored record (without the secret).
func (s *APIKeyStore) Create(name string, role Role) (string, APIKey, error) {
	if role.rank() == 0 {
		return "", APIKey{}, fmt.Errorf("invalid role %q", role)
	}
	idBytes := make([]byte, 6)
	if _, err := rand.Read(idBytes); err != nil {
		return "", APIKey{}, err
	}
	secretBytes := make([]byte, 24)
	if _, err := rand.Read(secretBytes); err != nil {
		return "", APIKey{}, err
	}
	id := hex.EncodeToString(idBytes)
	// The plaintext token embeds the id for readability; only the hash is stored.
	plaintext := "gm_" + id + "_" + hex.EncodeToString(secretBytes)
	rec := &APIKey{
		ID:        id,
		Name:      strings.TrimSpace(name),
		Role:      role,
		Hash:      hashToken(plaintext),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[id] = rec
	if err := s.saveLocked(); err != nil {
		delete(s.keys, id)
		return "", APIKey{}, err
	}
	public := *rec
	public.Hash = ""
	return plaintext, public, nil
}

// List returns all keys without their hashes.
func (s *APIKeyStore) List() []APIKey {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]APIKey, 0, len(s.keys))
	for _, k := range s.keys {
		public := *k
		public.Hash = ""
		out = append(out, public)
	}
	return out
}

// Revoke marks a key revoked. Returns false if no such key.
func (s *APIKeyStore) Revoke(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.keys[id]
	if !ok {
		return false, nil
	}
	if k.Revoked {
		return true, nil
	}
	k.Revoked = true
	if err := s.saveLocked(); err != nil {
		k.Revoked = false
		return false, err
	}
	return true, nil
}

// Lookup resolves a plaintext token to an actor if it matches a non-revoked key.
func (s *APIKeyStore) Lookup(plaintext string) (Actor, bool) {
	hash := hashToken(plaintext)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range s.keys {
		if k.Revoked {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(k.Hash), []byte(hash)) == 1 {
			return Actor{ID: "key:" + k.ID, DisplayName: k.Name, Role: k.Role}, true
		}
	}
	return Actor{}, false
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

// BearerAuthenticator authenticates via an API key presented as
// `Authorization: Bearer <token>` or `X-API-Key: <token>`. When no token is
// presented it falls back to Fallback (trust-all local actor) unless RequireKey
// is set, in which case a missing key is unauthorized. An invalid or revoked key
// is always rejected.
type BearerAuthenticator struct {
	Store      *APIKeyStore
	Fallback   Actor
	RequireKey bool
}

func (b BearerAuthenticator) Authenticate(r *http.Request) (Actor, error) {
	token := bearerToken(r)
	if token == "" {
		if b.RequireKey {
			return Actor{}, ErrUnauthorized
		}
		if b.Fallback.ID == "" {
			return LocalActor, nil
		}
		return b.Fallback, nil
	}
	if b.Store == nil {
		return Actor{}, ErrUnauthorized
	}
	actor, ok := b.Store.Lookup(token)
	if !ok {
		return Actor{}, ErrUnauthorized
	}
	return actor, nil
}

func (b BearerAuthenticator) Enforced() bool { return b.RequireKey }

func bearerToken(r *http.Request) string {
	if h := strings.TrimSpace(r.Header.Get("Authorization")); h != "" {
		if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
			return strings.TrimSpace(h[7:])
		}
	}
	return strings.TrimSpace(r.Header.Get("X-API-Key"))
}
