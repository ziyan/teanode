package db

import (
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
	now := time.Now().UTC()
	return self.db.Transaction(func(tx *gorm.DB) error {
		for _, delta := range deltas {
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
	now := time.Now().UTC()
	model := &spamTrainingModel{
		MailID:     training.MailID,
		Label:      training.Label,
		CreatedAt:  now,
		ModifiedAt: now,
	}
	return self.db.Clauses(clause.OnConflict{
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
