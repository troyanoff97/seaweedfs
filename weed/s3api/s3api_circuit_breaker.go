package s3api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/mux"
	"github.com/seaweedfs/seaweedfs/weed/filer"
	"github.com/seaweedfs/seaweedfs/weed/glog"
	"github.com/seaweedfs/seaweedfs/weed/pb"
	"github.com/seaweedfs/seaweedfs/weed/pb/filer_pb"
	"github.com/seaweedfs/seaweedfs/weed/pb/s3_pb"
	"github.com/seaweedfs/seaweedfs/weed/s3api/s3_constants"
	"github.com/seaweedfs/seaweedfs/weed/s3api/s3err"
	"github.com/seaweedfs/seaweedfs/weed/stats"
)

type CircuitBreaker struct {
	sync.RWMutex
	Enabled     bool
	counters    map[string]*int64
	limitations map[string]int64
	s3a         *S3ApiServer

	// Interceptor, if set, wraps the per-route handler ahead of ALL
	// circuit-breaker logic (upload limiting and the breaker checks) and runs
	// even when the breaker is disabled. It is a general per-request
	// interceptor seam: an implementation may reject the request (write its own
	// response and not call next) or wrap next to observe/shape it. Nil by
	// default, so it is a no-op unless explicitly set.
	Interceptor func(next http.HandlerFunc, action string) http.HandlerFunc
}

func NewCircuitBreaker(option *S3ApiServerOption) *CircuitBreaker {
	cb := &CircuitBreaker{
		counters:    make(map[string]*int64),
		limitations: make(map[string]int64),
	}

	// Use WithOneOfGrpcFilerClients to support multiple filers with failover
	err := pb.WithOneOfGrpcFilerClients(false, option.Filers, option.GrpcDialOption, func(client filer_pb.SeaweedFilerClient) error {
		content, err := filer.ReadInsideFiler(context.Background(), client, s3_constants.CircuitBreakerConfigDir, s3_constants.CircuitBreakerConfigFile)
		if errors.Is(err, filer_pb.ErrNotFound) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read S3 circuit breaker config: %w", err)
		}
		return cb.LoadS3ApiConfigurationFromBytes(content)
	})

	if err != nil {
		glog.Warningf("S3 circuit breaker disabled; failed to load config from any filer: %v", err)
	}

	return cb
}

func (cb *CircuitBreaker) LoadS3ApiConfigurationFromBytes(content []byte) error {
	cbCfg := &s3_pb.S3CircuitBreakerConfig{}
	if err := filer.ParseS3ConfigurationFromBytes(content, cbCfg); err != nil {
		glog.Warningf("unmarshal error: %v", err)
		return fmt.Errorf("unmarshal error: %w", err)
	}
	if err := cb.loadCircuitBreakerConfig(cbCfg); err != nil {
		return err
	}
	return nil
}

func (cb *CircuitBreaker) loadCircuitBreakerConfig(cfg *s3_pb.S3CircuitBreakerConfig) error {

	//global
	globalEnabled := false
	globalOptions := cfg.Global
	limitations := make(map[string]int64)
	if globalOptions != nil && globalOptions.Enabled && len(globalOptions.Actions) > 0 {
		globalEnabled = globalOptions.Enabled
		for action, limit := range globalOptions.Actions {
			limitations[action] = limit
		}
	}
	cb.Enabled = globalEnabled

	//buckets
	for bucket, cbOptions := range cfg.Buckets {
		if cbOptions.Enabled {
			for action, limit := range cbOptions.Actions {
				limitations[s3_constants.Concat(bucket, action)] = limit
			}
		}
	}

	cb.limitations = limitations
	return nil
}

func (cb *CircuitBreaker) Limit(f func(w http.ResponseWriter, r *http.Request), action string) (http.HandlerFunc, Action) {
	return cb.limitHandler(f, action, false)
}

