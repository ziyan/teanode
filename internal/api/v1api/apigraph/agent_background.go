package apigraph

import (
	"context"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
)

// Background commands: what the agent left running on the person's
// computers, for the person to see and to stop. The program on the
// computer holds them; each of these asks it.

// AgentBackgroundQuery reads them.
type AgentBackgroundQuery interface {
	// The commands running, or ended lately, in the background on the
	// caller's attached computers, newest first; only the ones a
	// conversation started when it is named. Without their output. Needs
	// agent:use.
	ListAgentBackgroundCommands(ctx context.Context, arguments ListAgentBackgroundCommandsArguments) ([]*AgentBackgroundCommandView, error)

	// One of them with the last of its output: tailBytes of each stream,
	// 64 KiB by default. Needs agent:use.
	ReadAgentBackgroundCommand(ctx context.Context, arguments ReadAgentBackgroundCommandArguments) (*AgentBackgroundCommandView, error)
}

// AgentBackgroundMutation stops them.
type AgentBackgroundMutation interface {
	// Stop one. The agent that started it is woken to hear that it
	// ended, as it is of any ending it did not ask for. Needs agent:use.
	StopAgentBackgroundCommand(ctx context.Context, arguments StopAgentBackgroundCommandArguments) (*AgentBackgroundCommandView, error)
}

// ListAgentBackgroundCommandsArguments may name a conversation.
type ListAgentBackgroundCommandsArguments struct {
	ConversationID string `json:"conversationId" graphapi:"nullable"`
}

// ReadAgentBackgroundCommandArguments name one, and how much output.
type ReadAgentBackgroundCommandArguments struct {
	Computer  string `json:"computer"`
	ID        string `json:"id"`
	TailBytes int    `json:"tailBytes" graphapi:"nullable"`
}

// StopAgentBackgroundCommandArguments name one.
type StopAgentBackgroundCommandArguments struct {
	Computer string `json:"computer"`
	ID       string `json:"id"`
}

// AgentBackgroundCommandView is one background command.
type AgentBackgroundCommandView struct {
	Computer       string     `json:"computer"`
	ID             string     `json:"id"`
	Command        string     `json:"command"`
	Directory      string     `json:"directory"`
	ConversationID string     `json:"conversationId"`
	StartedAt      time.Time  `json:"startedAt"`
	EndedAt        *time.Time `json:"endedAt" graphapi:"nullable"`
	IsRunning      bool       `json:"isRunning"`
	ExitCode       int        `json:"exitCode"`
	// StopReason is "stopped" or "lifetime" when it did not end by
	// itself, and empty otherwise.
	StopReason string `json:"stopReason"`

	// The output, when it was read: the last of each stream, whether that
	// is less than all of it, and how much each wrote in all.
	Stdout            string `json:"stdout"`
	Stderr            string `json:"stderr"`
	IsStdoutTruncated bool   `json:"isStdoutTruncated"`
	IsStderrTruncated bool   `json:"isStderrTruncated"`
	StdoutByteCount   int    `json:"stdoutByteCount"`
	StderrByteCount   int    `json:"stderrByteCount"`
}

// backgroundReadBytes is how much of each stream a read gives by default,
// and backgroundReadMostBytes the most it gives.
const (
	backgroundReadBytes     = 64 << 10
	backgroundReadMostBytes = 256 << 10
)

func (self *graph) ListAgentBackgroundCommands(ctx context.Context, arguments ListAgentBackgroundCommandsArguments) ([]*AgentBackgroundCommandView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return []*AgentBackgroundCommandView{}, nil
	}
	commands, err := worker.BackgroundCommands(ctx, found.ID)
	if err != nil {
		return nil, err
	}
	conversationId := strings.TrimSpace(arguments.ConversationID)
	views := []*AgentBackgroundCommandView{}
	for _, command := range commands {
		// Only this agent's: a program on the person's computer is theirs,
		// and what another server's agent left there is not shown here.
		if command.Origin.AgentID != found.ID {
			continue
		}
		if conversationId != "" && command.Origin.ConversationID != conversationId {
			continue
		}
		views = append(views, backgroundCommandView(command))
	}
	return views, nil
}

func (self *graph) ReadAgentBackgroundCommand(ctx context.Context, arguments ReadAgentBackgroundCommandArguments) (*AgentBackgroundCommandView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	tailBytes := arguments.TailBytes
	if tailBytes <= 0 {
		tailBytes = backgroundReadBytes
	}
	command, err := worker.ReadBackgroundCommand(ctx, found.ID, arguments.Computer, arguments.ID, min(tailBytes, backgroundReadMostBytes))
	if err != nil {
		return nil, err
	}
	return backgroundCommandView(command), nil
}

func (self *graph) StopAgentBackgroundCommand(ctx context.Context, arguments StopAgentBackgroundCommandArguments) (*AgentBackgroundCommandView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	command, err := worker.StopBackgroundCommand(ctx, found.ID, arguments.Computer, arguments.ID)
	if err != nil {
		return nil, err
	}
	return backgroundCommandView(command), nil
}

func backgroundCommandView(command *agent.BackgroundCommand) *AgentBackgroundCommandView {
	return &AgentBackgroundCommandView{
		Computer: command.Computer, ID: command.ID, Command: command.Command, Directory: command.Directory,
		ConversationID: command.Origin.ConversationID, StartedAt: command.StartedAt, EndedAt: command.EndedAt,
		IsRunning: command.IsRunning, ExitCode: command.ExitCode, StopReason: command.StopReason,
		Stdout: command.Stdout, Stderr: command.Stderr,
		IsStdoutTruncated: command.IsStdoutTruncated, IsStderrTruncated: command.IsStderrTruncated,
		StdoutByteCount: int(command.StdoutByteCount), StderrByteCount: int(command.StderrByteCount),
	}
}
