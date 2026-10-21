package passwords

import (
	"encoding/csv"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf16"
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

// exportItemsPattern finds the "items" key of the JSON export with its array,
// and the others find the keys that tell an export from other JSON.
var (
	exportItemsPattern    = regexp.MustCompile(`"items"\s*:\s*\[`)
	exportItemKindPattern = regexp.MustCompile(`"itemKind"\s*:`)
	exportVersionPattern  = regexp.MustCompile(`"version"\s*:`)
	exportTimePattern     = regexp.MustCompile(`"exportedAt"\s*:`)
)

// HasExportHeader says the first bytes of a file are those of a Passwords
// export: the JSON export (an object with an "items" array, whatever order
// its keys are in, with either an item's "itemKind" or both "version" and
// "exportedAt" in the part looked at), or a CSV file whose header row is one
// that `import csv` reads (Chrome, Apple Passwords or Bitwarden). A file that
// begins so holds the person's items in the clear whatever it is called.
func HasExportHeader(head []byte) bool {
	text := exportHeadText(head)
	text = strings.TrimPrefix(text, "\ufeff")
	text = strings.TrimLeft(text, " \t\r\n")
	if strings.HasPrefix(text, "{") {
		if !exportItemsPattern.MatchString(text) {
			return false
		}
		return exportItemKindPattern.MatchString(text) ||
			(exportVersionPattern.MatchString(text) && exportTimePattern.MatchString(text))
	}
	return hasPasswordsCSVHeader(text)
}

// exportHeadText is the start of a file as text. A file a shell wrote as
// UTF-16 (PowerShell's redirection does) is decoded first.
func exportHeadText(head []byte) string {
	if len(head) >= 2 && ((head[0] == 0xff && head[1] == 0xfe) || (head[0] == 0xfe && head[1] == 0xff)) {
		isBigEndian := head[0] == 0xfe
		units := make([]uint16, 0, len(head)/2)
		for index := 2; index+1 < len(head); index += 2 {
			if isBigEndian {
				units = append(units, uint16(head[index])<<8|uint16(head[index+1]))
			} else {
				units = append(units, uint16(head[index+1])<<8|uint16(head[index]))
			}
		}
		return string(utf16.Decode(units))
	}
	return string(head)
}

// hasPasswordsCSVHeader says the first line of the text is the header row of a
// CSV file that `import csv` reads.
func hasPasswordsCSVHeader(text string) bool {
	line, _, _ := strings.Cut(text, "\n")
	header, err := csv.NewReader(strings.NewReader(strings.TrimRight(line, "\r"))).Read()
	if err != nil {
		return false
	}
	_, err = DetectCSVFormat(header)
	return err == nil
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
