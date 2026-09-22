package storage

import (
	"context"
	"testing"
)

func TestStorageRejectsInvalidIdentifiers(test *testing.T) {
	for _, hasDirectory := range []bool{true, false} {
		var spool Storage
		if hasDirectory {
			opened, err := Open(&Settings{Directory: test.TempDir()})
			if err != nil {
				test.Fatal(err)
			}
			test.Cleanup(func() { _ = opened.Close() })
			spool = opened
		} else {
			// No client is needed: invalid keys must never reach the mirror.
			spool = &filesystem{settings: &Settings{}}
		}
		for _, identifier := range []string{"", "x", "../mail", "one/two", "one\\two", "one.two", "one\x00two"} {
			ctx := context.Background()
			if err := spool.Put(ctx, identifier, nil, nil); err == nil {
				test.Errorf("Put accepted %q", identifier)
			}
			if _, _, err := spool.Get(ctx, identifier); err == nil {
				test.Errorf("Get accepted %q", identifier)
			}
			if err := spool.Delete(ctx, identifier); err == nil {
				test.Errorf("Delete accepted %q", identifier)
			}
			if err := spool.PutFile(ctx, identifier, nil); err == nil {
				test.Errorf("PutFile accepted %q", identifier)
			}
			if _, err := spool.GetFile(ctx, identifier); err == nil {
				test.Errorf("GetFile accepted %q", identifier)
			}
			if err := spool.DeleteFile(ctx, identifier); err == nil {
				test.Errorf("DeleteFile accepted %q", identifier)
			}
		}
	}
}

func TestStorageAcceptsTwoCharacterIdentifiers(test *testing.T) {
	spool, err := Open(&Settings{Directory: test.TempDir()})
	if err != nil {
		test.Fatal(err)
	}
	defer func() { _ = spool.Close() }()
	ctx := context.Background()
	if err := spool.Put(ctx, "ab", []string{"Subject: fixture"}, []byte("body")); err != nil {
		test.Fatal(err)
	}
	if _, _, err := spool.Get(ctx, "ab"); err != nil {
		test.Fatal(err)
	}
	if err := spool.Delete(ctx, "ab"); err != nil {
		test.Fatal(err)
	}
	if err := spool.PutFile(ctx, "ab", []byte("file")); err != nil {
		test.Fatal(err)
	}
	if _, err := spool.GetFile(ctx, "ab"); err != nil {
		test.Fatal(err)
	}
	if err := spool.DeleteFile(ctx, "ab"); err != nil {
		test.Fatal(err)
	}
}
