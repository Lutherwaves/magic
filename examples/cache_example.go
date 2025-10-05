package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"github.com/tink3rlabs/magic/cache"
	magicMiddleware "github.com/tink3rlabs/magic/middlewares"
)

// Example demonstrates how to use the cache adapter with HTTP middleware

func setupCacheExample() {
	// Initialize Redis cache adapter
	cacheConfig := map[string]string{
		"addr":     "localhost:6379",
		"password": "", // set if Redis requires authentication
		"db":       "0",
	}

	cacheAdapter, err := cache.CacheAdapterFactory{}.GetInstance(cache.REDIS, cacheConfig)
	if err != nil {
		log.Fatalf("Failed to initialize cache: %v", err)
	}

	// Test cache connection
	if err := cacheAdapter.Ping(); err != nil {
		log.Fatalf("Failed to connect to cache: %v", err)
	}
	fmt.Println("Successfully connected to Redis cache")

	// Setup HTTP router with cache middleware
	r := chi.NewRouter()

	// Add standard middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Add cache middleware with configuration
	r.Use(magicMiddleware.CacheMiddleware(magicMiddleware.CacheConfig{
		Adapter:           cacheAdapter,
		TTL:               5 * time.Minute,
		Methods:           []string{"GET"},
		KeyPrefix:         "myapp:cache:",
		SkipCacheHeader:   "X-Skip-Cache",
		CacheStatusHeader: "X-Cache-Status",
	}))

	// Add cache invalidation middleware for mutating operations
	r.Use(magicMiddleware.InvalidateCacheMiddleware(cacheAdapter, "myapp:cache:"))

	// Define routes
	r.Get("/api/items", getItems)
	r.Get("/api/items/{id}", getItem)
	r.Post("/api/items", createItem)
	r.Put("/api/items/{id}", updateItem)
	r.Delete("/api/items/{id}", deleteItem)

	// Route with custom cache TTL
	r.Get("/api/expensive", expensiveOperation)

	// Route that bypasses cache
	r.Get("/api/no-cache", noCacheRoute)

	// Start server
	fmt.Println("Starting server on :8080")
	http.ListenAndServe(":8080", r)
}

// Handler examples

func getItems(w http.ResponseWriter, r *http.Request) {
	// This response will be cached for 5 minutes (default TTL)
	items := []map[string]interface{}{
		{"id": "1", "name": "Item 1"},
		{"id": "2", "name": "Item 2"},
		{"id": "3", "name": "Item 3"},
	}
	render.JSON(w, r, items)
}

func getItem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	
	// This response will be cached
	item := map[string]interface{}{
		"id":   id,
		"name": fmt.Sprintf("Item %s", id),
	}
	render.JSON(w, r, item)
}

func createItem(w http.ResponseWriter, r *http.Request) {
	// POST requests are not cached
	// The cache invalidation middleware will clear related cache entries
	item := map[string]interface{}{
		"id":      "new",
		"name":    "New Item",
		"created": time.Now(),
	}
	render.Status(r, http.StatusCreated)
	render.JSON(w, r, item)
}

func updateItem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	
	// PUT requests are not cached
	// The cache invalidation middleware will clear the cache for this item
	item := map[string]interface{}{
		"id":      id,
		"name":    "Updated Item",
		"updated": time.Now(),
	}
	render.JSON(w, r, item)
}

func deleteItem(w http.ResponseWriter, r *http.Request) {
	// DELETE requests are not cached
	// The cache invalidation middleware will clear the cache for this item
	w.WriteHeader(http.StatusNoContent)
}

func expensiveOperation(w http.ResponseWriter, r *http.Request) {
	// Custom cache TTL for this specific route
	magicMiddleware.SetCacheTTL(w, 15*time.Minute)
	
	// Simulate expensive operation
	time.Sleep(2 * time.Second)
	
	result := map[string]interface{}{
		"data":       "expensive result",
		"computed":   time.Now(),
		"cached_for": "15 minutes",
	}
	render.JSON(w, r, result)
}

func noCacheRoute(w http.ResponseWriter, r *http.Request) {
	// This route explicitly bypasses cache
	magicMiddleware.DisableCacheForRequest(r)
	
	result := map[string]interface{}{
		"timestamp": time.Now(),
		"message":   "This response is never cached",
	}
	render.JSON(w, r, result)
}

// Direct cache adapter usage example

func directCacheUsage() {
	// Initialize cache adapter
	cacheConfig := map[string]string{
		"addr":     "localhost:6379",
		"password": "",
		"db":       "0",
	}

	cacheAdapter, err := cache.CacheAdapterFactory{}.GetInstance(cache.REDIS, cacheConfig)
	if err != nil {
		log.Fatalf("Failed to initialize cache: %v", err)
	}

	// Test connection
	if err := cacheAdapter.Ping(); err != nil {
		log.Fatalf("Failed to ping cache: %v", err)
	}

	// Set a value
	key := "myapp:user:123"
	value := []byte(`{"id": "123", "name": "John Doe"}`)
	ttl := 10 * time.Minute

	if err := cacheAdapter.Set(key, value, ttl); err != nil {
		log.Printf("Failed to set cache: %v", err)
	}
	fmt.Println("Value stored in cache")

	// Check if key exists
	exists, err := cacheAdapter.Exists(key)
	if err != nil {
		log.Printf("Failed to check existence: %v", err)
	}
	fmt.Printf("Key exists: %v\n", exists)

	// Get the value
	cachedValue, err := cacheAdapter.Get(key)
	if err == cache.ErrCacheMiss {
		fmt.Println("Cache miss")
	} else if err != nil {
		log.Printf("Failed to get from cache: %v", err)
	} else {
		fmt.Printf("Retrieved from cache: %s\n", string(cachedValue))
	}

	// Delete the value
	if err := cacheAdapter.Delete(key); err != nil {
		log.Printf("Failed to delete from cache: %v", err)
	}
	fmt.Println("Value deleted from cache")

	// Verify deletion
	_, err = cacheAdapter.Get(key)
	if err == cache.ErrCacheMiss {
		fmt.Println("Confirmed: value no longer in cache")
	}

	// Close connection when done
	defer cacheAdapter.Close()
}
