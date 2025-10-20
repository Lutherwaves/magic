package storage

import (
	"errors"
	"io"
)

type ObjectStorageAdapter interface {
	Put(key string, data io.Reader, contentType string) error
	Get(key string) (io.ReadCloser, error)
	Delete(key string) error
	List(prefix string, limit int, cursor string) ([]string, string, error)
	Exists(key string) (bool, error)
	Ping() error
	GetType() ObjectStorageAdapterType
	GetProvider() ObjectStorageProviders
	GetBucket() string
}

type ObjectStorageAdapterType string
type ObjectStorageProviders string
type ObjectStorageAdapterFactory struct{}

const (
	S3  ObjectStorageAdapterType = "s3"
	GCS ObjectStorageAdapterType = "gcs"
)

const (
	AWS    ObjectStorageProviders = "aws"
	MINIO  ObjectStorageProviders = "minio"
	GOOGLE ObjectStorageProviders = "google"
)

var ErrObjectNotFound = errors.New("the requested object was not found")

func (o ObjectStorageAdapterFactory) GetInstance(adapterType ObjectStorageAdapterType, config map[string]string) (ObjectStorageAdapter, error) {
	if config == nil {
		config = make(map[string]string)
	}
	switch adapterType {
	case S3:
		return GetS3AdapterInstance(config), nil
	case GCS:
		return GetGCSAdapterInstance(config), nil
	default:
		return nil, errors.New("this object storage adapter type isn't supported")
	}
}
