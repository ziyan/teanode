package db

import (
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// SpamOperation is what the built-in spam filter needs of the database.
//
// The counts live here rather than on an instance's disk because several
// instances can run against one database, and a classifier that differed
// between them would score the same message differently depending on which
// one received it.
type SpamOperation interface {
	// LearnSpamTokens applies token deltas.
	//
	// Deltas rather than values, applied by the database rather than read and
	// written back, because two instances may be learning at the same time
	// and a read-modify-write would lose one of them.
	LearnSpamTokens(deltas []models.SpamTokenDelta) error

	// LearnSpamMail teaches the classifier one message, and is idempotent:
	// marking a message the way it is already marked does nothing, and
	// changing its label takes back what the previous label added and adds
	// the new one. The label is read, the counts applied and the label
	// written in one transaction under a lock on the message, so that two
	// instances marking the same message at once count it once.
	LearnSpamMail(mailId, label string, tokens []string) error

	// ForgetSpamMail takes back what LearnSpamMail added for a message, in
	// one transaction under the same lock. A message never marked is left
	// alone.
	ForgetSpamMail(mailId string, tokens []string) error

	// LoadSpamTokens reads the counts for the tokens of one message. Tokens
	// that have never been seen are absent from the result rather than zero,
	// so a caller can tell "never seen" from "seen and neutral".
	LoadSpamTokens(tokens []string) (map[string]models.SpamTokenCount, error)

	// GetSpamTraining returns the label a message was marked with, or nil
	// when it has not been used for training.
	GetSpamTraining(mailId string) (*models.SpamTraining, error)

	// SetSpamTraining records the label a message was marked with.
	SetSpamTraining(training *models.SpamTraining) error

	// DeleteSpamTraining forgets that a message was used.
	DeleteSpamTraining(mailId string) error

	// CountSpamTraining says how many messages carry each label, which is
	// what the classifier's minimum is compared against.
	CountSpamTraining() (spam int64, ham int64, err error)

	// SpamRuleSetVersion is what the stored rule set is at, for an instance
	// checking whether the copy it parsed is stale. Empty when there is
	// none. Cheap enough to ask on a timer, which is the point of it being
	// separate from LoadSpamRuleSet.
	SpamRuleSetVersion(channel string) (string, error)

	// LoadSpamRuleSet reads one channel's stored rules, or nil.
	LoadSpamRuleSet(channel string) (*models.SpamRuleSet, error)

	// SaveSpamRuleSet replaces one channel's stored rules.
	SaveSpamRuleSet(ruleSet *models.SpamRuleSet) error

	// RecordSpamRuleSetCounts writes how much of a channel's rules this
	// server could run, and nothing else about the set: the rules stay as
	// they are. Only while the stored set is still at the version the counts
	// were worked out from, so that a newer import is not marked with the
	// counts of the one before it.
	RecordSpamRuleSetCounts(channel, version string, rulesLoaded, rulesSkipped int) error

	// ListSpamRuleSets is what the dashboard shows: every channel, with how
	// much of it this server could use.
	ListSpamRuleSets() ([]*models.SpamRuleSet, error)
}

type spamTokenModel struct {
	Token      string    `gorm:"column:token;primaryKey;size:64"`
	SpamCount  int64     `gorm:"column:spam_count"`
	HamCount   int64     `gorm:"column:ham_count"`
	ModifiedAt time.Time `gorm:"column:modified_at"`
}

func (spamTokenModel) TableName() string {
	return "spam_token"
}

type spamTrainingModel struct {
	MailID     string    `gorm:"column:mail_id;primaryKey;size:32"`
	Label      string    `gorm:"column:label;size:16;index:spam_training_label"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	ModifiedAt time.Time `gorm:"column:modified_at"`
}

func (spamTrainingModel) TableName() string {
	return "spam_training"
}

// LearnSpamTokens adds the deltas to whatever is stored.
//
// One statement per row, but all inside a single transaction and expressed as
// "set the column to itself plus this", so the arithmetic happens in the
// database. Learning a message is a few hundred tokens, which is one round
// trip's worth of work and not worth a bulk-loading scheme.
func (self *database) LearnSpamTokens(deltas []models.SpamTokenDelta) error {
	if len(deltas) == 0 {
		return nil
	}
	return self.db.Transaction(func(tx *gorm.DB) error {
		return learnSpamTokens(tx, deltas)
	})
}

// learnSpamTokens applies deltas inside a transaction. In token order, so that
// two transactions touching the same tokens take their row locks in the same
// order and neither waits for the other while holding what it wants.
func learnSpamTokens(tx *gorm.DB, deltas []models.SpamTokenDelta) error {
	ordered := slices.Clone(deltas)
	slices.SortFunc(ordered, func(first, second models.SpamTokenDelta) int {
		return strings.Compare(first.Token, second.Token)
	})
	now := time.Now().UTC()
	for _, delta := range ordered {
		if delta.Token == "" {
			continue
		}
		model := &spamTokenModel{
			Token:      delta.Token,
			SpamCount:  delta.SpamCount,
			HamCount:   delta.HamCount,
			ModifiedAt: now,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "token"}},
			DoUpdates: clause.Assignments(map[string]any{
				"spam_count":  gorm.Expr(`"spam_token"."spam_count" + ?`, delta.SpamCount),
				"ham_count":   gorm.Expr(`"spam_token"."ham_count" + ?`, delta.HamCount),
				"modified_at": now,
			}),
		}).Create(model).Error; err != nil {
			return err
		}
	}
	return nil
}

// lockSpamTraining takes the lock that makes the marking of one message one
// transaction at a time, even on two instances, until the transaction ends.
func lockSpamTraining(tx *gorm.DB, mailId string) error {
	return tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, "spam-training:"+mailId).Error
}

func (self *database) LearnSpamMail(mailId, label string, tokens []string) error {
	if mailId == "" || (label != models.SpamTrainingLabelSpam && label != models.SpamTrainingLabelHam) {
		return ErrInvalidArguments
	}
	return self.db.Transaction(func(tx *gorm.DB) error {
		if err := lockSpamTraining(tx, mailId); err != nil {
			return err
		}
		var existing spamTrainingModel
		result := tx.Where("mail_id = ?", mailId).Limit(1).Find(&existing)
		if result.Error != nil {
			return result.Error
		}
		hasExisting := result.RowsAffected > 0
		if hasExisting && existing.Label == label {
			return nil
		}
		deltas := make([]models.SpamTokenDelta, 0, len(tokens))
		for _, token := range tokens {
			delta := models.SpamTokenDelta{Token: token}
			if label == models.SpamTrainingLabelSpam {
				delta.SpamCount = 1
			} else {
				delta.HamCount = 1
			}
			// Re-labeled: take back what the previous label contributed.
			if hasExisting {
				if existing.Label == models.SpamTrainingLabelSpam {
					delta.SpamCount--
				} else {
					delta.HamCount--
				}
			}
			deltas = append(deltas, delta)
		}
		if err := learnSpamTokens(tx, deltas); err != nil {
			return err
		}
		return setSpamTraining(tx, &models.SpamTraining{MailID: mailId, Label: label})
	})
}

func (self *database) ForgetSpamMail(mailId string, tokens []string) error {
	if mailId == "" {
		return ErrInvalidArguments
	}
	return self.db.Transaction(func(tx *gorm.DB) error {
		if err := lockSpamTraining(tx, mailId); err != nil {
			return err
		}
		var existing spamTrainingModel
		result := tx.Where("mail_id = ?", mailId).Limit(1).Find(&existing)
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
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
		if err := learnSpamTokens(tx, deltas); err != nil {
			return err
		}
		return tx.Where("mail_id = ?", mailId).Delete(&spamTrainingModel{}).Error
	})
}

func (self *database) LoadSpamTokens(tokens []string) (map[string]models.SpamTokenCount, error) {
	counts := make(map[string]models.SpamTokenCount, len(tokens))
	if len(tokens) == 0 {
		return counts, nil
	}

	// Asked in batches: a message can carry a few hundred distinct tokens,
	// and a single IN list of every one of them is a statement no plan cache
	// will ever reuse.
	const batchSize = 200
	for start := 0; start < len(tokens); start += batchSize {
		end := min(start+batchSize, len(tokens))

		var models_ []spamTokenModel
		if err := self.db.Where("token IN ?", tokens[start:end]).Find(&models_).Error; err != nil {
			return nil, err
		}
		for _, model := range models_ {
			counts[model.Token] = models.SpamTokenCount{
				SpamCount: model.SpamCount,
				HamCount:  model.HamCount,
			}
		}
	}
	return counts, nil
}

func (self *database) GetSpamTraining(mailId string) (*models.SpamTraining, error) {
	var model spamTrainingModel
	if err := self.db.Where("mail_id = ?", mailId).First(&model).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &models.SpamTraining{
		MailID:     model.MailID,
		Label:      model.Label,
		CreatedAt:  model.CreatedAt,
		ModifiedAt: model.ModifiedAt,
	}, nil
}

func (self *database) SetSpamTraining(training *models.SpamTraining) error {
	return setSpamTraining(self.db, training)
}

func setSpamTraining(tx *gorm.DB, training *models.SpamTraining) error {
	now := time.Now().UTC()
	model := &spamTrainingModel{
		MailID:     training.MailID,
		Label:      training.Label,
		CreatedAt:  now,
		ModifiedAt: now,
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "mail_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"label":       training.Label,
			"modified_at": now,
		}),
	}).Create(model).Error
}

func (self *database) DeleteSpamTraining(mailId string) error {
	return self.db.Where("mail_id = ?", mailId).Delete(&spamTrainingModel{}).Error
}

func (self *database) CountSpamTraining() (int64, int64, error) {
	type row struct {
		Label string
		Count int64
	}
	var rows []row
	if err := self.db.Model(&spamTrainingModel{}).
		Select("label, count(*) as count").
		Group("label").
		Scan(&rows).Error; err != nil {
		return 0, 0, err
	}

	var spam, ham int64
	for _, counted := range rows {
		switch counted.Label {
		case models.SpamTrainingLabelSpam:
			spam = counted.Count
		case models.SpamTrainingLabelHam:
			ham = counted.Count
		}
	}
	return spam, ham, nil
}

type spamRuleSetModel struct {
	Channel      string    `gorm:"column:channel;primaryKey;size:256"`
	Version      string    `gorm:"column:version;size:64"`
	Content      []byte    `gorm:"column:content"`
	RulesLoaded  int       `gorm:"column:rules_loaded"`
	RulesSkipped int       `gorm:"column:rules_skipped"`
	UpdatedAt    time.Time `gorm:"column:updated_at"`
	Error        string    `gorm:"column:error"`
}

func (spamRuleSetModel) TableName() string {
	return "spam_rule_set"
}

func spamRuleSetFromModel(model *spamRuleSetModel) *models.SpamRuleSet {
	return &models.SpamRuleSet{
		Channel:      model.Channel,
		Version:      model.Version,
		Content:      model.Content,
		RulesLoaded:  model.RulesLoaded,
		RulesSkipped: model.RulesSkipped,
		UpdatedAt:    model.UpdatedAt,
		Error:        model.Error,
	}
}

// SpamRuleSetVersion asks only for the version, because every instance asks
// on a timer and the rules themselves are megabytes.
func (self *database) SpamRuleSetVersion(channel string) (string, error) {
	var version string
	err := self.db.Model(&spamRuleSetModel{}).
		Where("channel = ?", channel).
		Pluck("version", &version).Error
	if err != nil {
		return "", err
	}
	return version, nil
}

func (self *database) LoadSpamRuleSet(channel string) (*models.SpamRuleSet, error) {
	var model spamRuleSetModel
	if err := self.db.Where("channel = ?", channel).First(&model).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return spamRuleSetFromModel(&model), nil
}

func (self *database) SaveSpamRuleSet(ruleSet *models.SpamRuleSet) error {
	now := time.Now().UTC()
	model := &spamRuleSetModel{
		Channel:      ruleSet.Channel,
		Version:      ruleSet.Version,
		Content:      ruleSet.Content,
		RulesLoaded:  ruleSet.RulesLoaded,
		RulesSkipped: ruleSet.RulesSkipped,
		UpdatedAt:    now,
		Error:        ruleSet.Error,
	}
	return self.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "channel"}},
		DoUpdates: clause.Assignments(map[string]any{
			"version":       model.Version,
			"content":       model.Content,
			"rules_loaded":  model.RulesLoaded,
			"rules_skipped": model.RulesSkipped,
			"updated_at":    now,
			"error":         model.Error,
		}),
	}).Create(model).Error
}

func (self *database) RecordSpamRuleSetCounts(channel, version string, rulesLoaded, rulesSkipped int) error {
	return self.db.Model(&spamRuleSetModel{}).
		Where("channel = ? AND version = ?", channel, version).
		Updates(map[string]any{"rules_loaded": rulesLoaded, "rules_skipped": rulesSkipped}).Error
}

func (self *database) ListSpamRuleSets() ([]*models.SpamRuleSet, error) {
	var found []spamRuleSetModel
	// Without the content: this is a listing for a page, and the rule text
	// is megabytes that nothing on it displays.
	if err := self.db.Model(&spamRuleSetModel{}).
		Select("channel, version, rules_loaded, rules_skipped, updated_at, error").
		Order("channel").Find(&found).Error; err != nil {
		return nil, err
	}
	sets := make([]*models.SpamRuleSet, 0, len(found))
	for index := range found {
		sets = append(sets, spamRuleSetFromModel(&found[index]))
	}
	return sets, nil
}
