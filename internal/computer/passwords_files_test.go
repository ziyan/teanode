//go:build !windows

package computer

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/passwords"
)

// The files `teanode passwords` keeps on this computer are not read, written,
// copied, moved or searched through the filesystem tool, by their own name or
// through a link, though the rest of the directory they are in is.
func TestTheFilesystemToolDoesNotOpenPasswordsFiles(t *testing.T) {
	home := t.TempDir()
	directory := filepath.Join(home, ".config", "teanode")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	unlockPath := filepath.Join(directory, passwords.UnlockFileName("work"))
	keysPath := filepath.Join(directory, passwords.KnownKeysFileName("work"))
	const invented = `{"note":"an invented Passwords file"}`
	for _, path := range []string{unlockPath, keysPath} {
		if err := os.WriteFile(path, []byte(invented), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	linkPath := filepath.Join(home, "notes.txt")
	if err := os.Symlink(unlockPath, linkPath); err != nil {
		t.Fatal(err)
	}
	ordinary := filepath.Join(directory, "profiles.json")
	if err := os.WriteFile(ordinary, []byte(`{"profiles":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	options := &Options{Home: home}

	for _, test := range []FilesystemArguments{
		{Action: "read", Path: unlockPath},
		{Action: "read", Path: "~/.config/teanode/" + passwords.KnownKeysFileName("work")},
		{Action: "read", Path: "~/.config/teanode/../teanode/" + passwords.UnlockFileName("work")},
		{Action: "read", Path: "~/notes.txt"},
		{Action: "fetch", Path: unlockPath},
		{Action: "put", Path: unlockPath, Base64: base64.StdEncoding.EncodeToString([]byte("x"))},
		{Action: "write", Path: keysPath, Content: "{}"},
		{Action: "append", Path: keysPath, Content: "{}"},
		{Action: "edit", Path: unlockPath, Find: "invented", Replace: "x"},
		{Action: "copy", Path: unlockPath, Destination: "~/copy.txt"},
		{Action: "copy", Path: ordinary, Destination: unlockPath},
		{Action: "move", Path: keysPath, Destination: "~/moved.txt"},
		{Action: "move", Path: ordinary, Destination: keysPath},
	} {
		if _, err := RunFilesystem(options, &FilesystemArguments{Action: test.Action, Path: test.Path, Destination: test.Destination, Content: test.Content, Base64: test.Base64, Find: test.Find, Replace: test.Replace}); err == nil || !strings.Contains(err.Error(), "Passwords") {
			t.Errorf("%s %s was not refused: %v", test.Action, test.Path, err)
		}
	}
	for _, name := range []string{"copy.txt", "moved.txt"} {
		if _, err := os.Stat(filepath.Join(home, name)); err == nil {
			t.Errorf("%s was made", name)
		}
	}
	if content, _ := os.ReadFile(unlockPath); string(content) != invented {
		t.Errorf("the unlock file was changed: %s", content)
	}

	// A search through the text of the directory leaves them out.
	answer, err := RunFilesystem(options, &FilesystemArguments{Action: "grep", Path: home, Pattern: "invented|profiles"})
	if err != nil {
		t.Fatal(err)
	}
	matches := answer.(map[string]any)["matches"].([]Match)
	if len(matches) != 1 || matches[0].Path != ordinary {
		t.Errorf("grep found %+v", matches)
	}
	if found, err := RunFilesystem(options, &FilesystemArguments{Action: "grep", Path: unlockPath, Pattern: "invented"}); err != nil || len(found.(map[string]any)["matches"].([]Match)) != 0 {
		t.Errorf("grep of the file itself read it: %v %v", found, err)
	}
	// The rest of the directory is the person's as before.
	if answer, err := RunFilesystem(options, &FilesystemArguments{Action: "read", Path: ordinary}); err != nil || !strings.Contains(fmt.Sprint(answer), "profiles") {
		t.Errorf("an ordinary file: %v %v", answer, err)
	}
	if _, err := RunFilesystem(options, &FilesystemArguments{Action: "list", Path: directory}); err != nil {
		t.Errorf("a listing: %v", err)
	}
}

// Every shell command and terminal the program starts for the agent carries
// the mark the Passwords commands look for, and a call cannot unset it.
func TestACommandTheAgentRunsIsMarkedForPasswords(t *testing.T) {
	t.Setenv(passwords.AgentCommandEnvironmentName, "")
	result, err := RunShell(context.Background(), &Options{Home: t.TempDir()}, nil, &ShellArguments{
		Command:     `printf %s "$` + passwords.AgentCommandEnvironmentName + `"`,
		Environment: map[string]string{passwords.AgentCommandEnvironmentName: "", "INVENTED_NAME": "x"},
	})
	if err != nil || result.Stdout != "1" {
		t.Fatalf("the shell command saw %q (%v)", result.Stdout, err)
	}

	var mutex sync.Mutex
	var said strings.Builder
	finished := make(chan int, 1)
	held := newSessions()
	defer held.closeAll()
	if _, err := held.start(context.Background(), withDefaults(&Options{Home: t.TempDir()}), &SessionStartArguments{
		Session: "marked", Kind: "stdio", Command: "sh", Arguments: []string{"-c", `printf %s "$` + passwords.AgentCommandEnvironmentName + `"`},
		Environment: map[string]string{passwords.AgentCommandEnvironmentName: ""},
	}, func(_, _, data string) {
		decoded, _ := base64.StdEncoding.DecodeString(data)
		mutex.Lock()
		said.Write(decoded)
		mutex.Unlock()
	}, func(_ string, code int) { finished <- code }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("the session never ended")
	}
	mutex.Lock()
	defer mutex.Unlock()
	if said.String() != "1" {
		t.Errorf("the session saw %q", said.String())
	}
}
