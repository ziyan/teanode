package db

import (
	"bytes"
	"encoding/json"
	"io"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/aggregate"
	"github.com/ziyan/teanode/internal/util/security"
)

type MailOperation interface {
	// list mails by domain id
	ListMails(domainId string, options *Options) ([]*models.Mail, error)

	// CountMailsBy groups mail by a column and counts each group, for a
	// filter menu that says how many rows each option would leave.
	CountMailsBy(domainIds []string, field string, options *Options) ([]*Facet, error)

	// get mail by id
	GetMail(mailId string, options *Options) (*models.Mail, error)

	// get multiple mails
	GetMails(mailIds []string, options *Options) ([]*models.Mail, error)

	// create mail
	CreateMail(mail *models.Mail, options *Options) (*models.Mail, error)

	// create multiple mails
	CreateMails(mails []*models.Mail, options *Options) ([]*models.Mail, error)

	// save mail
	ModifyMail(mailId string, modifier func(*models.Mail) error, options *Options) (*models.Mail, error)

	// save multiple mails
	ModifyMails(mailIds []string, modifier func([]*models.Mail) error, options *Options) ([]*models.Mail, error)

	// delete mail
	DeleteMail(mailId string, options *Options) error

	// scavenge mails
	ScavengeMails(options *Options) error
}

type mailModel struct {
	ID string `gorm:"primary_key:true;size:32"`

	CreatedAt  time.Time
	ModifiedAt time.Time

	DomainID     *string `gorm:"size:32"`
	CredentialID *string `gorm:"size:32"`
	DeliveryID   *string `gorm:"size:32"`

	EnvelopeID string `gorm:"size:32"`

	Hello string `gorm:"size:256"`

	IP   string `gorm:"size:64"`
	RDNS string `gorm:"column:rdns;size:256"`

	TLSVersion     string `gorm:"size:64"`
	TLSCipherSuite string `gorm:"size:64"`

	Location []byte `gorm:"type:jsonb"`

	Sender string `gorm:"size:320"`

	Recipients pq.StringArray `gorm:"type:varchar(320)[]"`

	MessageID string `gorm:"size:320"`
	From      string `gorm:"size:320"`
	Subject   string `gorm:"type:text"`

	Size uint64

	Status                string `gorm:"size:32"`
	AuthenticationResults []byte `gorm:"type:jsonb"`

	ReceivedAt time.Time

	Kind string `gorm:"size:32"`
}

func (self *mailModel) TableName() string {
	return "mail"
}

func getMailFromMailModel(model mailModel) *models.Mail {
	mail := &models.Mail{
		ID:             model.ID,
		CreatedAt:      model.CreatedAt.In(time.Local),
		ModifiedAt:     model.ModifiedAt.In(time.Local),
		EnvelopeID:     model.EnvelopeID,
		Hello:          model.Hello,
		IP:             model.IP,
		RDNS:           model.RDNS,
		TLSVersion:     model.TLSVersion,
		TLSCipherSuite: model.TLSCipherSuite,
		Sender:         model.Sender,
		Recipients:     model.Recipients,
		MessageID:      model.MessageID,
		From:           model.From,
		Subject:        model.Subject,
		Size:           model.Size,
		Status:         models.GetMailStatus(model.Status),
		ReceivedAt:     model.ReceivedAt,
		Kind:           models.GetMailKind(model.Kind),
	}
	if model.DomainID != nil {
		mail.DomainID = *model.DomainID
	}
	if model.CredentialID != nil {
		mail.CredentialID = *model.CredentialID
	}
	if model.DeliveryID != nil {
		mail.DeliveryID = *model.DeliveryID
	}
	if len(model.Location) > 0 {
		if err := json.Unmarshal(model.Location, &mail.Location); err != nil && err != io.EOF {
			log.Warningf("failed to unmarshal location in mail %q: %s", model.ID, err)
		}
	}
	if len(model.AuthenticationResults) > 0 {
		if err := json.Unmarshal(model.AuthenticationResults, &mail.AuthenticationResults); err != nil && err != io.EOF {
			log.Warningf("failed to unmarshal authentication results in mail %q: %s", model.ID, err)
		}
	}
	return mail
}

