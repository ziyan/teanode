package strainer

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/spamfilter"
)

// Classification learned from this server's own mail.
//
// This is the part of a spam filter that does most of the work, because it
// learns the mail you actually get rather than the mail somebody else got.
// The arithmetic is the standard naive Bayes over message tokens: hold, per
// token, how often it appeared in messages marked spam and in messages marked
// not spam, and combine the per-token probabilities into one.

const (
	// bayesMaximumTokens bounds how many distinct tokens one message
	// contributes. A message is untrusted input, and a classifier that
	// tokenised all of a ten megabyte body would spend the delivery doing it.
	bayesMaximumTokens = 500

	// bayesSignificantTokens is how many of the most opinionated tokens are
	// combined into the verdict. Using every token lets a long message drown
	// its own signal in filler that appears everywhere.
	bayesSignificantTokens = 15

	// bayesTokenMaximumLength matches the database column. Longer strings are
	// not words.
	bayesTokenMaximumLength = 64

	// bayesCorpusCacheDuration is how long the corpus totals are reused. They
	// change only when somebody marks a message, and reading them on every
	// delivery would be a query per message for a number that moves twice a
	// day.
	bayesCorpusCacheDuration = time.Minute
)

// tokenPattern is what counts as a word: letters, digits and the punctuation
// that holds addresses and domains together.
var tokenPattern = regexp.MustCompile(`[a-zA-Z0-9$!£€][a-zA-Z0-9$!£€'._@-]{1,62}`)

// corpusTotals is how many messages have been learned, cached briefly.
type corpusTotals struct {
	mutex   sync.Mutex
	spam    int64
	ham     int64
	expires time.Time
}

// bayesChecks scores the message against what has been learned.
func (self *Strainer) bayesChecks(ctx context.Context, message *spamfilter.Message) []check {
	if self.database == nil {
		return nil
	}

	spamMessages, hamMessages, err := self.corpus()
	if err != nil {
		log.Warningf("could not read how much the classifier has learned: %s", err)
		return nil
	}

	// A classifier trained on four messages is confidently wrong, so it says
	// nothing until it has seen enough of both kinds. Both, because a corpus
	// of spam alone would call everything spam.
	minimum := self.settings.Bayes.MinimumMessages
	if minimum <= 0 {
		minimum = 200
	}
	if spamMessages+hamMessages < minimum || spamMessages == 0 || hamMessages == 0 {
		return nil
	}

	tokens := tokenize(message)
	if len(tokens) == 0 {
		return nil
	}

	counts, err := self.database.LoadSpamTokens(tokens)
	if err != nil {
		log.Warningf("could not read the classifier's counts: %s", err)
		return nil
	}

	probability, ok := classify(tokens, counts, spamMessages, hamMessages)
	if !ok {
		return nil
	}

	// Expressed between -1 and 1 and then scaled, so that the weight setting
	// means what it says: a certain verdict costs the full weight, and an
	// uncertain one costs proportionally less.
	weight := self.settings.Bayes.Weight
	if weight <= 0 {
		weight = 3.0
	}
	opinion := (probability - 0.5) * 2
	score := opinion * weight

	// Silent when it has no opinion, rather than adding a symbol worth
	// nothing to every message.
	if math.Abs(score) < 0.1 {
		return nil
	}
	return []check{{
		symbol:      fmt.Sprintf("BAYES_%02d", int(probability*100)/10*10),
		score:       score,
		description: fmt.Sprintf("resembles mail marked as spam here with probability %.2f", probability),
	}}
}

// corpus reads how many messages have been learned, through a short cache.
func (self *Strainer) corpus() (int64, int64, error) {
	self.totals.mutex.Lock()
	defer self.totals.mutex.Unlock()

	if time.Now().Before(self.totals.expires) {
		return self.totals.spam, self.totals.ham, nil
	}
	spam, ham, err := self.database.CountSpamTraining()
	if err != nil {
		return 0, 0, err
	}
	self.totals.spam, self.totals.ham = spam, ham
	self.totals.expires = time.Now().Add(bayesCorpusCacheDuration)
	return spam, ham, nil
}

