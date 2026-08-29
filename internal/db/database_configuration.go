package db

import (
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The configuration tables, as rows.
//
// These carry the shape the database stores, not the shape the rest of the
// program works with; internal/config owns that, and maps between them. The
// split is what lets everything above config.Store stay the same whether the
// configuration came from a file or from here.

type ConfigurationRows struct {
	// Version is what the configuration was when it was read. Passing it back
	// to SaveConfiguration is how a write refuses to overwrite somebody
	// else's.
	Version int64

	Domains     []*DomainRow
	Aliases     []*AliasRow
	Credentials []*CredentialRow
	Users       []*UserRow
	// Settings holds each section as a YAML document, keyed by section name.
	//
	// YAML rather than JSON, and not because of taste: a secret is raw bytes,
	// and encoding/json replaces a byte that is not valid UTF-8 with the
	// replacement character. A server secret that came back changed would
	// invalidate every SMTP password on the server, silently. YAML writes
	// such a string as !!binary, which is also what the exported file does,
	// so the two forms cannot drift.
	Settings map[string]string
}

type DomainRow struct {
	ID                       string
	Domain                   string
	Subdomain                string
	Comment                  string
	SpamFilterScoreThreshold float64
	DKIMSelector             string
	DKIMPrivateKey           string
	Certificate              string
	CertificatePrivateKey    string
	MailServers              string
	LinkHost                 string
}

type AliasRow struct {
	ID         string
	DomainID   string
	Position   int
	Pattern    string
	Comment    string
	Kind       string
	Email      string
	Webhook    string
	MailServer string
	Disabled   bool
}

type CredentialRow struct {
	ID       string
	DomainID string
	Position int
	Key      string
	Comment  string
	Alias    string
	Disabled bool
}

// UserRow is an account as it is stored: keyed by an identifier, so that
// renaming one does not invalidate the sessions, tokens and passkeys that name
// it.
type UserRow struct {
	ID           string
	Username     string
	Name         string
	PasswordHash string
	Email        string
}

// gorm models. Separate from the rows above so the column names live in one
// place and the rest of the package does not carry struct tags around.

type domainModel struct {
	ID                       string    `gorm:"column:id;primaryKey"`
	CreatedAt                time.Time `gorm:"column:created_at"`
	ModifiedAt               time.Time `gorm:"column:modified_at"`
	Domain                   string    `gorm:"column:domain"`
	Subdomain                string    `gorm:"column:subdomain"`
	Comment                  string    `gorm:"column:comment"`
	SpamFilterScoreThreshold float64   `gorm:"column:spam_filter_score_threshold"`
	DKIMSelector             string    `gorm:"column:dkim_selector"`
	DKIMPrivateKey           string    `gorm:"column:dkim_private_key"`
	Certificate              string    `gorm:"column:certificate"`
	CertificatePrivateKey    string    `gorm:"column:certificate_private_key"`
	MailServers              string    `gorm:"column:mail_servers"`
	LinkHost                 string    `gorm:"column:link_host"`
}

func (domainModel) TableName() string { return "domain" }

type aliasModel struct {
	ID         string    `gorm:"column:id;primaryKey"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	ModifiedAt time.Time `gorm:"column:modified_at"`
	DomainID   string    `gorm:"column:domain_id"`
	Position   int       `gorm:"column:position"`
	Pattern    string    `gorm:"column:pattern"`
	Comment    string    `gorm:"column:comment"`
	Kind       string    `gorm:"column:kind"`
	Email      string    `gorm:"column:email"`
	Webhook    string    `gorm:"column:webhook"`
	MailServer string    `gorm:"column:mail_server;type:text"`
	Disabled   bool      `gorm:"column:disabled"`
}

func (aliasModel) TableName() string { return "alias" }

type credentialModel struct {
	ID         string    `gorm:"column:id;primaryKey"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	ModifiedAt time.Time `gorm:"column:modified_at"`
	DomainID   string    `gorm:"column:domain_id"`
	Position   int       `gorm:"column:position"`
	Key        string    `gorm:"column:key"`
	Comment    string    `gorm:"column:comment"`
	Alias      string    `gorm:"column:alias"`
	Disabled   bool      `gorm:"column:disabled"`
}

func (credentialModel) TableName() string { return "credential" }

type userModel struct {
	ID           string    `gorm:"column:id;primaryKey"`
	CreatedAt    time.Time `gorm:"column:created_at"`
	ModifiedAt   time.Time `gorm:"column:modified_at"`
	Username     string    `gorm:"column:username"`
	Name         string    `gorm:"column:name"`
	PasswordHash string    `gorm:"column:password_hash"`
	Email        string    `gorm:"column:email"`
}

// "user" is a reserved word in PostgreSQL, so it is quoted. Every identifier
// in this project is quoted already.
func (userModel) TableName() string { return "user" }

// One row per configuration section, each holding that section as a YAML
// document. A column per field would mean a migration every time a setting is
// added; the parts that are lists — domains, aliases, credentials, accounts —
// are tables of their own because they are queried.
type configurationModel struct {
	Key        string    `gorm:"column:key;primaryKey"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	ModifiedAt time.Time `gorm:"column:modified_at"`
	Value      string    `gorm:"column:value;type:text"`
}

func (configurationModel) TableName() string { return "configuration" }

type configurationVersionModel struct {
	ID         int       `gorm:"column:id;primaryKey"`
	Version    int64     `gorm:"column:version"`
	ModifiedAt time.Time `gorm:"column:modified_at"`
}

func (configurationVersionModel) TableName() string { return "configuration_version" }

// ConfigurationVersion returns the version the stored configuration is at.
//
// Cheap enough to ask for on a timer: it is one row, and it is how an instance
// notices that another one changed something.
func (self *database) ConfigurationVersion() (int64, error) {
	var version configurationVersionModel
	if err := self.db.First(&version, 1).Error; err != nil {
		return 0, err
	}
	return version.Version, nil
}

// LoadConfiguration reads the whole configuration.
//
// The whole of it, every time, because it is small — a few dozen rows — and a
// partial read would need the caller to know which parts changed, which is
// exactly the bookkeeping this avoids.
func (self *database) LoadConfiguration() (*ConfigurationRows, error) {
	rows := &ConfigurationRows{Settings: map[string]string{}}

	if err := self.db.Transaction(func(tx *gorm.DB) error {
		var version configurationVersionModel
		if err := tx.First(&version, 1).Error; err != nil {
			return err
		}
		rows.Version = version.Version

		var domains []domainModel
		if err := tx.Order("\"domain\" ASC").Find(&domains).Error; err != nil {
			return err
		}
		for _, model := range domains {
			rows.Domains = append(rows.Domains, &DomainRow{
				ID:                       model.ID,
				Domain:                   model.Domain,
				Subdomain:                model.Subdomain,
				Comment:                  model.Comment,
				SpamFilterScoreThreshold: model.SpamFilterScoreThreshold,
				DKIMSelector:             model.DKIMSelector,
				DKIMPrivateKey:           model.DKIMPrivateKey,
				Certificate:              model.Certificate,
				CertificatePrivateKey:    model.CertificatePrivateKey,
				MailServers:              model.MailServers,
				LinkHost:                 model.LinkHost,
			})
		}

		var aliases []aliasModel
		if err := tx.Order("\"domain_id\" ASC, \"position\" ASC, \"id\" ASC").Find(&aliases).Error; err != nil {
			return err
		}
		for _, model := range aliases {
			rows.Aliases = append(rows.Aliases, &AliasRow{
				ID: model.ID, DomainID: model.DomainID, Position: model.Position,
				Pattern: model.Pattern, Comment: model.Comment, Kind: model.Kind,
				Email: model.Email, Webhook: model.Webhook,
				MailServer: model.MailServer, Disabled: model.Disabled,
			})
		}

		var credentials []credentialModel
		if err := tx.Order("\"domain_id\" ASC, \"position\" ASC, \"id\" ASC").Find(&credentials).Error; err != nil {
			return err
		}
		for _, model := range credentials {
			rows.Credentials = append(rows.Credentials, &CredentialRow{
				ID: model.ID, DomainID: model.DomainID, Position: model.Position,
				Key: model.Key, Comment: model.Comment, Alias: model.Alias,
				Disabled: model.Disabled,
			})
		}

		var users []userModel
		if err := tx.Order("\"username\" ASC").Find(&users).Error; err != nil {
			return err
		}
		for _, model := range users {
			rows.Users = append(rows.Users, &UserRow{
				ID: model.ID, Username: model.Username, Name: model.Name,
				PasswordHash: model.PasswordHash, Email: model.Email,
			})
		}

		var settings []configurationModel
		if err := tx.Find(&settings).Error; err != nil {
			return err
		}
		for _, model := range settings {
			rows.Settings[model.Key] = model.Value
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return rows, nil
}

// SaveConfiguration replaces the stored configuration with this one.
//
// All of it at once, inside a transaction that holds the version row: two
// instances changing configuration at the same moment take turns rather than
// overwriting each other, and a caller whose copy went stale in between is
// told so instead of silently erasing the other change.
//
// Replacing rather than diffing because the caller hands over a whole
// configuration, not a list of edits — working out which rows changed would
// be bookkeeping with nothing to check it against.
func (self *database) SaveConfiguration(rows *ConfigurationRows) (int64, error) {
	var saved int64

	if err := self.db.Transaction(func(tx *gorm.DB) error {
		var version configurationVersionModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&version, 1).Error; err != nil {
			return err
		}
		// Checked even when the caller says zero, because zero is a real
		// version: it is what a database that has never been configured
		// holds, and it is what the first write is made against. Treating it
		// as "do not check" would let two instances starting together both
		// seed, and the loser would go on running with a server secret that
		// is no longer the stored one.
		if rows.Version != version.Version {
			return fmt.Errorf("%w: the configuration changed to version %d while this change was being made",
				ErrConfigurationChanged, version.Version)
		}

		now := time.Now()

		// Aliases and credentials go with their domain, so replacing the
		// domains replaces them.
		//
		// Not the accounts. Sessions, tokens and passkeys reference an
		// account and cascade from it, so deleting every row and putting it
		// back would sign everybody out and revoke every token on any change
		// to any setting — which is exactly what it did, and what the
		// deployment test caught: an API token stopped working the moment the
		// server saved a configuration. They are reconciled below instead.
		for _, statement := range []string{
			`DELETE FROM "alias"`,
			`DELETE FROM "credential"`,
			`DELETE FROM "domain"`,
			`DELETE FROM "configuration"`,
		} {
			if err := tx.Exec(statement).Error; err != nil {
				return err
			}
		}

		for _, row := range rows.Domains {
			if err := tx.Create(&domainModel{
				ID: row.ID, CreatedAt: now, ModifiedAt: now,
				Domain: row.Domain, Subdomain: row.Subdomain, Comment: row.Comment,
				SpamFilterScoreThreshold: row.SpamFilterScoreThreshold,
				DKIMSelector:             row.DKIMSelector, DKIMPrivateKey: row.DKIMPrivateKey,
				Certificate: row.Certificate, CertificatePrivateKey: row.CertificatePrivateKey,
				MailServers: row.MailServers, LinkHost: row.LinkHost,
			}).Error; err != nil {
				return err
			}
		}
		for _, row := range rows.Aliases {
			if err := tx.Create(&aliasModel{
				ID: row.ID, CreatedAt: now, ModifiedAt: now,
				DomainID: row.DomainID, Position: row.Position, Pattern: row.Pattern,
				Comment: row.Comment, Kind: row.Kind, Email: row.Email,
				Webhook: row.Webhook, MailServer: row.MailServer, Disabled: row.Disabled,
			}).Error; err != nil {
				return err
			}
		}
		for _, row := range rows.Credentials {
			if err := tx.Create(&credentialModel{
				ID: row.ID, CreatedAt: now, ModifiedAt: now,
				DomainID: row.DomainID, Position: row.Position, Key: row.Key,
				Comment: row.Comment, Alias: row.Alias, Disabled: row.Disabled,
			}).Error; err != nil {
				return err
			}
		}
		// The accounts, reconciled rather than replaced: updated where they
		// already exist, inserted where they do not, and deleted only when
		// they are actually gone — where the cascade taking their sessions
		// and tokens with them is the right answer.
		var existing []userModel
		if err := tx.Find(&existing).Error; err != nil {
			return err
		}
		keep := make(map[string]bool, len(rows.Users))
		known := make(map[string]bool, len(existing))
		for _, model := range existing {
			known[model.ID] = true
		}
		for _, row := range rows.Users {
			keep[row.ID] = true
			if known[row.ID] {
				if err := tx.Model(&userModel{}).Where("\"id\" = ?", row.ID).Updates(map[string]any{
					"modified_at":   now,
					"username":      row.Username,
					"name":          row.Name,
					"password_hash": row.PasswordHash,
					"email":         row.Email,
				}).Error; err != nil {
					return err
				}
				continue
			}
			if err := tx.Create(&userModel{
				ID: row.ID, CreatedAt: now, ModifiedAt: now,
				Username: row.Username, Name: row.Name,
				PasswordHash: row.PasswordHash, Email: row.Email,
			}).Error; err != nil {
				return err
			}
		}
		for _, model := range existing {
			if keep[model.ID] {
				continue
			}
			if err := tx.Where("\"id\" = ?", model.ID).Delete(&userModel{}).Error; err != nil {
				return err
			}
		}
		for key, value := range rows.Settings {
			if err := tx.Create(&configurationModel{
				Key: key, CreatedAt: now, ModifiedAt: now, Value: value,
			}).Error; err != nil {
				return err
			}
		}

		version.Version++
		version.ModifiedAt = now
		if err := tx.Save(&version).Error; err != nil {
			return err
		}
		saved = version.Version
		return nil
	}); err != nil {
		return 0, err
	}

	return saved, nil
}
