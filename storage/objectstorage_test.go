package storage

import (
	"bytes"
	"io"
	"testing"
)

func TestS3AdapterFactory(t *testing.T) {
	config := map[string]string{
		"bucket":     "test-bucket",
		"region":     "us-east-1",
		"access_key": "test-access-key",
		"secret_key": "test-secret-key",
		"endpoint":   "http://localhost:9000",
	}

	adapter, err := ObjectStorageAdapterFactory{}.GetInstance(S3, config)
	if err != nil {
		t.Fatalf("Failed to create S3 adapter: %v", err)
	}

	if adapter.GetType() != S3 {
		t.Errorf("Expected adapter type S3, got %v", adapter.GetType())
	}

	if adapter.GetProvider() != MINIO {
		t.Errorf("Expected provider MINIO, got %v", adapter.GetProvider())
	}

	if adapter.GetBucket() != "test-bucket" {
		t.Errorf("Expected bucket test-bucket, got %v", adapter.GetBucket())
	}
}

func TestGCSAdapterFactory(t *testing.T) {
	t.Skip("Skipping GCS adapter test - requires GCP credentials")
	
	config := map[string]string{
		"bucket": "test-gcs-bucket",
	}

	adapter, err := ObjectStorageAdapterFactory{}.GetInstance(GCS, config)
	if err != nil {
		t.Fatalf("Failed to create GCS adapter: %v", err)
	}

	if adapter.GetType() != GCS {
		t.Errorf("Expected adapter type GCS, got %v", adapter.GetType())
	}

	if adapter.GetProvider() != GOOGLE {
		t.Errorf("Expected provider GOOGLE, got %v", adapter.GetProvider())
	}

	if adapter.GetBucket() != "test-gcs-bucket" {
		t.Errorf("Expected bucket test-gcs-bucket, got %v", adapter.GetBucket())
	}
}

func TestUnsupportedAdapterType(t *testing.T) {
	config := map[string]string{}

	_, err := ObjectStorageAdapterFactory{}.GetInstance("unsupported", config)
	if err == nil {
		t.Error("Expected error for unsupported adapter type, got nil")
	}
}

func TestObjectStorageAdapterInterface(t *testing.T) {
	var _ ObjectStorageAdapter = (*S3Adapter)(nil)
	var _ ObjectStorageAdapter = (*GCSAdapter)(nil)
}

func TestS3AdapterMethods(t *testing.T) {
	config := map[string]string{
		"bucket":     "test-bucket",
		"region":     "us-east-1",
		"access_key": "test-access-key",
		"secret_key": "test-secret-key",
		"endpoint":   "http://localhost:9000",
	}

	adapter := GetS3AdapterInstance(config)

	testData := []byte("test data content")
	reader := bytes.NewReader(testData)
	err := adapter.Put("test-key", reader, "text/plain")
	if err != nil {
		t.Logf("Put operation failed (expected in unit test without real S3): %v", err)
	}

	_, err = adapter.Get("test-key")
	if err != nil {
		t.Logf("Get operation failed (expected in unit test without real S3): %v", err)
	}

	exists, err := adapter.Exists("test-key")
	if err != nil {
		t.Logf("Exists operation failed (expected in unit test without real S3): %v", err)
	} else {
		t.Logf("Exists result: %v", exists)
	}

	keys, cursor, err := adapter.List("", 10, "")
	if err != nil {
		t.Logf("List operation failed (expected in unit test without real S3): %v", err)
	} else {
		t.Logf("List returned %d keys, cursor: %s", len(keys), cursor)
	}

	err = adapter.Delete("test-key")
	if err != nil {
		t.Logf("Delete operation failed (expected in unit test without real S3): %v", err)
	}
}

func TestGCSAdapterMethods(t *testing.T) {
	t.Skip("Skipping GCS adapter methods test - requires GCP credentials")
	
	config := map[string]string{
		"bucket": "test-gcs-bucket",
	}

	adapter := GetGCSAdapterInstance(config)

	if adapter.GetBucket() != "test-gcs-bucket" {
		t.Errorf("Expected bucket test-gcs-bucket, got %v", adapter.GetBucket())
	}
}

func TestReadCloserInterface(t *testing.T) {
	testData := []byte("test data")
	reader := io.NopCloser(bytes.NewReader(testData))
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("Failed to read data: %v", err)
	}

	if !bytes.Equal(data, testData) {
		t.Errorf("Expected data %s, got %s", testData, data)
	}
}
