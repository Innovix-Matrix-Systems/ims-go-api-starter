package telemetry

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/tenancy"
)

// HTTPMiddleware captures request/response pairs. It never touches request
// handling: bodies are bounded and non-JSON payloads are skipped; entries are
// handed to the collector through bounded channels. /dev traffic is excluded to
// avoid capturing the dashboard itself.
func (c *Collector) HTTPMiddleware(next http.Handler) http.Handler {
	if c == nil || !c.capture.Enabled || !c.capture.Capture.HTTP {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/dev") {
			next.ServeHTTP(w, r)
			return
		}
		sampled := c.Sample()
		requestBody := c.readRequestBody(r)
		ctx := WithCapture(r.Context(), sampled)
		started := time.Now()
		recorder := &captureWriter{ResponseWriter: w, limit: c.capture.MaxBodyBytes}

		defer func() {
			panicValue := recover()
			status := recorder.status
			if status == 0 {
				status = http.StatusOK
			}
			if panicValue != nil && !recorder.wroteHeader {
				status = http.StatusInternalServerError
			}
			if sampled {
				c.recordHTTPRequest(ctx, r, recorder, requestBody, status, time.Since(started))
				if panicValue != nil {
					c.Exception(ExceptionEntry{
						RequestID: httpx.RequestIDFromContext(ctx),
						Class:     "panic",
						Message:   fmt.Sprint(panicValue),
						Stack:     string(debug.Stack()),
						CreatedAt: time.Now(),
					})
				}
			}
			if panicValue != nil {
				panic(panicValue)
			}
		}()

		next.ServeHTTP(recorder, r.WithContext(ctx))
	})
}

func (c *Collector) recordHTTPRequest(ctx context.Context, r *http.Request, recorder *captureWriter, requestBody []byte, status int, duration time.Duration) {
	route := ""
	if routeCtx := chi.RouteContext(ctx); routeCtx != nil {
		route = routeCtx.RoutePattern()
	}
	identity, hasIdentity := authn.IdentityFromContext(ctx)
	entry := RequestEntry{
		RequestID:     httpx.RequestIDFromContext(ctx),
		Method:        r.Method,
		Path:          r.URL.Path,
		Route:         route,
		Status:        status,
		DurationMS:    float64(duration.Microseconds()) / 1000.0,
		RequestBytes:  int64(len(requestBody)),
		ResponseBytes: recorder.bytes,
		IP:            r.RemoteAddr,
		UserAgent:     r.UserAgent(),
		Headers:       c.redactor.Headers(r.Header),
		RequestBody:   c.redactor.JSON(requestBody),
		ResponseBody:  c.redactor.JSON(recorder.body),
		CreatedAt:     time.Now(),
	}
	if hasIdentity {
		userID := identity.UserID
		entry.UserID = &userID
	}
	if scope, ok := tenancy.FromContext(ctx); ok {
		if tenantID := scope.EffectiveTenantID(); tenantID > 0 {
			entry.TenantID = &tenantID
		}
		if branchID := scope.EffectiveBranchID(); branchID > 0 {
			entry.BranchID = &branchID
		}
	}
	c.Request(entry)
}

// readRequestBody tees up to MaxBodyBytes of a JSON request body and restores
// the full stream for the handler.
func (c *Collector) readRequestBody(r *http.Request) []byte {
	if r.Body == nil || c.capture.MaxBodyBytes <= 0 {
		return nil
	}
	if !strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "json") {
		return nil
	}
	limited := io.LimitReader(r.Body, c.capture.MaxBodyBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil
	}
	original := r.Body
	r.Body = &multiReadCloser{Reader: io.MultiReader(bytes.NewReader(body), original), Closer: original}
	if int64(len(body)) > c.capture.MaxBodyBytes {
		body = body[:c.capture.MaxBodyBytes]
	}
	return body
}

type multiReadCloser struct {
	io.Reader
	io.Closer
}

type captureWriter struct {
	http.ResponseWriter
	status      int
	bytes       int64
	body        []byte
	limit       int64
	wroteHeader bool
}

func (w *captureWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.status = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *captureWriter) Write(payload []byte) (int, error) {
	if !w.wroteHeader {
		w.status = http.StatusOK
		w.wroteHeader = true
	}
	w.bytes += int64(len(payload))
	if w.limit > 0 && int64(len(w.body)) < w.limit {
		remaining := w.limit - int64(len(w.body))
		if int64(len(payload)) > remaining {
			w.body = append(w.body, payload[:remaining]...)
		} else {
			w.body = append(w.body, payload...)
		}
	}
	return w.ResponseWriter.Write(payload)
}

// Flush forwards to the underlying writer when it supports flushing.
func (w *captureWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
