package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

const (
	defaultS3Region            = "us-east-1"
	defaultS3UploadPartSize    = int64(16 * 1024 * 1024)
	defaultS3UploadConcurrency = 4
	minimumS3UploadPartSize    = int64(5 * 1024 * 1024)
)

type S3Options struct {
	Bucket            string
	Region            string
	Endpoint          string
	Prefix            string
	AccessKeyID       string
	SecretAccessKey   string
	SessionToken      string
	UsePathStyle      bool
	UploadPartSize    int64
	UploadConcurrency int
}

type s3APIClient interface {
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	ListObjectsV2(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
}

type s3ObjectUploader interface {
	UploadObject(context.Context, *transfermanager.UploadObjectInput, ...func(*transfermanager.Options)) (*transfermanager.UploadObjectOutput, error)
}

type S3Store struct {
	bucket   string
	prefix   string
	client   s3APIClient
	uploader s3ObjectUploader
}

func NewS3Store(ctx context.Context, options S3Options) (*S3Store, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	normalized, err := normalizeS3Options(options)
	if err != nil {
		return nil, err
	}

	loadOptions := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(normalized.Region),
	}
	if normalized.AccessKeyID != "" {
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				normalized.AccessKeyID,
				normalized.SecretAccessKey,
				normalized.SessionToken,
			),
		))
	}
	awsConfig, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("load S3 configuration: %w", err)
	}
	client := s3.NewFromConfig(awsConfig, func(clientOptions *s3.Options) {
		clientOptions.UsePathStyle = normalized.UsePathStyle
		if normalized.Endpoint != "" {
			clientOptions.BaseEndpoint = aws.String(normalized.Endpoint)
		}
	})
	uploader := transfermanager.New(client, func(uploadOptions *transfermanager.Options) {
		uploadOptions.PartSizeBytes = normalized.UploadPartSize
		uploadOptions.MultipartUploadThreshold = normalized.UploadPartSize
		uploadOptions.Concurrency = normalized.UploadConcurrency
		uploadOptions.FailTimeout = 30 * time.Second
	})
	return newS3Store(normalized, client, uploader), nil
}

func newS3Store(options S3Options, client s3APIClient, uploader s3ObjectUploader) *S3Store {
	return &S3Store{
		bucket:   options.Bucket,
		prefix:   options.Prefix,
		client:   client,
		uploader: uploader,
	}
}

func normalizeS3Options(options S3Options) (S3Options, error) {
	options.Bucket = strings.TrimSpace(options.Bucket)
	if options.Bucket == "" {
		return S3Options{}, errors.New("S3 bucket is empty")
	}
	options.Region = strings.TrimSpace(options.Region)
	if options.Region == "" {
		options.Region = defaultS3Region
	}
	options.Endpoint = strings.TrimSpace(options.Endpoint)
	if options.Endpoint != "" {
		parsed, err := url.Parse(options.Endpoint)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return S3Options{}, fmt.Errorf("invalid S3 endpoint %q", options.Endpoint)
		}
	}
	options.Prefix = strings.Trim(strings.TrimSpace(options.Prefix), "/")
	if options.Prefix != "" {
		prefix, err := ParseKey(options.Prefix)
		if err != nil {
			return S3Options{}, fmt.Errorf("invalid S3 prefix: %w", err)
		}
		options.Prefix = prefix.String()
	}
	if (options.AccessKeyID == "") != (options.SecretAccessKey == "") {
		return S3Options{}, errors.New("S3 access key ID and secret access key must be configured together")
	}
	if options.SessionToken != "" && options.AccessKeyID == "" {
		return S3Options{}, errors.New("S3 session token requires static access credentials")
	}
	if options.UploadPartSize == 0 {
		options.UploadPartSize = defaultS3UploadPartSize
	}
	if options.UploadPartSize < minimumS3UploadPartSize {
		return S3Options{}, fmt.Errorf("S3 upload part size must be at least %d bytes", minimumS3UploadPartSize)
	}
	if options.UploadConcurrency == 0 {
		options.UploadConcurrency = defaultS3UploadConcurrency
	}
	if options.UploadConcurrency < 1 || options.UploadConcurrency > 64 {
		return S3Options{}, errors.New("S3 upload concurrency must be between 1 and 64")
	}
	return options, nil
}

