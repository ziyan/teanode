package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
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
	settings   *S3Settings
	uploader   *manager.Uploader
	downloader *manager.Downloader
	client     *s3.Client
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
	return &s3Storage{
		settings:   settings,
		client:     client,
		uploader:   manager.NewUploader(client),
		downloader: manager.NewDownloader(client),
	}, nil
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

	if _, err := self.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(self.settings.Bucket),
		Key:    aws.String(self.key(id)),
		Body:   buffer,
	}); err != nil {
		return fmt.Errorf("storage: cannot upload %s: %w", id, err)
	}
	return nil
}

func (self *s3Storage) Get(ctx context.Context, id string) ([]string, []byte, error) {
	writeAtBuffer := manager.NewWriteAtBuffer(nil)
	if _, err := self.downloader.Download(ctx, writeAtBuffer, &s3.GetObjectInput{
		Bucket: aws.String(self.settings.Bucket),
		Key:    aws.String(self.key(id)),
	}); err != nil {
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &noSuchKey) {
			return nil, nil, fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return nil, nil, fmt.Errorf("storage: cannot download %s: %w", id, err)
	}
	return mailparse.Split(bytes.NewReader(writeAtBuffer.Bytes()))
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
func (self *s3Storage) Sweep(ctx context.Context, cutoff time.Time) (int, error) {
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
