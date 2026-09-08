package db

import (
	"time"

	"gorm.io/gorm/clause"
)

// The cached logo a sending domain publishes, and the record of having asked.
//
// A row exists for every domain that has been looked up, whether or not it
// published anything: "this domain has no logo" is worth remembering, or every
// listing asks DNS about every sender again.

type BimiQuery interface {
	// GetBimiLogo is what is cached for a domain, or nil when it has never
	// been looked up.
	GetBimiLogo(domain, selector string) (*BimiLogo, error)

	// ListBimiLogos is what is cached for these domains, keyed by domain, for
	// a listing that needs many at once.
	ListBimiLogos(domains []string, selector string) (map[string]*BimiLogo, error)

	// SaveBimiLogo records what was found, including finding nothing.
	SaveBimiLogo(logo *BimiLogo) error

	// ListSenderDomainsWithoutLogo is the domains this mailbox's subscription
	// mail comes from that have not been looked up since the given time —
	// what the background job works through.
	ListSenderDomainsWithoutLogo(before time.Time, limit int) ([]string, error)

	// GetBimiPublication is the logo this server publishes for one of its own
	// domains, or nil when it publishes none.
	GetBimiPublication(domainId string) (*BimiPublication, error)

	// GetBimiPublicationByFile is the same row found by the name in the
	// address a receiver fetches.
	GetBimiPublicationByFile(fileId string) (*BimiPublication, error)

	// SaveBimiPublication records an uploaded logo, replacing whatever the
	// domain published before.
	SaveBimiPublication(publication *BimiPublication) error

	// DeleteBimiPublication stops publishing one, and says which file to
	// remove from storage.
	DeleteBimiPublication(domainId string) (string, error)
}

// BimiPublication is a logo this server publishes for one of its own domains.
// The bytes are in the file store under FileID; this is what they are.
type BimiPublication struct {
	DomainID   string `gorm:"primary_key:true;column:domain_id;size:32"`
	FileID     string `gorm:"column:file_id;size:32"`
	Filename   string `gorm:"column:filename;type:text"`
	Title      string `gorm:"column:title;type:text"`
	CreatedAt  time.Time
	ModifiedAt time.Time
}

func (self *BimiPublication) TableName() string {
	return "bimi_publication"
}

func (self *transaction) GetBimiPublication(domainId string) (*BimiPublication, error) {
	return self.findPublication("\"domain_id\" = ?", domainId)
}

func (self *transaction) GetBimiPublicationByFile(fileId string) (*BimiPublication, error) {
	return self.findPublication("\"file_id\" = ?", fileId)
}

func (self *transaction) findPublication(where string, value string) (*BimiPublication, error) {
	var rows []BimiPublication
	if err := self.tx.Where(where, value).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	publication := rows[0]
	publication.CreatedAt = publication.CreatedAt.In(time.Local)
	publication.ModifiedAt = publication.ModifiedAt.In(time.Local)
	return &publication, nil
}

func (self *transaction) SaveBimiPublication(publication *BimiPublication) error {
	now := time.Now().In(time.Local)
	publication.ModifiedAt = now
	if publication.CreatedAt.IsZero() {
		publication.CreatedAt = now
	}
	return self.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "domain_id"}},
		UpdateAll: true,
	}).Create(publication).Error
}

func (self *transaction) DeleteBimiPublication(domainId string) (string, error) {
	publication, err := self.GetBimiPublication(domainId)
	if err != nil || publication == nil {
		return "", err
	}
	if err := self.tx.Where("\"domain_id\" = ?", domainId).Delete(&BimiPublication{}).Error; err != nil {
		return "", err
	}
	return publication.FileID, nil
}

// BimiLogo is one domain's logo as this server holds it.
type BimiLogo struct {
	Domain      string    `gorm:"primary_key:true;column:domain;type:text"`
	Selector    string    `gorm:"primary_key:true;column:selector;type:text"`
	CheckedAt   time.Time `gorm:"column:checked_at"`
	LogoURL     string    `gorm:"column:logo_url;type:text"`
	ContentType string    `gorm:"column:content_type;size:64"`
	Content     []byte    `gorm:"column:content"`
	Error       string    `gorm:"column:error;type:text"`
}

func (self *BimiLogo) TableName() string {
	return "bimi_logo"
}

func (self *transaction) GetBimiLogo(domain, selector string) (*BimiLogo, error) {
	var rows []BimiLogo
	if err := self.tx.Where("\"domain\" = ? AND \"selector\" = ?", domain, selector).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	logo := rows[0]
	logo.CheckedAt = logo.CheckedAt.In(time.Local)
	return &logo, nil
}

func (self *transaction) ListBimiLogos(domains []string, selector string) (map[string]*BimiLogo, error) {
	logos := map[string]*BimiLogo{}
	if len(domains) == 0 {
		return logos, nil
	}
	var rows []BimiLogo
	// Without the content: a listing wants to know whether there is a logo,
	// and the bytes are fetched one at a time by the page that shows them.
	if err := self.tx.Select("\"domain\", \"selector\", \"checked_at\", \"logo_url\", \"content_type\", \"error\"").
		Where("\"domain\" IN ? AND \"selector\" = ?", domains, selector).Find(&rows).Error; err != nil {
		return nil, err
	}
	for index := range rows {
		logo := rows[index]
		logo.CheckedAt = logo.CheckedAt.In(time.Local)
		logos[logo.Domain] = &logo
	}
	return logos, nil
}

func (self *transaction) SaveBimiLogo(logo *BimiLogo) error {
	return self.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "domain"}, {Name: "selector"}},
		UpdateAll: true,
	}).Create(logo).Error
}

func (self *transaction) ListSenderDomainsWithoutLogo(before time.Time, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 20
	}
	var domains []string
	// The sending domain of subscription mail a mailbox holds. Subscription
	// mail only: a logo is shown beside a newsletter, and looking up every
	// domain that ever wrote would ask DNS about a great many strangers.
	err := self.tx.Raw(`
		SELECT DISTINCT lower(split_part("mail"."from", '@', 2)) AS domain
		FROM "mail"
		WHERE "mail"."list_key" <> ''
		  AND position('@' in "mail"."from") > 0
		  AND EXISTS (SELECT 1 FROM "mailbox_item" WHERE "mailbox_item"."mail_id" = "mail"."id")
		  AND NOT EXISTS (
		    SELECT 1 FROM "bimi_logo"
		    WHERE "bimi_logo"."domain" = lower(split_part("mail"."from", '@', 2))
		      AND "bimi_logo"."checked_at" > ?)
		LIMIT ?`, before, limit).Scan(&domains).Error
	return domains, err
}
