package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/ziyan/teanode/internal/util/bufferpool"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

// S3Settings describes the optional object store mirror.
type S3Settings struct {
	Bucket string
	Region string

	// AccessKeyID and SecretAccessKey are credentials from the configuration
	// file. Empty means fall back to CredentialsFile, and then to the default
	// credential chain, so an instance role works.
	AccessKeyID     string
	SecretAccessKey string

	// CredentialsFile is an AWS shared credentials file.
	CredentialsFile string

	// Endpoint points at an S3-compatible service that is not AWS — MinIO,
	// Ceph, Garage, a bucket behind a proxy. Empty means AWS itself.
	//
	// Anything self-hosted needs this and PathStyle together: the
	// virtual-hosted addressing AWS prefers puts the bucket in the hostname,
	// which needs a wildcard DNS entry that a MinIO on the next machine
	// does not have.
	Endpoint string

	// PathStyle addresses a bucket as endpoint/bucket rather than
	// bucket.endpoint. Implied by Endpoint unless explicitly set otherwise.
	PathStyle bool
}

type s3Storage struct {
	settings *S3Settings
	client   *s3.Client
}

func openS3(settings *S3Settings) (*s3Storage, error) {
	if settings.Bucket == "" {
		return nil, fmt.Errorf("storage: no S3 bucket configured")
	}

	options := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(settings.Region)}
	switch {
	case settings.AccessKeyID != "" && settings.SecretAccessKey != "":
		options = append(options, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(settings.AccessKeyID, settings.SecretAccessKey, "")))
	case settings.CredentialsFile != "":
		options = append(options, awsconfig.WithSharedCredentialsFiles([]string{settings.CredentialsFile}))
	}
	awsConfig, err := awsconfig.LoadDefaultConfig(context.Background(), options...)
	if err != nil {
		return nil, fmt.Errorf("storage: cannot load AWS configuration: %w", err)
	}

	client := s3.NewFromConfig(awsConfig, func(options *s3.Options) {
		if settings.Endpoint != "" {
			options.BaseEndpoint = aws.String(settings.Endpoint)
			// Path style unless the operator turned it off: a self-hosted
			// endpoint almost never has the wildcard DNS that
			// virtual-hosted addressing needs.
			options.UsePathStyle = true
		}
		if settings.PathStyle {
			options.UsePathStyle = true
		}
	})
	return &s3Storage{settings: settings, client: client}, nil
}

// putObject writes one object in a single request.
//
// A message is bounded by smtp.maxMessageSize and a picture by the upload
// limit, both a few tens of megabytes at the most, so the multipart upload
// the SDK's transfer manager exists for would never be used — and the
// manager the code used to reach for is deprecated. The body is a seekable
// reader so the SDK can send a length and retry without buffering it again.
func (self *s3Storage) putObject(ctx context.Context, key string, content []byte) error {
	_, err := self.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(self.settings.Bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(content),
	})
	return err
}

// A single PutObject cannot carry more than 5 GB, and a video can. A file over
// s3MultipartThresholdBytes goes up in pieces of s3MultipartPartBytes instead,
// each of which can be sent again on its own if it fails, which a single
// request of several gigabytes cannot. Variables so that a test can use
// small ones.
var (
	s3MultipartThresholdBytes int64 = 256 << 20
	s3MultipartPartBytes      int64 = 64 << 20
)

// s3MultipartMaximumPartCount is the most pieces S3 accepts in one upload.
const s3MultipartMaximumPartCount = 10000

// putObjectInParts writes a file as a multipart upload. The upload is aborted
// on any failure, because an unfinished one keeps its uploaded pieces, and
// is billed for them, until somebody aborts it.
func (self *s3Storage) putObjectInParts(ctx context.Context, key string, content io.ReadSeeker, sizeBytes int64) error {
	if (sizeBytes+s3MultipartPartBytes-1)/s3MultipartPartBytes > s3MultipartMaximumPartCount {
		return fmt.Errorf("storage: a file of %d bytes does not fit in %d parts of %d bytes", sizeBytes, s3MultipartMaximumPartCount, s3MultipartPartBytes)
	}
	started, err := self.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: aws.String(self.settings.Bucket), Key: aws.String(key)})
	if err != nil {
		return err
	}
	uploadId := started.UploadId
	parts, err := self.uploadParts(ctx, key, uploadId, content, sizeBytes)
	if err == nil {
		_, err = self.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
			Bucket: aws.String(self.settings.Bucket), Key: aws.String(key), UploadId: uploadId,
			MultipartUpload: &types.CompletedMultipartUpload{Parts: parts},
		})
	}
	if err != nil {
		// The caller's context may be what ended, and aborting with it would
		// leave the pieces behind.
		abortContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		if _, abortError := self.client.AbortMultipartUpload(abortContext, &s3.AbortMultipartUploadInput{Bucket: aws.String(self.settings.Bucket), Key: aws.String(key), UploadId: uploadId}); abortError != nil {
			log.Warningf("failed to abort the multipart upload of %s: %s", key, abortError)
		}
		return err
	}
	return nil
}

func (self *s3Storage) uploadParts(ctx context.Context, key string, uploadId *string, content io.ReadSeeker, sizeBytes int64) ([]types.CompletedPart, error) {
	var parts []types.CompletedPart
	for offsetBytes := int64(0); offsetBytes < sizeBytes; offsetBytes += s3MultipartPartBytes {
		partBytes := min(s3MultipartPartBytes, sizeBytes-offsetBytes)
		partNumber := int32(len(parts) + 1)
		uploaded, err := self.client.UploadPart(ctx, &s3.UploadPartInput{
			Bucket: aws.String(self.settings.Bucket), Key: aws.String(key), UploadId: uploadId,
			PartNumber: aws.Int32(partNumber), ContentLength: aws.Int64(partBytes),
			Body: &seekerSection{source: content, startBytes: offsetBytes, sizeBytes: partBytes},
		})
		if err != nil {
			return nil, err
		}
		parts = append(parts, types.CompletedPart{PartNumber: aws.Int32(partNumber), ETag: uploaded.ETag})
	}
	return parts, nil
}

