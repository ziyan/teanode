package db

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/secretbox"
)

// The labels that scope the keys sealing a domain's secrets. Fixed for the
// lifetime of the rows sealed under them: nothing sealed under one opens
// under another, so changing a label would be a re-encryption of every
// domain rather than an edit. The suffix is there for the day that is
// deliberate.
const (
	domainKeyLabel         = "teanode-domain-dkim-privatekey-v1"
	domainCertificateLabel = "teanode-domain-tls-privatekey-v1"
)

// DomainOperation changes the domain table, in a transaction and with an
// audit row for every change. A domain is read with its aliases and
// credentials, which are its own tables and have their own operations.
type DomainOperation interface {
	ListDomains() ([]*models.Domain, error)
	GetDomain(domainId string) (*models.Domain, error)
	GetDomainByName(name string) (*models.Domain, error)
	CreateDomain(domain *models.Domain) (*models.Domain, error)
	UpdateDomain(domainId string, modify func(*models.Domain) error) (*models.Domain, error)
	DeleteDomain(domainId string) error

	// SetDomainCertificate stores a certificate obtained for the domain's own
	// mail server name. The server's own doing, on renewal, so no audit row.
	SetDomainCertificate(domainId string, certificate models.DomainCertificate) error
}

// AliasOperation changes a domain's aliases.
type AliasOperation interface {
	GetAlias(aliasId string) (*models.Alias, error)
	CreateAlias(alias *models.Alias) (*models.Alias, error)
	UpdateAlias(aliasId string, modify func(*models.Alias) error) (*models.Alias, error)
	DeleteAlias(aliasId string) error

	// ReorderAliases puts a domain's aliases in this order; ones not named
	// keep their relative order after the named ones.
	ReorderAliases(domainId string, aliasIds []string) error
}

// CredentialOperation changes a domain's credentials.
type CredentialOperation interface {
	GetCredential(credentialId string) (*models.Credential, error)
	CreateCredential(credential *models.Credential) (*models.Credential, error)
	UpdateCredential(credentialId string, modify func(*models.Credential) error) (*models.Credential, error)
	DeleteCredential(credentialId string) error
}

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
	MailboxID  string    `gorm:"column:mailbox_id"`
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

// sealer holds the two ciphers a domain's secrets are stored under, derived
// from the server secret. Nil boxes mean "store as it stands": the one moment
// that happens is a first run, before a secret exists, and the next write
// seals what this one could not.
type sealer struct {
	key         *secretbox.Box
	certificate *secretbox.Box
}

func newSealer(secret []byte) (*sealer, error) {
	if len(secret) == 0 {
		return &sealer{}, nil
	}
	key, err := secretbox.New(secret, domainKeyLabel)
	if err != nil {
		return nil, fmt.Errorf("db: cannot derive the key that protects the signing keys: %w", err)
	}
	certificate, err := secretbox.New(secret, domainCertificateLabel)
	if err != nil {
		return nil, fmt.Errorf("db: cannot derive the key that protects the certificate keys: %w", err)
	}
	return &sealer{key: key, certificate: certificate}, nil
}

func (self *sealer) seal(box *secretbox.Box, value string) (string, error) {
	if self == nil || box == nil || value == "" {
		return value, nil
	}
	return box.Seal([]byte(value))
}

// open reads a value that may have been written before the column was
// encrypted, which is taken as it stands; a sealed one that will not open is
// fatal and not skipped: a domain silently losing its key signs nothing and
// says nothing.
func (self *sealer) open(box *secretbox.Box, value, what string) (string, error) {
	if !secretbox.Sealed(value) {
		return value, nil
	}
	if self == nil || box == nil {
		return "", fmt.Errorf("db: the %s is encrypted and there is no server secret to open it with", what)
	}
	opened, err := box.Open(value)
	if err != nil {
		return "", fmt.Errorf("db: cannot open the %s: %w", what, err)
	}
	return string(opened), nil
}

func (self *database) SetSecret(secret []byte) error {
	sealer, err := newSealer(secret)
	if err != nil {
		return err
	}
	self.sealerMutex.Lock()
	defer self.sealerMutex.Unlock()
	self.sealer = sealer
	return nil
}

