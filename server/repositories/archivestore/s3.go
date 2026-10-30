package archivestore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/gsoultan/metis/internal/pkg/envvar"
)

// The S3 store's settings. Credentials are the SDK's usual ones: the
// AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY variables, a shared profile,
// or the role of the machine or pod the server runs as.
const (
	EnvS3Bucket = "METIS_ARCHIVE_S3_BUCKET"
	// EnvS3Prefix is put before every record's name, so one bucket can hold
	// several installations' archives.
	EnvS3Prefix = "METIS_ARCHIVE_S3_PREFIX"
	EnvS3Region = "METIS_ARCHIVE_S3_REGION"
	// EnvS3Endpoint is an S3-compatible service's address — MinIO, Ceph,
	// R2 — in place of AWS's.
	EnvS3Endpoint = "METIS_ARCHIVE_S3_ENDPOINT"
	// EnvS3PathStyle addresses the bucket in the path rather than the host
	// name, which most S3-compatible services need.
	EnvS3PathStyle = "METIS_ARCHIVE_S3_PATH_STYLE"
)

// s3CallTimeout bounds one call to the bucket. A record is written inside the
// transaction that archives its instance, so a bucket that stops answering
// must not hold that transaction's locks for long.
const s3CallTimeout = 30 * time.Second

// maxS3RecordBytes bounds how much of an object is read: a record is written
// by this server, so this only keeps a wrong object from being read without
// end.
const maxS3RecordBytes = 512 << 20

// s3Store keeps each record as an object.
type s3Store struct {
	client *s3.Client
	bucket string
	prefix string
}

// S3Options are the S3 store's settings.
type S3Options struct {
	Bucket    string
	Prefix    string
	Region    string
	Endpoint  string
	PathStyle bool
}

func s3FromEnv(ctx context.Context) (Store, error) {
	bucket := strings.TrimSpace(envvar.Get(EnvS3Bucket))
	if bucket == "" {
		return nil, nil
	}
	return OpenS3(ctx, S3Options{
		Bucket:    bucket,
		Prefix:    strings.Trim(strings.TrimSpace(envvar.Get(EnvS3Prefix)), "/"),
		Region:    strings.TrimSpace(envvar.Get(EnvS3Region)),
		Endpoint:  strings.TrimSpace(envvar.Get(EnvS3Endpoint)),
		PathStyle: strings.EqualFold(strings.TrimSpace(envvar.Get(EnvS3PathStyle)), "true"),
	})
}

// OpenS3 opens a bucket as a store. It does not call the bucket: a server
// that cannot reach it yet still starts, and the first archive batch says so.
func OpenS3(ctx context.Context, o S3Options) (Store, error) {
	// Checksums only where S3 requires them: several S3-compatible services
	// refuse the ones the SDK otherwise adds to every upload.
	loads := []func(*config.LoadOptions) error{
		config.WithRequestChecksumCalculation(aws.RequestChecksumCalculationWhenRequired),
		config.WithResponseChecksumValidation(aws.ResponseChecksumValidationWhenRequired),
	}
	if o.Region != "" {
		loads = append(loads, config.WithRegion(o.Region))
	}
	cfg, err := config.LoadDefaultConfig(ctx, loads...)
	if err != nil {
		return nil, fmt.Errorf("%s: could not load the S3 credentials: %w", EnvS3Bucket, err)
	}
	client := s3.NewFromConfig(cfg, func(opts *s3.Options) {
		if o.Endpoint != "" {
			opts.BaseEndpoint = aws.String(o.Endpoint)
		}
		opts.UsePathStyle = o.PathStyle
	})
	return &s3Store{client: client, bucket: o.Bucket, prefix: o.Prefix}, nil
}

func (s *s3Store) Kind() string { return KindS3 }

func (s *s3Store) Where() string {
	if s.prefix == "" {
		return "s3://" + s.bucket
	}
	return "s3://" + s.bucket + "/" + s.prefix
}

func (s *s3Store) key(name string) string {
	if s.prefix == "" {
		return name + recordSuffix
	}
	return s.prefix + "/" + name + recordSuffix
}

func (s *s3Store) Put(ctx context.Context, name string, record []byte) error {
	ctx, cancel := context.WithTimeout(ctx, s3CallTimeout)
	defer cancel()
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(s.key(name)),
		Body:          bytes.NewReader(record),
		ContentLength: aws.Int64(int64(len(record))),
	})
	if err != nil {
		return fmt.Errorf("could not write archive record %s to %s: %w", name, s.Where(), err)
	}
	return nil
}

func (s *s3Store) Get(ctx context.Context, name string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, s3CallTimeout)
	defer cancel()
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key(name)),
	})
	var missing *types.NoSuchKey
	if errors.As(err, &missing) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	if err != nil {
		return nil, fmt.Errorf("could not read archive record %s from %s: %w", name, s.Where(), err)
	}
	record, err := io.ReadAll(io.LimitReader(out.Body, maxS3RecordBytes+1))
	err = errors.Join(err, out.Body.Close())
	if err != nil {
		return nil, fmt.Errorf("could not read archive record %s from %s: %w", name, s.Where(), err)
	}
	if len(record) > maxS3RecordBytes {
		return nil, fmt.Errorf("archive record %s in %s is larger than %d MiB", name, s.Where(), maxS3RecordBytes>>20)
	}
	return record, nil
}

func (s *s3Store) Delete(ctx context.Context, name string) error {
	ctx, cancel := context.WithTimeout(ctx, s3CallTimeout)
	defer cancel()
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key(name)),
	})
	if err != nil {
		return fmt.Errorf("could not remove archive record %s from %s: %w", name, s.Where(), err)
	}
	return nil
}
