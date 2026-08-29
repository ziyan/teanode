package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/ziyan/teanode/internal/util/atomicfile"
)

// Files stores opaque bytes, as distinct from messages.
//
// A message has headers and a body and is parsed on the way in and out; a
// picture an operator uploaded is neither, and putting it through the message
// path would mean inventing headers for it and parsing them back. This is the
// same storage — the same directory, the same optional mirror, the same
// fallback from one to the other — with none of that in the way.
//
// The lifetime is different too, and that is why the sweep does not touch
// these. A message is swept once it is older than the retention; a logo in a
// template is wanted for as long as the template is, which may be years.
type Files interface {
	// PutFile stores bytes under an identifier, overwriting any already
	// there.
	PutFile(ctx context.Context, id string, content []byte) error

	// GetFile returns stored bytes, or ErrNotFound.
	GetFile(ctx context.Context, id string) ([]byte, error)

	// DeleteFile removes them. Removing what is not there is not an error.
	DeleteFile(ctx context.Context, id string) error
}

// fileDirectory is where uploaded files live under the spool root, kept apart
// from the messages so that a person looking at the directory can tell them
// apart, and so the message sweep's listing never meets one.
const fileDirectory = "media"

// fileKeyPrefix is the same separation inside the bucket.
const fileKeyPrefix = "media/"

func (self *filesystem) filePath(id string) (string, error) {
	if id == "" || strings.ContainsAny(id, "/\\.") {
		return "", fmt.Errorf("storage: %q is not a usable identifier", id)
	}
	// Sharded on the last two characters, as the messages are: a directory
	// with a hundred thousand entries in it is slow to list on every
	// filesystem that has ever been shipped.
	shard := id[len(id)-2:]
	return filepath.Join(self.settings.Directory, fileDirectory, shard, id), nil
}

func (self *filesystem) PutFile(ctx context.Context, id string, content []byte) error {
	filename, err := self.filePath(id)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return fmt.Errorf("storage: cannot create %s: %w", filepath.Dir(filename), err)
	}

	file, err := atomicfile.Create(filename)
	if err != nil {
		return fmt.Errorf("storage: cannot write %s: %w", filename, err)
	}
	defer func() {
		_ = atomicfile.Discard(file)
	}()
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("storage: cannot write %s: %w", filename, err)
	}
	if _, err := file.Write(content); err != nil {
		return fmt.Errorf("storage: cannot write %s: %w", filename, err)
	}
	if err := atomicfile.Commit(file); err != nil {
		return fmt.Errorf("storage: cannot write %s: %w", filename, err)
	}

	// The mirror is a copy, not the record — the same rule the messages
	// follow. Failing to reach it must not fail the upload, which has already
	// been written somewhere durable.
	if self.mirror != nil {
		if err := self.mirror.PutFile(ctx, id, content); err != nil {
			log.Warningf("failed to mirror file %s: %s", id, err)
		}
	}
	return nil
}

func (self *filesystem) GetFile(ctx context.Context, id string) ([]byte, error) {
	filename, err := self.filePath(id)
	if err != nil {
		return nil, err
	}

	content, err := os.ReadFile(filename)
	if err == nil {
		return content, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("storage: cannot read %s: %w", filename, err)
	}

	// Not on this machine. It may be in the mirror, which is how a file
	// uploaded through one instance is served by another, and how it survives
	// the local disk being lost.
	if self.mirror != nil {
		content, mirrorError := self.mirror.GetFile(ctx, id)
		if mirrorError == nil {
			return content, nil
		}
		if !errors.Is(mirrorError, ErrNotFound) {
			log.Warningf("failed to read file %s from the mirror: %s", id, mirrorError)
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
}

func (self *filesystem) DeleteFile(ctx context.Context, id string) error {
	filename, err := self.filePath(id)
	if err != nil {
		return err
	}
	if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("storage: cannot remove %s: %w", filename, err)
	}
	if self.mirror != nil {
		if err := self.mirror.DeleteFile(ctx, id); err != nil {
			log.Warningf("failed to remove file %s from the mirror: %s", id, err)
		}
	}
	return nil
}

func (self *s3Storage) fileKey(id string) string {
	return fileKeyPrefix + id
}

func (self *s3Storage) PutFile(ctx context.Context, id string, content []byte) error {
	if _, err := self.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(self.settings.Bucket),
		Key:    aws.String(self.fileKey(id)),
		Body:   bytes.NewReader(content),
	}); err != nil {
		return fmt.Errorf("storage: cannot upload %s: %w", id, err)
	}
	return nil
}

func (self *s3Storage) GetFile(ctx context.Context, id string) ([]byte, error) {
	writeAtBuffer := manager.NewWriteAtBuffer(nil)
	if _, err := self.downloader.Download(ctx, writeAtBuffer, &s3.GetObjectInput{
		Bucket: aws.String(self.settings.Bucket),
		Key:    aws.String(self.fileKey(id)),
	}); err != nil {
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &noSuchKey) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return nil, fmt.Errorf("storage: cannot download %s: %w", id, err)
	}
	return writeAtBuffer.Bytes(), nil
}

func (self *s3Storage) DeleteFile(ctx context.Context, id string) error {
	if _, err := self.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(self.settings.Bucket),
		Key:    aws.String(self.fileKey(id)),
	}); err != nil {
		return fmt.Errorf("storage: cannot remove %s: %w", id, err)
	}
	return nil
}