// LimitBodyUpload is like Limit but also enforces ConcurrentUploadLimit /
// ConcurrentFileUploadLimit. Use only for handlers that accept a client body
// destined for volume storage (PutObject, PutObjectPart, PostPolicy). Metadata
// writes (Delete, Abort, Complete MPU, Copy, …) must use Limit so they are not
// starved by upload backpressure.
func (cb *CircuitBreaker) LimitBodyUpload(f func(w http.ResponseWriter, r *http.Request), action string) (http.HandlerFunc, Action) {
	return cb.limitHandler(f, action, true)
}

// unknownUploadByteEstimate is used when Content-Length is absent/chunked so
// ConcurrentUploadLimit (bytes) cannot be bypassed by undercounting as 0.
// Matches the hardcoded S3 chunk size in putToFiler.
const unknownUploadByteEstimate int64 = 8 * 1024 * 1024

func (cb *CircuitBreaker) limitHandler(f func(w http.ResponseWriter, r *http.Request), action string, applyUploadCap bool) (http.HandlerFunc, Action) {
	inner := func(w http.ResponseWriter, r *http.Request) {
		// Fail fast with 503 instead of blocking on Wait(): blocking holds the
		// accepted TCP connection (and its buffers) until a slot frees, which
		// under sustained load balloons FD count and RSS into memcg OOM.
		if applyUploadCap && cb.s3a != nil &&
			(cb.s3a.option.ConcurrentUploadLimit != 0 || cb.s3a.option.ConcurrentFileUploadLimit != 0) {

			contentLength := r.ContentLength
			if contentLength < 0 {
				contentLength = unknownUploadByteEstimate
			}

			cb.s3a.inFlightDataLimitCond.L.Lock()
			inFlightDataSize := atomic.LoadInt64(&cb.s3a.inFlightDataSize)
			inFlightUploads := atomic.LoadInt64(&cb.s3a.inFlightUploads)
			overBytes := cb.s3a.option.ConcurrentUploadLimit != 0 && inFlightDataSize+contentLength > cb.s3a.option.ConcurrentUploadLimit
			overFiles := cb.s3a.option.ConcurrentFileUploadLimit != 0 && inFlightUploads >= cb.s3a.option.ConcurrentFileUploadLimit
			if overBytes || overFiles {
				cb.s3a.inFlightDataLimitCond.L.Unlock()
				glog.V(1).Infof("reject S3 upload: inflight uploads=%d/%d bytes=%d/%d contentLength=%d",
					inFlightUploads, cb.s3a.option.ConcurrentFileUploadLimit,
					inFlightDataSize, cb.s3a.option.ConcurrentUploadLimit, contentLength)
				rejectBusyUpload(w, r, s3err.ErrTooManyRequest)
				return
			}
			newUploads := atomic.AddInt64(&cb.s3a.inFlightUploads, 1)
			newSize := atomic.AddInt64(&cb.s3a.inFlightDataSize, contentLength)
			cb.s3a.inFlightDataLimitCond.L.Unlock()

			stats.S3InFlightUploadCountGauge.Set(float64(newUploads))
			stats.S3InFlightUploadBytesGauge.Set(float64(newSize))
			defer func() {
				newUploads := atomic.AddInt64(&cb.s3a.inFlightUploads, -1)
				newSize := atomic.AddInt64(&cb.s3a.inFlightDataSize, -contentLength)
				stats.S3InFlightUploadCountGauge.Set(float64(newUploads))
				stats.S3InFlightUploadBytesGauge.Set(float64(newSize))
			}()
		}

		// Apply circuit breaker logic
		if !cb.Enabled {
			f(w, r)
			return
		}

		vars := mux.Vars(r)
		bucket := vars["bucket"]

		rollback, errCode := cb.limit(r, bucket, action)
		defer func() {
			for _, rf := range rollback {
				rf()
			}
		}()

		if errCode == s3err.ErrNone {
			f(w, r)
			return
		}
		s3err.WriteErrorResponse(w, r, errCode)
	}

	// The interceptor is consulted per request rather than captured here, so it
	// can be installed after the routes are registered (e.g. once the server and
	// its dependencies are constructed) and so a nil CircuitBreaker is never
	// dereferenced at registration time. When unset this is just a nil check.
	// It runs outermost: before upload limiting and the breaker checks, and
	// regardless of cb.Enabled.
	return func(w http.ResponseWriter, r *http.Request) {
		if cb.Interceptor != nil {
			cb.Interceptor(inner, action)(w, r)
			return
		}
		inner(w, r)
	}, Action(action)
}

