package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

func TestS3StoreConformance(t *testing.T) {
	backend := newMemoryS3(1)
	store := newS3Store(S3Options{Bucket: "media", Prefix: "tenant/videos"}, backend, backend)
	ctx := context.Background()
	key := mustParseKey(t, "file/720p/out0.ts")
	expectedSize := int64(len("segment-data"))

	info, err := store.Put(ctx, key, strings.NewReader("segment-data"), PutOptions{
		ExpectedSize: &expectedSize,
		ContentType:  "video/mp2t",
		CacheControl: "public, max-age=60",
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if info.Size != expectedSize || info.ContentType != "video/mp2t" || info.CacheControl != "public, max-age=60" {
		t.Fatalf("Put() info = %#v", info)
	}
	if backend.lastUploadKey != "tenant/videos/file/720p/out0.ts" {
		t.Fatalf("physical upload key = %q", backend.lastUploadKey)
	}
	if backend.lastContentLength != expectedSize || backend.lastMultipartSize != expectedSize {
		t.Fatalf("upload sizes = %d/%d, want %d", backend.lastContentLength, backend.lastMultipartSize, expectedSize)
	}

	object, err := store.Open(ctx, key)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if _, err := object.Body.Seek(8, io.SeekStart); err != nil {
		t.Fatalf("Seek() error = %v", err)
	}
	data, err := io.ReadAll(object.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(data) != "data" {
		t.Fatalf("range data = %q, want data", data)
	}
	if _, err := object.Body.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("second Seek() error = %v", err)
	}
	first := make([]byte, 7)
	if _, err := io.ReadFull(object.Body, first); err != nil {
		t.Fatalf("ReadFull() error = %v", err)
	}
	if string(first) != "segment" {
		t.Fatalf("second read data = %q, want segment", first)
	}
	if offset, err := object.Body.Seek(0, io.SeekEnd); err != nil || offset != expectedSize {
		t.Fatalf("SeekEnd() = %d, %v, want %d", offset, err, expectedSize)
	}
	if err := object.Body.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if got := backend.getRanges(); len(got) != 2 || got[0] != "bytes=8-" || got[1] != "" {
		t.Fatalf("GetObject ranges = %v, want [bytes=8- <full>]", got)
	}
	if got := backend.getIfMatches(); len(got) != 2 || got[0] != info.ETag || got[1] != info.ETag {
		t.Fatalf("GetObject If-Match values = %v, want %q", got, info.ETag)
	}

	outsidePrefix := mustParseKey(t, "file-other/720p/out0.ts")
	if _, err := store.Put(ctx, outsidePrefix, strings.NewReader("other"), PutOptions{}); err != nil {
		t.Fatalf("Put() outside prefix error = %v", err)
	}
	backend.putRaw("tenant/videos/file/", memoryS3Object{})
	var walked []string
	if err := store.Walk(ctx, mustParseKey(t, "file"), func(info ObjectInfo) error {
		walked = append(walked, info.Key.String())
		return nil
	}); err != nil {
		t.Fatalf("Walk() error = %v", err)
	}
	if len(walked) != 1 || walked[0] != key.String() {
		t.Fatalf("Walk() = %v, want [%s]", walked, key.String())
	}
	if backend.listCallCount() < 2 {
		t.Fatalf("Walk() used %d list call(s), want pagination", backend.listCallCount())
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.Open(canceled, key); !errors.Is(err, context.Canceled) {
		t.Fatalf("Open() canceled error = %v, want context.Canceled", err)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("second Delete() error = %v", err)
	}
	if _, err := store.Open(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open() after delete error = %v, want ErrNotFound", err)
	}
}

func TestS3StoreRejectsSizeMismatchAndRemovesUploadedObject(t *testing.T) {
	backend := newMemoryS3(1000)
	store := newS3Store(S3Options{Bucket: "media"}, backend, backend)
	key := mustParseKey(t, "file/source/original.mp4")
	expected := int64(100)

	if _, err := store.Put(context.Background(), key, strings.NewReader("short"), PutOptions{ExpectedSize: &expected}); err == nil {
		t.Fatal("Put() seekable size mismatch error = nil")
	}
	if backend.uploadCallCount() != 0 {
		t.Fatalf("seekable size mismatch made %d upload call(s), want 0", backend.uploadCallCount())
	}

	nonSeekable := struct{ io.Reader }{Reader: strings.NewReader("short")}
	if _, err := store.Put(context.Background(), key, nonSeekable, PutOptions{ExpectedSize: &expected}); err == nil {
		t.Fatal("Put() stored size mismatch error = nil")
	}
	if _, err := store.Stat(context.Background(), key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("mismatched object remains stored: %v", err)
	}
}

func TestS3StoreRejectsRangeResponseForWrongOffset(t *testing.T) {
	backend := newMemoryS3(1000)
	backend.wrongContentRange = true
	store := newS3Store(S3Options{Bucket: "media"}, backend, backend)
	key := mustParseKey(t, "file/source/original.mp4")
	if _, err := store.Put(context.Background(), key, strings.NewReader("source"), PutOptions{}); err != nil {
		t.Fatal(err)
	}
	object, err := store.Open(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer object.Body.Close()
	if _, err := object.Body.Seek(2, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if _, err := object.Body.Read(make([]byte, 1)); err == nil || !strings.Contains(err.Error(), "invalid content range") {
		t.Fatalf("Read() error = %v, want invalid content range", err)
	}
}

func TestNormalizeS3Options(t *testing.T) {
	valid, err := normalizeS3Options(S3Options{
		Bucket:          " media ",
		Endpoint:        "https://objects.example.com",
		Prefix:          "/tenant/videos/",
		AccessKeyID:     "access",
		SecretAccessKey: "secret",
	})
	if err != nil {
		t.Fatalf("normalizeS3Options() error = %v", err)
	}
	if valid.Bucket != "media" || valid.Region != defaultS3Region || valid.Prefix != "tenant/videos" {
		t.Fatalf("normalized options = %#v", valid)
	}
	if valid.UploadPartSize != defaultS3UploadPartSize || valid.UploadConcurrency != defaultS3UploadConcurrency {
		t.Fatalf("upload defaults = %d/%d", valid.UploadPartSize, valid.UploadConcurrency)
	}

	tests := []S3Options{
		{},
		{Bucket: "media", Endpoint: "objects.example.com"},
		{Bucket: "media", Prefix: "../outside"},
		{Bucket: "media", AccessKeyID: "access"},
		{Bucket: "media", SessionToken: "token"},
		{Bucket: "media", UploadPartSize: minimumS3UploadPartSize - 1},
		{Bucket: "media", UploadConcurrency: 65},
	}
	for _, options := range tests {
		if _, err := normalizeS3Options(options); err == nil {
			t.Errorf("normalizeS3Options(%#v) error = nil", options)
		}
	}
}

type memoryS3Object struct {
	data         []byte
	contentType  string
	cacheControl string
	modified     time.Time
	etag         string
}

type memoryS3 struct {
	mu                sync.Mutex
	objects           map[string]memoryS3Object
	pageSize          int
	listCalls         int
	uploadCalls       int
	ranges            []string
	ifMatches         []string
	lastUploadKey     string
	lastContentLength int64
	lastMultipartSize int64
	wrongContentRange bool
}

func newMemoryS3(pageSize int) *memoryS3 {
	return &memoryS3{objects: make(map[string]memoryS3Object), pageSize: pageSize}
}

func (m *memoryS3) UploadObject(ctx context.Context, input *transfermanager.UploadObjectInput, _ ...func(*transfermanager.Options)) (*transfermanager.UploadObjectOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}
	key := aws.ToString(input.Key)
	object := memoryS3Object{
		data:         data,
		contentType:  aws.ToString(input.ContentType),
		cacheControl: aws.ToString(input.CacheControl),
		modified:     time.Unix(1_700_000_000, 0).UTC(),
		etag:         fmt.Sprintf("\"etag-%d\"", len(data)),
	}
	m.mu.Lock()
	m.objects[key] = object
	m.uploadCalls++
	m.lastUploadKey = key
	m.lastContentLength = aws.ToInt64(input.ContentLength)
	m.lastMultipartSize = aws.ToInt64(input.MpuObjectSize)
	m.mu.Unlock()
	return &transfermanager.UploadObjectOutput{
		Bucket:        input.Bucket,
		Key:           input.Key,
		ETag:          aws.String(object.etag),
		ContentLength: aws.Int64(int64(len(data))),
	}, nil
}

func (m *memoryS3) HeadObject(ctx context.Context, input *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	object, ok := m.objects[aws.ToString(input.Key)]
	m.mu.Unlock()
	if !ok {
		return nil, &smithy.GenericAPIError{Code: "NoSuchKey", Message: "missing"}
	}
	return &s3.HeadObjectOutput{
		ContentLength: aws.Int64(int64(len(object.data))),
		ContentType:   aws.String(object.contentType),
		CacheControl:  aws.String(object.cacheControl),
		LastModified:  aws.Time(object.modified),
		ETag:          aws.String(object.etag),
	}, nil
}

func (m *memoryS3) GetObject(ctx context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := aws.ToString(input.Key)
	rangeValue := aws.ToString(input.Range)
	ifMatch := aws.ToString(input.IfMatch)
	m.mu.Lock()
	object, ok := m.objects[key]
	m.ranges = append(m.ranges, rangeValue)
	m.ifMatches = append(m.ifMatches, ifMatch)
	wrongContentRange := m.wrongContentRange
	m.mu.Unlock()
	if !ok {
		return nil, &smithy.GenericAPIError{Code: "NoSuchKey", Message: "missing"}
	}
	if ifMatch != "" && ifMatch != object.etag {
		return nil, &smithy.GenericAPIError{Code: "PreconditionFailed", Message: "etag changed"}
	}
	start := int64(0)
	contentRange := ""
	if rangeValue != "" {
		var err error
		start, err = strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(rangeValue, "bytes="), "-"), 10, 64)
		if err != nil || start < 0 || start >= int64(len(object.data)) {
			return nil, &smithy.GenericAPIError{Code: "InvalidRange", Message: "invalid range"}
		}
		contentStart := start
		if wrongContentRange {
			contentStart = 0
		}
		contentRange = fmt.Sprintf("bytes %d-%d/%d", contentStart, len(object.data)-1, len(object.data))
	}
	data := append([]byte(nil), object.data[start:]...)
	return &s3.GetObjectOutput{
		Body:          io.NopCloser(bytes.NewReader(data)),
		ContentLength: aws.Int64(int64(len(data))),
		ContentRange:  optionalString(contentRange),
		ContentType:   aws.String(object.contentType),
		CacheControl:  aws.String(object.cacheControl),
		LastModified:  aws.Time(object.modified),
		ETag:          aws.String(object.etag),
	}, nil
}

func (m *memoryS3) DeleteObject(ctx context.Context, input *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	delete(m.objects, aws.ToString(input.Key))
	m.mu.Unlock()
	return &s3.DeleteObjectOutput{}, nil
}

func (m *memoryS3) ListObjectsV2(ctx context.Context, input *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	prefix := aws.ToString(input.Prefix)
	m.mu.Lock()
	keys := make([]string, 0, len(m.objects))
	for key := range m.objects {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	m.listCalls++
	pageSize := m.pageSize
	m.mu.Unlock()

	start := 0
	if input.ContinuationToken != nil {
		var err error
		start, err = strconv.Atoi(aws.ToString(input.ContinuationToken))
		if err != nil {
			return nil, err
		}
	}
	end := min(start+pageSize, len(keys))
	contents := make([]types.Object, 0, end-start)
	for _, key := range keys[start:end] {
		m.mu.Lock()
		object := m.objects[key]
		m.mu.Unlock()
		contents = append(contents, types.Object{
			Key:          aws.String(key),
			Size:         aws.Int64(int64(len(object.data))),
			LastModified: aws.Time(object.modified),
			ETag:         aws.String(object.etag),
		})
	}
	truncated := end < len(keys)
	output := &s3.ListObjectsV2Output{
		Contents:    contents,
		IsTruncated: aws.Bool(truncated),
	}
	if truncated {
		output.NextContinuationToken = aws.String(strconv.Itoa(end))
	}
	return output, nil
}

func (m *memoryS3) putRaw(key string, object memoryS3Object) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = object
}

func (m *memoryS3) getRanges() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.ranges...)
}

func (m *memoryS3) getIfMatches() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.ifMatches...)
}

func (m *memoryS3) listCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listCalls
}

func (m *memoryS3) uploadCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.uploadCalls
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return aws.String(value)
}