func updateMailModelFromMail(model *mailModel, mail *models.Mail) bool {
	var dirty bool
	if !optionalReferencesAreEqual(model.DomainID, mail.DomainID) {
		model.DomainID = nil
		if domainId := mail.DomainID; domainId != "" {
			model.DomainID = &domainId
		}
		dirty = true
	}
	if !optionalReferencesAreEqual(model.CredentialID, mail.CredentialID) {
		model.CredentialID = nil
		if credentialId := mail.CredentialID; credentialId != "" {
			model.CredentialID = &credentialId
		}
		dirty = true
	}
	if !optionalReferencesAreEqual(model.DeliveryID, mail.DeliveryID) {
		model.DeliveryID = nil
		if deliveryId := mail.DeliveryID; deliveryId != "" {
			model.DeliveryID = &deliveryId
		}
		dirty = true
	}
	if model.EnvelopeID != mail.EnvelopeID {
		model.EnvelopeID = mail.EnvelopeID
		dirty = true
	}
	if model.Hello != mail.Hello {
		model.Hello = mail.Hello
		dirty = true
	}
	if model.IP != mail.IP {
		model.IP = mail.IP
		dirty = true
	}
	if model.RDNS != mail.RDNS {
		model.RDNS = mail.RDNS
		dirty = true
	}
	if model.TLSVersion != mail.TLSVersion {
		model.TLSVersion = mail.TLSVersion
		dirty = true
	}
	if model.TLSCipherSuite != mail.TLSCipherSuite {
		model.TLSCipherSuite = mail.TLSCipherSuite
		dirty = true
	}
	if rawLocation, err := json.Marshal(mail.Location); err == nil {
		if !bytes.Equal(model.Location, rawLocation) {
			model.Location = rawLocation
			dirty = true
		}
	} else {
		log.Warningf("failed to marshal location in mail %q: %s", model.ID, err)
	}
	if model.Sender != mail.Sender {
		model.Sender = mail.Sender
		dirty = true
	}
	if !stringSlicesAreEqual(model.Recipients, mail.Recipients) {
		model.Recipients = mail.Recipients
		dirty = true
	}
	if model.MessageID != mail.MessageID {
		model.MessageID = mail.MessageID
		dirty = true
	}
	if model.From != mail.From {
		model.From = mail.From
		dirty = true
	}
	if model.Subject != mail.Subject {
		model.Subject = mail.Subject
		dirty = true
	}
	if model.Size != mail.Size {
		model.Size = mail.Size
		dirty = true
	}
	if model.Status != mail.Status.String() {
		model.Status = mail.Status.String()
		dirty = true
	}
	if rawAuthenticationResults, err := json.Marshal(mail.AuthenticationResults); err == nil {
		if !bytes.Equal(model.AuthenticationResults, rawAuthenticationResults) {
			model.AuthenticationResults = rawAuthenticationResults
			dirty = true
		}
	} else {
		log.Warningf("failed to marshal authentication results in mail %q: %s", model.ID, err)
	}
	if model.ReceivedAt != mail.ReceivedAt {
		model.ReceivedAt = mail.ReceivedAt
		dirty = true
	}
	if model.Kind != mail.Kind.String() {
		model.Kind = mail.Kind.String()
		dirty = true
	}

	// initialize
	if model.Status == "" {
		model.Status = models.MailStatusReceived.String()
		mail.Status = models.MailStatusReceived
	}
	return dirty
}

func (self *transaction) queryMails(options *Options) *gorm.DB {
	return self.query(&mailModel{}, options)
}

