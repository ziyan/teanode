package db

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// AcmeChallengeOperation is the challenges a certificate authority is about to check (acme_challenge).
type AcmeChallengeOperation interface {
	// PutAcmeChallenge stores the answer to a challenge, replacing one under
	// the same key, so that whichever instance the certificate authority's
	// check reaches can give it. The challengeKey is the token for http-01
	// and the lower case domain name for tls-alpn-01. Answers that expired
	// before now are removed on the way, so that one that is never cleaned
	// up (an instance that stopped mid-order) does not stay.
	PutAcmeChallenge(challengeKey, challengeKind, keyAuthorization string, expiresAt, now time.Time) error

	// DeleteAcmeChallenge removes the answer to a challenge. Not an error
	// when there is none.
	DeleteAcmeChallenge(challengeKey string) error

	// FindAcmeChallenge returns the key authorization stored for a challenge
	// of that kind that has not expired at now. ErrNotFound when there is
	// none.
	FindAcmeChallenge(challengeKey, challengeKind string, now time.Time) (string, error)

	// ListAcmeChallenges is every challenge that has not expired at now, by
	// kind and key: usually none, and never more than an order has names.
	ListAcmeChallenges(now time.Time) (map[AcmeChallengeKey]string, error)
}

// AcmeChallengeKey names one challenge: its kind and its key.
type AcmeChallengeKey struct {
	ChallengeKind string
	ChallengeKey  string
}

// acmeChallengeModel is the answer to one challenge a certificate authority is
// about to check.
type acmeChallengeModel struct {
	ChallengeKey     string    `gorm:"column:challenge_key;primaryKey"`
	ChallengeKind    string    `gorm:"column:challenge_kind"`
	KeyAuthorization string    `gorm:"column:key_authorization"`
	ExpiresAt        time.Time `gorm:"column:expires_at"`
}

func (acmeChallengeModel) TableName() string { return "acme_challenge" }

func (self *transaction) PutAcmeChallenge(challengeKey, challengeKind, keyAuthorization string, expiresAt, now time.Time) error {
	if err := self.tx.Where(`"expires_at" < ?`, now).Delete(&acmeChallengeModel{}).Error; err != nil {
		return err
	}
	return self.tx.Exec(`INSERT INTO "acme_challenge" ("challenge_key", "challenge_kind", "key_authorization", "expires_at") VALUES (?, ?, ?, ?)
		ON CONFLICT ("challenge_key") DO UPDATE SET "challenge_kind" = excluded."challenge_kind",
			"key_authorization" = excluded."key_authorization", "expires_at" = excluded."expires_at"`,
		challengeKey, challengeKind, keyAuthorization, expiresAt).Error
}

func (self *transaction) DeleteAcmeChallenge(challengeKey string) error {
	return self.tx.Where(`"challenge_key" = ?`, challengeKey).Delete(&acmeChallengeModel{}).Error
}

func (self *transaction) FindAcmeChallenge(challengeKey, challengeKind string, now time.Time) (string, error) {
	var model acmeChallengeModel
	err := self.tx.Where(`"challenge_key" = ? AND "challenge_kind" = ? AND "expires_at" > ?`, challengeKey, challengeKind, now).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return model.KeyAuthorization, nil
}

func (self *transaction) ListAcmeChallenges(now time.Time) (map[AcmeChallengeKey]string, error) {
	var models []acmeChallengeModel
	if err := self.tx.Where(`"expires_at" > ?`, now).Find(&models).Error; err != nil {
		return nil, err
	}
	challenges := make(map[AcmeChallengeKey]string, len(models))
	for _, model := range models {
		challenges[AcmeChallengeKey{ChallengeKind: model.ChallengeKind, ChallengeKey: model.ChallengeKey}] = model.KeyAuthorization
	}
	return challenges, nil
}