// rejectBusyUpload closes the connection promptly so unread request bodies do
// not pin FDs after a 503 backpressure response.
func rejectBusyUpload(w http.ResponseWriter, r *http.Request, code s3err.ErrorCode) {
	w.Header().Set("Connection", "close")
	if rc := http.NewResponseController(w); rc != nil {
		_ = rc.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	}
	if r.Body != nil {
		_, _ = io.CopyN(io.Discard, r.Body, 1<<20)
		_ = r.Body.Close()
	}
	s3err.WriteErrorResponse(w, r, code)
}

func (cb *CircuitBreaker) limit(r *http.Request, bucket string, action string) (rollback []func(), errCode s3err.ErrorCode) {

	//bucket simultaneous request count
	bucketCountRollBack, errCode := cb.loadCounterAndCompare(s3_constants.Concat(bucket, action, s3_constants.LimitTypeCount), 1, s3err.ErrTooManyRequest)
	if bucketCountRollBack != nil {
		rollback = append(rollback, bucketCountRollBack)
	}
	if errCode != s3err.ErrNone {
		return
	}

	//bucket simultaneous request content bytes
	bucketContentLengthRollBack, errCode := cb.loadCounterAndCompare(s3_constants.Concat(bucket, action, s3_constants.LimitTypeBytes), r.ContentLength, s3err.ErrRequestBytesExceed)
	if bucketContentLengthRollBack != nil {
		rollback = append(rollback, bucketContentLengthRollBack)
	}
	if errCode != s3err.ErrNone {
		return
	}

	//global simultaneous request count
	globalCountRollBack, errCode := cb.loadCounterAndCompare(s3_constants.Concat(action, s3_constants.LimitTypeCount), 1, s3err.ErrTooManyRequest)
	if globalCountRollBack != nil {
		rollback = append(rollback, globalCountRollBack)
	}
	if errCode != s3err.ErrNone {
		return
	}

	//global simultaneous request content bytes
	globalContentLengthRollBack, errCode := cb.loadCounterAndCompare(s3_constants.Concat(action, s3_constants.LimitTypeBytes), r.ContentLength, s3err.ErrRequestBytesExceed)
	if globalContentLengthRollBack != nil {
		rollback = append(rollback, globalContentLengthRollBack)
	}
	if errCode != s3err.ErrNone {
		return
	}
	return
}

func (cb *CircuitBreaker) loadCounterAndCompare(key string, inc int64, errCode s3err.ErrorCode) (f func(), e s3err.ErrorCode) {
	e = s3err.ErrNone
	if max, ok := cb.limitations[key]; ok {
		cb.RLock()
		counter, exists := cb.counters[key]
		cb.RUnlock()

		if !exists {
			cb.Lock()
			counter, exists = cb.counters[key]
			if !exists {
				var newCounter int64
				counter = &newCounter
				cb.counters[key] = counter
			}
			cb.Unlock()
		}
		current := atomic.LoadInt64(counter)
		if current+inc > max {
			e = errCode
			return
		} else {
			current := atomic.AddInt64(counter, inc)
			f = func() {
				atomic.AddInt64(counter, -inc)
			}
			if current > max {
				e = errCode
				return
			}
		}
	}
	return
}
