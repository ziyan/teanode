package upgrade

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fakeApplyLock stands in for the database's advisory lock: one holder at a
// time, and a record of how many times it was let go.
type fakeApplyLock struct {
	mutex         sync.Mutex
	isHeld        bool
	releasedCount int
}

func (self *fakeApplyLock) take(context.Context) (func(), bool, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.isHeld {
		return nil, false, nil
	}
	self.isHeld = true
	return func() {
		self.mutex.Lock()
		defer self.mutex.Unlock()
		self.isHeld = false
		self.releasedCount++
	}, true, nil
}

func (self *fakeApplyLock) held() (bool, int) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.isHeld, self.releasedCount
}

func writeOldBinary(t *testing.T) string {
	t.Helper()
	executable := filepath.Join(t.TempDir(), "teanode")
	if err := os.WriteFile(executable, []byte("the old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	return executable
}

// An instance that finds another installing turns away without touching the
// binary or saying on its status that it failed.
func TestApplyWaitsForAnotherInstanceInstalling(t *testing.T) {
	executable := writeOldBinary(t)
	newBinary := []byte("the new binary")
	server := releaseServer(t, "0.9.0", newBinary, sha256Of(newBinary))
	defer server.Close()

	lock := &fakeApplyLock{}
	_, _, _ = lock.take(context.Background())
	manager := testManager(t, executable, server.URL, server.Client(), "0.1.0", func() {})
	manager.applyLock = lock.take

	err := manager.Apply(context.Background(), "")
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("Apply: %v, want a refusal because another instance is installing", err)
	}
	if kept, _ := os.ReadFile(executable); string(kept) != "the old binary" {
		t.Errorf("the binary was touched: %q", kept)
	}
	if status := manager.Status(); status.Upgrading || status.Error != "" {
		t.Errorf("the turned away instance says something: %+v", status)
	}
}

// A failed install lets the lock go, so that the next instance can try; a
// successful one keeps it until the process has gone.
func TestApplyReleasesTheLockOnlyWhenItFailed(t *testing.T) {
	executable := writeOldBinary(t)
	badServer := releaseServer(t, "0.9.0", []byte("not what was promised"), sha256Of([]byte("the promise")))
	defer badServer.Close()

	lock := &fakeApplyLock{}
	manager := testManager(t, executable, badServer.URL, badServer.Client(), "0.1.0", func() {})
	manager.applyLock = lock.take
	if err := manager.Apply(context.Background(), ""); err == nil {
		t.Fatal("a binary that does not match its checksum was installed")
	}
	if isHeld, releasedCount := lock.held(); isHeld || releasedCount != 1 {
		t.Fatalf("after a failure the lock is held=%v released=%d", isHeld, releasedCount)
	}

	newBinary := []byte("the new binary")
	goodServer := releaseServer(t, "0.9.0", newBinary, sha256Of(newBinary))
	defer goodServer.Close()
	restarted := make(chan struct{})
	manager = testManager(t, executable, goodServer.URL, goodServer.Client(), "0.1.0", func() { close(restarted) })
	manager.applyLock = lock.take
	if err := manager.Apply(context.Background(), ""); err != nil {
		t.Fatalf("Apply: %s", err)
	}
	select {
	case <-restarted:
	case <-time.After(5 * time.Second):
		t.Error("no restart was requested")
	}
	if isHeld, releasedCount := lock.held(); !isHeld || releasedCount != 1 {
		t.Fatalf("after a success the lock is held=%v released=%d, want held until the process exits", isHeld, releasedCount)
	}
}
