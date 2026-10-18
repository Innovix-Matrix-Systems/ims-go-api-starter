package ratelimit

import (
	"context"
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
)

// Limiter decides whether a key may proceed. The in-memory implementation
// shipped here is an in-process fixed-window limiter; the cache-backed limiter
// implements the same interface.
type Limiter interface {
	Allow(ctx context.Context, key string) (allowed bool, retryAfter time.Duration)
	Close()
}

type windowEntry struct {
	count int
	start time.Time
}

type memoryLimiter struct {
	limit  int
	window time.Duration
	now    func() time.Time

	mu      sync.Mutex
	entries map[string]*windowEntry
	stop    chan struct{}
	once    sync.Once
}

// NewMemoryLimiter creates a fixed-window in-memory limiter.
func NewMemoryLimiter(limit int, window time.Duration) Limiter {
	l := &memoryLimiter{
		limit:   limit,
		window:  window,
		now:     time.Now,
		entries: map[string]*windowEntry{},
		stop:    make(chan struct{}),
	}
	go l.janitor()
	return l
}

// Allow implements Limiter.
func (l *memoryLimiter) Allow(ctx context.Context, key string) (bool, time.Duration) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, ok := l.entries[key]
	if !ok || now.Sub(entry.start) >= l.window {
		l.entries[key] = &windowEntry{count: 1, start: now}
		return true, 0
	}
	if entry.count >= l.limit {
		return false, l.window - now.Sub(entry.start)
	}
	entry.count++
	return true, 0
}

// Close stops the janitor goroutine.
func (l *memoryLimiter) Close() {
	l.once.Do(func() { close(l.stop) })
}

func (l *memoryLimiter) janitor() {
	ticker := time.NewTicker(l.window)
	defer ticker.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-ticker.C:
			now := l.now()
			l.mu.Lock()
			for key, entry := range l.entries {
				if now.Sub(entry.start) >= l.window {
					delete(l.entries, key)
				}
			}
			l.mu.Unlock()
		}
	}
}

// KeyByIP keys requests by client IP.
func KeyByIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// Middleware rejects requests over the limit with a 429 problem.
func Middleware(limiter Limiter, keyFn func(*http.Request) string, responder *httpx.Responder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			allowed, retryAfter := limiter.Allow(r.Context(), keyFn(r))
			if allowed {
				next.ServeHTTP(w, r)
				return
			}
			retry := int(math.Ceil(retryAfter.Seconds()))
			if retry < 1 {
				retry = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			responder.Error(w, r, apperr.New(apperr.RateLimitExceeded, "Too many requests. Please retry later."))
		})
	}
}
