// Package computer is the person's own computer as their agent reaches it:
// the program `teanode computer` runs there, answering the agent's requests
// over a websocket to the server, as the person, anywhere on the machine,
// and running what the server sends. The one rule over a shell command is
// the server's: it says whether the person is asked first, on a
// confirmation card, before the request is sent at all. Nothing is refused
// outright — it is the person's machine, and their yes is the last word.
package computer

import (
	"regexp"
	"strings"
)

// Action is what the rule says about a command.
type Action string

const (
	// ActionAllow runs without a word from the person.
	ActionAllow Action = "allow"
	// ActionAsk runs after the person says yes.
	ActionAsk Action = "ask"
)

// Decision is the rule's answer, with why.
type Decision struct {
	Action Action
	Reason string
}

type pattern struct {
	expression *regexp.Regexp
	reason     string
}

// A word at the start of a command, or after a separator; never a word
// inside another.
const boundary = `(?:^|[;&|(\s])`

// A block device by its usual names.
const device = `/dev/(?:sd[a-z]|vd[a-z]|xvd[a-z]|nvme\d+n\d+|mmcblk\d+|disk\d+|mapper/[\w.-]+)`

// grave is what takes the machine itself: asked with the reason spelled
// out, so the person knows what they are saying yes to.
var grave = []pattern{
	{regexp.MustCompile(boundary + `rm\s+(?:[^;&|]*\s)?-[a-zA-Z]*[rR][a-zA-Z]*\s+(?:[^;&|]*\s)?/+(?:\*|\s|$|[;&|])`), "removing the root of the filesystem"},
	{regexp.MustCompile(boundary + `rm\s+(?:[^;&|]*\s)?-[a-zA-Z]*[rR][a-zA-Z]*\s+(?:[^;&|]*\s)?(?:~|\$HOME|/home/[\w.-]+|/Users/[\w.-]+)/?(?:\*|\s|$|[;&|])`), "removing the whole home directory"},
	{regexp.MustCompile(boundary + `mkfs(?:\.\w+)?\s`), "formatting a disk"},
	{regexp.MustCompile(boundary + `dd\s+[^;&|]*of=` + device), "writing over a disk"},
	{regexp.MustCompile(boundary + `shred\s+[^;&|]*` + device), "shredding a disk"},
	{regexp.MustCompile(`>\s*` + device), "writing over a disk"},
	{regexp.MustCompile(`:\s*\(\s*\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:`), "a fork bomb"},
	{regexp.MustCompile(boundary + `chmod\s+(?:-R\s+)?[0-7]*\s+/+(?:\s|$|[;&|])`), "changing the permissions of the root"},
}

// asked is what changes the machine or reaches out of it: the person says
// yes first.
var asked = []pattern{
	{regexp.MustCompile(boundary + `(?:rm|rmdir|unlink)\s`), "removes files"},
	{regexp.MustCompile(boundary + `(?:mv|rename)\s`), "moves files"},
	{regexp.MustCompile(boundary + `(?:sudo|doas|su)\s`), "runs as another user"},
	{regexp.MustCompile(boundary + `(?:chmod|chown|chgrp)\s`), "changes who may reach files"},
	{regexp.MustCompile(boundary + `(?:kill|pkill|killall)\s`), "stops a process"},
	{regexp.MustCompile(boundary + `(?:shutdown|reboot|halt|poweroff)(?:\s|$)`), "turns the computer off"},
	{regexp.MustCompile(boundary + `(?:apt|apt-get|dnf|yum|pacman|zypper|brew|snap|flatpak|port)\s+(?:[^;&|]*\s)?(?:install|remove|purge|uninstall|upgrade|update|autoremove)(?:\s|$)`), "installs or removes software"},
	{regexp.MustCompile(boundary + `(?:pip3?|pipx|npm|pnpm|yarn|gem|cargo|go)\s+(?:[^;&|]*\s)?(?:install|uninstall|remove|add)(?:\s|$)`), "installs or removes software"},
	{regexp.MustCompile(boundary + `git\s+(?:[^;&|]*\s)?(?:push|reset\s+--hard|clean\s+-[a-zA-Z]*[fdx]|checkout\s+--\s|restore\s|branch\s+-[dD]|rebase)(?:\s|$)`), "changes a git history or a remote"},
	{regexp.MustCompile(`(?:curl|wget)\s[^;&|]*\|\s*(?:sudo\s+)?(?:ba|z|da|k)?sh(?:\s|$)`), "runs what it downloads"},
	{regexp.MustCompile(boundary + `(?:ssh|scp|sftp|rsync|ftp|nc|ncat|telnet)\s`), "reaches another machine"},
	{regexp.MustCompile(boundary + `(?:crontab|systemctl|launchctl|service|sc)\s`), "changes what runs on its own"},
	{regexp.MustCompile(boundary + `(?:docker|podman)\s+(?:[^;&|]*\s)?(?:rm|rmi|prune|kill|stop|down)(?:\s|$)`), "removes or stops containers"},
	{regexp.MustCompile(boundary + `(?:dd|truncate|shred|wipefs|fdisk|parted|diskutil)\s`), "writes over storage"},
	{regexp.MustCompile(boundary + `(?:mkfs|mount|umount|swapoff)\s`), "changes the disks"},
	{regexp.MustCompile(boundary + `(?:useradd|userdel|usermod|passwd|adduser|deluser)\s`), "changes accounts"},
	{regexp.MustCompile(`\s>{1,2}\s*(?:~|\$HOME|/)`), "writes a file by its whole path"},
	{regexp.MustCompile(boundary + `(?:defaults\s+write|reg\s+add|regedit|osascript|xdotool)\s`), "changes the system's settings"},
}

// Classify says whether a command runs or asks first, and why. It
// reads the command as text, so a command that hides what it does — an
// alias, a script, a variable — is judged by what it shows; that is why
// the person is asked for the dangerous shapes, and why nothing here is a
// substitute for their eyes on the conversation. Nothing is refused: the
// person's yes is the last word on their own machine.
func Classify(command string) Decision {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return Decision{Action: ActionAllow}
	}
	for _, rule := range grave {
		if rule.expression.MatchString(trimmed) {
			return Decision{Action: ActionAsk, Reason: rule.reason}
		}
	}
	for _, rule := range asked {
		if rule.expression.MatchString(trimmed) {
			return Decision{Action: ActionAsk, Reason: rule.reason}
		}
	}
	return Decision{Action: ActionAllow}
}

// PathAsks says whether a write to a path is one the person is asked
// about first: what a shell reads when it starts, keys, and what the
// machine runs on its own — the same shapes the command rule asks for.
func PathAsks(path string) bool {
	lower := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(path), "\\", "/"))
	base := lower
	if index := strings.LastIndex(lower, "/"); index >= 0 {
		base = lower[index+1:]
	}
	for _, name := range []string{".bashrc", ".bash_profile", ".profile", ".zshrc", ".zprofile", ".zshenv", ".xinitrc", ".xsession", "authorized_keys", "known_hosts", "crontab", "sudoers", "hosts", "passwd", "shadow"} {
		if base == name {
			return true
		}
	}
	for _, part := range []string{"/.ssh/", "/.gnupg/", "/.config/autostart/", "/.config/systemd/", "/library/launchagents/", "/library/launchdaemons/", "/etc/", "/.git/hooks/"} {
		if strings.Contains(lower+"/", part) {
			return true
		}
	}
	return false
}
