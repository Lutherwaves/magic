# Cache Adapter

The Cache Adapter provides a simple and consistent interface for caching in your Go applications, with Redis support and HTTP middleware for transparent response caching.

## Features

- **Simple Interface**: Consistent API similar to StorageAdapter
- **Redis Support**: Production-ready Redis adapter with connection pooling
- **HTTP Middleware**: Transparent caching of HTTP responses
- **Cache Invalidation**: Automatic cache invalidation on mutating operations
- **Configurable TTL**: Global and per-response TTL configuration
- **Cache Control**: Fine-grained control over what gets cached
- **Health Checks**: Built-in health check support

## Installation

The cache adapter requires the Redis Go client:

```bash
go get github.com/redis/go-redis/v9
```

## Basic Usage

### Direct Cache Operations

```go
package main

import (
    "time"
    "github.com/tink3rlabs/magic/cache"
)

func main() {
    // Configure Redis connection
    config := map[string]string{
        "addr":     "localhost:6379",
        "password": "",  // set if authentication is required
        "db":       "0", // Redis database number
    }

    // Create cache adapter instance
    cacheAdapter, err := cache.CacheAdapterFactory{}.GetInstance(cache.REDIS, config)
    if err != nil {
        panic(err)
    }
    defer cacheAdapter.Close()

    // Test connection
    if err := cacheAdapter.Ping(); err != nil {
        panic(err)
    }

    // Set a value with TTL
    key := "user:123"
    value := []byte(`{"id": "123", "name": "John Doe"}`)
    ttl := 10 * time.Minute
    
    if err := cacheAdapter.Set(key, value, ttl); err != nil {
        panic(err)
    }

    // Get a value
    cached, err := cacheAdapter.Get(key)
    if err == cache.ErrCacheMiss {
        // Handle cache miss
    } else if err != nil {
        panic(err)
    }

    // Check if key exists
    exists, err := cacheAdapter.Exists(key)
    
    // Delete a key
    if err := cacheAdapter.Delete(key); err != nil {
        panic(err)
    }
}
```

## HTTP Middleware

### Basic Middleware Setup

```go
package main

import (
    "time"
    "net/http"
    "github.com/go-chi/chi/v5"
    "github.com/tink3rlabs/magic/cache"
    "github.com/tink3rlabs/magic/middlewares"
)

func main() {
    // Initialize cache adapter
    config := map[string]string{
        "addr": "localhost:6379",
    }
    
    cacheAdapter, err := cache.CacheAdapterFactory{}.GetInstance(cache.REDIS, config)
    if err != nil {
        panic(err)
    }

    // Setup router
    r := chi.NewRouter()

    // Add cache middleware
    r.Use(middlewares.CacheMiddleware(middlewares.CacheConfig{
        Adapter:           cacheAdapter,
        TTL:               5 * time.Minute,
        Methods:           []string{"GET"},       // Only cache GET requests
        KeyPrefix:         "myapp:cache:",        // Prefix for all cache keys
        SkipCacheHeader:   "X-Skip-Cache",        // Header to skip caching
        CacheStatusHeader: "X-Cache-Status",      // Response header showing HIT/MISS
    }))

    // Add cache invalidation for mutating operations
    r.Use(middlewares.InvalidateCacheMiddleware(cacheAdapter, "myapp:cache:"))

    // Define routes
    r.Get("/api/users", getUsers)
    r.Post("/api/users", createUser)  // Automatically invalidates cache

    http.ListenAndServe(":8080", r)
}
```

### Advanced Usage

#### Custom TTL per Response

```go
func expensiveHandler(w http.ResponseWriter, r *http.Request) {
    // Set custom TTL for this response
    middlewares.SetCacheTTL(w, 15*time.Minute)
    
    // ... expensive operation ...
    
    render.JSON(w, r, result)
}
```

#### Bypassing Cache

```go
func alwaysFreshHandler(w http.ResponseWriter, r *http.Request) {
    // This response will never be cached
    middlewares.DisableCacheForRequest(r)
    
    render.JSON(w, r, result)
}
```

#### Conditional Caching

Clients can bypass cache by setting the skip header:

```bash
curl -H "X-Skip-Cache: true" http://localhost:8080/api/users
```

#### Cache Status

The middleware adds a `X-Cache-Status` header to all responses:
- `HIT`: Response served from cache
- `MISS`: Response generated and cached

```bash
curl -v http://localhost:8080/api/users
# < X-Cache-Status: MISS

curl -v http://localhost:8080/api/users
# < X-Cache-Status: HIT
```

## Configuration

### Redis Configuration Options

| Option | Description | Default |
|--------|-------------|---------|
| `addr` | Redis server address | `localhost:6379` |
| `password` | Redis password | `""` |
| `db` | Redis database number | `0` |

### Cache Middleware Configuration

| Option | Description | Default |
|--------|-------------|---------|
| `Adapter` | CacheAdapter instance | Required |
| `TTL` | Default cache TTL | `5m` |
| `Methods` | HTTP methods to cache | `["GET"]` |
| `KeyPrefix` | Prefix for cache keys | `magic:cache:` |
| `SkipCacheHeader` | Header to bypass cache | `X-Skip-Cache` |
| `CacheStatusHeader` | Response header for cache status | `X-Cache-Status` |

## Best Practices

### 1. Use Appropriate TTLs

```go
// Short TTL for frequently changing data
middlewares.SetCacheTTL(w, 1*time.Minute)

// Longer TTL for stable data
middlewares.SetCacheTTL(w, 1*time.Hour)
```

