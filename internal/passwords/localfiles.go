package passwords

import (
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
	localFileSuffix     = ".json"
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

// IsLocalFileName says a file name is one of the two, whatever the profile.
func IsLocalFileName(name string) bool {
	name = strings.ToLower(name)
	return strings.HasSuffix(name, localFileSuffix) &&
		(strings.HasPrefix(name, unlockFilePrefix) || strings.HasPrefix(name, knownKeysFilePrefix))
}

// IsLocalFile says a path is one of those files: by its own name and by the
// name of the file it is a link to, so that a link under another name does not
// open it. A file with such a name anywhere counts; it does no harm to refuse
// one that is not the real thing.
func IsLocalFile(path string) bool {
	if IsLocalFileName(filepath.Base(path)) {
		return true
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil && IsLocalFileName(filepath.Base(resolved)) {
		return true
	}
	return false
}

// IsAgentCommand says the program is running a command for the agent (see
// AgentCommandEnvironmentName).
func IsAgentCommand() bool {
	return os.Getenv(AgentCommandEnvironmentName) != ""
}