func (s *S3Store) Open(ctx context.Context, key Key) (*Object, error) {
	info, err := s.Stat(ctx, key)
	if err != nil {
		return nil, err
	}
	remoteKey, err := s.remoteKey(key)
	if err != nil {
		return nil, err
	}
	return &Object{
		Body: &s3ReadSeekCloser{
			ctx:    ctx,
			client: s.client,
			bucket: s.bucket,
			key:    remoteKey,
			size:   info.Size,
			etag:   info.ETag,
		},
		Info: info,
	}, nil
}

func (s *S3Store) Put(ctx context.Context, key Key, src io.Reader, options PutOptions) (ObjectInfo, error) {
	if err := ctx.Err(); err != nil {
		return ObjectInfo{}, err
	}
	if src == nil {
		return ObjectInfo{}, errors.New("S3 upload source is nil")
	}
	remoteKey, err := s.remoteKey(key)
	if err != nil {
		return ObjectInfo{}, err
	}
	if options.ExpectedSize != nil {
		if *options.ExpectedSize < 0 {
			return ObjectInfo{}, errors.New("S3 expected upload size is negative")
		}
		if err := validateSeekableSize(src, *options.ExpectedSize); err != nil {
			return ObjectInfo{}, fmt.Errorf("object %s size mismatch: %w", key.String(), err)
		}
	}

	input := &transfermanager.UploadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(remoteKey),
		Body:   src,
	}
	if options.ExpectedSize != nil {
		input.ContentLength = options.ExpectedSize
		input.MpuObjectSize = options.ExpectedSize
	}
	if options.ContentType != "" {
		input.ContentType = aws.String(options.ContentType)
	}
	if options.CacheControl != "" {
		input.CacheControl = aws.String(options.CacheControl)
	}
	if _, err := s.uploader.UploadObject(ctx, input); err != nil {
		return ObjectInfo{}, fmt.Errorf("upload S3 object %s: %w", key.String(), err)
	}
	info, err := s.Stat(ctx, key)
	if err != nil {
		return ObjectInfo{}, err
	}
	if options.ExpectedSize != nil && info.Size != *options.ExpectedSize {
		sizeErr := fmt.Errorf("object %s size mismatch: stored %d, expected %d", key.String(), info.Size, *options.ExpectedSize)
		return ObjectInfo{}, errors.Join(sizeErr, s.Delete(context.WithoutCancel(ctx), key))
	}
	return info, nil
}

func validateSeekableSize(src io.Reader, expected int64) error {
	seeker, ok := src.(io.Seeker)
	if !ok {
		return nil
	}
	current, err := seeker.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	end, endErr := seeker.Seek(0, io.SeekEnd)
	_, restoreErr := seeker.Seek(current, io.SeekStart)
	if endErr != nil || restoreErr != nil {
		return errors.Join(endErr, restoreErr)
	}
	if remaining := end - current; remaining != expected {
		return fmt.Errorf("source has %d bytes remaining, expected %d", remaining, expected)
	}
	return nil
}

func (s *S3Store) Stat(ctx context.Context, key Key) (ObjectInfo, error) {
	if err := ctx.Err(); err != nil {
		return ObjectInfo{}, err
	}
	remoteKey, err := s.remoteKey(key)
	if err != nil {
		return ObjectInfo{}, err
	}
	output, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(remoteKey),
	})
	if err != nil {
		return ObjectInfo{}, normalizeS3Error(key, err)
	}
	return ObjectInfo{
		Key:          key,
		Size:         aws.ToInt64(output.ContentLength),
		ModTime:      aws.ToTime(output.LastModified),
		ContentType:  aws.ToString(output.ContentType),
		CacheControl: aws.ToString(output.CacheControl),
		ETag:         aws.ToString(output.ETag),
	}, nil
}

