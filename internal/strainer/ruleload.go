package strainer

import (
	"context"
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

// loadedVersions is what this instance has parsed, per channel.
type loadedVersion struct {
	channel string
	version string
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

// refreshRules reparses when the stored version has moved.
func (self *Strainer) refreshRules(ctx context.Context) {
	if !self.settings.Rules.Enabled {
		return
	}
	for _, channel := range self.settings.Rules.Channels {
		if ctx.Err() != nil {
			return
		}
		if err := self.refreshChannel(channel); err != nil {
			log.Warningf("could not refresh the rules from %s: %s", channel, err)
		}
	}
}

func (self *Strainer) refreshChannel(channel string) error {
	version, err := self.database.SpamRuleSetVersion(channel)
	if err != nil {
		return err
	}
	if version == "" {
		return nil
	}

	self.rulesMutex.RLock()
	current := self.loaded
	self.rulesMutex.RUnlock()
	if current.channel == channel && current.version == version {
		return nil
	}

	stored, err := self.database.LoadSpamRuleSet(channel)
	if err != nil {
		return err
	}
	if stored == nil {
		return nil
	}

	loaded, skipped := self.SetRules(string(stored.Content))
	self.rulesMutex.Lock()
	self.loaded = loadedVersion{channel: channel, version: stored.Version}
	self.rulesMutex.Unlock()

	log.Noticef("spam rules from %s version %s: %d loaded, %d skipped",
		channel, stored.Version, loaded, skipped)

	// Written back so the dashboard can say how much of the published set
	// this server can actually run, which is not something the publisher
	// knows.
	if stored.RulesLoaded != loaded || stored.RulesSkipped != skipped {
		stored.RulesLoaded, stored.RulesSkipped = loaded, skipped
		if err := self.database.SaveSpamRuleSet(stored); err != nil {
			log.Warningf("could not record how much of %s loaded: %s", channel, err)
		}
	}
	return nil
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
