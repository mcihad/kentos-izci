package ingest

import (
	"context"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/jackc/pgx/v5"

	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/secret"
	"github.com/mcihad/kentos-izci/internal/store"
)

// ErrUnknownKey means the key is malformed, unknown, revoked or disabled.
var ErrUnknownKey = errors.New("unknown, revoked or disabled API key")

// KeyResolver resolves API keys with a positive cache (so ingest does not
// hit the database per event) and a short negative cache (so garbage keys
// cannot hammer it either).
type KeyResolver struct {
	db       *db.DB
	positive *expirable.LRU[string, *store.ResolvedKey]
	negative *expirable.LRU[string, struct{}]

	mu      sync.Mutex
	touched map[uuid.UUID]time.Time
}

// NewKeyResolver creates a resolver. Revocations propagate within ttl (or
// immediately through Invalidate).
func NewKeyResolver(database *db.DB, ttl time.Duration) *KeyResolver {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &KeyResolver{
		db:       database,
		positive: expirable.NewLRU[string, *store.ResolvedKey](20000, nil, ttl),
		negative: expirable.NewLRU[string, struct{}](20000, nil, 10*time.Second),
		touched:  map[uuid.UUID]time.Time{},
	}
}

// KeyFromHeader extracts an ingest key from "Authorization: Bearer izk_…" or
// "X-Izci-Key: izk_…".
func KeyFromHeader(h http.Header) string {
	if v := strings.TrimSpace(h.Get("X-Izci-Key")); v != "" {
		return v
	}
	auth := strings.TrimSpace(h.Get("Authorization"))
	if len(auth) > 7 && strings.EqualFold(auth[:7], "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return ""
}

// Resolve returns the usable key for token, or ErrUnknownKey.
func (r *KeyResolver) Resolve(ctx context.Context, token string) (*store.ResolvedKey, error) {
	return r.resolve(ctx, token, 0)
}

// ResolveForProject is Resolve restricted to a project's numeric public id
// (Sentry DSNs carry both).
func (r *KeyResolver) ResolveForProject(ctx context.Context, token string, publicID int64) (*store.ResolvedKey, error) {
	return r.resolve(ctx, token, publicID)
}

func (r *KeyResolver) resolve(ctx context.Context, token string, publicID int64) (*store.ResolvedKey, error) {
	if !secret.HasPrefix(token, secret.PrefixAPIKey) || len(token) > 128 {
		return nil, ErrUnknownKey
	}
	cacheKey := hex.EncodeToString(secret.Hash(token))
	if rk, ok := r.positive.Get(cacheKey); ok {
		if publicID != 0 && rk.ProjectPublicID != publicID {
			return nil, ErrUnknownKey
		}
		r.touch(rk.Key.ID)
		return rk, nil
	}
	if _, ok := r.negative.Get(cacheKey); ok {
		return nil, ErrUnknownKey
	}
	var rk *store.ResolvedKey
	err := r.db.InSystem(ctx, func(tx pgx.Tx) error {
		var err error
		rk, err = store.ResolveAPIKey(ctx, tx, token)
		return err
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		r.negative.Add(cacheKey, struct{}{})
		return nil, ErrUnknownKey
	case err != nil:
		return nil, err
	case !rk.Key.Usable():
		r.negative.Add(cacheKey, struct{}{})
		return nil, ErrUnknownKey
	}
	r.positive.Add(cacheKey, rk)
	if publicID != 0 && rk.ProjectPublicID != publicID {
		return nil, ErrUnknownKey
	}
	r.touch(rk.Key.ID)
	return rk, nil
}

// InvalidateAll drops every cached key (after revocations or settings
// changes).
func (r *KeyResolver) InvalidateAll() {
	r.positive.Purge()
	r.negative.Purge()
}

func (r *KeyResolver) touch(id uuid.UUID) {
	r.mu.Lock()
	r.touched[id] = time.Now()
	r.mu.Unlock()
}

// FlushUsage writes last_used_at of recently used keys; call periodically.
func (r *KeyResolver) FlushUsage(ctx context.Context, log *slog.Logger) {
	r.mu.Lock()
	if len(r.touched) == 0 {
		r.mu.Unlock()
		return
	}
	ids := make([]uuid.UUID, 0, len(r.touched))
	latest := time.Time{}
	for id, t := range r.touched {
		ids = append(ids, id)
		if t.After(latest) {
			latest = t
		}
	}
	r.touched = map[uuid.UUID]time.Time{}
	r.mu.Unlock()
	err := r.db.InSystem(ctx, func(tx pgx.Tx) error { return store.TouchAPIKeys(ctx, tx, ids, latest) })
	if err != nil {
		log.Warn("record api key usage", slog.Any("err", err))
	}
}

// originAllowed checks a browser Origin against the key's allowed origins:
// exact origins, "https://*.example.com" subdomain wildcards or "*".
func originAllowed(allowed []string, origin string) bool {
	// No list means any origin, as for Sentry DSNs: the key is public in
	// browser code anyway, and requests without an Origin header (servers,
	// curl) are never restricted.
	if origin == "" || len(allowed) == 0 {
		return true
	}
	o := strings.TrimRight(strings.ToLower(origin), "/")
	for _, a := range allowed {
		a = strings.TrimRight(strings.ToLower(strings.TrimSpace(a)), "/")
		if a == "*" || a == o {
			return true
		}
		// "https://*.example.com" wildcard subdomains.
		if strings.Contains(a, "://*.") {
			scheme, host, _ := strings.Cut(a, "://*.")
			if strings.HasPrefix(o, scheme+"://") && strings.HasSuffix(o, "."+host) {
				return true
			}
		}
	}
	return false
}
