package middleware

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

func Chain(h http.Handler) http.Handler {
	return Logging(CORS(Recovery(h)))
}

func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Add a simple request ID for tracing
		requestID := r.Header.Get("X-Request-Id")
		if requestID == "" {
			requestID = fmt.Sprintf("req-%d", time.Now().UnixNano())
		}

		// Wrap ResponseWriter to capture headers
		wr := &responseWriter{ResponseWriter: w, headers: http.Header{}}

		next.ServeHTTP(wr, r)

		retryAfter := wr.Header().Get("Retry-After")
		if retryAfter != "" {
			log.Printf("%s %s %s %v [Retry-After: %s]", r.Method, r.URL.Path, requestID, time.Since(start), retryAfter)
		} else {
			log.Printf("%s %s %s %v", r.Method, r.URL.Path, requestID, time.Since(start))
		}
	})
}

// responseWriter wraps http.ResponseWriter to capture headers
// needed for logging after next.ServeHTTP call.
type responseWriter struct {
	http.ResponseWriter
	headers http.Header
}

func (rw *responseWriter) Header() http.Header {
	return rw.headers
}

func (rw *responseWriter) WriteHeader(statusCode int) {
	rw.ResponseWriter.WriteHeader(statusCode)

	// Capture headers when WriteHeader is called
	for k, v := range rw.ResponseWriter.Header() {
		rw.headers[k] = v
	}
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	return rw.ResponseWriter.Write(b)
}


func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("panic recovered: %v", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func JSON(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next(w, r)
	}
}
