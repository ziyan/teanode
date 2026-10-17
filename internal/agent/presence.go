package agent

import (
	"sync"
	"time"
)

// presenceFresh is how long a report says the person is there: the
// dashboard reports every minute, so two minutes is one missed report.
const presenceFresh = 2 * time.Minute

// presenceReportersAtMost bounds the tabs and phones kept for one person.
// Each tab names itself afresh when it loads, so somebody reloading over and
// over within presenceFresh would otherwise add a reporter each time.
const presenceReportersAtMost = 32

// presenceReport is what one dashboard tab or one phone last said about the
// person.
type presenceReport struct {
	reportedAt  time.Time
	isVisible   bool
	idleSeconds int
}

// Presence is who has a dashboard or the app open, kept in memory and
// nowhere else: a restart forgets it until each open tab or app reports
// again, within a minute. The agent speaks first only to somebody who is
// there to read it, offers an idea only to somebody who has gone quiet, and
// tells a phone what nobody is there to see.
//
// Reports are kept per person and per reporter: each tab and each phone
// names itself, so the app going to the background hides only the app,
// and a tab hidden in one window does not hide a tab shown in another.
//
// One Presence is shared by the agent and by the chat hub, so that a server
// with no model, which has no agent, still knows who is looking at a chat.
type Presence struct {
	mutex   sync.Mutex
	reports map[string]map[string]presenceReport
	// hasArrival says somebody came into view since the agent last looked
	// for whom to speak to first, so it looks again without waiting out its
	// minute: somebody who has just switched their agent on, and whose page
	// reloaded after the restart that took, is greeted while they are there.
	hasArrival bool
}

// NewPresence is a Presence with nobody in it.
func NewPresence() *Presence {
	return &Presence{}
}

// Report records one tab's or one phone's report for a person:
// whether it is shown, and how long since their last keypress, click or
// scroll in it. reporterId names the tab or phone; a report without one is
// an older dashboard, whose hidden tab is not taken to hide a visible one
// that reported lately, since it cannot say which tab it is.
func (self *Presence) Report(userId, reporterId string, isVisible bool, idleSeconds int, now time.Time) {
	if userId == "" {
		return
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.reports == nil {
		self.reports = map[string]map[string]presenceReport{}
	}
	reporters := self.reports[userId]
	if reporters == nil {
		reporters = map[string]presenceReport{}
		self.reports[userId] = reporters
	}
	if reporterId == "" {
		if existing, ok := reporters[""]; ok && existing.isVisible && !isVisible && now.Sub(existing.reportedAt) < presenceFresh {
			return
		}
	}
	// Reporters that went quiet are forgotten, so a person who opened many
	// tabs over a day keeps a short list.
	for name, report := range reporters {
		if now.Sub(report.reportedAt) > presenceFresh {
			delete(reporters, name)
		}
	}
	// The one that reported longest ago makes room for a new one.
	if _, isKnown := reporters[reporterId]; !isKnown && len(reporters) >= presenceReportersAtMost {
		oldestName, oldestAt := "", time.Time{}
		for name, report := range reporters {
			if oldestAt.IsZero() || report.reportedAt.Before(oldestAt) {
				oldestName, oldestAt = name, report.reportedAt
			}
		}
		delete(reporters, oldestName)
	}
	if isVisible && !hasVisibleReport(reporters, now) {
		self.hasArrival = true
	}
	reporters[reporterId] = presenceReport{reportedAt: now, isVisible: isVisible, idleSeconds: max(idleSeconds, 0)}
}

// hasVisibleReport says whether any of a person's tabs or phones reported
// itself shown lately.
func hasVisibleReport(reporters map[string]presenceReport, now time.Time) bool {
	for _, report := range reporters {
		if report.isVisible && now.Sub(report.reportedAt) <= presenceFresh {
			return true
		}
	}
	return false
}

// takeArrival says whether somebody came into view since it was last asked.
func (self *Presence) takeArrival() bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	hasArrival := self.hasArrival
	self.hasArrival = false
	return hasArrival
}

// isPresent says whether the person has a visible dashboard tab or the app
// in front of them that reported lately, and how long they have been idle
// in the busiest of them.
func (self *Presence) isPresent(userId string, now time.Time) (bool, time.Duration) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	isPresent, idle := false, time.Duration(0)
	for _, report := range self.reports[userId] {
		if !report.isVisible || now.Sub(report.reportedAt) > presenceFresh {
			continue
		}
		reportIdle := time.Duration(report.idleSeconds)*time.Second + now.Sub(report.reportedAt)
		if !isPresent || reportIdle < idle {
			isPresent, idle = true, reportIdle
		}
	}
	return isPresent, idle
}

// phoneQuietAfter is how long a visible dashboard may sit untouched before
// the person is taken to have walked away from it, and their phone is told
// what waits for them: a tab left open on a desk is not somebody reading.
const phoneQuietAfter = 2 * time.Minute

// IsWatching says whether the person is looking at a dashboard or the app
// now, so that their phone need not be told. lastWroteAt is when they last
// wrote to the turn at hand, zero when nobody did: typing a question sends
// no report, so without it somebody who asked a moment ago could be judged
// away on the strength of a report from before they typed.
func (self *Presence) IsWatching(userId string, lastWroteAt, now time.Time) bool {
	isPresent, idle := self.isPresent(userId, now)
	if !isPresent {
		return false
	}
	if !lastWroteAt.IsZero() {
		idle = min(idle, max(now.Sub(lastWroteAt), 0))
	}
	return idle < phoneQuietAfter
}

// ReportPresence records a report for a person in the presence the agent
// shares with the chat hub; see Presence.Report.
func (self *Agent) ReportPresence(userId, reporterId string, isVisible bool, idleSeconds int, now time.Time) {
	self.presence.Report(userId, reporterId, isVisible, idleSeconds, now)
}

// isWatching is Presence.IsWatching for the agent's own notifications.
func (self *Agent) isWatching(userId string, lastWroteAt, now time.Time) bool {
	return self.presence.IsWatching(userId, lastWroteAt, now)
}
