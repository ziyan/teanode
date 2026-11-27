package agent

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
