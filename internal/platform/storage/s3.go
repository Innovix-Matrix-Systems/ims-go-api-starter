package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/config"
)

const s3PresignTTL = 15 * time.Minute

// s3Disk stores files in an S3-compatible bucket. URLs use S3_PUBLIC_URL when
// set, otherwise short-lived presigned GETs.
type s3Disk struct {
	client    *s3.Client
	presign   *s3.PresignClient
	bucket    string
	publicURL string
}

func newS3Disk(cfg config.S3) (*s3Disk, error) {
	awsConfig := aws.Config{
		Region:      cfg.Region,
		Credentials: credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
	}
	client := s3.NewFromConfig(awsConfig, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		o.UsePathStyle = cfg.PathStyle
	})
	return &s3Disk{
		client:    client,
		presign:   s3.NewPresignClient(client),
		bucket:    cfg.Bucket,
		publicURL: cfg.PublicURL,
	}, nil
}

func (d *s3Disk) Put(ctx context.Context, path string, contents io.Reader, size int64) error {
	input := &s3.PutObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(path),
		Body:   contents,
	}
	if size > 0 {
		input.ContentLength = aws.Int64(size)
	}
	// S3 otherwise serves everything as binary/octet-stream (M52).
	if contentType := mime.TypeByExtension(filepath.Ext(path)); contentType != "" {
		input.ContentType = aws.String(contentType)
	}
	if _, err := d.client.PutObject(ctx, input); err != nil {
		return fmt.Errorf("storage: s3 put %q: %w", path, err)
	}
	return nil
}

func (d *s3Disk) Get(ctx context.Context, path string) (io.ReadCloser, error) {
	out, err := d.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(path),
	})
	if err != nil {
		return nil, fmt.Errorf("storage: s3 get %q: %w", path, err)
	}
	return out.Body, nil
}

func (d *s3Disk) Delete(ctx context.Context, path string) error {
	if _, err := d.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(path),
	}); err != nil {
		return fmt.Errorf("storage: s3 delete %q: %w", path, err)
	}
	return nil
}

func (d *s3Disk) Exists(ctx context.Context, path string) (bool, error) {
	if _, err := d.head(ctx, path); err != nil {
		if isS3NotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (d *s3Disk) Size(ctx context.Context, path string) (int64, error) {
	out, err := d.head(ctx, path)
	if err != nil {
		return 0, err
	}
	if out.ContentLength == nil {
		return 0, nil
	}
	return *out.ContentLength, nil
}

func (d *s3Disk) URL(ctx context.Context, path string) (string, error) {
	if d.publicURL != "" {
		return d.publicURL + "/" + path, nil
	}
	out, err := d.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(path),
	}, func(o *s3.PresignOptions) {
		o.Expires = s3PresignTTL
	})
	if err != nil {
		return "", fmt.Errorf("storage: s3 presign %q: %w", path, err)
	}
	return out.URL, nil
}

func (d *s3Disk) head(ctx context.Context, path string) (*s3.HeadObjectOutput, error) {
	out, err := d.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(path),
	})
	if err != nil {
		return nil, fmt.Errorf("storage: s3 head %q: %w", path, err)
	}
	return out, nil
}

func isS3NotFound(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NotFound", "NoSuchKey":
			return true
		}
	}
	return false
}
