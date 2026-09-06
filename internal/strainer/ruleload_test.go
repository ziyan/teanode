package strainer

import (
	"context"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
)

// fakeRuleStore counts what the refresher asks of the database, which is the
// whole point of the test below: the cost of a tick that has nothing to do.
type fakeRuleStore struct {
	sets     map[string]*models.SpamRuleSet
	versions int
	loads    int
	saves    int
}

func (self *fakeRuleStore) SpamRuleSetVersion(channel string) (string, error) {
	self.versions++
	if set, ok := self.sets[channel]; ok {
		return set.Version, nil
	}
	return "", nil
}

func (self *fakeRuleStore) LoadSpamRuleSet(channel string) (*models.SpamRuleSet, error) {
	self.loads++
	return self.sets[channel], nil
}

func (self *fakeRuleStore) SaveSpamRuleSet(set *models.SpamRuleSet) error {
	self.saves++
	self.sets[set.Channel] = set
	return nil
}

func (self *fakeRuleStore) LearnSpamTokens([]models.SpamTokenDelta) error { return nil }
func (self *fakeRuleStore) LoadSpamTokens([]string) (map[string]models.SpamTokenCount, error) {
	return nil, nil
}
func (self *fakeRuleStore) GetSpamTraining(string) (*models.SpamTraining, error) { return nil, nil }
func (self *fakeRuleStore) SetSpamTraining(*models.SpamTraining) error           { return nil }
func (self *fakeRuleStore) DeleteSpamTraining(string) error                      { return nil }
func (self *fakeRuleStore) CountSpamTraining() (int64, int64, error)             { return 0, 0, nil }
func (self *fakeRuleStore) ListSpamRuleSets() ([]*models.SpamRuleSet, error)     { return nil, nil }

// Several channels must not make every tick rebuild the corpus.
//
// The first version of this tracked one channel's version. With two
// configured, each tick found a mismatch against whichever was recorded last,
// rebuilt from that one alone, and recorded its version — so the next tick
// found the other and did it again, reparsing the whole corpus every minute
// for ever. On a real rule set that is megabytes of regular expressions.
func TestSeveralChannelsSettleRatherThanThrash(t *testing.T) {
	t.Parallel()

	store := &fakeRuleStore{sets: map[string]*models.SpamRuleSet{
		"first":  {Channel: "first", Version: "1", Content: []byte("body FIRST /alpha/\nscore FIRST 1.0\n")},
		"second": {Channel: "second", Version: "1", Content: []byte("body SECOND /beta/\nscore SECOND 1.0\n")},
	}}

	filter := New(&config.AntispamBuiltin{
		Rules: config.AntispamRules{Enabled: true, Channels: []string{"first", "second"}},
	}, nil, store)

	filter.refreshRules(context.Background())
	afterFirst := store.loads
	if afterFirst == 0 {
		t.Fatal("the first refresh loaded nothing")
	}

	// Nothing has changed, so three more ticks must load nothing at all.
	for index := 0; index < 3; index++ {
		filter.refreshRules(context.Background())
	}
	if store.loads != afterFirst {
		t.Errorf("loaded the rules %d times over four ticks with nothing changing, want %d",
			store.loads, afterFirst)
	}

	// Both channels' rules are in force, not just the last one seen.
	loaded, _ := filter.RuleCounts()
	if loaded != 2 {
		t.Errorf("loaded %d rules from two channels, want both", loaded)
	}

	// A version moving rebuilds, once.
	store.sets["second"] = &models.SpamRuleSet{
		Channel: "second", Version: "2",
		Content: []byte("body SECOND /gamma/\nscore SECOND 1.0\nbody THIRD /delta/\nscore THIRD 1.0\n"),
	}
	before := store.loads
	filter.refreshRules(context.Background())
	filter.refreshRules(context.Background())
	if store.loads != before+2 {
		t.Errorf("a changed version caused %d loads, want one rebuild of two channels", store.loads-before)
	}
	if loaded, _ := filter.RuleCounts(); loaded != 3 {
		t.Errorf("loaded %d rules after the change, want 3", loaded)
	}
}

// A tick with nothing to do costs one short query per channel, not the rules.
func TestAQuietTickIsCheap(t *testing.T) {
	t.Parallel()

	store := &fakeRuleStore{sets: map[string]*models.SpamRuleSet{
		"only": {Channel: "only", Version: "1", Content: []byte("body ONLY /alpha/\nscore ONLY 1.0\n")},
	}}
	filter := New(&config.AntispamBuiltin{
		Rules: config.AntispamRules{Enabled: true, Channels: []string{"only"}},
	}, nil, store)

	filter.refreshRules(context.Background())
	loadsAfterBuild := store.loads

	start := time.Now()
	for index := 0; index < 100; index++ {
		filter.refreshRules(context.Background())
	}
	if store.loads != loadsAfterBuild {
		t.Errorf("a hundred quiet ticks read the rules %d times", store.loads-loadsAfterBuild)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("a hundred quiet ticks took %s", elapsed)
	}
}
