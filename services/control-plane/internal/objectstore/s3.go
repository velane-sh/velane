package objectstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

type s3Store struct {
	client    *s3.Client
	presigner *s3.PresignClient
	bucket    string
	prefix    string
	kmsKeyID  string
}

func NewS3(ctx context.Context, cfg Config) (Store, error) {
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.S3Region),
	}
	if cfg.S3Endpoint != "" {
		opts = append(opts, awsconfig.WithBaseEndpoint(cfg.S3Endpoint))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load AWS configuration: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = cfg.S3ForcePathStyle
	})
	return &s3Store{
		client: client, presigner: s3.NewPresignClient(client),
		bucket: cfg.Bucket,
		prefix: cfg.Prefix,
		kmsKeyID: cfg.S3KMSKeyID,
	}, nil
}

func (s *s3Store) BeginSnapshotMultipart(ctx context.Context, objectRef string, expected []SnapshotPartExpectation, expiry time.Duration) (SnapshotMultipartUpload, error) {
	if objectRef == "" || len(expected) == 0 || expiry <= 0 || expiry > 15*time.Minute {
		return SnapshotMultipartUpload{}, fmt.Errorf("invalid snapshot multipart request")
	}
	for i, part := range expected {
		if part.Number != i+1 || part.Size <= 0 || part.ChecksumSHA256 == "" {
			return SnapshotMultipartUpload{}, fmt.Errorf("invalid snapshot part expectation")
		}
	}
	key := key(s.prefix, objectRef)
	input := &s3.CreateMultipartUploadInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), ChecksumAlgorithm: types.ChecksumAlgorithmSha256,
	}
	if s.kmsKeyID != "" {
		input.ServerSideEncryption = types.ServerSideEncryptionAwsKms
		input.SSEKMSKeyId = aws.String(s.kmsKeyID)
	}
	created, err := s.client.CreateMultipartUpload(ctx, input)
	if err != nil {
		return SnapshotMultipartUpload{}, fmt.Errorf("create S3 multipart upload: %w", err)
	}
	plan := SnapshotMultipartUpload{ObjectRef: objectRef, UploadID: aws.ToString(created.UploadId), Parts: make([]SnapshotPartURL, 0, len(expected))}
	for _, part := range expected {
		presigned, err := s.presigner.PresignUploadPart(ctx, &s3.UploadPartInput{Bucket: aws.String(s.bucket), Key: aws.String(key), UploadId: created.UploadId, PartNumber: aws.Int32(int32(part.Number)), ChecksumSHA256: aws.String(part.ChecksumSHA256)}, s3.WithPresignExpires(expiry))
		if err != nil {
			_ = s.AbortSnapshotMultipart(ctx, plan)
			return SnapshotMultipartUpload{}, fmt.Errorf("presign S3 multipart part: %w", err)
		}
		plan.Parts = append(plan.Parts, SnapshotPartURL{Number: part.Number, URL: presigned.URL, ChecksumSHA256: part.ChecksumSHA256, ExpiresAt: time.Now().UTC().Add(expiry)})
	}
	return plan, nil
}

func (s *s3Store) CompleteSnapshotMultipart(ctx context.Context, upload SnapshotMultipartUpload, parts []SnapshotCompletedPart) (SnapshotObject, error) {
	if upload.ObjectRef == "" || upload.UploadID == "" || len(parts) != len(upload.Parts) {
		return SnapshotObject{}, fmt.Errorf("invalid snapshot multipart completion")
	}
	completed := make([]types.CompletedPart, 0, len(parts))
	for i, part := range parts {
		if part.Number != i+1 || part.ETag == "" || part.ChecksumSHA256 == "" || part.ChecksumSHA256 != upload.Parts[i].ChecksumSHA256 {
			return SnapshotObject{}, fmt.Errorf("multipart completion does not match the upload plan")
		}
		completed = append(completed, types.CompletedPart{PartNumber: aws.Int32(int32(part.Number)), ETag: aws.String(part.ETag), ChecksumSHA256: aws.String(part.ChecksumSHA256)})
	}
	out, err := s.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{Bucket: aws.String(s.bucket), Key: aws.String(key(s.prefix, upload.ObjectRef)), UploadId: aws.String(upload.UploadID), MultipartUpload: &types.CompletedMultipartUpload{Parts: completed}, ChecksumType: types.ChecksumTypeComposite})
	if err != nil {
		return SnapshotObject{}, fmt.Errorf("complete S3 multipart upload: %w", err)
	}
	return s.HeadSnapshotObject(ctx, upload.ObjectRef, aws.ToString(out.VersionId))
}

func (s *s3Store) AbortSnapshotMultipart(ctx context.Context, upload SnapshotMultipartUpload) error {
	if upload.ObjectRef == "" || upload.UploadID == "" {
		return nil
	}
	_, err := s.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{Bucket: aws.String(s.bucket), Key: aws.String(key(s.prefix, upload.ObjectRef)), UploadId: aws.String(upload.UploadID)})
	if err != nil {
		return fmt.Errorf("abort S3 multipart upload: %w", err)
	}
	return nil
}

func (s *s3Store) HeadSnapshotObject(ctx context.Context, objectRef, version string) (SnapshotObject, error) {
	in := &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key(s.prefix, objectRef)), ChecksumMode: types.ChecksumModeEnabled}
	if version != "" {
		in.VersionId = aws.String(version)
	}
	out, err := s.client.HeadObject(ctx, in)
	if err != nil {
		return SnapshotObject{}, fmt.Errorf("head S3 snapshot object: %w", err)
	}
	versionID := aws.ToString(out.VersionId)
	if versionID == "" {
		versionID = "null"
	}
	return SnapshotObject{ObjectRef: objectRef, Version: versionID, Size: aws.ToInt64(out.ContentLength), ETag: strings.Trim(aws.ToString(out.ETag), `"`), ChecksumSHA256: strings.TrimSuffix(aws.ToString(out.ChecksumSHA256), "-1")}, nil
}

func (s *s3Store) Put(ctx context.Context, objectKey, contentType, contentEncoding string, body []byte) error {
	input := &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key(s.prefix, objectKey)),
		Body:        bytes.NewReader(body),
		ContentType: aws.String(contentType),
	}
	if contentEncoding != "" {
		input.ContentEncoding = aws.String(contentEncoding)
	}
	_, err := s.client.PutObject(ctx, input)
	if err != nil {
		return fmt.Errorf("put S3 object: %w", err)
	}
	return nil
}

func (s *s3Store) Get(ctx context.Context, objectKey string) ([]byte, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key(s.prefix, objectKey)),
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NoSuchKey" || apiErr.ErrorCode() == "NotFound") {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get S3 object: %w", err)
	}
	return readAll(out.Body)
}

func (s *s3Store) Delete(ctx context.Context, objectKey string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key(s.prefix, objectKey)),
	})
	if err != nil {
		return fmt.Errorf("delete S3 object: %w", err)
	}
	return nil
}
