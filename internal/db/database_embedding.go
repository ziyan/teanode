package db

import (
	"fmt"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm/clause"
)

// EmbeddingOperation is the meaning of messages, as vectors.
type EmbeddingOperation interface {
	// PutMailEmbedding writes or replaces a message's vector for a model.
	PutMailEmbedding(mailboxId, mailId, model string, vector []float32) error

	// ListMailEmbeddings is the newest vectors a mailbox has for a model,
	// the candidates a search ranks.
	ListMailEmbeddings(mailboxId, model string, limit int) ([]*MailEmbedding, error)

	// ListMailWithoutEmbedding is the newest messages of a mailbox that
	// have no vector for the model yet, for a backfill.
	ListMailWithoutEmbedding(mailboxId, model string, limit int) ([]string, error)

	// DeleteMailEmbeddings forgets a mailbox's vectors.
	DeleteMailEmbeddings(mailboxId string) (int64, error)

	// ScavengeMailEmbeddings removes vectors of a mailbox for models other
	// than the one given: what a change of model left behind.
	ScavengeMailEmbeddings(mailboxId, model string) (int64, error)
}

// MailEmbedding is one message's vector.
type MailEmbedding struct {
	MailID    string
	MailboxID string
	Model     string
	Vector    []float32
	CreatedAt time.Time
}

type mailEmbeddingModel struct {
	MailID    string          `gorm:"column:mail_id;primaryKey"`
	MailboxID string          `gorm:"column:mailbox_id;primaryKey"`
	Model     string          `gorm:"column:model;primaryKey"`
	Vector    pq.Float32Array `gorm:"column:vector;type:real[]"`
	CreatedAt time.Time       `gorm:"column:created_at"`
}

func (mailEmbeddingModel) TableName() string { return "mail_embedding" }

func (self *transaction) PutMailEmbedding(mailboxId, mailId, model string, vector []float32) error {
	if mailboxId == "" || mailId == "" || model == "" || len(vector) == 0 {
		return fmt.Errorf("db: an embedding needs a mailbox, a message, a model and a vector")
	}
	row := &mailEmbeddingModel{MailID: mailId, MailboxID: mailboxId, Model: model, Vector: pq.Float32Array(vector), CreatedAt: time.Now()}
	return self.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "mail_id"}, {Name: "mailbox_id"}, {Name: "model"}},
		DoUpdates: clause.AssignmentColumns([]string{"vector", "created_at"}),
	}).Create(row).Error
}

func (self *transaction) ListMailEmbeddings(mailboxId, model string, limit int) ([]*MailEmbedding, error) {
	if limit <= 0 {
		limit = 2000
	}
	var rows []mailEmbeddingModel
	if err := self.tx.Where("\"mailbox_id\" = ? AND \"model\" = ?", mailboxId, model).Order("\"created_at\" DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	embeddings := make([]*MailEmbedding, 0, len(rows))
	for index := range rows {
		row := &rows[index]
		embeddings = append(embeddings, &MailEmbedding{MailID: row.MailID, MailboxID: row.MailboxID, Model: row.Model, Vector: []float32(row.Vector), CreatedAt: row.CreatedAt})
	}
	return embeddings, nil
}

func (self *transaction) ListMailWithoutEmbedding(mailboxId, model string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 200
	}
	var ids []string
	err := self.tx.Raw(`SELECT DISTINCT ON ("mail"."received_at", "mail"."id") "mail"."id"
		FROM "mailbox_item"
		JOIN "mailbox_folder" ON "mailbox_folder"."id" = "mailbox_item"."folder_id"
		JOIN "mail" ON "mail"."id" = "mailbox_item"."mail_id"
		LEFT JOIN "mail_embedding" ON "mail_embedding"."mail_id" = "mail"."id" AND "mail_embedding"."mailbox_id" = "mailbox_folder"."mailbox_id" AND "mail_embedding"."model" = ?
		WHERE "mailbox_folder"."mailbox_id" = ? AND "mail_embedding"."mail_id" IS NULL AND "mailbox_folder"."kind" NOT IN ('trash', 'junk')
		ORDER BY "mail"."received_at" DESC, "mail"."id" DESC
		LIMIT ?`, model, mailboxId, limit).Scan(&ids).Error
	return ids, err
}

func (self *transaction) DeleteMailEmbeddings(mailboxId string) (int64, error) {
	result := self.tx.Where("\"mailbox_id\" = ?", mailboxId).Delete(&mailEmbeddingModel{})
	return result.RowsAffected, result.Error
}

func (self *transaction) ScavengeMailEmbeddings(mailboxId, model string) (int64, error) {
	result := self.tx.Where("\"mailbox_id\" = ? AND \"model\" <> ?", mailboxId, model).Delete(&mailEmbeddingModel{})
	return result.RowsAffected, result.Error
}
