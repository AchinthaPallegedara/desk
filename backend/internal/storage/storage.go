// Package storage stores binary objects (PDFs, logos, fonts): local disk in
// development, Cloudflare R2 (S3-compatible) in production.
package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/claviq/desk/backend/internal/config"
)

var ErrNotFound = errors.New("storage: object not found")

type Storage interface {
	Put(ctx context.Context, key string, data []byte, contentType string) error
	Get(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
}

func New(cfg config.Config) (Storage, error) {
	switch cfg.Storage.Driver {
	case "local", "":
		return NewLocal(filepath.Join(cfg.DataDir, "storage"))
	case "r2":
		return NewR2(cfg.Storage), nil
	default:
		return nil, fmt.Errorf("unknown STORAGE_DRIVER %q", cfg.Storage.Driver)
	}
}

func validKey(key string) error {
	if key == "" || strings.HasPrefix(key, "/") || strings.Contains(key, "..") {
		return fmt.Errorf("storage: invalid key %q", key)
	}
	return nil
}

// ---------- local ----------

type Local struct{ root string }

func NewLocal(root string) (*Local, error) {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, err
	}
	return &Local{root: root}, nil
}

func (l *Local) path(key string) string { return filepath.Join(l.root, filepath.FromSlash(key)) }

func (l *Local) Put(_ context.Context, key string, data []byte, _ string) error {
	if err := validKey(key); err != nil {
		return err
	}
	p := l.path(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func (l *Local) Get(_ context.Context, key string) ([]byte, error) {
	if err := validKey(key); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(l.path(key))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return b, err
}

func (l *Local) Delete(_ context.Context, key string) error {
	if err := validKey(key); err != nil {
		return err
	}
	err := os.Remove(l.path(key))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// ---------- Cloudflare R2 ----------

type R2 struct {
	client *s3.Client
	bucket string
}

func NewR2(c config.StorageConfig) *R2 {
	client := s3.New(s3.Options{
		Region:       "auto",
		BaseEndpoint: aws.String(fmt.Sprintf("https://%s.r2.cloudflarestorage.com", c.R2AccountID)),
		Credentials:  credentials.NewStaticCredentialsProvider(c.R2AccessKeyID, c.R2SecretAccessKey, ""),
		UsePathStyle: true,
	})
	return &R2{client: client, bucket: c.R2Bucket}
}

func (r *R2) Put(ctx context.Context, key string, data []byte, contentType string) error {
	if err := validKey(key); err != nil {
		return err
	}
	_, err := r.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(r.bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(data),
		ContentLength: aws.Int64(int64(len(data))),
		ContentType:   aws.String(contentType),
	})
	return err
}

func (r *R2) Get(ctx context.Context, key string) ([]byte, error) {
	if err := validKey(key); err != nil {
		return nil, err
	}
	out, err := r.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(r.bucket), Key: aws.String(key)})
	if err != nil {
		var nsk *s3types.NoSuchKey
		if errors.As(err, &nsk) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

func (r *R2) Delete(ctx context.Context, key string) error {
	if err := validKey(key); err != nil {
		return err
	}
	_, err := r.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(r.bucket), Key: aws.String(key)})
	return err
}