func (self *database) currentSealer() *sealer {
	self.sealerMutex.RLock()
	defer self.sealerMutex.RUnlock()
	return self.sealer
}

func domainFromModel(model *domainModel, sealer *sealer, aliases []*models.Alias, credentials []*models.Credential) (*models.Domain, error) {
	privateKey, err := sealer.open(sealerKeyBox(sealer), model.DKIMPrivateKey, fmt.Sprintf("signing key of domain %q", model.Domain))
	if err != nil {
		return nil, err
	}
	certificateKey, err := sealer.open(sealerCertificateBox(sealer), model.CertificatePrivateKey, fmt.Sprintf("certificate key of domain %q", model.Domain))
	if err != nil {
		return nil, err
	}
	if aliases == nil {
		aliases = []*models.Alias{}
	}
	if credentials == nil {
		credentials = []*models.Credential{}
	}
	return &models.Domain{
		ID:                       model.ID,
		CreatedAt:                model.CreatedAt.In(time.Local),
		ModifiedAt:               model.ModifiedAt.In(time.Local),
		Domain:                   model.Domain,
		Subdomain:                model.Subdomain,
		Comment:                  model.Comment,
		SpamFilterScoreThreshold: model.SpamFilterScoreThreshold,
		MailServers:              splitHosts(model.MailServers),
		LinkHost:                 model.LinkHost,
		DKIM:                     models.DomainKey{Selector: model.DKIMSelector, PrivateKey: privateKey},
		TLS:                      models.DomainCertificate{Certificate: model.Certificate, PrivateKey: certificateKey},
		Aliases:                  aliases,
		Credentials:              credentials,
	}, nil
}

func sealerKeyBox(sealer *sealer) *secretbox.Box {
	if sealer == nil {
		return nil
	}
	return sealer.key
}

func sealerCertificateBox(sealer *sealer) *secretbox.Box {
	if sealer == nil {
		return nil
	}
	return sealer.certificate
}

func (self *transaction) domainToModel(domain *models.Domain) (*domainModel, error) {
	sealer := self.database.currentSealer()
	privateKey, err := sealer.seal(sealerKeyBox(sealer), domain.DKIM.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("db: cannot encrypt the signing key of domain %q: %w", domain.Domain, err)
	}
	certificateKey, err := sealer.seal(sealerCertificateBox(sealer), domain.TLS.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("db: cannot encrypt the certificate key of domain %q: %w", domain.Domain, err)
	}
	return &domainModel{
		ID:                       domain.ID,
		CreatedAt:                domain.CreatedAt,
		ModifiedAt:               domain.ModifiedAt,
		Domain:                   domain.Domain,
		Subdomain:                domain.Subdomain,
		Comment:                  domain.Comment,
		SpamFilterScoreThreshold: domain.SpamFilterScoreThreshold,
		DKIMSelector:             domain.DKIM.Selector,
		DKIMPrivateKey:           privateKey,
		Certificate:              domain.TLS.Certificate,
		CertificatePrivateKey:    certificateKey,
		MailServers:              strings.Join(domain.MailServers, ","),
		LinkHost:                 domain.LinkHost,
	}, nil
}

// splitHosts reads back the comma separated list of mail server names. Empty
// means the default, which is derived rather than stored.
func splitHosts(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	hosts := make([]string, 0, 2)
	for _, host := range strings.Split(value, ",") {
		if host = strings.TrimSpace(host); host != "" {
			hosts = append(hosts, host)
		}
	}
	if len(hosts) == 0 {
		return nil
	}
	return hosts
}

