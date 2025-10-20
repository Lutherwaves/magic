package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"

	"github.com/tink3rlabs/magic/logger"
)

type S3Adapter struct {
	Client   *s3.Client
	config   map[string]string
	bucket   string
	provider ObjectStorageProviders
}

var s3AdapterLock = &sync.Mutex{}
var s3AdapterInstance *S3Adapter

func GetS3AdapterInstance(config map[string]string) *S3Adapter {
	if s3AdapterInstance == nil {
		s3AdapterLock.Lock()
		defer s3AdapterLock.Unlock()
		if s3AdapterInstance == nil {
			s3AdapterInstance = &S3Adapter{config: config}
			s3AdapterInstance.OpenConnection()
		}
	}
	return s3AdapterInstance
}

func (s *S3Adapter) OpenConnection() {
	s.bucket = s.config["bucket"]
	if s.bucket == "" {
		logger.Fatal("bucket name is required for S3 adapter")
	}

	region := s.config["region"]
	if region == "" {
		region = "us-east-1"
	}

	accessKey := s.config["access_key"]
	secretKey := s.config["secret_key"]

	var cfg aws.Config
	var err error

	if accessKey != "" && secretKey != "" {
		cfg, err = config.LoadDefaultConfig(context.TODO(),
			config.WithRegion(region),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
		)
	} else {
		cfg, err = config.LoadDefaultConfig(context.TODO(),
			config.WithRegion(region),
		)
	}

	if err != nil {
		logger.Fatal("failed to load AWS config", slog.Any("error", err.Error()))
	}

	endpoint := s.config["endpoint"]
	if endpoint != "" {
		s.provider = MINIO
		s.Client = s3.NewFromConfig(cfg, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true
		})
	} else {
		s.provider = AWS
		s.Client = s3.NewFromConfig(cfg)
	}
}

func (s *S3Adapter) Put(key string, data io.Reader, contentType string) error {
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	_, err := s.Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        data,
		ContentType: aws.String(contentType),
	})

	if err != nil {
		return fmt.Errorf("failed to put object %s: %v", key, err)
	}

	return nil
}

func (s *S3Adapter) Get(key string) (io.ReadCloser, error) {
	result, err := s.Client.GetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})

	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			if apiErr.ErrorCode() == "NoSuchKey" {
				return nil, ErrObjectNotFound
			}
		}
		return nil, fmt.Errorf("failed to get object %s: %v", key, err)
	}

	return result.Body, nil
}

func (s *S3Adapter) Delete(key string) error {
	_, err := s.Client.DeleteObject(context.TODO(), &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})

	if err != nil {
		return fmt.Errorf("failed to delete object %s: %v", key, err)
	}

	return nil
}

func (s *S3Adapter) List(prefix string, limit int, cursor string) ([]string, string, error) {
	if limit <= 0 {
		limit = 100
	}

	input := &s3.ListObjectsV2Input{
		Bucket:  aws.String(s.bucket),
		Prefix:  aws.String(prefix),
		MaxKeys: aws.Int32(int32(limit)),
	}

	if cursor != "" {
		input.ContinuationToken = aws.String(cursor)
	}

	result, err := s.Client.ListObjectsV2(context.TODO(), input)
	if err != nil {
		return nil, "", fmt.Errorf("failed to list objects with prefix %s: %v", prefix, err)
	}

	keys := make([]string, 0, len(result.Contents))
	for _, obj := range result.Contents {
		if obj.Key != nil {
			keys = append(keys, *obj.Key)
		}
	}

	nextToken := ""
	if result.NextContinuationToken != nil {
		nextToken = *result.NextContinuationToken
	}

	return keys, nextToken, nil
}

func (s *S3Adapter) Exists(key string) (bool, error) {
	_, err := s.Client.HeadObject(context.TODO(), &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})

	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			code := apiErr.ErrorCode()
			if code == "NotFound" || code == "NoSuchKey" || strings.Contains(err.Error(), "404") {
				return false, nil
			}
		}
		return false, fmt.Errorf("failed to check if object %s exists: %v", key, err)
	}

	return true, nil
}

func (s *S3Adapter) Ping() error {
	_, err := s.Client.HeadBucket(context.TODO(), &s3.HeadBucketInput{
		Bucket: aws.String(s.bucket),
	})
	return err
}

func (s *S3Adapter) GetType() ObjectStorageAdapterType {
	return S3
}

func (s *S3Adapter) GetProvider() ObjectStorageProviders {
	return s.provider
}

func (s *S3Adapter) GetBucket() string {
	return s.bucket
}
