package strainer

import (
	"context"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/deferutil"
)

// Keeping the parsed rules in step with what the database holds.
//
// The rules live in the database because several instances share one, so an
// instance cannot assume its parsed copy is current: another instance, or an
// operator with the command line, may have replaced it. Each one therefore
// asks for the stored version on a timer and reparses when it moves. Asking
// for the version is a single short column; the rules themselves are
// megabytes and are fetched only when they have actually changed.

// ruleRefreshInterval is how often an instance checks. Rules change when
// somebody updates them, which is daily at most, so this is about noticing
// within a reasonable time rather than about being current to the second.
const ruleRefreshInterval = time.Minute

// loadedVersions is what this instance has parsed: the stored version of
// every configured channel, as of the last time the rules were built.
//
// Per channel, and all of them together, because SetRules replaces the whole
// corpus rather than adding to it. Tracking one channel's version while
// several were configured made each tick notice a mismatch, rebuild from that
// channel alone, and record its version — so the next tick noticed the next
// channel and did it again, reparsing megabytes every minute for ever.
type loadedVersions map[string]string

// matches reports whether what is stored is what this instance has parsed.
func (self loadedVersions) matches(stored map[string]string) bool {
	if len(self) != len(stored) {
		return false
	}
	for channel, version := range stored {
		if self[channel] != version {
			return false
		}
	}
	return true
}

// StartRuleRefresh keeps this instance's parsed rules in step with the
// database until the context ends.
//
// Started even when rules are disabled, and does nothing in that case, so
// that turning them on in the dashboard takes effect without a restart.
func (self *Strainer) StartRuleRefresh(ctx context.Context) {
	if self.database == nil {
		return
	}
	go func() {
		defer deferutil.Recover()

		// Once at startup, so a restarted server is not scoring without its
		// rules for the first minute.
		self.refreshRules(ctx)

		ticker := time.NewTicker(ruleRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				self.refreshRules(ctx)
			}
		}
	}()
}

// refreshRules rebuilds the corpus when any configured channel has moved.
func (self *Strainer) refreshRules(ctx context.Context) {
	if !self.settings.Rules.Enabled {
		return
	}

	// Versions first, for every channel: a short column each, against the
	// megabytes the rules themselves are.
	stored := make(map[string]string, len(self.settings.Rules.Channels))
	for _, channel := range self.settings.Rules.Channels {
		if ctx.Err() != nil {
			return
		}
		version, err := self.database.SpamRuleSetVersion(channel)
		if err != nil {
			log.Warningf("could not check the rules from %s: %s", channel, err)
			return
		}
		if version != "" {
			stored[channel] = version
		}
	}

	self.rulesMutex.RLock()
	unchanged := self.loaded.matches(stored)
	self.rulesMutex.RUnlock()
	if unchanged {
		return
	}

	// Every channel's rules, concatenated and parsed as one corpus, because
	// that is what a rule set is: meta rules in one file refer to rules
	// defined in another.
	var corpus strings.Builder
	sets := make([]*models.SpamRuleSet, 0, len(stored))
	for _, channel := range self.settings.Rules.Channels {
		if _, ok := stored[channel]; !ok {
			continue
		}
		set, err := self.database.LoadSpamRuleSet(channel)
		if err != nil {
			log.Warningf("could not read the rules from %s: %s", channel, err)
			return
		}
		if set == nil {
			continue
		}
		corpus.Write(set.Content)
		corpus.WriteString("\n")
		sets = append(sets, set)
	}

	loaded, skipped := self.SetRules(corpus.String())

	self.rulesMutex.Lock()
	self.loaded = stored
	self.rulesMutex.Unlock()

	log.Noticef("spam rules from %d channel(s): %d loaded, %d skipped",
		len(sets), loaded, skipped)

	// Recorded against the single channel when there is one, so the
	// dashboard can say how much of a published set this server can actually
	// run — which is not something the publisher knows. With several, the
	// counts are of the corpus as a whole and cannot be attributed, so they
	// are left alone.
	if len(sets) == 1 && (sets[0].RulesLoaded != loaded || sets[0].RulesSkipped != skipped) {
		sets[0].RulesLoaded, sets[0].RulesSkipped = loaded, skipped
		if err := self.database.SaveSpamRuleSet(sets[0]); err != nil {
			log.Warningf("could not record how much of %s loaded: %s", sets[0].Channel, err)
		}
	}
}

// ImportRules stores a rule set, for the command line and the dashboard.
//
// Parsed before it is stored, so that a file which turns out to be something
// else is refused now rather than at the next delivery, and so the counts are
// known without waiting for an instance to pick it up.
func ImportRules(database SpamRuleStore, channel, version string, content []byte) (*models.SpamRuleSet, error) {
	set := parseRules(string(content))
	ruleSet := &models.SpamRuleSet{
		Channel:      channel,
		Version:      version,
		Content:      content,
		RulesLoaded:  set.loaded,
		RulesSkipped: set.skipped,
	}
	if err := database.SaveSpamRuleSet(ruleSet); err != nil {
		return nil, err
	}
	return ruleSet, nil
}

// SpamRuleStore is the part of the database this file needs.
type SpamRuleStore interface {
	SaveSpamRuleSet(ruleSet *models.SpamRuleSet) error
}
