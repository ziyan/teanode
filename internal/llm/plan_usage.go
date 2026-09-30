package llm

import (
	"math"
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
	// Name is which of the service's windows this is, such as "primary",
	// which is how a later answer that leaves its length out finds it.
	Name string

	UsedPercent   int
	WindowMinutes int

	// ResetsAt is when the window starts over; zero when not said.
	ResetsAt time.Time
}

// hasReset says the window has started over since it was read, so what was
// said of its use no longer holds.
func (self PlanWindow) hasReset(now time.Time) bool {
	return !self.ResetsAt.IsZero() && !self.ResetsAt.After(now)
}

// planUsageReporter is a provider that keeps what its plan said.
type planUsageReporter interface {
	planUsageNow(now time.Time) *PlanUsage
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
	return reporter.planUsageNow(time.Now())
}

// planWindowOf is one window, named by its header prefix such as
// "X-Codex-Primary", with what these headers say of it laid over what was
// said before: its length, use and reset time each change only when the
// headers say them. A use read before a reset that has since passed is not
// kept. It is not reported while its length has never been said.
func planWindowOf(header http.Header, prefix string, before PlanWindow, now time.Time) (PlanWindow, bool) {
	window := before
	window.Name = strings.ToLower(strings.TrimPrefix(prefix, "X-Codex-"))
	if before.hasReset(now) {
		window.UsedPercent, window.ResetsAt = 0, time.Time{}
	}
	if minutes, isSaid := finiteHeader(header, prefix+"-Window-Minutes"); isSaid && minutes > 0 {
		window.WindowMinutes = int(minutes)
	}
	if used, isSaid := usedPercent(header, prefix+"-Used-Percent"); isSaid {
		window.UsedPercent = used
	}
	if resetsAt, isSaid := resetTimeOf(header, prefix, now); isSaid {
		window.ResetsAt = resetsAt
	}
	return window, window.WindowMinutes > 0
}

// resetTimeOf is when a window resets: the moment the service names, in Unix
// seconds or as a timestamp, or else now plus the seconds it says remain. It
// is not said when neither header holds a usable value.
func resetTimeOf(header http.Header, prefix string, now time.Time) (time.Time, bool) {
	at := strings.TrimSpace(header.Get(prefix + "-Reset-At"))
	if seconds, err := strconv.ParseInt(at, 10, 64); err == nil && seconds > 0 {
		return time.Unix(seconds, 0), true
	}
	if parsed, err := time.Parse(time.RFC3339, at); err == nil {
		return parsed, true
	}
	after, isSaid := finiteHeader(header, prefix+"-Reset-After-Seconds")
	if !isSaid || after <= 0 {
		return time.Time{}, false
	}
	return now.Add(time.Duration(after * float64(time.Second))).Round(time.Second), true
}

// finiteHeader reads a header holding a number. A value that is not one, or
// is NaN or infinite, which ParseFloat accepts, is not said: turned into an
// int it would be a nonsense percent or length.
func finiteHeader(header http.Header, name string) (float64, bool) {
	value, err := strconv.ParseFloat(strings.TrimSpace(header.Get(name)), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	return value, true
}