// seekerSection is a window onto part of a seekable source, so that a piece
// of a large file can be sent, and sent again, without being read into
// memory. Reading the window moves the source, so only one is used at a time.
type seekerSection struct {
	source     io.ReadSeeker
	startBytes int64
	sizeBytes  int64
	readBytes  int64
}

func (self *seekerSection) Read(content []byte) (int, error) {
	if self.readBytes >= self.sizeBytes {
		return 0, io.EOF
	}
	if _, err := self.source.Seek(self.startBytes+self.readBytes, io.SeekStart); err != nil {
		return 0, err
	}
	if remainingBytes := self.sizeBytes - self.readBytes; int64(len(content)) > remainingBytes {
		content = content[:remainingBytes]
	}
	count, err := self.source.Read(content)
	self.readBytes += int64(count)
	return count, err
}

func (self *seekerSection) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += self.readBytes
	case io.SeekEnd:
		offset += self.sizeBytes
	default:
		return 0, fmt.Errorf("storage: invalid seek")
	}
	if offset < 0 || offset > self.sizeBytes {
		return 0, fmt.Errorf("storage: seek outside the part")
	}
	self.readBytes = offset
	return offset, nil
}

// getObject reads one object whole. ErrNotFound when it does not exist.
func (self *s3Storage) getObject(ctx context.Context, key, id string) ([]byte, error) {
	output, err := self.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(self.settings.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &noSuchKey) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return nil, fmt.Errorf("storage: cannot download %s: %w", id, err)
	}
	defer func() {
		_ = output.Body.Close()
	}()
	content, err := io.ReadAll(output.Body)
	if err != nil {
		return nil, fmt.Errorf("storage: cannot download %s: %w", id, err)
	}
	return content, nil
}

// keyPrefix is where messages go in the bucket, and what the sweep lists.
// Anything else an operator keeps in the same bucket is left alone.
const keyPrefix = "mail/"

func (self *s3Storage) key(id string) string {
	return keyPrefix + id + ".eml"
}

func (self *s3Storage) Put(ctx context.Context, id string, headers []string, body []byte) error {
	buffer, releaseBuffer := bufferpool.AcquireBuffer()
	defer releaseBuffer()
	if err := mailparse.Unsplit(buffer, body, headers); err != nil {
		return err
	}

	if err := self.putObject(ctx, self.key(id), buffer.Bytes()); err != nil {
		return fmt.Errorf("storage: cannot upload %s: %w", id, err)
	}
	return nil
}

func (self *s3Storage) Get(ctx context.Context, id string) ([]string, []byte, error) {
	content, err := self.getObject(ctx, self.key(id), id)
	if err != nil {
		return nil, nil, err
	}
	return mailparse.Split(bytes.NewReader(content))
}

func (self *s3Storage) Delete(ctx context.Context, id string) error {
	if _, err := self.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(self.settings.Bucket),
		Key:    aws.String(self.key(id)),
	}); err != nil {
		return fmt.Errorf("storage: cannot remove %s: %w", id, err)
	}
	return nil
}

// Sweep removes every stored message last modified before the cutoff, and
// returns how many it removed.
//
// The object store needs a sweep of its own. The filesystem sweep walks a
// local spool and only ever sees what this instance wrote; a message another
// instance handled has no local file here to expire, and without this would
// stay in the bucket forever. That is the whole difference between a mirror
// of one server's disk and a spool several servers share.
//
// Several instances sweeping the same bucket is fine. They delete the same
// objects, and deleting one twice is not an error.
func (self *s3Storage) Sweep(ctx context.Context, cutoff time.Time, keep func(context.Context, string) (bool, error)) (int, error) {
	var removed int

	paginator := s3.NewListObjectsV2Paginator(self.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(self.settings.Bucket),
		Prefix: aws.String(keyPrefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return removed, fmt.Errorf("storage: cannot list %s: %w", self.settings.Bucket, err)
		}

		var expired []types.ObjectIdentifier
		for _, object := range page.Contents {
			if object.Key == nil || object.LastModified == nil {
				continue
			}
			// The object store's own timestamp, not this machine's clock, so
			// instances whose clocks differ agree on what is old.
			if object.LastModified.After(cutoff) {
				continue
			}
			if keep != nil {
				id := strings.TrimSuffix(strings.TrimPrefix(*object.Key, keyPrefix), ".eml")
				wanted, err := keep(ctx, id)
				if err != nil {
					return removed, fmt.Errorf("storage: cannot tell whether %s is still wanted: %w", id, err)
				}
				if wanted {
					continue
				}
			}
			expired = append(expired, types.ObjectIdentifier{Key: object.Key})
		}
		if len(expired) == 0 {
			continue
		}

		// One request per page rather than per object: a bucket that has been
		// running for a year holds a lot of them.
		output, err := self.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(self.settings.Bucket),
			Delete: &types.Delete{Objects: expired, Quiet: aws.Bool(true)},
		})
		if err != nil {
			return removed, fmt.Errorf("storage: cannot remove expired messages: %w", err)
		}
		for _, failure := range output.Errors {
			log.Warningf("failed to remove %s from the object store: %s",
				aws.ToString(failure.Key), aws.ToString(failure.Message))
		}
		removed += len(expired) - len(output.Errors)
	}
	return removed, nil
}

func (self *s3Storage) Close() error {
	return nil
}