// CountMailsBy groups mail by one column and counts each group, so a filter
// menu can offer the values that exist and say how many rows each would
// leave. Counted over everything the match stages accept, not over the page
// that happens to be on screen.
func (self *transaction) CountMailsBy(domainIds []string, field string, options *Options) ([]*Facet, error) {
	column, err := aggregate.BuildDistinct([]string{field}, options.Columns)
	if err != nil {
		return nil, err
	}

	query := self.tx.Model(&mailModel{}).Where("\"domain_id\" IN (?)", domainIds)
	query, _, err = applyAggregations(query, options)
	if err != nil {
		return nil, err
	}

	var rows []struct {
		Value string
		Count int
	}
	if err := query.
		Select(column + " AS value, COUNT(*) AS count").
		Group(column).
		Order("count DESC, value ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	facets := make([]*Facet, 0, len(rows))
	for _, row := range rows {
		facets = append(facets, &Facet{Value: row.Value, Count: row.Count})
	}
	return facets, nil
}

func (self *transaction) ListMails(domainId string, options *Options) ([]*models.Mail, error) {
	var existingModels []mailModel
	if err := self.queryMails(options).Where("\"domain_id\" = ?", domainId).Find(&existingModels).Error; err != nil {
		return nil, err
	}
	mails := make([]*models.Mail, 0, len(existingModels))
	for _, existingModel := range existingModels {
		mails = append(mails, getMailFromMailModel(existingModel))
	}
	return mails, nil
}

func (self *transaction) GetMail(mailId string, options *Options) (*models.Mail, error) {
	mails, err := self.GetMails([]string{mailId}, options)
	if err != nil {
		return nil, err
	}
	return mails[0], nil
}

func (self *transaction) GetMails(mailIds []string, options *Options) ([]*models.Mail, error) {
	if len(mailIds) == 0 {
		return nil, nil
	}
	var existingModels []mailModel
	if err := self.queryMails(options).Where(mailIds).Find(&existingModels).Error; err != nil {
		return nil, err
	}
	existingModelsMap := make(map[string]mailModel)
	for _, existingModel := range existingModels {
		existingModelsMap[existingModel.ID] = existingModel
	}
	mails := make([]*models.Mail, 0, len(mailIds))
	for _, mailId := range mailIds {
		existingModel, ok := existingModelsMap[mailId]
		if ok {
			mails = append(mails, getMailFromMailModel(existingModel))
		} else {
			mails = append(mails, nil)
		}
	}
	return mails, nil
}

func (self *transaction) CreateMail(mail *models.Mail, options *Options) (*models.Mail, error) {
	if _, err := self.CreateMails([]*models.Mail{mail}, options); err != nil {
		return nil, err
	}
	return mail, nil
}

func (self *transaction) CreateMails(mails []*models.Mail, options *Options) ([]*models.Mail, error) {
	now := time.Now().In(time.Local)
	for _, mail := range mails {
		id := security.NewULID()
		newModel := mailModel{
			ID:         id,
			CreatedAt:  now,
			ModifiedAt: now,
		}
		updateMailModelFromMail(&newModel, mail)
		if err := self.tx.Create(&newModel).Error; err != nil {
			return nil, err
		}
		mail.ID = id
		mail.CreatedAt = now
		mail.ModifiedAt = now
	}
	return mails, nil
}

func (self *transaction) ModifyMail(mailId string, modifier func(*models.Mail) error, options *Options) (*models.Mail, error) {
	mails, err := self.ModifyMails([]string{mailId}, func(mails []*models.Mail) error {
		return modifier(mails[0])
	}, options)
	if err != nil {
		return nil, err
	}
	return mails[0], nil
}

func (self *transaction) ModifyMails(mailIds []string, modifier func([]*models.Mail) error, options *Options) ([]*models.Mail, error) {
	if len(mailIds) == 0 {
		return nil, nil
	}
	var existingModels []mailModel
	if err := self.tx.Model(&mailModel{}).Where(mailIds).Find(&existingModels).Error; err != nil {
		return nil, err
	}
	existingModelsMap := make(map[string]mailModel)
	for _, existingModel := range existingModels {
		existingModelsMap[existingModel.ID] = existingModel
	}
	mails := make([]*models.Mail, 0, len(mailIds))
	for _, mailId := range mailIds {
		existingModel, ok := existingModelsMap[mailId]
		if ok {
			mails = append(mails, getMailFromMailModel(existingModel))
		} else {
			mails = append(mails, &models.Mail{ID: mailId})
		}
	}
	if err := modifier(mails); err != nil {
		return nil, err
	}
	now := time.Now().In(time.Local)
	for _, mail := range mails {
		existingModel, ok := existingModelsMap[mail.ID]
		if ok {
			if updateMailModelFromMail(&existingModel, mail) {
				existingModel.ModifiedAt = now
				if err := self.tx.Save(&existingModel).Error; err != nil {
					return nil, err
				}
				mail.ModifiedAt = now
			}
		} else {
			newModel := mailModel{
				ID:         mail.ID,
				CreatedAt:  now,
				ModifiedAt: now,
			}
			updateMailModelFromMail(&newModel, mail)
			if err := self.tx.Create(&newModel).Error; err != nil {
				return nil, err
			}
			mail.CreatedAt = now
			mail.ModifiedAt = now
		}
	}
	return mails, nil
}

func (self *transaction) DeleteMail(mailId string, options *Options) error {
	if err := self.tx.Where([]string{mailId}).Delete(&mailModel{}).Error; err != nil {
		return err
	}
	return nil
}

func (self *transaction) ScavengeMails(options *Options) error {
	if err := self.tx.Where("\"received_at\" < ?", time.Now().In(time.Local).Add(-7*24*time.Hour)).Delete(&mailModel{}).Error; err != nil {
		return err
	}
	return nil
}