### 2. Cache Key Design

Cache keys are automatically generated based on:
- HTTP method
- Request path
- Query parameters
- Request body (for non-GET requests)

Keys are hashed to ensure consistent length and prevent collisions.

### 3. Cache Invalidation

The `InvalidateCacheMiddleware` automatically clears cache entries for POST, PUT, PATCH, and DELETE requests:

```go
// Automatically invalidates cache for this resource
r.Put("/api/users/{id}", updateUser)
r.Delete("/api/users/{id}", deleteUser)
```

### 4. Selective Caching

Only successful responses (2xx status codes) are cached. Errors and redirects are never cached.

### 5. Sensitive Data

Certain headers are automatically excluded from caching:
- `Set-Cookie`
- `Authorization`
- `X-Cache-Status`
- `X-Cache-TTL`
- `Date`
- `Age`

### 6. Health Checks

Integrate cache health checks with your health endpoint:

```go
func healthCheck(w http.ResponseWriter, r *http.Request) {
    if err := cacheAdapter.Ping(); err != nil {
        w.WriteHeader(http.StatusServiceUnavailable)
        return
    }
    w.WriteHeader(http.StatusOK)
}
```

### 7. Connection Management

The Redis adapter uses a singleton pattern with connection pooling. Configure pool settings in production:

```go
// In cache/redis.go, adjust these values:
DialTimeout:  5 * time.Second,
ReadTimeout:  3 * time.Second,
WriteTimeout: 3 * time.Second,
PoolSize:     10,
MinIdleConns: 5,
```

## Production Considerations

### High Availability

For production environments, consider using Redis Sentinel or Redis Cluster:

```go
// Redis Sentinel example (requires additional configuration)
config := map[string]string{
    "addr":           "sentinel1:26379,sentinel2:26379,sentinel3:26379",
    "master_name":    "mymaster",
    "sentinel_password": "sentinel-password",
}
```

### Monitoring

Monitor cache hit rates and performance:

```go
// Custom middleware to track metrics
func CacheMetricsMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        next.ServeHTTP(w, r)
        
        status := w.Header().Get("X-Cache-Status")
        // Track metrics: status (HIT/MISS), latency, etc.
    })
}
```

### Cache Warming

Pre-populate cache for critical data:

```go
func warmCache(cacheAdapter cache.CacheAdapter) {
    criticalData := fetchCriticalData()
    for key, value := range criticalData {
        cacheAdapter.Set(key, value, 1*time.Hour)
    }
}
```

### Error Handling

The middleware gracefully handles cache failures and continues serving requests:

```go
// Cache failures are logged but don't break the request
// The response will be generated normally if cache is unavailable
```

## Testing

### Unit Tests

```go
func TestCacheAdapter(t *testing.T) {
    config := map[string]string{"addr": "localhost:6379"}
    adapter, err := cache.CacheAdapterFactory{}.GetInstance(cache.REDIS, config)
    if err != nil {
        t.Fatal(err)
    }
    
    // Test Set
    err = adapter.Set("test:key", []byte("value"), time.Minute)
    assert.NoError(t, err)
    
    // Test Get
    val, err := adapter.Get("test:key")
    assert.NoError(t, err)
    assert.Equal(t, "value", string(val))
    
    // Test Delete
    err = adapter.Delete("test:key")
    assert.NoError(t, err)
    
    // Verify deletion
    _, err = adapter.Get("test:key")
    assert.Equal(t, cache.ErrCacheMiss, err)
}
```

### Integration Tests

```go
func TestCacheMiddleware(t *testing.T) {
    // Setup cache and router
    adapter, _ := cache.CacheAdapterFactory{}.GetInstance(cache.REDIS, config)
    r := chi.NewRouter()
    r.Use(middlewares.CacheMiddleware(middlewares.CacheConfig{
        Adapter: adapter,
        TTL:     time.Minute,
    }))
    r.Get("/test", testHandler)
    
    // First request - cache miss
    req1 := httptest.NewRequest("GET", "/test", nil)
    w1 := httptest.NewRecorder()
    r.ServeHTTP(w1, req1)
    assert.Equal(t, "MISS", w1.Header().Get("X-Cache-Status"))
    
    // Second request - cache hit
    req2 := httptest.NewRequest("GET", "/test", nil)
    w2 := httptest.NewRecorder()
    r.ServeHTTP(w2, req2)
    assert.Equal(t, "HIT", w2.Header().Get("X-Cache-Status"))
}
```

## Migration from Other Caching Solutions

If you're migrating from another caching solution:

1. Update import statements
2. Replace cache initialization code
3. Update middleware configuration
4. Test thoroughly in staging

The CacheAdapter interface makes it easy to swap implementations without changing business logic.

## Troubleshooting

### Connection Issues

```
Failed to connect to cache: dial tcp [::1]:6379: connect: connection refused
```

**Solution**: Verify Redis is running and accessible:
```bash
redis-cli ping
# Should return: PONG
```

### Cache Misses

If you're experiencing unexpected cache misses:
- Verify TTL is sufficient
- Check cache key generation (path, query params)
- Monitor Redis memory usage
- Check eviction policies

### Performance Issues

- Increase Redis connection pool size
- Use Redis cluster for high traffic
- Implement cache warming
- Monitor slow queries

## See Also

- [Storage Adapter Documentation](./storage/)
- [Middleware Documentation](./middlewares/)
- [Redis Documentation](https://redis.io/documentation)
- [go-redis Documentation](https://redis.uptrace.dev/)
