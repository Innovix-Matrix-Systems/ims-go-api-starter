package health

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
)

// Handler serves the health endpoint with a cached result store; ?fresh=1
// forces a live run.
type Handler struct {
	registry *Registry
	ttl      time.Duration
	now      func() time.Time

	mu     sync.Mutex
	last   StoredResults
	lastAt time.Time

	refreshMu sync.Mutex
}

// NewHandler creates a cached health handler with the given result TTL.
func NewHandler(registry *Registry, ttl time.Duration) *Handler {
	return &Handler{registry: registry, ttl: ttl, now: time.Now}
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// ?fresh=1 re-runs every check (DB, queue GROUP BY, ...): only local
	// callers (deploy scripts, the box's own monitoring) may force it, so the
	// public endpoint cannot be used to amplify load (M49).
	fresh := r.URL.Query().Get("fresh") == "1" && httpx.IsLoopback(r)

	results, ok := h.cached(fresh)
	if !ok {
		// Refresh outside the read lock so slow checks never serialize readers;
		// refreshMu collapses concurrent refreshes into one run.
		h.refreshMu.Lock()
		results, ok = h.cached(fresh)
		if !ok {
			results = h.registry.Run(r.Context())
			h.mu.Lock()
			h.last = results
			h.lastAt = h.now()
			h.mu.Unlock()
		}
		h.refreshMu.Unlock()
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, post-check=0, pre-check=0")
	// Readiness must fail closed: load balancers and deploy scripts read the
	// status code, not the body (H18). /up stays a plain liveness 200.
	if results.HasFailures() {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(results)
}

func (h *Handler) cached(fresh bool) (StoredResults, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !fresh && !h.lastAt.IsZero() && h.now().Sub(h.lastAt) <= h.ttl {
		return h.last, true
	}
	return StoredResults{}, false
}
