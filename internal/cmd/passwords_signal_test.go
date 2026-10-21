//go:build !windows

package cmd

import (
	"os"
	"syscall"
	"testing"
	"time"
)

// Asking the command to end clears the clipboard at once.
func TestPasswordsCopyClearsOnTermination(test *testing.T) {
	useConfigurationDirectory(test)
	clipboardPath, _ := fakeClipboard(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.mustRun(testToken, testPassphrase+"\n", "unlock", "--passphrase-stdin")
	fake.mustRun(testToken, "", "add", "example.com", "--password", "clipboard-secret-example")

	go func() {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if content, _ := os.ReadFile(clipboardPath); string(content) == "clipboard-secret-example" {
				// The command caught the signals before it copied.
				_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	started := time.Now()
	fake.mustRun(testToken, "", "copy", "example.com", "--clear-after", "60")
	if time.Since(started) > 30*time.Second {
		test.Error("the command waited for the time to pass")
	}
	if content, _ := os.ReadFile(clipboardPath); len(content) != 0 {
		test.Errorf("the secret was left on the clipboard: %q", content)
	}
}
