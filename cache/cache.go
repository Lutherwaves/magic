package cache

import (
	"errors"
	"time"
)

var ErrCacheMiss = errors.New("cache miss: the requested key was not found in cache")

// CacheAdapter defines the interface for cache operations
type CacheAdapter interface {
	// Get retrieves a value from the cache by key
	Get(key string) ([]byte, error)
	
	// Set stores a value in the cache with the specified TTL
	Set(key string, value []byte, ttl time.Duration) error
	
	// Delete removes a key from the cache
	Delete(key string) error
	
	// Exists checks if a key exists in the cache
	Exists(key string) (bool, error)
	
	// Ping checks the health of the cache connection
	Ping() error
	
	// GetType returns the type of cache adapter
	GetType() CacheAdapterType
	
	// Close closes the cache connection
	Close() error
}

type CacheAdapterType string
type CacheAdapterFactory struct{}

const (
	REDIS CacheAdapterType = "redis"
)

// GetInstance creates and returns a cache adapter instance based on the specified type
func (c CacheAdapterFactory) GetInstance(adapterType CacheAdapterType, config map[string]string) (CacheAdapter, error) {
	if config == nil {
		config = make(map[string]string)
	}
	switch adapterType {
	case REDIS:
		return GetRedisAdapterInstance(config), nil
	default:
		return nil, errors.New("this cache adapter type isn't supported")
	}
}
