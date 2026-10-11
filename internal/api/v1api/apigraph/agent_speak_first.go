package apigraph

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
)

// AgentSpeakFirstMutation is what the dashboard tells the agent about the
// person being there, and how the person asks it to speak first now.
type AgentSpeakFirstMutation interface {
	// Say whether the caller has the dashboard or the app in front of them:
	// sent by every open tab and the phone app each minute and whenever it
	// is shown or hidden, each naming itself. The agent starts a
	// conversation on its own only with somebody who is there to read it,
	// and tells their phone what nobody is there to see. Needs agent:use.
	ReportAgentPresence(ctx context.Context, arguments ReportAgentPresenceArguments) (bool, error)

	// Have the agent start a conversation now, for a reason: onboarding,
	// phone_tour, memory_check or idea. Outside the rules about when it may, which are
	// for the times nobody asked. Needs agent:use.
	SpeakFirstNow(ctx context.Context, arguments SpeakFirstNowArguments) (bool, error)
}

// ReportAgentPresenceArguments is one tab's or one phone's report: whether
// it is shown, and how long since the person last typed, clicked or
// scrolled in it.
type ReportAgentPresenceArguments struct {
	IsVisible   bool `json:"isVisible"`
	IdleSeconds int  `json:"idleSeconds"`

	// ReporterID names the tab or the phone reporting, so that one going
	// out of sight hides only itself. Optional: a report without it is
	// taken as an older dashboard's.
	ReporterID *string `json:"reporterId"`
}

// SpeakFirstNowArguments names why.
type SpeakFirstNowArguments struct {
	SpeakFirstReason string `json:"speakFirstReason"`
}

func (self *graph) ReportAgentPresence(ctx context.Context, arguments ReportAgentPresenceArguments) (bool, error) {
	principal, _, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return false, nil
	}
	reporterId := ""
	if arguments.ReporterID != nil {
		reporterId = strings.TrimSpace(*arguments.ReporterID)
		if len(reporterId) > 64 {
			return false, fmt.Errorf("%w: a reporter id is at most 64 characters", api.ErrInvalidArguments)
		}
	}
	worker.ReportPresence(principal.User.ID, reporterId, arguments.IsVisible, arguments.IdleSeconds, time.Now())
	return true, nil
}

func (self *graph) SpeakFirstNow(ctx context.Context, arguments SpeakFirstNowArguments) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return false, fmt.Errorf("%w: no agent worker runs on this server", api.ErrInvalidArguments)
	}
	if err := worker.SpeakFirstNow(self.writing(ctx), found, strings.TrimSpace(arguments.SpeakFirstReason)); err != nil {
		return false, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	return true, nil
}
