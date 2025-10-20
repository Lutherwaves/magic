package storage

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	"github.com/tink3rlabs/magic/logger"
)

type GCSAdapter struct {
	Client *storage.Client
	config map[string]string
	bucket string
}

var gcsAdapterLock = &sync.Mutex{}
var gcsAdapterInstance *GCSAdapter

func GetGCSAdapterInstance(config map[string]string) *GCSAdapter {
	if gcsAdapterInstance == nil {
		gcsAdapterLock.Lock()
		defer gcsAdapterLock.Unlock()
		if gcsAdapterInstance == nil {
			gcsAdapterInstance = &GCSAdapter{config: config}
			gcsAdapterInstance.OpenConnection()
		}
	}
	return gcsAdapterInstance
}

func (g *GCSAdapter) OpenConnection() {
	g.bucket = g.config["bucket"]
	if g.bucket == "" {
		logger.Fatal("bucket name is required for GCS adapter")
	}

	ctx := context.Background()
	var client *storage.Client
	var err error

	credentialsFile := g.config["credentials_file"]
	if credentialsFile != "" {
		client, err = storage.NewClient(ctx, option.WithCredentialsFile(credentialsFile))
	} else {
		client, err = storage.NewClient(ctx)
	}

	if err != nil {
		logger.Fatal("failed to create GCS client", slog.Any("error", err.Error()))
	}

	g.Client = client
}

func (g *GCSAdapter) Put(key string, data io.Reader, contentType string) error {
	ctx := context.Background()

	if contentType == "" {
		contentType = "application/octet-stream"
	}

	obj := g.Client.Bucket(g.bucket).Object(key)
	writer := obj.NewWriter(ctx)
	writer.ContentType = contentType

	if _, err := io.Copy(writer, data); err != nil {
		writer.Close()
		return fmt.Errorf("failed to write object %s: %v", key, err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close writer for object %s: %v", key, err)
	}

	return nil
}

func (g *GCSAdapter) Get(key string) (io.ReadCloser, error) {
	ctx := context.Background()

	obj := g.Client.Bucket(g.bucket).Object(key)
	reader, err := obj.NewReader(ctx)
	if err != nil {
		if err == storage.ErrObjectNotExist {
			return nil, ErrObjectNotFound
		}
		return nil, fmt.Errorf("failed to get object %s: %v", key, err)
	}

	return reader, nil
}

func (g *GCSAdapter) Delete(key string) error {
	ctx := context.Background()

	obj := g.Client.Bucket(g.bucket).Object(key)
	if err := obj.Delete(ctx); err != nil {
		return fmt.Errorf("failed to delete object %s: %v", key, err)
	}

	return nil
}

func (g *GCSAdapter) List(prefix string, limit int, cursor string) ([]string, string, error) {
	ctx := context.Background()

	if limit <= 0 {
		limit = 100
	}

	query := &storage.Query{
		Prefix:    prefix,
		StartOffset: cursor,
	}

	it := g.Client.Bucket(g.bucket).Objects(ctx, query)

	result := make([]string, 0, limit)
	count := 0
	lastKey := ""

	for {
		if count >= limit {
			break
		}

		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, "", fmt.Errorf("failed to iterate objects: %v", err)
		}

		result = append(result, attrs.Name)
		lastKey = attrs.Name
		count++
	}

	nextToken := ""
	if count >= limit {
		nextToken = lastKey
	}

	return result, nextToken, nil
}

func (g *GCSAdapter) Exists(key string) (bool, error) {
	ctx := context.Background()

	obj := g.Client.Bucket(g.bucket).Object(key)
	_, err := obj.Attrs(ctx)
	if err != nil {
		if err == storage.ErrObjectNotExist {
			return false, nil
		}
		return false, fmt.Errorf("failed to check if object %s exists: %v", key, err)
	}

	return true, nil
}

func (g *GCSAdapter) Ping() error {
	ctx := context.Background()

	_, err := g.Client.Bucket(g.bucket).Attrs(ctx)
	return err
}

func (g *GCSAdapter) GetType() ObjectStorageAdapterType {
	return GCS
}

func (g *GCSAdapter) GetProvider() ObjectStorageProviders {
	return GOOGLE
}

func (g *GCSAdapter) GetBucket() string {
	return g.bucket
}
