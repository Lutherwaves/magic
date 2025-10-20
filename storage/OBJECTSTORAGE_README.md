# Object Storage Adapter

This package provides adapters for interacting with various object storage systems, including S3-compatible endpoints and Google Cloud Storage (GCS).

## Features

- **S3 Adapter**: Supports AWS S3 and S3-compatible endpoints (MinIO, Wasabi, etc.)
- **GCS Adapter**: Supports Google Cloud Storage
- **Consistent Interface**: All adapters implement the same `ObjectStorageAdapter` interface
- **Factory Pattern**: Easy instantiation through `ObjectStorageAdapterFactory`
- **Singleton Pattern**: Follows the same singleton pattern as existing storage adapters

## Object Storage Adapter Interface

```go
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
```

## Usage Examples

### S3 with AWS

```go
import "github.com/tink3rlabs/magic/storage"

config := map[string]string{
    "bucket":     "my-bucket",
    "region":     "us-east-1",
    "access_key": "your-access-key",
    "secret_key": "your-secret-key",
}

adapter, err := storage.ObjectStorageAdapterFactory{}.GetInstance(storage.S3, config)
if err != nil {
    log.Fatal(err)
}

// Upload a file
data := bytes.NewReader([]byte("Hello, World!"))
err = adapter.Put("path/to/file.txt", data, "text/plain")

// Download a file
reader, err := adapter.Get("path/to/file.txt")
if err != nil {
    log.Fatal(err)
}
defer reader.Close()

// List objects
keys, nextCursor, err := adapter.List("path/", 100, "")

// Check if object exists
exists, err := adapter.Exists("path/to/file.txt")

// Delete an object
err = adapter.Delete("path/to/file.txt")
```

### S3-Compatible Endpoints (MinIO, Wasabi, etc.)

```go
config := map[string]string{
    "bucket":     "my-minio-bucket",
    "region":     "us-east-1",
    "access_key": "minioadmin",
    "secret_key": "minioadmin",
    "endpoint":   "http://localhost:9000", // Custom endpoint
}

adapter, err := storage.ObjectStorageAdapterFactory{}.GetInstance(storage.S3, config)
```

### Google Cloud Storage (GCS)

```go
config := map[string]string{
    "bucket":           "my-gcs-bucket",
    "credentials_file": "/path/to/service-account.json", // Optional
}

adapter, err := storage.ObjectStorageAdapterFactory{}.GetInstance(storage.GCS, config)
```

## Configuration Options

### S3 Adapter

| Key | Required | Description |
|-----|----------|-------------|
| `bucket` | Yes | S3 bucket name |
| `region` | No | AWS region (default: us-east-1) |
| `access_key` | No | AWS access key (uses AWS credentials chain if not provided) |
| `secret_key` | No | AWS secret key (uses AWS credentials chain if not provided) |
| `endpoint` | No | Custom S3-compatible endpoint URL |

### GCS Adapter

| Key | Required | Description |
|-----|----------|-------------|
| `bucket` | Yes | GCS bucket name |
| `credentials_file` | No | Path to service account JSON file (uses Application Default Credentials if not provided) |

## Adapter Types and Providers

### Adapter Types
- `storage.S3` - S3 and S3-compatible storage
- `storage.GCS` - Google Cloud Storage

### Providers
- `storage.AWS` - Amazon Web Services S3
- `storage.MINIO` - MinIO or other S3-compatible endpoints
- `storage.GOOGLE` - Google Cloud Storage

## Error Handling

The package defines a standard error for object not found scenarios:

```go
storage.ErrObjectNotFound
```

This error is returned by `Get()` and checked by `Exists()` when an object doesn't exist.

## Testing

The package includes comprehensive tests:

```bash
go test ./storage -v
```

Note: Some tests require actual cloud credentials and are skipped by default.

## Architecture

The implementation follows the same conventions as the existing `StorageAdapter`:

1. **Singleton Pattern**: Each adapter uses a singleton pattern with mutex locks
2. **Factory Pattern**: Adapters are instantiated through `ObjectStorageAdapterFactory`
3. **Interface-based Design**: All adapters implement `ObjectStorageAdapter` interface
4. **Configuration via Map**: Adapters are configured using `map[string]string`
5. **Consistent Error Handling**: Standard errors for common scenarios

## Files

- `objectstorage.go` - Interface definition and factory
- `s3.go` - S3 adapter implementation
- `gcs.go` - GCS adapter implementation  
- `objectstorage_test.go` - Unit tests
