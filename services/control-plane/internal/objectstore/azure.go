package objectstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
)

type azureStore struct {
	client    *azblob.Client
	container string
	prefix    string
}

func NewAzure(cfg Config) (Store, error) {
	var (
		client *azblob.Client
		err    error
	)
	if cfg.AzureConnectionString != "" {
		client, err = azblob.NewClientFromConnectionString(cfg.AzureConnectionString, nil)
	} else {
		credential, credErr := azidentity.NewDefaultAzureCredential(nil)
		if credErr != nil {
			return nil, fmt.Errorf("create Azure credential: %w", credErr)
		}
		client, err = azblob.NewClient(cfg.AzureAccountURL, credential, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("create Azure Blob client: %w", err)
	}
	return &azureStore{client: client, container: cfg.Bucket, prefix: cfg.Prefix}, nil
}

func (s *azureStore) Put(ctx context.Context, objectKey, contentType, contentEncoding string, body []byte) error {
	headers := blob.HTTPHeaders{BlobContentType: &contentType}
	if contentEncoding != "" {
		headers.BlobContentEncoding = &contentEncoding
	}
	_, err := s.client.UploadBuffer(ctx, s.container, key(s.prefix, objectKey), body, &azblob.UploadBufferOptions{
		HTTPHeaders: &headers,
	})
	if err != nil {
		return fmt.Errorf("put Azure blob: %w", err)
	}
	return nil
}

func (s *azureStore) Get(ctx context.Context, objectKey string) ([]byte, error) {
	var buf bytes.Buffer
	response, err := s.client.DownloadStream(ctx, s.container, key(s.prefix, objectKey), nil)
	if err != nil {
		var responseErr *azcore.ResponseError
		if errors.As(err, &responseErr) && responseErr.StatusCode == 404 {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get Azure blob: %w", err)
	}
	_, err = buf.ReadFrom(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read Azure blob: %w", err)
	}
	_ = response.Body.Close()
	return buf.Bytes(), nil
}

func (s *azureStore) Delete(ctx context.Context, objectKey string) error {
	_, err := s.client.DeleteBlob(ctx, s.container, key(s.prefix, objectKey), nil)
	if err != nil {
		return fmt.Errorf("delete Azure blob: %w", err)
	}
	return nil
}
