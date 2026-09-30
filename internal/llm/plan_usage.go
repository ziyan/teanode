package llm

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// A provider paid for by a subscription rather than by the token spends an
// allowance, and the service says how much of it is gone on every answer.
// What it last said is kept, so an operator can see how much is left before a
// turn is refused for want of it.

// PlanUsage is what the service last said of a plan's allowance.
type PlanUsage struct {
	// PlanName is the plan the account is on, as the service names it;
	// empty when it did not say.
	PlanName string

	// Windows are the allowances, each spent over a window of its own
	// length, shortest first. A window the service does not report is left
	// out rather than shown as unused.
	Windows []PlanWindow

	// ObservedAt is when the service said it.
	ObservedAt time.Time
}

// PlanWindow is one allowance.
type PlanWindow struct {
	UsedPercent   int
	WindowMinutes int

	// ResetsAt is when the window starts over; zero when not said.
	ResetsAt time.Time
}

// planUsageReporter is a provider that keeps what its plan said.
type planUsageReporter interface {
	planUsageNow() *PlanUsage
}

// PlanUsage is what the named provider's plan last said of its allowance, or
// nil when the provider has no plan or has not been answered yet.
func (self *Registry) PlanUsage(provider string) *PlanUsage {
	if self == nil {
		return nil
	}
	entry := self.providers[provider]
	if entry == nil {
		return nil
	}
	reporter, isReporter := entry.service.(planUsageReporter)
	if !isReporter {
		return nil
	}
	return reporter.planUsageNow()
}

// planWindowOf reads one window from the headers, named by its prefix such as
// "X-Codex-Primary". It is not reported when its length is not said.
func planWindowOf(header http.Header, prefix string, now time.Time) (PlanWindow, bool) {
	minutes, err := strconv.ParseFloat(strings.TrimSpace(header.Get(prefix+"-Window-Minutes")), 64)
	if err != nil || minutes <= 0 {
		return PlanWindow{}, false
	}
	window := PlanWindow{WindowMinutes: int(minutes)}
	if used, isKnown := usedPercent(header, prefix+"-Used-Percent"); isKnown {
		window.UsedPercent = used
	}
	window.ResetsAt = resetTimeOf(header, prefix, now)
	return window, true
}

// resetTimeOf is when a window resets: the moment the service names, in Unix
// seconds or as a timestamp, or else now plus the seconds it says remain.
func resetTimeOf(header http.Header, prefix string, now time.Time) time.Time {
	at := strings.TrimSpace(header.Get(prefix + "-Reset-At"))
	if seconds, err := strconv.ParseInt(at, 10, 64); err == nil && seconds > 0 {
		return time.Unix(seconds, 0)
	}
	if parsed, err := time.Parse(time.RFC3339, at); err == nil {
		return parsed
	}
	after, err := strconv.ParseFloat(strings.TrimSpace(header.Get(prefix+"-Reset-After-Seconds")), 64)
	if err != nil || after <= 0 {
		return time.Time{}
	}
	return now.Add(time.Duration(after * float64(time.Second))).Round(time.Second)
}