func (s *S3Store) Delete(ctx context.Context, key Key) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	remoteKey, err := s.remoteKey(key)
	if err != nil {
		return err
	}
	_, err = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(remoteKey),
	})
	if err != nil {
		normalized := normalizeS3Error(key, err)
		if errors.Is(normalized, ErrNotFound) {
			return nil
		}
		return normalized
	}
	return nil
}

func (s *S3Store) Walk(ctx context.Context, prefix Key, fn func(ObjectInfo) error) error {
	if fn == nil {
		return errors.New("storage walk callback is nil")
	}
	remotePrefix, err := s.remoteKey(prefix)
	if err != nil {
		return err
	}
	var continuation *string
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		output, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(s.bucket),
			Prefix:            aws.String(remotePrefix),
			ContinuationToken: continuation,
		})
		if err != nil {
			return fmt.Errorf("list S3 objects below %s: %w", prefix.String(), err)
		}
		for _, object := range output.Contents {
			remoteObjectKey := aws.ToString(object.Key)
			if remoteObjectKey != remotePrefix && !strings.HasPrefix(remoteObjectKey, remotePrefix+"/") {
				continue
			}
			if strings.HasSuffix(remoteObjectKey, "/") {
				continue
			}
			logicalKey, err := s.logicalKey(remoteObjectKey)
			if err != nil {
				return err
			}
			if err := fn(ObjectInfo{
				Key:     logicalKey,
				Size:    aws.ToInt64(object.Size),
				ModTime: aws.ToTime(object.LastModified),
				ETag:    aws.ToString(object.ETag),
			}); err != nil {
				return err
			}
		}
		if !aws.ToBool(output.IsTruncated) {
			return nil
		}
		if aws.ToString(output.NextContinuationToken) == "" {
			return errors.New("S3 object listing was truncated without a continuation token")
		}
		continuation = output.NextContinuationToken
	}
}

func (s *S3Store) Close() error {
	return nil
}

func (s *S3Store) Check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	input := &s3.ListObjectsV2Input{
		Bucket:  aws.String(s.bucket),
		MaxKeys: aws.Int32(1),
	}
	if s.prefix != "" {
		input.Prefix = aws.String(s.prefix + "/")
	}
	if _, err := s.client.ListObjectsV2(ctx, input); err != nil {
		return fmt.Errorf("check S3 bucket access: %w", err)
	}
	return nil
}

func (s *S3Store) remoteKey(key Key) (string, error) {
	validated, err := ParseKey(key.String())
	if err != nil {
		return "", err
	}
	if s == nil || s.bucket == "" || s.client == nil || s.uploader == nil {
		return "", ErrStoreNotConfigured
	}
	if s.prefix == "" {
		return validated.String(), nil
	}
	return s.prefix + "/" + validated.String(), nil
}

func (s *S3Store) logicalKey(remoteKey string) (Key, error) {
	if s.prefix != "" {
		base := s.prefix + "/"
		if !strings.HasPrefix(remoteKey, base) {
			return Key{}, fmt.Errorf("S3 object %q is outside configured prefix %q", remoteKey, s.prefix)
		}
		remoteKey = strings.TrimPrefix(remoteKey, base)
	}
	key, err := ParseKey(remoteKey)
	if err != nil {
		return Key{}, fmt.Errorf("invalid S3 object key: %w", err)
	}
	return key, nil
}

