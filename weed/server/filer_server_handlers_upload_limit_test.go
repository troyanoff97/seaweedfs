package weed_server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/seaweedfs/seaweedfs/weed/security"
)

func TestFilerUploadLimitRejectsBusyPut(t *testing.T) {
	fs := &FilerServer{
		option: &FilerOption{
			ConcurrentFileUploadLimit: 1,
			MaxMB:                     16,
		},
		inFlightDataLimitCond: sync.NewCond(&sync.Mutex{}),
		filerGuard:            security.NewGuard([]string{}, "", 0, "", 0),
	}
	atomic.StoreInt64(&fs.inFlightUploads, 1)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/buckets/b/obj", strings.NewReader("payload"))
	req.ContentLength = int64(len("payload"))

	done := make(chan struct{})
	go func() {
		fs.filerHandler(rec, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("filer upload limit must fail fast, not block on Wait()")
	}

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
	if rec.Header().Get("Connection") != "close" {
		t.Fatalf("expected Connection: close, got %q", rec.Header().Get("Connection"))
	}
	if got := atomic.LoadInt64(&fs.inFlightUploads); got != 1 {
		t.Fatalf("inflight uploads should stay at 1, got %d", got)
	}
}
