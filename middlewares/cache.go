package middlewares

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tink3rlabs/magic/cache"
	"github.com/tink3rlabs/magic/logger"
)

// CacheConfig holds the configuration for the cache middleware
type CacheConfig struct {
	// Adapter is the cache adapter to use
	Adapter cache.CacheAdapter
	
	// TTL is the default time-to-live for cached responses
	TTL time.Duration
	
	// Methods are the HTTP methods that should be cached (defaults to GET)
	Methods []string
	
	// KeyPrefix is prepended to all cache keys
	KeyPrefix string
	
	// SkipCacheHeader is the request header name to skip caching for a specific request
	SkipCacheHeader string
	
	// CacheStatusHeader is the response header name to indicate cache hit/miss status
	CacheStatusHeader string
}

// responseWriter is a wrapper around http.ResponseWriter to capture the response
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	body       *bytes.Buffer
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
		body:           &bytes.Buffer{},
	}
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	rw.body.Write(b)
	return rw.ResponseWriter.Write(b)
}

// cachedResponse represents a cached HTTP response
type cachedResponse struct {
	StatusCode int                 `json:"status_code"`
	Headers    map[string][]string `json:"headers"`
	Body       []byte              `json:"body"`
}

// CacheMiddleware creates a middleware that caches HTTP responses
func CacheMiddleware(config CacheConfig) func(http.Handler) http.Handler {
	// Set defaults
	if config.TTL == 0 {
		config.TTL = 5 * time.Minute
	}
	if len(config.Methods) == 0 {
		config.Methods = []string{"GET"}
	}
	if config.KeyPrefix == "" {
		config.KeyPrefix = "magic:cache:"
	}
	if config.SkipCacheHeader == "" {
		config.SkipCacheHeader = "X-Skip-Cache"
	}
	if config.CacheStatusHeader == "" {
		config.CacheStatusHeader = "X-Cache-Status"
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip caching if adapter is nil or cache is explicitly skipped
			if config.Adapter == nil || r.Header.Get(config.SkipCacheHeader) != "" {
				next.ServeHTTP(w, r)
				return
			}

			// Only cache specified HTTP methods
			if !contains(config.Methods, r.Method) {
				next.ServeHTTP(w, r)
				return
			}

			// Generate cache key based on request
			cacheKey := generateCacheKey(config.KeyPrefix, r)

			// Try to get from cache
			cachedData, err := config.Adapter.Get(cacheKey)
			if err == nil {
				// Cache hit - unmarshal and return cached response
				var cached cachedResponse
				if err := json.Unmarshal(cachedData, &cached); err != nil {
					logger.Warn("failed to unmarshal cached response", slog.Any("error", err.Error()))
				} else {
					// Write cached response
					for key, values := range cached.Headers {
						for _, value := range values {
							w.Header().Add(key, value)
						}
					}
					w.Header().Set(config.CacheStatusHeader, "HIT")
					w.WriteHeader(cached.StatusCode)
					w.Write(cached.Body)
					return
				}
			} else if err != cache.ErrCacheMiss {
				// Log cache errors but don't fail the request
				logger.Warn("cache get error", slog.Any("error", err.Error()))
			}

			// Cache miss - capture response
			rw := newResponseWriter(w)
			next.ServeHTTP(rw, r)

			// Only cache successful responses (2xx status codes)
			if rw.statusCode >= 200 && rw.statusCode < 300 {
				// Prepare cached response
				cached := cachedResponse{
					StatusCode: rw.statusCode,
					Headers:    make(map[string][]string),
					Body:       rw.body.Bytes(),
				}

				// Copy headers (excluding certain headers that shouldn't be cached)
				for key, values := range rw.Header() {
					if !shouldSkipHeader(key) {
						cached.Headers[key] = values
					}
				}

				// Marshal and store in cache
				cachedData, err := json.Marshal(cached)
				if err != nil {
					logger.Warn("failed to marshal response for caching", slog.Any("error", err.Error()))
				} else {
					// Determine TTL (can be customized per request via header)
					ttl := config.TTL
					if ttlHeader := rw.Header().Get("X-Cache-TTL"); ttlHeader != "" {
						if customTTL, err := time.ParseDuration(ttlHeader); err == nil {
							ttl = customTTL
						}
					}

					if err := config.Adapter.Set(cacheKey, cachedData, ttl); err != nil {
						logger.Warn("failed to cache response", slog.Any("error", err.Error()))
					}
				}
			}

			// Set cache status header
			rw.Header().Set(config.CacheStatusHeader, "MISS")
		})
	}
}

// InvalidateCacheMiddleware creates a middleware that invalidates cache on mutating operations
func InvalidateCacheMiddleware(cacheAdapter cache.CacheAdapter, keyPrefix string) func(http.Handler) http.Handler {
	if keyPrefix == "" {
		keyPrefix = "magic:cache:"
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// For mutating operations (POST, PUT, PATCH, DELETE), invalidate related cache entries
			if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" || r.Method == "DELETE" {
				// Generate cache key for the resource
				cacheKey := generateCacheKey(keyPrefix, r)
				
				// Attempt to delete the cache entry
				if err := cacheAdapter.Delete(cacheKey); err != nil {
					logger.Warn("failed to invalidate cache", 
						slog.String("key", cacheKey),
						slog.Any("error", err.Error()))
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// generateCacheKey creates a unique cache key based on the request
func generateCacheKey(prefix string, r *http.Request) string {
	// Include method, path, and query parameters in the key
	keyComponents := []string{
		r.Method,
		r.URL.Path,
		r.URL.RawQuery,
	}

	// Optionally include request body for non-GET requests
	if r.Method != "GET" && r.Method != "HEAD" {
		bodyBytes, err := io.ReadAll(r.Body)
		if err == nil {
			keyComponents = append(keyComponents, string(bodyBytes))
			// Restore the body for downstream handlers
			r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		}
	}

	// Create hash of key components
	hasher := sha256.New()
	hasher.Write([]byte(strings.Join(keyComponents, "|")))
	hashBytes := hasher.Sum(nil)
	
	return prefix + hex.EncodeToString(hashBytes)
}

// contains checks if a string slice contains a specific string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// shouldSkipHeader determines if a header should be excluded from caching
func shouldSkipHeader(headerKey string) bool {
	// Normalize header key to lowercase for comparison
	key := strings.ToLower(headerKey)
	
	// Skip caching certain headers
	skipHeaders := []string{
		"set-cookie",
		"authorization",
		"x-cache-status",
		"x-cache-ttl",
		"date",
		"age",
	}
	
	for _, skip := range skipHeaders {
		if key == skip {
			return true
		}
	}
	
	return false
}

// CacheControl is a helper to set cache control headers
type CacheControl struct {
	TTL time.Duration
}

// SetCacheTTL sets a custom TTL for the current response
func SetCacheTTL(w http.ResponseWriter, ttl time.Duration) {
	w.Header().Set("X-Cache-TTL", ttl.String())
	w.Header().Set("Cache-Control", "private, max-age="+strconv.Itoa(int(ttl.Seconds())))
}

// DisableCacheForRequest disables caching for the current request
func DisableCacheForRequest(r *http.Request) {
	r.Header.Set("X-Skip-Cache", "true")
}
