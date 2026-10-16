package agent

import "context"

// toolCallLimitsKey carries a run's limits on how often a tool may be
// called down to the turn it starts, the way recordingEvidenceSafetyKey
// carries its safety: the thinking functions take a dozen arguments already,
// and a limit is not something every caller has.
type toolCallLimitsKey struct{}

// withToolCallLimits is ctx with limits on how many times each named tool
// may be called by the headless turns started from it.
func withToolCallLimits(ctx context.Context, limits map[string]int) context.Context {
	return context.WithValue(ctx, toolCallLimitsKey{}, limits)
}

// toolCallLimitsOf is the limits ctx carries, or nil.
func toolCallLimitsOf(ctx context.Context) map[string]int {
	limits, _ := ctx.Value(toolCallLimitsKey{}).(map[string]int)
	return limits
}

// personOmittedKey marks a run whose headless turns are not shown what the
// agent knows of the person: their own page and the index of the rest. A
// turn that searches the web on their behalf is given only the few lines it
// needs, so that what goes into a query cannot be a street address read off
// their page, whatever the prompt says about it.
type personOmittedKey struct{}

// withPersonOmitted is ctx with the person left out of the turns started
// from it.
func withPersonOmitted(ctx context.Context) context.Context {
	return context.WithValue(ctx, personOmittedKey{}, true)
}

// isPersonOmittedIn says whether ctx leaves the person out.
func isPersonOmittedIn(ctx context.Context) bool {
	isOmitted, _ := ctx.Value(personOmittedKey{}).(bool)
	return isOmitted
}

// takeCallAllowance counts one call of the named tool against the turn's
// limits and says whether it may run. A tool with no limit always may.
// The calls of a round run together, so the count is kept under a lock and
// the call that crosses the limit is the one refused, whichever it is.
func (self *AskRun) takeCallAllowance(name string) bool {
	limit, isLimited := self.settings.CallLimits[name]
	if !isLimited {
		return true
	}
	self.callCountsMutex.Lock()
	defer self.callCountsMutex.Unlock()
	if self.callCounts == nil {
		self.callCounts = map[string]int{}
	}
	if self.callCounts[name] >= limit {
		return false
	}
	self.callCounts[name]++
	return true
}
