package storage

import (
	"archive/zip"
	"bytes"
	"context"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"learning_buddy/backend/internal/config"
)

func TestValidateUploadChecksExtensionMIMEAndSignature(t *testing.T) {
	t.Parallel()
	fileType, contentType, err := ValidateUpload("guide.md", "text/plain", []byte("# 配置\n正文"))
	require.NoError(t, err)
	assert.Equal(t, "md", fileType)
	assert.Equal(t, "text/plain", contentType)

	_, _, err = ValidateUpload("guide.pdf", "application/pdf", []byte("not a pdf"))
	assert.ErrorContains(t, err, "signature")
	_, _, err = ValidateUpload("guide.pdf", "text/plain", []byte("%PDF-1.7"))
	assert.ErrorContains(t, err, "MIME")
	_, _, err = ValidateUpload("guide.txt", "text/plain", []byte{0xff, 0x00})
	assert.ErrorContains(t, err, "UTF-8")
}

func TestPresignUsesBrowserReachablePublicEndpoint(t *testing.T) {
	t.Parallel()
	store, err := New(&config.Config{
		MinIOEndpoint:       "minio:9000",
		MinIOPublicEndpoint: "localhost:9000",
		MinIOAccessKey:      "test-access",
		MinIOSecretKey:      "test-secret",
		MinIORegion:         "us-east-1",
	})
	require.NoError(t, err)
	value, err := store.PresignSource(context.Background(), "teams/1/guide.pdf")
	require.NoError(t, err)
	parsed, err := url.Parse(value)
	require.NoError(t, err)
	assert.Equal(t, "localhost:9000", parsed.Host)
}

func TestValidateUploadRequiresRealDOCXPackage(t *testing.T) {
	t.Parallel()
	_, _, err := ValidateUpload("fake.docx", "application/octet-stream", []byte("PKfake"))
	assert.ErrorContains(t, err, "DOCX")

	var payload bytes.Buffer
	writer := zip.NewWriter(&payload)
	for _, name := range []string{"[Content_Types].xml", "word/document.xml"} {
		file, createErr := writer.Create(name)
		require.NoError(t, createErr)
		_, writeErr := file.Write([]byte("<xml/>"))
		require.NoError(t, writeErr)
	}
	require.NoError(t, writer.Close())
	fileType, _, err := ValidateUpload("real.docx", "application/octet-stream", payload.Bytes())
	require.NoError(t, err)
	assert.Equal(t, "docx", fileType)
}
