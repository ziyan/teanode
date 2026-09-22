package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type failingObjectStore struct {
	Storage
	operationError error
}

func (self *failingObjectStore) Put(ctx context.Context, id string, headers []string, body []byte) error {
	if self.operationError != nil {
		return self.operationError
	}
	return self.Storage.Put(ctx, id, headers, body)
}

func (self *failingObjectStore) PutFile(ctx context.Context, id string, content []byte) error {
	if self.operationError != nil {
		return self.operationError
	}
	return self.Storage.PutFile(ctx, id, content)
}

func (self *failingObjectStore) Sweep(context.Context, time.Time, func(context.Context, string) (bool, error)) (int, error) {
	return 0, nil
}

func TestStorageModesRejectInvalidSettingsBeforeOpeningObjectStore(test *testing.T) {
	for _, settings := range []*Settings{
		{Mode: "unknown"},
		{Mode: "local", S3: &S3Settings{}},
		{Mode: "shared"},
		{Mode: "shared", Directory: test.TempDir(), S3: &S3Settings{}},
	} {
		if spool, err := Open(settings); err == nil {
			_ = spool.Close()
			test.Errorf("accepted invalid settings: %+v", settings)
		}
	}
}

func TestStorageModeOutageAndCrossInstanceRead(test *testing.T) {
	ctx := context.Background()
	backingStore, err := Open(&Settings{Directory: test.TempDir(), Mode: "local"})
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() { _ = backingStore.Close() })
	objectStore := &failingObjectStore{Storage: backingStore}
	firstInstance := &filesystem{settings: &Settings{Mode: "shared"}, mirror: objectStore}
	secondInstance := &filesystem{settings: &Settings{Mode: "shared"}, mirror: objectStore}
	localInstance := &filesystem{settings: &Settings{Mode: "local", Directory: test.TempDir()}, mirror: objectStore}
	objectStore.operationError = errors.New("object store unavailable")
	for _, operation := range []struct {
		operationName string
		put           func(Storage, string) error
		get           func(Storage, string) ([]byte, error)
	}{
		{"message", func(spool Storage, id string) error {
			return spool.Put(ctx, id, []string{"Subject: fixture"}, []byte("stored bytes"))
		}, func(spool Storage, id string) ([]byte, error) {
			_, body, err := spool.Get(ctx, id)
			return body, err
		}},
		{"file", func(spool Storage, id string) error {
			return spool.PutFile(ctx, id, []byte("stored bytes"))
		}, func(spool Storage, id string) ([]byte, error) {
			return spool.GetFile(ctx, id)
		}},
	} {
		test.Run(operation.operationName, func(test *testing.T) {
			objectStore.operationError = errors.New("object store unavailable")
			if err := operation.put(firstInstance, "failed-write"); !errors.Is(err, objectStore.operationError) {
				test.Fatalf("shared write did not return outage: %v", err)
			}
			if _, err := operation.get(secondInstance, "failed-write"); !errors.Is(err, ErrNotFound) {
				test.Fatalf("failed shared write became readable: %v", err)
			}
			if err := operation.put(localInstance, "local-write"); err != nil {
				test.Fatalf("mirror failure rejected local write: %v", err)
			}
			if content, err := operation.get(localInstance, "local-write"); err != nil || string(content) != "stored bytes" {
				test.Fatalf("local copy unavailable: %q, %v", content, err)
			}
			if _, err := operation.get(secondInstance, "local-write"); !errors.Is(err, ErrNotFound) {
				test.Fatalf("failed mirror unexpectedly shared the local write: %v", err)
			}
			objectStore.operationError = nil
			if err := operation.put(firstInstance, "shared-write"); err != nil {
				test.Fatal(err)
			}
			if content, err := operation.get(secondInstance, "shared-write"); err != nil || string(content) != "stored bytes" {
				test.Fatalf("second instance cannot read shared write: %q, %v", content, err)
			}
		})
	}
}

func TestLocalFileWritesCreatePrivateDirectoriesAndReplaceAtomically(test *testing.T) {
	filename := filepath.Join(test.TempDir(), "nested", "shard", "fixture")
	for _, content := range []string{"original", "replacement"} {
		if err := writeLocalFile(filename, []byte(content)); err != nil {
			test.Fatal(err)
		}
		stored, err := os.ReadFile(filename)
		if err != nil || string(stored) != content {
			test.Fatalf("stored file = %q, %v", stored, err)
		}
	}
	for path, permissions := range map[string]os.FileMode{filename: 0o600, filepath.Dir(filename): 0o700} {
		entry, err := os.Stat(path)
		if err != nil {
			test.Fatal(err)
		}
		if entry.Mode().Perm() != permissions {
			test.Fatalf("permissions = %o, want %o", entry.Mode().Perm(), permissions)
		}
	}
	entries, err := os.ReadDir(filepath.Dir(filename))
	if err != nil || len(entries) != 1 {
		test.Fatalf("temporary files remained: %v, %v", entries, err)
	}
	if err := writeLocalFile(filepath.Join(filename, "child"), nil); err == nil {
		test.Fatal("accepted a regular file as a directory")
	}
	if err := writeLocalFile(filepath.Dir(filename), []byte("cannot replace directory")); err == nil {
		test.Fatal("replaced a populated directory with a file")
	}
	entries, err = os.ReadDir(filepath.Dir(filepath.Dir(filename)))
	if err != nil || len(entries) != 1 {
		test.Fatalf("failed rename left temporary files: %v, %v", entries, err)
	}
}
