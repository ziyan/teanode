package db_test

import (
	"sync"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// Learning is expressed as deltas applied by the database rather than values
// read and written back, because two instances can be learning at the same
// time. This proves the arithmetic accumulates rather than replaces, which is
// the difference between the two designs and is invisible in a single call.
func TestLearningAccumulatesRatherThanReplacing(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	for round := 0; round < 3; round++ {
		if err := database.LearnSpamTokens([]models.SpamTokenDelta{
			{Token: "viagra", SpamCount: 1},
			{Token: "invoice", HamCount: 1},
		}); err != nil {
			t.Fatalf("LearnSpamTokens() = %v", err)
		}
	}

	counts, err := database.LoadSpamTokens([]string{"viagra", "invoice", "never-seen"})
	if err != nil {
		t.Fatalf("LoadSpamTokens() = %v", err)
	}
	if counts["viagra"].SpamCount != 3 {
		t.Errorf("viagra has spam count %d after three rounds, want 3", counts["viagra"].SpamCount)
	}
	if counts["invoice"].HamCount != 3 {
		t.Errorf("invoice has ham count %d after three rounds, want 3", counts["invoice"].HamCount)
	}

	// A token nobody has seen is absent rather than zero, so a caller can
	// tell "never seen" from "seen and neutral".
	if _, present := counts["never-seen"]; present {
		t.Errorf("a token that was never learned came back present")
	}

	// Negative deltas take it back, which is what un-marking a message does.
	if err := database.LearnSpamTokens([]models.SpamTokenDelta{{Token: "viagra", SpamCount: -3}}); err != nil {
		t.Fatalf("LearnSpamTokens() = %v", err)
	}
	counts, err = database.LoadSpamTokens([]string{"viagra"})
	if err != nil {
		t.Fatalf("LoadSpamTokens() = %v", err)
	}
	if counts["viagra"].SpamCount != 0 {
		t.Errorf("viagra has spam count %d after being taken back, want 0", counts["viagra"].SpamCount)
	}
}

// The corpus totals are what the classifier's minimum is compared against, so
// they have to follow the labels exactly — including when one is changed.
func TestTheCorpusFollowsTheLabels(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	for _, mailId := range []string{"a", "b", "c"} {
		if err := database.SetSpamTraining(&models.SpamTraining{
			MailID: mailId, Label: models.SpamTrainingLabelSpam,
		}); err != nil {
			t.Fatalf("SetSpamTraining() = %v", err)
		}
	}
	// Marking the same message again must not count it twice.
	if err := database.SetSpamTraining(&models.SpamTraining{
		MailID: "a", Label: models.SpamTrainingLabelSpam,
	}); err != nil {
		t.Fatalf("SetSpamTraining() = %v", err)
	}

	spam, ham, err := database.CountSpamTraining()
	if err != nil {
		t.Fatalf("CountSpamTraining() = %v", err)
	}
	if spam != 3 || ham != 0 {
		t.Errorf("counted %d spam and %d ham, want 3 and 0", spam, ham)
	}

	// Changing a label moves it rather than adding to both.
	if err := database.SetSpamTraining(&models.SpamTraining{
		MailID: "a", Label: models.SpamTrainingLabelHam,
	}); err != nil {
		t.Fatalf("SetSpamTraining() = %v", err)
	}
	spam, ham, err = database.CountSpamTraining()
	if err != nil {
		t.Fatalf("CountSpamTraining() = %v", err)
	}
	if spam != 2 || ham != 1 {
		t.Errorf("counted %d spam and %d ham after relabelling, want 2 and 1", spam, ham)
	}

	if err := database.DeleteSpamTraining("a"); err != nil {
		t.Fatalf("DeleteSpamTraining() = %v", err)
	}
	if training, err := database.GetSpamTraining("a"); err != nil || training != nil {
		t.Errorf("GetSpamTraining() = %v, %v; want nil for a forgotten message", training, err)
	}
}

// Two marks of one message arriving together, on two instances, count it once:
// the label is read and the counts changed in one transaction under a lock on
// the message.
func TestMarkingOneMessageTwiceAtOnceCountsItOnce(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	tokens := []string{"alpha", "beta", "gamma"}
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := database.LearnSpamMail("mail-one", models.SpamTrainingLabelSpam, tokens); err != nil {
				t.Errorf("LearnSpamMail() = %v", err)
			}
		}()
	}
	group.Wait()

	counts, err := database.LoadSpamTokens(tokens)
	if err != nil {
		t.Fatalf("LoadSpamTokens() = %v", err)
	}
	for _, token := range tokens {
		if counts[token].SpamCount != 1 || counts[token].HamCount != 0 {
			t.Errorf("%s counts %+v after eight marks of one message, want one spam", token, counts[token])
		}
	}

	// Changing the label takes back the old one's counts.
	if err := database.LearnSpamMail("mail-one", models.SpamTrainingLabelHam, tokens); err != nil {
		t.Fatalf("LearnSpamMail() = %v", err)
	}
	counts, _ = database.LoadSpamTokens(tokens)
	if counts["alpha"].SpamCount != 0 || counts["alpha"].HamCount != 1 {
		t.Errorf("alpha counts %+v after the label changed, want one ham", counts["alpha"])
	}

	// And forgetting, twice at once, takes it back once.
	for range 4 {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := database.ForgetSpamMail("mail-one", tokens); err != nil {
				t.Errorf("ForgetSpamMail() = %v", err)
			}
		}()
	}
	group.Wait()
	counts, _ = database.LoadSpamTokens(tokens)
	if counts["alpha"].SpamCount != 0 || counts["alpha"].HamCount != 0 {
		t.Errorf("alpha counts %+v after the message was forgotten, want none", counts["alpha"])
	}
	if training, err := database.GetSpamTraining("mail-one"); err != nil || training != nil {
		t.Errorf("the message is still marked: %+v %v", training, err)
	}
}

// Recording how much of a rule set loaded changes those two numbers and
// nothing else, and not at all when an import has replaced the set since.
func TestRecordingRuleCountsLeavesAnImportAlone(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	if err := database.SaveSpamRuleSet(&models.SpamRuleSet{Channel: "channel-one", Version: "1", Content: []byte("older rules")}); err != nil {
		t.Fatal(err)
	}
	// An import lands after this instance read version 1.
	if err := database.SaveSpamRuleSet(&models.SpamRuleSet{Channel: "channel-one", Version: "2", Content: []byte("newer rules")}); err != nil {
		t.Fatal(err)
	}
	if err := database.RecordSpamRuleSetCounts("channel-one", "1", 5, 6); err != nil {
		t.Fatal(err)
	}
	stored, err := database.LoadSpamRuleSet("channel-one")
	if err != nil || stored.Version != "2" || string(stored.Content) != "newer rules" || stored.RulesLoaded != 0 {
		t.Fatalf("the newer import was disturbed: %+v %v", stored, err)
	}

	if err := database.RecordSpamRuleSetCounts("channel-one", "2", 5, 6); err != nil {
		t.Fatal(err)
	}
	stored, _ = database.LoadSpamRuleSet("channel-one")
	if stored.RulesLoaded != 5 || stored.RulesSkipped != 6 || string(stored.Content) != "newer rules" {
		t.Fatalf("the counts were not recorded as they should be: %+v", stored)
	}
}
