package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fixedCheck struct {
	name   string
	status Status
}

func (c fixedCheck) Name() string { return c.name }

func (c fixedCheck) Run(context.Context) CheckResult { return CheckResult{Status: c.status} }

func TestHandlerStatusCode(t *testing.T) {
	cases := []struct {
		name  string
		setup func(r *Registry)
		want  int
	}{
		{"all ok", func(r *Registry) { r.Add(fixedCheck{"db", StatusOK}) }, http.StatusOK},
		{"warning stays ready", func(r *Registry) { r.Add(fixedCheck{"db", StatusWarning}) }, http.StatusOK},
		{"critical failure is unready", func(r *Registry) { r.Add(fixedCheck{"db", StatusFailed}) }, http.StatusServiceUnavailable},
		{"crash is unready", func(r *Registry) { r.Add(fixedCheck{"db", StatusCrashed}) }, http.StatusServiceUnavailable},
		{"advisory failure stays ready", func(r *Registry) {
			r.Add(fixedCheck{"db", StatusOK})
			r.AddAdvisory(fixedCheck{"backups", StatusFailed})
		}, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			registry := NewRegistry()
			tc.setup(registry)
			rec := httptest.NewRecorder()
			NewHandler(registry, time.Minute).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}
