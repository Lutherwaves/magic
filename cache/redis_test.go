package cache

import (
	"testing"
	"time"
)

// Note: These tests gracefully skip if Redis is not available
// To run tests with Redis: docker run -d -p 6379:6379 redis:alpine --requirepass testpass

func TestRedisAdapter_SetAndGet(t *testing.T) {
	ResetRedisAdapterInstance() // Reset singleton for clean test state
	
	config := map[string]string{
		"addr":     "localhost:6379",
		"password": "testpass",
		"db":       "0",
	}

	adapter, err := CacheAdapterFactory{}.GetInstance(REDIS, config)
	if err != nil {
		t.Skipf("Skipping test: failed to initialize Redis adapter: %v", err)
	}
	defer adapter.Close()

	// Test Ping
	if err := adapter.Ping(); err != nil {
		t.Skipf("Skipping test: Redis not available: %v", err)
	}

	// Test Set and Get
	key := "test:key"
	value := []byte("test value")
	ttl := 1 * time.Minute

	err = adapter.Set(key, value, ttl)
	if err != nil {
		t.Fatalf("Failed to set value: %v", err)
	}

	retrieved, err := adapter.Get(key)
	if err != nil {
		t.Fatalf("Failed to get value: %v", err)
	}

	if string(retrieved) != string(value) {
		t.Errorf("Expected %s, got %s", string(value), string(retrieved))
	}

	// Cleanup
	if err := adapter.Delete(key); err != nil {
		t.Logf("Cleanup failed: %v", err)
	}
}

func TestRedisAdapter_CacheMiss(t *testing.T) {
	ResetRedisAdapterInstance() // Reset singleton for clean test state
	
	config := map[string]string{
		"addr":     "localhost:6379",
		"password": "testpass",
	}

	adapter, err := CacheAdapterFactory{}.GetInstance(REDIS, config)
	if err != nil {
		t.Skipf("Skipping test: failed to initialize Redis adapter: %v", err)
	}
	defer adapter.Close()

	if err := adapter.Ping(); err != nil {
		t.Skipf("Skipping test: Redis not available: %v", err)
	}

	// Try to get non-existent key
	_, err = adapter.Get("non:existent:key")
	if err != ErrCacheMiss {
		t.Errorf("Expected ErrCacheMiss, got %v", err)
	}
}

func TestRedisAdapter_Exists(t *testing.T) {
	ResetRedisAdapterInstance() // Reset singleton for clean test state
	
	config := map[string]string{
		"addr":     "localhost:6379",
		"password": "testpass",
	}

	adapter, err := CacheAdapterFactory{}.GetInstance(REDIS, config)
	if err != nil {
		t.Skipf("Skipping test: failed to initialize Redis adapter: %v", err)
	}
	defer adapter.Close()

	if err := adapter.Ping(); err != nil {
		t.Skipf("Skipping test: Redis not available: %v", err)
	}

	key := "test:exists"
	value := []byte("test")

	// Key should not exist initially
	exists, err := adapter.Exists(key)
	if err != nil {
		t.Fatalf("Failed to check existence: %v", err)
	}
	if exists {
		t.Error("Key should not exist initially")
	}

	// Set the key
	if err := adapter.Set(key, value, time.Minute); err != nil {
		t.Fatalf("Failed to set key: %v", err)
	}

	// Key should now exist
	exists, err = adapter.Exists(key)
	if err != nil {
		t.Fatalf("Failed to check existence: %v", err)
	}
	if !exists {
		t.Error("Key should exist after setting")
	}

	// Cleanup
	if err := adapter.Delete(key); err != nil {
		t.Logf("Cleanup failed: %v", err)
	}
}

func TestRedisAdapter_Delete(t *testing.T) {
	ResetRedisAdapterInstance() // Reset singleton for clean test state
	
	config := map[string]string{
		"addr":     "localhost:6379",
		"password": "testpass",
	}

	adapter, err := CacheAdapterFactory{}.GetInstance(REDIS, config)
	if err != nil {
		t.Skipf("Skipping test: failed to initialize Redis adapter: %v", err)
	}
	defer adapter.Close()

	if err := adapter.Ping(); err != nil {
		t.Skipf("Skipping test: Redis not available: %v", err)
	}

	key := "test:delete"
	value := []byte("test")

	// Set a value
	if err := adapter.Set(key, value, time.Minute); err != nil {
		t.Fatalf("Failed to set value: %v", err)
	}

	// Verify it exists
	retrieved, err := adapter.Get(key)
	if err != nil {
		t.Fatalf("Failed to get value: %v", err)
	}
	if string(retrieved) != string(value) {
		t.Errorf("Value mismatch")
	}

	// Delete the key
	err = adapter.Delete(key)
	if err != nil {
		t.Fatalf("Failed to delete key: %v", err)
	}

	// Verify it's gone
	_, err = adapter.Get(key)
	if err != ErrCacheMiss {
		t.Errorf("Expected ErrCacheMiss after delete, got %v", err)
	}
}

func TestRedisAdapter_TTL(t *testing.T) {
	ResetRedisAdapterInstance() // Reset singleton for clean test state
	
	config := map[string]string{
		"addr":     "localhost:6379",
		"password": "testpass",
	}

	adapter, err := CacheAdapterFactory{}.GetInstance(REDIS, config)
	if err != nil {
		t.Skipf("Skipping test: failed to initialize Redis adapter: %v", err)
	}
	defer adapter.Close()

	if err := adapter.Ping(); err != nil {
		t.Skipf("Skipping test: Redis not available: %v", err)
	}

	key := "test:ttl"
	value := []byte("test")
	ttl := 2 * time.Second

	// Set with short TTL
	if err := adapter.Set(key, value, ttl); err != nil {
		t.Fatalf("Failed to set value: %v", err)
	}

	// Verify it exists
	retrieved, err := adapter.Get(key)
	if err != nil {
		t.Fatalf("Failed to get value: %v", err)
	}
	if string(retrieved) != string(value) {
		t.Errorf("Value mismatch")
	}

	// Wait for expiration
	time.Sleep(3 * time.Second)

	// Verify it's expired
	_, err = adapter.Get(key)
	if err != ErrCacheMiss {
		t.Errorf("Expected ErrCacheMiss after TTL expiration, got %v", err)
	}
}

func TestRedisAdapter_GetType(t *testing.T) {
	ResetRedisAdapterInstance() // Reset singleton for clean test state
	
	config := map[string]string{
		"addr":     "localhost:6379",
		"password": "testpass",
	}

	adapter, err := CacheAdapterFactory{}.GetInstance(REDIS, config)
	if err != nil {
		t.Skipf("Skipping test: failed to initialize Redis adapter: %v", err)
	}
	defer adapter.Close()

	if adapter.GetType() != REDIS {
		t.Errorf("Expected type REDIS, got %v", adapter.GetType())
	}
}
