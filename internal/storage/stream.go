package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/ziyan/teanode/internal/util/atomicfile"
)

// StreamFiles extends the existing blob store without buffering whole media.
// Writers own their seekable source; storage rewinds it before writing or retrying.
type StreamFiles interface {
	PutFileStream(context.Context, string, io.ReadSeeker, int64) error
	OpenFileRange(context.Context, string, int64, int64) (*FileStream, error)
	DeleteFileEverywhere(context.Context, string) error
}

// DeleteFileEverywhere reports mirror failures so durable cleanup can retry them.
func (self *filesystem) DeleteFileEverywhere(ctx context.Context, id string) error {
	if err := validateIdentifier(id); err != nil {
		return err
	}
	if self.settings.Directory != "" {
		filename, err := self.filePath(id)
		if err != nil {
			return err
		}
		if err := os.Remove(filename); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := syncDirectory(filepath.Dir(filename)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if self.mirror != nil {
		return self.mirror.DeleteFile(ctx, id)
	}
	return nil
}

func (self *s3Storage) DeleteFileEverywhere(ctx context.Context, id string) error {
	if err := validateIdentifier(id); err != nil {
		return err
	}
	return self.DeleteFile(ctx, id)
}

// FileStream is a bounded readable portion of a blob. The caller closes it.
type FileStream struct {
	io.ReadCloser
	SizeBytes int64
}

func rewindFileSource(content io.ReadSeeker, sizeBytes int64) error {
	if content == nil || sizeBytes < 0 {
		return fmt.Errorf("storage: invalid file source")
	}
	actualSizeBytes, err := content.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if actualSizeBytes != sizeBytes {
		return fmt.Errorf("storage: file size does not match its declaration")
	}
	_, err = content.Seek(0, io.SeekStart)
	return err
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (self *contextReader) Read(content []byte) (int, error) {
	if err := self.ctx.Err(); err != nil {
		return 0, err
	}
	return self.reader.Read(content)
}

func (self *filesystem) PutFileStream(ctx context.Context, id string, content io.ReadSeeker, sizeBytes int64) error {
	if err := validateIdentifier(id); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := rewindFileSource(content, sizeBytes); err != nil {
		return err
	}
	if self.settings.Directory == "" {
		streaming, isStreaming := self.mirror.(StreamFiles)
		if !isStreaming {
			return fmt.Errorf("storage: object store does not support streaming files")
		}
		return streaming.PutFileStream(ctx, id, content, sizeBytes)
	}
	filename, err := self.filePath(id)
	if err != nil {
		return err
	}
	directory := filepath.Dir(filename)
	if err := ensureLocalDirectory(directory); err != nil {
		return err
	}
	file, err := atomicfile.CreateWithMode(filename, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = atomicfile.Discard(file) }()
	if _, err := io.CopyN(file, &contextReader{ctx: ctx, reader: content}, sizeBytes); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := atomicfile.Commit(file); err != nil {
		return err
	}
	if err := syncDirectory(directory); err != nil {
		return err
	}
	if streaming, isStreaming := self.mirror.(StreamFiles); isStreaming {
		if err := streaming.PutFileStream(ctx, id, content, sizeBytes); err != nil {
			log.Warningf("failed to mirror file %s: %s", id, err)
		}
	}
	return nil
}

type boundedFileReader struct {
	io.Reader
	closer io.Closer
}

func (self *boundedFileReader) Close() error { return self.closer.Close() }

// A negative size means the remainder of the file; offsets are measured in bytes.
func (self *filesystem) OpenFileRange(ctx context.Context, id string, offsetBytes, sizeBytes int64) (*FileStream, error) {
	if err := validateIdentifier(id); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if offsetBytes < 0 || sizeBytes < -1 || sizeBytes == 0 {
		return nil, fmt.Errorf("storage: invalid file range")
	}
	if self.settings.Directory == "" {
		streaming, isStreaming := self.mirror.(StreamFiles)
		if !isStreaming {
			return nil, fmt.Errorf("storage: object store does not support streaming files")
		}
		return streaming.OpenFileRange(ctx, id, offsetBytes, sizeBytes)
	}
	filename, err := self.filePath(id)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(filename)
	if errors.Is(err, os.ErrNotExist) {
		if streaming, isStreaming := self.mirror.(StreamFiles); isStreaming {
			return streaming.OpenFileRange(ctx, id, offsetBytes, sizeBytes)
		}
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	metadata, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if offsetBytes > metadata.Size() || (sizeBytes >= 0 && sizeBytes > metadata.Size()-offsetBytes) {
		_ = file.Close()
		return nil, fmt.Errorf("storage: invalid file range")
	}
	if sizeBytes < 0 {
		sizeBytes = metadata.Size() - offsetBytes
	}
	if _, err := file.Seek(offsetBytes, io.SeekStart); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &FileStream{ReadCloser: &boundedFileReader{Reader: &contextReader{ctx: ctx, reader: io.LimitReader(file, sizeBytes)}, closer: file}, SizeBytes: sizeBytes}, nil
}

func (self *s3Storage) PutFileStream(ctx context.Context, id string, content io.ReadSeeker, sizeBytes int64) error {
	if err := validateIdentifier(id); err != nil {
		return err
	}
	if err := rewindFileSource(content, sizeBytes); err != nil {
		return err
	}
	if sizeBytes > s3MultipartThresholdBytes {
		return self.putObjectInParts(ctx, self.fileKey(id), content, sizeBytes)
	}
	_, err := self.client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(self.settings.Bucket), Key: aws.String(self.fileKey(id)), Body: content, ContentLength: aws.Int64(sizeBytes)})
	return err
}

func (self *s3Storage) OpenFileRange(ctx context.Context, id string, offsetBytes, sizeBytes int64) (*FileStream, error) {
	if err := validateIdentifier(id); err != nil {
		return nil, err
	}
	if offsetBytes < 0 || sizeBytes < -1 || (sizeBytes > 0 && offsetBytes > math.MaxInt64-sizeBytes) {
		return nil, fmt.Errorf("storage: invalid file range")
	}
	if sizeBytes == 0 {
		return nil, fmt.Errorf("storage: empty object range")
	}
	input := &s3.GetObjectInput{Bucket: aws.String(self.settings.Bucket), Key: aws.String(self.fileKey(id))}
	if sizeBytes >= 0 {
		input.Range = aws.String(fmt.Sprintf("bytes=%d-%d", offsetBytes, offsetBytes+sizeBytes-1))
	} else if offsetBytes > 0 {
		input.Range = aws.String(fmt.Sprintf("bytes=%d-", offsetBytes))
	}
	output, err := self.client.GetObject(ctx, input)
	if err != nil {
		var missing *types.NoSuchKey
		if errors.As(err, &missing) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return nil, err
	}
	if output.ContentLength == nil || *output.ContentLength < 0 || (sizeBytes >= 0 && *output.ContentLength != sizeBytes) {
		_ = output.Body.Close()
		return nil, fmt.Errorf("storage: object store returned an invalid range length")
	}
	if input.Range != nil {
		if output.ContentRange == nil {
			_ = output.Body.Close()
			return nil, fmt.Errorf("storage: object store omitted the requested range")
		}
		bounds, _, isPresent := strings.Cut(strings.TrimPrefix(*output.ContentRange, "bytes "), "/")
		firstByte, lastByte, hasBounds := strings.Cut(bounds, "-")
		firstOffsetBytes, firstError := strconv.ParseInt(firstByte, 10, 64)
		lastOffsetBytes, lastError := strconv.ParseInt(lastByte, 10, 64)
		if !strings.HasPrefix(*output.ContentRange, "bytes ") || !isPresent || !hasBounds || firstError != nil || lastError != nil || firstOffsetBytes != offsetBytes || lastOffsetBytes < firstOffsetBytes || lastOffsetBytes-firstOffsetBytes != *output.ContentLength-1 {
			_ = output.Body.Close()
			return nil, fmt.Errorf("storage: object store returned a different range")
		}
	}
	return &FileStream{ReadCloser: &boundedFileReader{Reader: &contextReader{ctx: ctx, reader: io.LimitReader(output.Body, *output.ContentLength)}, closer: output.Body}, SizeBytes: *output.ContentLength}, nil
}
