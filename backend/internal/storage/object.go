// package storage —— MinIO 原文件与派生资产访问；业务权限由调用它的 repository/service 保证。
package storage

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"learning_buddy/backend/internal/config"
)

const MaxUploadBytes int64 = 50 << 20

var allowedExtensions = map[string]string{
	".txt":  "text/plain",
	".md":   "text/markdown",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".pdf":  "application/pdf",
}

// ObjectStore 封装 source/derived 两个 bucket。
type ObjectStore struct {
	client        *minio.Client
	presignClient *minio.Client
	sourceBucket  string
	derivedBucket string
	urlTTL        time.Duration
}

func New(cfg *config.Config) (*ObjectStore, error) {
	endpoint, accessKey, secretKey := cfg.MinIOEndpoint, cfg.MinIOAccessKey, cfg.MinIOSecretKey
	if endpoint == "" {
		endpoint = "localhost:9000"
	}
	if accessKey == "" {
		accessKey = "minioadmin"
	}
	if secretKey == "" {
		secretKey = "minioadmin"
	}
	sourceBucket, derivedBucket := cfg.MinIOSourceBucket, cfg.MinIODerivedBucket
	if sourceBucket == "" {
		sourceBucket = "materials-source"
	}
	if derivedBucket == "" {
		derivedBucket = "materials-derived"
	}
	ttlSeconds := cfg.AssetURLTTLSeconds
	if ttlSeconds <= 0 {
		ttlSeconds = 600
	}
	region := cfg.MinIORegion
	if region == "" {
		region = "us-east-1"
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: cfg.MinIOSecure,
		Region: region,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio client: %w", err)
	}
	publicEndpoint := cfg.MinIOPublicEndpoint
	publicSecure := cfg.MinIOPublicSecure
	if publicEndpoint == "" {
		publicEndpoint = endpoint
		publicSecure = cfg.MinIOSecure
	}
	presignClient, err := minio.New(publicEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: publicSecure,
		Region: region,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio presign client: %w", err)
	}
	return &ObjectStore{
		client: client, presignClient: presignClient,
		sourceBucket: sourceBucket, derivedBucket: derivedBucket,
		urlTTL: time.Duration(ttlSeconds) * time.Second,
	}, nil
}

func (s *ObjectStore) EnsureBuckets(ctx context.Context) error {
	for _, bucket := range []string{s.sourceBucket, s.derivedBucket} {
		exists, err := s.client.BucketExists(ctx, bucket)
		if err != nil {
			return fmt.Errorf("check bucket %s: %w", bucket, err)
		}
		if !exists {
			if err := s.client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
				return fmt.Errorf("create bucket %s: %w", bucket, err)
			}
		}
	}
	return nil
}

func ValidateUpload(filename, contentType string, data []byte) (string, string, error) {
	extension := strings.ToLower(filepath.Ext(filename))
	expected, ok := allowedExtensions[extension]
	if !ok {
		return "", "", fmt.Errorf("unsupported file type: %s", extension)
	}
	if int64(len(data)) > MaxUploadBytes {
		return "", "", fmt.Errorf("file exceeds 50 MiB")
	}
	if len(data) == 0 {
		return "", "", fmt.Errorf("empty file")
	}
	declared := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if declared != "" && declared != "application/octet-stream" && declared != expected &&
		(extension != ".md" || declared != "text/plain") {
		return "", "", fmt.Errorf("MIME type does not match extension")
	}
	if extension == ".pdf" && !bytes.HasPrefix(data, []byte("%PDF-")) {
		return "", "", fmt.Errorf("invalid PDF signature")
	}
	if extension == ".docx" {
		if !bytes.HasPrefix(data, []byte("PK")) || !validDOCX(data) {
			return "", "", fmt.Errorf("invalid DOCX signature")
		}
	}
	if (extension == ".txt" || extension == ".md") &&
		(!utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0) {
		return "", "", fmt.Errorf("invalid UTF-8 text file")
	}
	if contentType == "" || contentType == "application/octet-stream" {
		detected := http.DetectContentType(data)
		if extension == ".txt" || extension == ".md" || detected == "application/zip" {
			contentType = expected
		} else {
			contentType = detected
		}
	}
	return strings.TrimPrefix(extension, "."), contentType, nil
}

func validDOCX(data []byte) bool {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return false
	}
	foundDocument, foundTypes := false, false
	for _, file := range reader.File {
		foundDocument = foundDocument || file.Name == "word/document.xml"
		foundTypes = foundTypes || file.Name == "[Content_Types].xml"
	}
	return foundDocument && foundTypes
}

func (s *ObjectStore) PutSource(
	ctx context.Context,
	teamID int64,
	filename string,
	contentType string,
	data []byte,
) (string, string, error) {
	fileType, contentType, err := ValidateUpload(filename, contentType, data)
	if err != nil {
		return "", "", err
	}
	if err := s.EnsureBuckets(ctx); err != nil {
		return "", "", err
	}
	key := fmt.Sprintf("teams/%d/%s%s", teamID, uuid.NewString(), filepath.Ext(filename))
	_, err = s.client.PutObject(
		ctx, s.sourceBucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: contentType},
	)
	if err != nil {
		return "", "", fmt.Errorf("put source object: %w", err)
	}
	return key, fileType, nil
}

func (s *ObjectStore) PresignSource(ctx context.Context, key string) (string, error) {
	return s.presign(ctx, s.sourceBucket, key)
}

func (s *ObjectStore) PresignDerived(ctx context.Context, key string) (string, error) {
	return s.presign(ctx, s.derivedBucket, key)
}

func (s *ObjectStore) DeleteSource(ctx context.Context, key string) error {
	if err := s.client.RemoveObject(ctx, s.sourceBucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("delete source object: %w", err)
	}
	return nil
}

func (s *ObjectStore) presign(ctx context.Context, bucket, key string) (string, error) {
	value, err := s.presignClient.PresignedGetObject(ctx, bucket, key, s.urlTTL, url.Values{})
	if err != nil {
		return "", fmt.Errorf("presign %s object: %w", bucket, err)
	}
	return value.String(), nil
}