func aliasFromModel(model *aliasModel) (*models.Alias, error) {
	alias := &models.Alias{
		ID:         model.ID,
		CreatedAt:  model.CreatedAt.In(time.Local),
		ModifiedAt: model.ModifiedAt.In(time.Local),
		DomainID:   model.DomainID,
		Position:   model.Position,
		Pattern:    model.Pattern,
		Comment:    model.Comment,
		Kind:       models.AliasKind(model.Kind),
		Email:      model.Email,
		Webhook:    model.Webhook,
		MailboxID:  model.MailboxID,
		Disabled:   model.Disabled,
	}
	if len(model.MailServer) > 0 {
		alias.MailServer = &models.MailServer{}
		if err := yaml.Unmarshal([]byte(model.MailServer), alias.MailServer); err != nil {
			return nil, fmt.Errorf("db: cannot read the mail server of alias %q: %w", model.ID, err)
		}
	}
	return alias, nil
}

func aliasToModel(alias *models.Alias) (*aliasModel, error) {
	model := &aliasModel{
		ID:         alias.ID,
		CreatedAt:  alias.CreatedAt,
		ModifiedAt: alias.ModifiedAt,
		DomainID:   alias.DomainID,
		Position:   alias.Position,
		Pattern:    alias.Pattern,
		Comment:    alias.Comment,
		Kind:       string(alias.Kind),
		Email:      alias.Email,
		Webhook:    alias.Webhook,
		MailboxID:  alias.MailboxID,
		Disabled:   alias.Disabled,
	}
	if alias.MailServer != nil {
		encoded, err := yaml.Marshal(alias.MailServer)
		if err != nil {
			return nil, fmt.Errorf("db: cannot write the mail server of alias %q: %w", alias.ID, err)
		}
		model.MailServer = string(encoded)
	}
	return model, nil
}

func credentialFromModel(model *credentialModel) *models.Credential {
	return &models.Credential{
		ID:         model.ID,
		CreatedAt:  model.CreatedAt.In(time.Local),
		ModifiedAt: model.ModifiedAt.In(time.Local),
		DomainID:   model.DomainID,
		Position:   model.Position,
		Key:        model.Key,
		Comment:    model.Comment,
		Alias:      model.Alias,
		Disabled:   model.Disabled,
	}
}

func credentialToModel(credential *models.Credential) *credentialModel {
	return &credentialModel{
		ID:         credential.ID,
		CreatedAt:  credential.CreatedAt,
		ModifiedAt: credential.ModifiedAt,
		DomainID:   credential.DomainID,
		Position:   credential.Position,
		Key:        credential.Key,
		Comment:    credential.Comment,
		Alias:      credential.Alias,
		Disabled:   credential.Disabled,
	}
}

// loadDomainChildren reads the aliases and credentials of these domains, in
// the order the operator arranged them.
func loadDomainChildren(db *gorm.DB, domainIds []string) (map[string][]*models.Alias, map[string][]*models.Credential, error) {
	aliases := map[string][]*models.Alias{}
	credentials := map[string][]*models.Credential{}
	if len(domainIds) == 0 {
		return aliases, credentials, nil
	}
	var aliasRows []aliasModel
	if err := db.Where("\"domain_id\" IN ?", domainIds).Order("\"domain_id\" ASC, \"position\" ASC, \"id\" ASC").Find(&aliasRows).Error; err != nil {
		return nil, nil, err
	}
	for index := range aliasRows {
		alias, err := aliasFromModel(&aliasRows[index])
		if err != nil {
			return nil, nil, err
		}
		aliases[alias.DomainID] = append(aliases[alias.DomainID], alias)
	}
	var credentialRows []credentialModel
	if err := db.Where("\"domain_id\" IN ?", domainIds).Order("\"domain_id\" ASC, \"position\" ASC, \"id\" ASC").Find(&credentialRows).Error; err != nil {
		return nil, nil, err
	}
	for index := range credentialRows {
		credential := credentialFromModel(&credentialRows[index])
		credentials[credential.DomainID] = append(credentials[credential.DomainID], credential)
	}
	return aliases, credentials, nil
}

func (self *transaction) getDomain(condition string, value string) (*models.Domain, error) {
	var model domainModel
	result := self.tx.Where(condition, value).Limit(1).Find(&model)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	aliases, credentials, err := loadDomainChildren(self.tx, []string{model.ID})
	if err != nil {
		return nil, err
	}
	return domainFromModel(&model, self.database.currentSealer(), aliases[model.ID], credentials[model.ID])
}