// classify combines the per-token probabilities.
//
// Only the most opinionated tokens are used, and the combination is done in
// log space: multiplying fifteen small probabilities underflows to zero in
// floating point, and the filter would then be certain about everything.
func classify(tokens []string, counts map[string]models.SpamTokenCount, spamMessages, hamMessages int64) (float64, bool) {
	type scored struct {
		probability float64
		interest    float64
	}
	interesting := make([]scored, 0, len(tokens))

	for _, token := range tokens {
		count, ok := counts[token]
		if !ok {
			continue
		}
		// A token seen once or twice says nothing; it is noise that happens
		// to have landed on one side.
		if count.SpamCount+count.HamCount < 3 {
			continue
		}

		spamRate := float64(count.SpamCount) / float64(spamMessages)
		hamRate := float64(count.HamCount) / float64(hamMessages)
		if spamRate+hamRate == 0 {
			continue
		}
		probability := spamRate / (spamRate + hamRate)

		// Pulled towards neutral in proportion to how little evidence there
		// is, so a token seen three times cannot claim certainty.
		evidence := float64(count.SpamCount + count.HamCount)
		probability = (0.5*3 + evidence*probability) / (3 + evidence)
		probability = math.Max(0.01, math.Min(0.99, probability))

		interesting = append(interesting, scored{
			probability: probability,
			interest:    math.Abs(probability - 0.5),
		})
	}
	if len(interesting) == 0 {
		return 0, false
	}

	// The most opinionated first, then the top few.
	for index := 1; index < len(interesting); index++ {
		for inner := index; inner > 0 && interesting[inner].interest > interesting[inner-1].interest; inner-- {
			interesting[inner], interesting[inner-1] = interesting[inner-1], interesting[inner]
		}
	}
	if len(interesting) > bayesSignificantTokens {
		interesting = interesting[:bayesSignificantTokens]
	}

	var logSpam, logHam float64
	for _, token := range interesting {
		logSpam += math.Log(token.probability)
		logHam += math.Log(1 - token.probability)
	}
	// Back out of log space with the difference, which is bounded, rather
	// than by exponentiating each side, which is not.
	return 1 / (1 + math.Exp(logHam-logSpam)), true
}

// tokenize turns a message into the words the classifier counts.
//
// Headers are included, prefixed so that a word in a subject is a different
// token from the same word in a body: "free" in a subject line is far more
// telling than "free" in a paragraph.
func tokenize(message *spamfilter.Message) []string {
	seen := make(map[string]bool, bayesMaximumTokens)
	tokens := make([]string, 0, bayesMaximumTokens)

	add := func(prefix, word string) bool {
		if len(word) > bayesTokenMaximumLength {
			return true
		}
		token := prefix + strings.ToLower(word)
		if len(token) > bayesTokenMaximumLength || seen[token] {
			return true
		}
		seen[token] = true
		tokens = append(tokens, token)
		return len(tokens) < bayesMaximumTokens
	}

	for _, header := range message.Headers {
		name, value, found := strings.Cut(header, ":")
		if !found {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "subject", "from", "to", "reply-to", "content-type", "x-mailer", "list-unsubscribe":
			prefix := strings.ToLower(strings.TrimSpace(name)) + ":"
			for _, word := range tokenPattern.FindAllString(value, -1) {
				if !add(prefix, word) {
					return tokens
				}
			}
		}
	}

	// Bounded before tokenising rather than after: the cost worth avoiding is
	// scanning the whole of a large body, not the tokens it would produce.
	body := message.Body
	const bodyLimit = 256 * 1024
	if len(body) > bodyLimit {
		body = body[:bodyLimit]
	}
	for _, word := range tokenPattern.FindAll(body, -1) {
		if !add("", string(word)) {
			break
		}
	}
	return tokens
}

// Learn teaches the classifier one message, and is idempotent.
//
// Marking a message that was already marked the same way does nothing.
// Changing its label subtracts what the previous label added and adds the
// new one, so the counts always describe exactly the set of messages in
// spam_training and cannot drift.
func Learn(database db.SpamOperation, mailId, label string, headers []string, body []byte) error {
	if label != models.SpamTrainingLabelSpam && label != models.SpamTrainingLabelHam {
		return fmt.Errorf("strainer: %q is not a label; use %q or %q",
			label, models.SpamTrainingLabelSpam, models.SpamTrainingLabelHam)
	}

	existing, err := database.GetSpamTraining(mailId)
	if err != nil {
		return err
	}
	if existing != nil && existing.Label == label {
		return nil
	}

	tokens := tokenize(&spamfilter.Message{Headers: headers, Body: body})
	deltas := make([]models.SpamTokenDelta, 0, len(tokens))
	for _, token := range tokens {
		delta := models.SpamTokenDelta{Token: token}
		if label == models.SpamTrainingLabelSpam {
			delta.SpamCount = 1
		} else {
			delta.HamCount = 1
		}
		// Re-labelled: take back what the previous label contributed.
		if existing != nil {
			if existing.Label == models.SpamTrainingLabelSpam {
				delta.SpamCount--
			} else {
				delta.HamCount--
			}
		}
		deltas = append(deltas, delta)
	}

	if err := database.LearnSpamTokens(deltas); err != nil {
		return err
	}
	return database.SetSpamTraining(&models.SpamTraining{MailID: mailId, Label: label})
}

// Forget undoes what Learn did for one message.
func Forget(database db.SpamOperation, mailId string, headers []string, body []byte) error {
	existing, err := database.GetSpamTraining(mailId)
	if err != nil || existing == nil {
		return err
	}

	tokens := tokenize(&spamfilter.Message{Headers: headers, Body: body})
	deltas := make([]models.SpamTokenDelta, 0, len(tokens))
	for _, token := range tokens {
		delta := models.SpamTokenDelta{Token: token}
		if existing.Label == models.SpamTrainingLabelSpam {
			delta.SpamCount = -1
		} else {
			delta.HamCount = -1
		}
		deltas = append(deltas, delta)
	}

	if err := database.LearnSpamTokens(deltas); err != nil {
		return err
	}
	return database.DeleteSpamTraining(mailId)
}
