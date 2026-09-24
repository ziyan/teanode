package agent

import (
	"sync"
	"time"
)

// presenceFresh is how long a report says the person is there: the
// dashboard reports every minute, so two minutes is one missed report.
const presenceFresh = 2 * time.Minute

// presenceReport is what one dashboard tab last said about the person.
type presenceReport struct {
	reportedAt  time.Time
	isVisible   bool
	idleSeconds int
}

// presence is who has a dashboard open, kept in memory and nowhere else:
// a restart forgets it until each open tab reports again, within a
// minute. The agent speaks first only to somebody who is there to read
// it, and gives a tip only to somebody who has gone quiet.
type presence struct {
	mutex   sync.Mutex
	reports map[string]presenceReport
}

// ReportPresence records a tab's report for a person: whether it is
// visible, and how long since their last keypress, click or scroll.
func (self *Agent) ReportPresence(userId string, isVisible bool, idleSeconds int, now time.Time) {
	if userId == "" {
		return
	}
	self.presence.mutex.Lock()
	defer self.presence.mutex.Unlock()
	if self.presence.reports == nil {
		self.presence.reports = map[string]presenceReport{}
	}
	// A hidden tab does not hide a visible one: the person with two tabs
	// open is looking at one of them.
	if existing, ok := self.presence.reports[userId]; ok && existing.isVisible && !isVisible && now.Sub(existing.reportedAt) < presenceFresh {
		return
	}
	self.presence.reports[userId] = presenceReport{reportedAt: now, isVisible: isVisible, idleSeconds: max(idleSeconds, 0)}
}

// isPresent says whether the person has a visible dashboard tab that
// reported lately, and how long they have been idle in it.
func (self *Agent) isPresent(userId string, now time.Time) (bool, time.Duration) {
	self.presence.mutex.Lock()
	defer self.presence.mutex.Unlock()
	report, ok := self.presence.reports[userId]
	if !ok || !report.isVisible || now.Sub(report.reportedAt) > presenceFresh {
		return false, 0
	}
	return true, time.Duration(report.idleSeconds)*time.Second + now.Sub(report.reportedAt)
}