func (self *transaction) GetDomain(domainId string) (*models.Domain, error) {
	if domainId == "" {
		return nil, nil
	}
	return self.getDomain("\"id\" = ?", domainId)
}

func (self *transaction) GetDomainByName(name string) (*models.Domain, error) {
	name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
	if name == "" {
		return nil, nil
	}
	return self.getDomain("lower(\"domain\") = ?", name)
}

func (self *transaction) ListDomains() ([]*models.Domain, error) {
	var rows []domainModel
	if err := self.tx.Order("\"domain\" ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	domainIds := make([]string, 0, len(rows))
	for _, row := range rows {
		domainIds = append(domainIds, row.ID)
	}
	aliases, credentials, err := loadDomainChildren(self.tx, domainIds)
	if err != nil {
		return nil, err
	}
	sealer := self.database.currentSealer()
	domains := make([]*models.Domain, 0, len(rows))
	for index := range rows {
		domain, err := domainFromModel(&rows[index], sealer, aliases[rows[index].ID], credentials[rows[index].ID])
		if err != nil {
			return nil, err
		}
		domains = append(domains, domain)
	}
	return domains, nil
}

func (self *transaction) CreateDomain(domain *models.Domain) (*models.Domain, error) {
	if err := domain.Validate(); err != nil {
		return nil, err
	}
	if existing, err := self.GetDomainByName(domain.Domain); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, ErrAlreadyExists
	}
	now := time.Now()
	created := *domain
	if created.ID == "" {
		created.ID = newID()
	}
	created.CreatedAt = now
	created.ModifiedAt = now
	created.Aliases = nil
	created.Credentials = nil
	model, err := self.domainToModel(&created)
	if err != nil {
		return nil, err
	}
	if err := self.applyMutation(models.AuditResourceDomain, created.ID, models.AuditActionCreate, nil, &created, func(tx *gorm.DB) error {
		return tx.Create(model).Error
	}); err != nil {
		return nil, err
	}
	return self.GetDomain(created.ID)
}

func (self *transaction) UpdateDomain(domainId string, modify func(*models.Domain) error) (*models.Domain, error) {
	if err := lockRow(self.tx, &domainModel{}, domainId); err != nil {
		return nil, err
	}
	before, err := self.GetDomain(domainId)
	if err != nil {
		return nil, err
	}
	if before == nil {
		return nil, ErrNotFound
	}
	after := *before
	after.MailServers = append([]string(nil), before.MailServers...)
	if err := modify(&after); err != nil {
		return nil, err
	}
	if err := after.Validate(); err != nil {
		return nil, err
	}
	if !strings.EqualFold(after.Domain, before.Domain) {
		if other, err := self.GetDomainByName(after.Domain); err != nil {
			return nil, err
		} else if other != nil && other.ID != domainId {
			return nil, ErrAlreadyExists
		}
	}
	after.ID = before.ID
	after.CreatedAt = before.CreatedAt
	after.ModifiedAt = time.Now()
	model, err := self.domainToModel(&after)
	if err != nil {
		return nil, err
	}
	// The audit row records the domain, not its children: those have rows
	// of their own.
	auditBefore, auditAfter := *before, after
	auditBefore.Aliases, auditBefore.Credentials = nil, nil
	auditAfter.Aliases, auditAfter.Credentials = nil, nil
	if err := self.applyMutation(models.AuditResourceDomain, domainId, models.AuditActionUpdate, &auditBefore, &auditAfter, func(tx *gorm.DB) error {
		return tx.Model(&domainModel{}).Where("\"id\" = ?", domainId).Select("*").Omit("id", "created_at").Updates(model).Error
	}); err != nil {
		return nil, err
	}
	return self.GetDomain(domainId)
}

func (self *transaction) DeleteDomain(domainId string) error {
	before, err := self.GetDomain(domainId)
	if err != nil {
		return err
	}
	if before == nil {
		return ErrNotFound
	}
	auditBefore := *before
	auditBefore.Aliases, auditBefore.Credentials = nil, nil
	return self.applyMutation(models.AuditResourceDomain, domainId, models.AuditActionDelete, &auditBefore, nil, func(tx *gorm.DB) error {
		return tx.Where("\"id\" = ?", domainId).Delete(&domainModel{}).Error
	})
}

