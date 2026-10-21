package passwords

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

// The files `teanode passwords` keeps on a computer, and the mark on the
// commands the agent runs for the person. They are named in one place so that
// the command line that writes them and the computer program that refuses to
// let the agent read them agree.

// AgentCommandEnvironmentName is the environment variable the computer program
// sets, to "1", on every shell command and terminal session it starts for the
// agent. The Passwords commands refuse to open Passwords when it is set:
// the agent's commands cannot open Passwords on this computer.
const AgentCommandEnvironmentName = "TEANODE_AGENT_COMMAND"

// AgentCommandRefusalWords is what a Passwords command says when the agent runs
// it.
const AgentCommandRefusalWords = "the agent's commands cannot open Passwords on this computer"

const (
	unlockFilePrefix    = "passwords-unlock-"
	knownKeysFilePrefix = "passwords-keys-"
	exportFilePrefix    = "passwords-export-"
	localFileSuffix     = ".json"
	exportCSVSuffix     = ".csv"
)

// UnlockFileName is the name of the file `teanode passwords unlock` keeps the
// opened keys in, for a profile (already made safe to use in a file name).
func UnlockFileName(profile string) string {
	return unlockFilePrefix + profile + localFileSuffix
}

// KnownKeysFileName is the name of the file that holds the public keys of the
// people a profile shared with or received from.
func KnownKeysFileName(profile string) string {
	return knownKeysFilePrefix + profile + localFileSuffix
}

// ExportFileName is the name `teanode passwords export` gives a file when it
// is not asked for one: a name IsLocalFileName refuses, so that the computer
// program's file tool does not read the plaintext it holds. The stamp is a time
// already made safe to use in a file name, and the format is "json" or "csv".
func ExportFileName(stamp, format string) string {
	if format == "csv" {
		return exportFilePrefix + stamp + exportCSVSuffix
	}
	return exportFilePrefix + stamp + localFileSuffix
}

// IsLocalFileName says a file name is one of the three, whatever the profile or
// the time: the opened keys, the known keys, or an export of every item in the
// clear.
func IsLocalFileName(name string) bool {
	name = strings.ToLower(name)
	if strings.HasPrefix(name, exportFilePrefix) {
		return strings.HasSuffix(name, localFileSuffix) || strings.HasSuffix(name, exportCSVSuffix)
	}
	return strings.HasSuffix(name, localFileSuffix) &&
		(strings.HasPrefix(name, unlockFilePrefix) || strings.HasPrefix(name, knownKeysFilePrefix))
}

// exportHeaderLength is how much of the start of a file is looked at for an
// export's header.
const exportHeaderLength = 512

// exportCSVHeaderStart is how the CSV export begins: the columns Bitwarden
// uses, which `import csv` reads back, and which Bitwarden's own plaintext
// export has too.
const exportCSVHeaderStart = "folder,favorite,type,name,notes,fields,reprompt,login_uri,login_username,"

// HasExportHeader says the first bytes of a file are those of a Passwords
// export: the JSON export (an object whose first two keys are "version" and
// "exportedAt", which the command line and the dashboard both write) or the CSV
// one. A file that begins so holds the person's items in the clear whatever it
// is called.
func HasExportHeader(head []byte) bool {
	text := strings.TrimPrefix(string(head), "\ufeff")
	text = strings.TrimLeft(text, " \t\r\n")
	if strings.HasPrefix(text, exportCSVHeaderStart) {
		return true
	}
	if !strings.HasPrefix(text, "{") {
		return false
	}
	rest := strings.TrimLeft(text[1:], " \t\r\n")
	if !strings.HasPrefix(rest, `"version"`) {
		return false
	}
	return strings.Contains(rest, `"exportedAt"`)
}

// hasExportHeaderFile says the file at the path is a regular file that begins
// as a Passwords export does.
func hasExportHeaderFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()
	head := make([]byte, exportHeaderLength)
	count, _ := io.ReadFull(file, head)
	return HasExportHeader(head[:count])
}

// IsLocalFile says a path is one of those files: by its own name and by the
// name of the file it is a link to, so that a link under another name does not
// open it, and, for a file that exists, by its first bytes, so that an export
// the person named as they liked is not read either. A file with such a name
// anywhere counts; it does no harm to refuse one that is not the real thing.
func IsLocalFile(path string) bool {
	if IsLocalFileName(filepath.Base(path)) {
		return true
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil && IsLocalFileName(filepath.Base(resolved)) {
		return true
	}
	return hasExportHeaderFile(path)
}

// IsAgentCommand says the program is running a command for the agent (see
// AgentCommandEnvironmentName).
func IsAgentCommand() bool {
	return os.Getenv(AgentCommandEnvironmentName) != ""
}
