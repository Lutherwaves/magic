package cache

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/tink3rlabs/magic/logger"
)

type RedisAdapter struct {
	client *redis.Client
	config map[string]string
}

var redisAdapterLock = &sync.Mutex{}
var redisAdapterInstance *RedisAdapter

// GetRedisAdapterInstance returns a singleton instance of the Redis adapter
func GetRedisAdapterInstance(config map[string]string) *RedisAdapter {
	if redisAdapterInstance == nil {
		redisAdapterLock.Lock()
		defer redisAdapterLock.Unlock()
		if redisAdapterInstance == nil {
			redisAdapterInstance = &RedisAdapter{config: config}
			redisAdapterInstance.OpenConnection()
		}
	}
	return redisAdapterInstance
}

// OpenConnection establishes a connection to Redis
func (r *RedisAdapter) OpenConnection() {
	addr := r.config["addr"]
	if addr == "" {
		logger.Fatal("redis address is required", slog.String("error", "addr config cannot be empty"))
	}

	password := r.config["password"]
	if password == "" || password == "off" {
		logger.Fatal("redis password is required", slog.String("error", "password config cannot be empty or 'off'"))
	}
	
	db := 0
	if dbStr, ok := r.config["db"]; ok && dbStr != "" {
		var err error
		db, err = strconv.Atoi(dbStr)
		if err != nil {
			logger.Fatal("failed to parse redis db number", slog.Any("error", err.Error()))
		}
	}

	r.client = redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     10,
		MinIdleConns: 5,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := r.client.Ping(ctx).Err(); err != nil {
		logger.Fatal("failed to connect to redis", slog.Any("error", err.Error()))
	}
}

// Get retrieves a value from Redis by key
func (r *RedisAdapter) Get(key string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	val, err := r.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, ErrCacheMiss
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get key from cache: %w", err)
	}

	return val, nil
}

// Set stores a value in Redis with the specified TTL
func (r *RedisAdapter) Set(key string, value []byte, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := r.client.Set(ctx, key, value, ttl).Err(); err != nil {
		return fmt.Errorf("failed to set key in cache: %w", err)
	}

	return nil
}

// Delete removes a key from Redis
func (r *RedisAdapter) Delete(key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := r.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to delete key from cache: %w", err)
	}

	return nil
}

// Exists checks if a key exists in Redis
func (r *RedisAdapter) Exists(key string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := r.client.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check if key exists: %w", err)
	}

	return result > 0, nil
}

// Ping checks the health of the Redis connection
func (r *RedisAdapter) Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	return r.client.Ping(ctx).Err()
}

// GetType returns the cache adapter type
func (r *RedisAdapter) GetType() CacheAdapterType {
	return REDIS
}

// Close closes the Redis connection
func (r *RedisAdapter) Close() error {
	if r.client != nil {
		return r.client.Close()
	}
	return nil
}