func (self *transaction) SetDomainCertificate(domainId string, certificate models.DomainCertificate) error {
	sealer := self.database.currentSealer()
	privateKey, err := sealer.seal(sealerCertificateBox(sealer), certificate.PrivateKey)
	if err != nil {
		return fmt.Errorf("db: cannot encrypt the certificate key of domain %q: %w", domainId, err)
	}
	result := self.tx.Model(&domainModel{}).Where("\"id\" = ?", domainId).Updates(map[string]any{
		"modified_at": time.Now(), "certificate": certificate.Certificate, "certificate_private_key": privateKey,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Aliases.

func (self *transaction) GetAlias(aliasId string) (*models.Alias, error) {
	if aliasId == "" {
		return nil, nil
	}
	var model aliasModel
	result := self.tx.Where("\"id\" = ?", aliasId).Limit(1).Find(&model)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return aliasFromModel(&model)
}

func (self *transaction) CreateAlias(alias *models.Alias) (*models.Alias, error) {
	if err := alias.Validate(); err != nil {
		return nil, err
	}
	if alias.DomainID == "" {
		return nil, ErrInvalidArguments
	}
	if err := lockRow(self.tx, &domainModel{}, alias.DomainID); err != nil {
		return nil, err
	}
	var position int
	if err := self.tx.Model(&aliasModel{}).Where("\"domain_id\" = ?", alias.DomainID).Select("COALESCE(MAX(\"position\"), -1) + 1").Scan(&position).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	created := *alias
	if created.ID == "" {
		created.ID = newID()
	}
	created.CreatedAt = now
	created.ModifiedAt = now
	created.Position = position
	model, err := aliasToModel(&created)
	if err != nil {
		return nil, err
	}
	if err := self.applyMutation(models.AuditResourceAlias, created.ID, models.AuditActionCreate, nil, &created, func(tx *gorm.DB) error {
		return tx.Create(model).Error
	}); err != nil {
		return nil, err
	}
	return self.GetAlias(created.ID)
}

func (self *transaction) UpdateAlias(aliasId string, modify func(*models.Alias) error) (*models.Alias, error) {
	if err := lockRow(self.tx, &aliasModel{}, aliasId); err != nil {
		return nil, err
	}
	before, err := self.GetAlias(aliasId)
	if err != nil {
		return nil, err
	}
	if before == nil {
		return nil, ErrNotFound
	}
	after := *before
	if before.MailServer != nil {
		mailServer := *before.MailServer
		after.MailServer = &mailServer
	}
	if err := modify(&after); err != nil {
		return nil, err
	}
	if err := after.Validate(); err != nil {
		return nil, err
	}
	after.ID = before.ID
	after.DomainID = before.DomainID
	after.CreatedAt = before.CreatedAt
	after.Position = before.Position
	after.ModifiedAt = time.Now()
	model, err := aliasToModel(&after)
	if err != nil {
		return nil, err
	}
	if err := self.applyMutation(models.AuditResourceAlias, aliasId, models.AuditActionUpdate, before, &after, func(tx *gorm.DB) error {
		return tx.Model(&aliasModel{}).Where("\"id\" = ?", aliasId).Select("*").Omit("id", "created_at", "domain_id", "position").Updates(model).Error
	}); err != nil {
		return nil, err
	}
	return self.GetAlias(aliasId)
}

func (self *transaction) DeleteAlias(aliasId string) error {
	before, err := self.GetAlias(aliasId)
	if err != nil {
		return err
	}
	if before == nil {
		return ErrNotFound
	}
	return self.applyMutation(models.AuditResourceAlias, aliasId, models.AuditActionDelete, before, nil, func(tx *gorm.DB) error {
		return tx.Where("\"id\" = ?", aliasId).Delete(&aliasModel{}).Error
	})
}

func (self *transaction) ReorderAliases(domainId string, aliasIds []string) error {
	if err := lockRow(self.tx, &domainModel{}, domainId); err != nil {
		return err
	}
	var rows []aliasModel
	if err := self.tx.Where("\"domain_id\" = ?", domainId).Order("\"position\" ASC, \"id\" ASC").Find(&rows).Error; err != nil {
		return err
	}
	order := make([]string, 0, len(rows))
	seen := map[string]bool{}
	byId := map[string]bool{}
	for _, row := range rows {
		byId[row.ID] = true
	}
	for _, aliasId := range aliasIds {
		if byId[aliasId] && !seen[aliasId] {
			order = append(order, aliasId)
			seen[aliasId] = true
		}
	}
	for _, row := range rows {
		if !seen[row.ID] {
			order = append(order, row.ID)
		}
	}
	now := time.Now()
	for position, aliasId := range order {
		if err := self.tx.Model(&aliasModel{}).Where("\"id\" = ?", aliasId).Updates(map[string]any{"position": position, "modified_at": now}).Error; err != nil {
			return err
		}
	}
	return nil
}

// Credentials.

func (self *transaction) GetCredential(credentialId string) (*models.Credential, error) {
	if credentialId == "" {
		return nil, nil
	}
	var model credentialModel
	result := self.tx.Where("\"id\" = ?", credentialId).Limit(1).Find(&model)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return credentialFromModel(&model), nil
}

func (self *transaction) CreateCredential(credential *models.Credential) (*models.Credential, error) {
	if err := credential.Validate(); err != nil {
		return nil, err
	}
	if credential.DomainID == "" {
		return nil, ErrInvalidArguments
	}
	if err := lockRow(self.tx, &domainModel{}, credential.DomainID); err != nil {
		return nil, err
	}
	var position int
	if err := self.tx.Model(&credentialModel{}).Where("\"domain_id\" = ?", credential.DomainID).Select("COALESCE(MAX(\"position\"), -1) + 1").Scan(&position).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	created := *credential
	if created.ID == "" {
		created.ID = newID()
	}
	created.CreatedAt = now
	created.ModifiedAt = now
	created.Position = position
	if err := self.applyMutation(models.AuditResourceCredential, created.ID, models.AuditActionCreate, nil, &created, func(tx *gorm.DB) error {
		return tx.Create(credentialToModel(&created)).Error
	}); err != nil {
		return nil, err
	}
	return self.GetCredential(created.ID)
}

func (self *transaction) UpdateCredential(credentialId string, modify func(*models.Credential) error) (*models.Credential, error) {
	if err := lockRow(self.tx, &credentialModel{}, credentialId); err != nil {
		return nil, err
	}
	before, err := self.GetCredential(credentialId)
	if err != nil {
		return nil, err
	}
	if before == nil {
		return nil, ErrNotFound
	}
	after := *before
	if err := modify(&after); err != nil {
		return nil, err
	}
	if err := after.Validate(); err != nil {
		return nil, err
	}
	after.ID = before.ID
	after.DomainID = before.DomainID
	after.CreatedAt = before.CreatedAt
	after.Position = before.Position
	after.ModifiedAt = time.Now()
	if err := self.applyMutation(models.AuditResourceCredential, credentialId, models.AuditActionUpdate, before, &after, func(tx *gorm.DB) error {
		return tx.Model(&credentialModel{}).Where("\"id\" = ?", credentialId).Select("*").Omit("id", "created_at", "domain_id", "position").Updates(credentialToModel(&after)).Error
	}); err != nil {
		return nil, err
	}
	return self.GetCredential(credentialId)
}

func (self *transaction) DeleteCredential(credentialId string) error {
	before, err := self.GetCredential(credentialId)
	if err != nil {
		return err
	}
	if before == nil {
		return ErrNotFound
	}
	return self.applyMutation(models.AuditResourceCredential, credentialId, models.AuditActionDelete, before, nil, func(tx *gorm.DB) error {
		return tx.Where("\"id\" = ?", credentialId).Delete(&credentialModel{}).Error
	})
}
