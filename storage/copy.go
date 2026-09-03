package storage

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"sort"
	"strings"
)

type VerifiedCopyResult struct {
	Info       ObjectInfo
	SourceInfo ObjectInfo
	Checksum   string
	Copied     bool
}

func PrefixInventory(ctx context.Context, store Store, prefix Key) ([]ObjectInfo, error) {
	if store == nil {
		return nil, ErrStoreNotConfigured
	}
	objects := make([]ObjectInfo, 0)
	if err := store.Walk(ctx, prefix, func(info ObjectInfo) error {
		objects = append(objects, info)
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Slice(objects, func(i, j int) bool {
		iManifest := strings.EqualFold(objectExtension(objects[i].Key.String()), ".m3u8")
		jManifest := strings.EqualFold(objectExtension(objects[j].Key.String()), ".m3u8")
		if iManifest != jManifest {
			return !iManifest
		}
		return objects[i].Key.String() < objects[j].Key.String()
	})
	return objects, nil
}

func CopyObjectVerified(ctx context.Context, source, destination Store, sourceInfo ObjectInfo) (VerifiedCopyResult, error) {
	if source == nil || destination == nil {
		return VerifiedCopyResult{}, ErrStoreNotConfigured
	}
	if err := ctx.Err(); err != nil {
		return VerifiedCopyResult{}, err
	}
	if destinationInfo, err := destination.Stat(ctx, sourceInfo.Key); err == nil && destinationInfo.Size == sourceInfo.Size {
		sourceChecksum, sourceSize, sourceErr := hashStoredObject(ctx, source, sourceInfo.Key)
		if sourceErr != nil {
			return VerifiedCopyResult{}, sourceErr
		}
		destinationChecksum, destinationSize, destinationErr := hashStoredObject(ctx, destination, sourceInfo.Key)
		if destinationErr != nil {
			return VerifiedCopyResult{}, destinationErr
		}
		if sourceSize == destinationSize && sourceChecksum == destinationChecksum {
			return VerifiedCopyResult{Info: destinationInfo, SourceInfo: sourceInfo, Checksum: sourceChecksum, Copied: false}, nil
		}
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return VerifiedCopyResult{}, err
	}

	object, err := source.Open(ctx, sourceInfo.Key)
	if err != nil {
		return VerifiedCopyResult{}, err
	}
	defer object.Body.Close()
	expectedSize := object.Info.Size
	digest := sha256.New()
	reader := io.TeeReader(&contextReader{ctx: ctx, reader: object.Body}, digest)
	written, err := destination.Put(ctx, sourceInfo.Key, reader, PutOptions{
		ContentType: object.Info.ContentType, CacheControl: object.Info.CacheControl, ExpectedSize: &expectedSize,
	})
	if err != nil {
		return VerifiedCopyResult{}, err
	}
	if written.Size != expectedSize {
		return VerifiedCopyResult{}, fmt.Errorf("copied object %s size mismatch: wrote %d, expected %d", sourceInfo.Key.String(), written.Size, expectedSize)
	}
	sourceChecksum := fmt.Sprintf("%x", digest.Sum(nil))
	destinationChecksum, destinationSize, err := hashStoredObject(ctx, destination, sourceInfo.Key)
	if err != nil {
		return VerifiedCopyResult{}, err
	}
	if destinationSize != expectedSize || destinationChecksum != sourceChecksum {
		return VerifiedCopyResult{}, fmt.Errorf("copied object %s failed checksum verification", sourceInfo.Key.String())
	}
	return VerifiedCopyResult{Info: written, SourceInfo: object.Info, Checksum: sourceChecksum, Copied: true}, nil
}

// CopyObjectValidated copies an object once and relies on Store.Put's atomic,
// size-checked, transport-validated contract. S3 uploads are protected by the
// SDK's request checksums, SFTP by SSH integrity and atomic rename, and local
// writes by a temporary file and atomic rename. The source SHA-256 is retained
// for the caller's durable checkpoint, avoiding an expensive destination
// download after every successful upload.
func CopyObjectValidated(ctx context.Context, source, destination Store, sourceInfo ObjectInfo) (VerifiedCopyResult, error) {
	if source == nil || destination == nil {
		return VerifiedCopyResult{}, ErrStoreNotConfigured
	}
	if err := ctx.Err(); err != nil {
		return VerifiedCopyResult{}, err
	}
	object, err := source.Open(ctx, sourceInfo.Key)
	if err != nil {
		return VerifiedCopyResult{}, err
	}
	defer object.Body.Close()
	if object.Info.Size != sourceInfo.Size {
		return VerifiedCopyResult{}, fmt.Errorf("source object %s changed size from %d to %d", sourceInfo.Key.String(), sourceInfo.Size, object.Info.Size)
	}
	expectedSize := object.Info.Size
	digest := sha256.New()
	reader := io.TeeReader(&contextReader{ctx: ctx, reader: object.Body}, digest)
	written, err := destination.Put(ctx, sourceInfo.Key, reader, PutOptions{
		ContentType: object.Info.ContentType, CacheControl: object.Info.CacheControl, ExpectedSize: &expectedSize,
	})
	if err != nil {
		return VerifiedCopyResult{}, err
	}
	if written.Size != expectedSize {
		return VerifiedCopyResult{}, fmt.Errorf("copied object %s size mismatch: wrote %d, expected %d", sourceInfo.Key.String(), written.Size, expectedSize)
	}
	return VerifiedCopyResult{
		Info: written, SourceInfo: object.Info, Checksum: fmt.Sprintf("%x", digest.Sum(nil)), Copied: true,
	}, nil
}

func hashStoredObject(ctx context.Context, store Store, key Key) (string, int64, error) {
	object, err := store.Open(ctx, key)
	if err != nil {
		return "", 0, err
	}
	defer object.Body.Close()
	digest := sha256.New()
	written, err := copyHashWithContext(ctx, digest, object.Body)
	if err != nil {
		return "", written, err
	}
	if written != object.Info.Size {
		return "", written, fmt.Errorf("object %s size mismatch while hashing: read %d, expected %d", key.String(), written, object.Info.Size)
	}
	return fmt.Sprintf("%x", digest.Sum(nil)), written, nil
}

func copyHashWithContext(ctx context.Context, destination hash.Hash, source io.Reader) (int64, error) {
	return copyWithContext(ctx, destination, source)
}

func objectExtension(value string) string {
	index := strings.LastIndexByte(value, '.')
	separator := strings.LastIndexByte(value, '/')
	if index < 0 || index < separator {
		return ""
	}
	return value[index:]
}
