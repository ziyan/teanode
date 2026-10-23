package computer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Codex sessions herdr was not told of.
//
// Herdr learns which Codex session runs in a pane from Codex's SessionStart
// hook, which reports the pane named in its environment. Codex can run its
// hooks in a background process it shares between sessions, its app server,
// whose environment is that of the pane it was first started from. A later
// session's report then names that first pane, which herdr refuses, and the
// pane is left with no session and its history unread.
//
// Such a session is found from the Codex running in the pane: the session
// it was resumed with, when its command line names one, or else the newest
// session started in its directory since the process started that no other
// pane holds. When two panes herdr knows no session for run Codex in the
// same directory, which session is whose is not known, and neither is
// given one: the screen is still there.

// codexSessionLookEvery is how often a pane's session is looked for again:
// a new conversation started inside the same Codex is a newer session.
const codexSessionLookEvery = 15 * time.Second

// codexSessionStartSlack is how long before the process started a session
// may say it started, for the start time being read in coarser steps than
// the session's.
const codexSessionStartSlack = 2 * time.Second

// codexSessionDayCount bounds how many days of session folders one look
// reads, for a Codex left running for weeks.
const codexSessionDayCount = 31

// processStartedAt is when a process started; a variable so a test can say.
var processStartedAt = startTimeOfProcess

// herdrFoundSession is the session found for one pane, and when.
type herdrFoundSession struct {
	processID       int
	codingSessionID string
	lookedAt        time.Time
}

// codexSessionHeader is the first line of a Codex session's history, in
// Codex's own names.
type codexSessionHeader struct {
	Type    string `json:"type"`
	Payload struct {
		ID         string          `json:"id"`
		Timestamp  time.Time       `json:"timestamp"`
		Cwd        string          `json:"cwd"`
		Originator string          `json:"originator"`
		Source     json.RawMessage `json:"source"`
	} `json:"payload"`
}

// findCodexSessions gives each Codex pane herdr knows no session for the
// session found for it. It is called with refreshes held.
func (self *Herdr) findCodexSessions(ctx context.Context, agents []*herdrAgent) {
	heldIds := map[string]bool{}
	unknownCounts := map[string]int{}
	for _, agent := range agents {
		if agent.AgentSession != nil {
			heldIds[agent.AgentSession.Value] = true
		} else if agent.Agent == CodingAgentKindCodex {
			unknownCounts[agentDirectory(agent)]++
		}
	}
	seen := map[string]bool{}
	for _, agent := range agents {
		if agent.AgentSession != nil || agent.Agent != CodingAgentKindCodex || unknownCounts[agentDirectory(agent)] > 1 {
			continue
		}
		seen[agent.PaneID] = true
		found := self.foundSessions[agent.PaneID]
		if found == nil || time.Since(found.lookedAt) >= codexSessionLookEvery {
			found = self.findCodexSession(ctx, agent.PaneID, heldIds)
			self.foundSessions[agent.PaneID] = found
		}
		if found.codingSessionID != "" && !heldIds[found.codingSessionID] {
			agent.AgentSession = &herdrAgentSession{Value: found.codingSessionID}
		}
	}
	for paneId := range self.foundSessions {
		if !seen[paneId] {
			delete(self.foundSessions, paneId)
		}
	}
}

// agentDirectory is the directory an agent runs in, as herdr says it.
func agentDirectory(agent *herdrAgent) string {
	if agent.ForegroundCwd != "" {
		return agent.ForegroundCwd
	}
	return agent.Cwd
}

// findCodexSession is the session of the Codex in one pane, or none.
func (self *Herdr) findCodexSession(ctx context.Context, paneId string, heldIds map[string]bool) *herdrFoundSession {
	found := &herdrFoundSession{lookedAt: time.Now()}
	processes, err := self.client.paneProcesses(ctx, paneId)
	if err != nil {
		return found
	}
	index := slices.IndexFunc(processes, func(process *herdrProcess) bool { return process.Name == "codex" })
	if index < 0 {
		return found
	}
	codex := processes[index]
	found.processID = codex.ProcessID
	if codingSessionId := resumedCodexSession(codex.Argv); codingSessionId != "" {
		found.codingSessionID = codingSessionId
		return found
	}
	startedAt, err := processStartedAt(codex.ProcessID)
	if err != nil || codex.Cwd == "" {
		return found
	}
	found.codingSessionID = newestCodexSessionSince(self.home, codex.Cwd, startedAt.Add(-codexSessionStartSlack), heldIds)
	return found
}

// resumedCodexSession is the session a Codex command line resumes by its
// identifier ("codex resume <id>"), or none.
func resumedCodexSession(argv []string) string {
	for index, argument := range argv {
		if argument != "resume" || index+1 >= len(argv) {
			continue
		}
		next := argv[index+1]
		if !strings.HasPrefix(next, "-") && codingSessionIDPattern.MatchString(next) {
			return next
		}
	}
	return ""
}

// newestCodexSessionSince is the newest session a person started in Codex
// in directory since a time that no pane holds already, or none. Codex
// files a session under the day it started on, by the local clock.
func newestCodexSessionSince(home, directory string, since time.Time, heldIds map[string]bool) string {
	var newest *codexSessionHeader
	day := since.Local()
	for dayIndex := 0; dayIndex < codexSessionDayCount && !day.After(time.Now()); dayIndex++ {
		folder := filepath.Join(home, ".codex", "sessions", day.Format("2006"), day.Format("01"), day.Format("02"))
		paths, _ := filepath.Glob(filepath.Join(folder, "rollout-*.jsonl"))
		for _, path := range paths {
			header := readCodexSessionHeader(path)
			if header == nil || header.Payload.Cwd != directory || header.Payload.Timestamp.Before(since) ||
				heldIds[header.Payload.ID] || !isPersonsCodexSession(header) {
				continue
			}
			if newest == nil || header.Payload.Timestamp.After(newest.Payload.Timestamp) {
				newest = header
			}
		}
		day = day.AddDate(0, 0, 1)
	}
	if newest == nil {
		return ""
	}
	return newest.Payload.ID
}

// readCodexSessionHeader is the first line of a session's history, or nil
// when it is not one. Only the first line is read.
func readCodexSessionHeader(path string) *codexSessionHeader {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()
	var header codexSessionHeader
	if json.NewDecoder(file).Decode(&header) != nil || header.Type != "session_meta" || !codingSessionIDPattern.MatchString(header.Payload.ID) {
		return nil
	}
	return &header
}

// isPersonsCodexSession is whether a session is one a person talks to in
// Codex's terminal interface, which is what runs in a pane: not a "codex
// exec" run (TeaNode's own Codex tool starts those), not a session of the
// Codex app, and not an agent a session started.
func isPersonsCodexSession(header *codexSessionHeader) bool {
	if !slices.Contains(codexTerminalOriginators, header.Payload.Originator) {
		return false
	}
	var source string
	if len(header.Payload.Source) > 0 && json.Unmarshal(header.Payload.Source, &source) != nil {
		// An object: {"subagent": ...}.
		return false
	}
	return source != "exec"
}

// codexTerminalOriginators are what Codex's terminal interface calls itself
// in a session's history, by version.
var codexTerminalOriginators = []string{"codex-tui", "codex_cli_rs"}
