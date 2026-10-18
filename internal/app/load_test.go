package app

import (
	"net/http"
	"sync"
	"testing"
)

// TestLoadSmokeBoundedConcurrency drives the router with bounded concurrency to
// catch data races and gross regressions under load. It is deliberately small
// enough to run in CI on every push.
func TestLoadSmokeBoundedConcurrency(t *testing.T) {
	router := newTestRouter(t)

	const workers = 16
	const perWorker = 50

	var wg sync.WaitGroup
	errs := make(chan string, workers*perWorker)

	paths := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/up"},
		{http.MethodGet, "/api/health"},
		{http.MethodGet, "/api/v1/system/info"},
	}

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				req := paths[(w+i)%len(paths)]
				rec := doRequest(t, router, req.method, req.path, "")
				if rec.Code != http.StatusOK {
					errs <- req.method + " " + req.path + ": " + http.StatusText(rec.Code)
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("unexpected non-200 under load: %s", err)
	}
}