func normalizeS3Error(key Key, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var apiError smithy.APIError
	if errors.As(err, &apiError) {
		switch apiError.ErrorCode() {
		case "NoSuchKey", "NotFound", "NoSuchObject":
			return fmt.Errorf("%w: %s", ErrNotFound, key.String())
		}
	}
	var responseError *smithyhttp.ResponseError
	if errors.As(err, &responseError) && responseError.HTTPStatusCode() == http.StatusNotFound {
		return fmt.Errorf("%w: %s", ErrNotFound, key.String())
	}
	return fmt.Errorf("S3 object %s: %w", key.String(), err)
}

type s3ReadSeekCloser struct {
	mu       sync.Mutex
	ctx      context.Context
	client   s3APIClient
	bucket   string
	key      string
	size     int64
	etag     string
	position int64
	body     io.ReadCloser
	closed   bool
}

func (r *s3ReadSeekCloser) Read(buffer []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, fsClosedError()
	}
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(buffer) == 0 {
		return 0, nil
	}
	if r.position >= r.size {
		return 0, io.EOF
	}
	if r.body == nil {
		if err := r.openBody(); err != nil {
			return 0, err
		}
	}
	n, err := r.body.Read(buffer)
	r.position += int64(n)
	if errors.Is(err, io.EOF) {
		closeErr := r.body.Close()
		r.body = nil
		if r.position < r.size {
			err = io.ErrUnexpectedEOF
		}
		if closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}
	return n, err
}

func (r *s3ReadSeekCloser) Seek(offset int64, whence int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, fsClosedError()
	}
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	var target int64
	switch whence {
	case io.SeekStart:
		target = offset
	case io.SeekCurrent:
		target = r.position + offset
	case io.SeekEnd:
		target = r.size + offset
	default:
		return 0, errors.New("invalid seek whence")
	}
	if target < 0 {
		return 0, errors.New("negative seek position")
	}
	if target == r.position {
		return target, nil
	}
	if r.body != nil {
		if err := r.body.Close(); err != nil {
			r.body = nil
			return 0, err
		}
		r.body = nil
	}
	r.position = target
	return target, nil
}

func (r *s3ReadSeekCloser) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	if r.body == nil {
		return nil
	}
	err := r.body.Close()
	r.body = nil
	return err
}

func (r *s3ReadSeekCloser) openBody() error {
	input := &s3.GetObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(r.key),
	}
	if r.etag != "" {
		input.IfMatch = aws.String(r.etag)
	}
	if r.position > 0 {
		input.Range = aws.String(fmt.Sprintf("bytes=%d-", r.position))
	}
	output, err := r.client.GetObject(r.ctx, input)
	if err != nil {
		return fmt.Errorf("read S3 object %s: %w", r.key, err)
	}
	if output.Body == nil {
		return fmt.Errorf("read S3 object %s: response body is nil", r.key)
	}
	if r.position > 0 {
		start, err := contentRangeStart(aws.ToString(output.ContentRange))
		if err != nil || start != r.position {
			_ = output.Body.Close()
			return fmt.Errorf("read S3 object %s: invalid content range %q for offset %d", r.key, aws.ToString(output.ContentRange), r.position)
		}
	}
	r.body = output.Body
	return nil
}

func contentRangeStart(value string) (int64, error) {
	if !strings.HasPrefix(value, "bytes ") {
		return 0, errors.New("invalid content range unit")
	}
	span := strings.TrimPrefix(value, "bytes ")
	rangeAndSize := strings.SplitN(span, "/", 2)
	if len(rangeAndSize) != 2 {
		return 0, errors.New("invalid content range")
	}
	bounds := strings.SplitN(rangeAndSize[0], "-", 2)
	if len(bounds) != 2 {
		return 0, errors.New("invalid content range bounds")
	}
	return strconv.ParseInt(bounds[0], 10, 64)
}

func fsClosedError() error {
	return errors.New("storage object is closed")
}

var _ Store = (*S3Store)(nil)
var _ HealthChecker = (*S3Store)(nil)
var _ ReadSeekCloser = (*s3ReadSeekCloser)(nil)
