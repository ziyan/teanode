package db

import (
	"strings"
	"time"

	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// PhoneOperation is the phones people talk to their agent from.
type PhoneOperation interface {
	// PutAgentPhone writes a phone, keyed on the OAuth client the app
	// registered as: a phone registering again updates its row, whichever
	// of the client's tokens it registers with. A new phone is sent the
	// agent's own turns from the main conversation's newest message on,
	// never what was said before it was connected.
	PutAgentPhone(phone *models.AgentPhone) (*models.AgentPhone, error)
	// GetAgentPhone, ListAgentPhones and ListNotifiableAgentPhones see only
	// phones still signed in: their client holds a token that works or can
	// be renewed. A phone signed out any other way, its token revoked or
	// every session ended, drops out with it.
	GetAgentPhone(agentId, phoneId string) (*models.AgentPhone, error)
	ListAgentPhones(agentId string) ([]*models.AgentPhone, error)
	// ListNotifiableAgentPhones is every such phone with a registration, for
	// the loop that sends the agent's own turns.
	ListNotifiableAgentPhones() ([]*models.AgentPhone, error)
	UpdateAgentPhoneSettings(agentId, phoneId, phoneName string, notificationKinds []string) error
	DeleteAgentPhone(agentId, phoneId string) error

	// NoteAgentPhoneDelivery records how the last notification went; an
	// empty pushRegistration with isGone clears the registration, which is
	// what Apple saying the phone is gone means.
	NoteAgentPhoneDelivery(phoneId string, at time.Time, deliveryError string, isGone bool) error

	// AdvanceAgentPhonePush moves how far the agent's own turns have been
	// sent from one message to another, and says whether it did: only if
	// nobody moved it since it was read, which is what keeps a turn from
	// being sent twice when two instances look at once.
	AdvanceAgentPhonePush(phoneId, fromMessageId, throughMessageId string) (bool, error)

	// MarkAgentPhoneToured says the agent showed the person around the
	// phone, and whether this call was the one that did: of two instances
	// starting the tour at once, one goes on.
	MarkAgentPhoneToured(phoneId string, at time.Time) (bool, error)
}

type agentPhoneModel struct {
	ID                string     `gorm:"column:id;primaryKey"`
	CreatedAt         time.Time  `gorm:"column:created_at"`
	ModifiedAt        time.Time  `gorm:"column:modified_at"`
	AgentID           string     `gorm:"column:agent_id"`
	ClientID          string     `gorm:"column:client_id"`
	PhoneName         string     `gorm:"column:phone_name"`
	PhoneModel        string     `gorm:"column:phone_model"`
	PushRegistration  string     `gorm:"column:push_registration"`
	PayloadKey        string     `gorm:"column:payload_key"`
	NotificationKinds string     `gorm:"column:notification_kinds"`
	PushedThrough     string     `gorm:"column:pushed_through"`
	LastSeenAt        *time.Time `gorm:"column:last_seen_at"`
	LastDeliveredAt   *time.Time `gorm:"column:last_delivered_at"`
	LastDeliveryError string     `gorm:"column:last_delivery_error"`
	TouredAt          *time.Time `gorm:"column:toured_at"`
}

func (agentPhoneModel) TableName() string { return "agent_phone" }

func (self *agentPhoneModel) toModel() *models.AgentPhone {
	var kinds []string
	if self.NotificationKinds != "" {
		kinds = strings.Split(self.NotificationKinds, ",")
	}
	return &models.AgentPhone{
		ID: self.ID, CreatedAt: self.CreatedAt, ModifiedAt: self.ModifiedAt, AgentID: self.AgentID, ClientID: self.ClientID,
		PhoneName: self.PhoneName, PhoneModel: self.PhoneModel, PushRegistration: self.PushRegistration, PayloadKey: self.PayloadKey,
		NotificationKinds: kinds, PushedThrough: self.PushedThrough, LastSeenAt: self.LastSeenAt,
		LastDeliveredAt: self.LastDeliveredAt, LastDeliveryError: self.LastDeliveryError, TouredAt: self.TouredAt,
	}
}

func (self *transaction) MarkAgentPhoneToured(phoneId string, at time.Time) (bool, error) {
	result := self.tx.Model(&agentPhoneModel{}).Where(`"id" = ? AND "toured_at" IS NULL`, phoneId).Update("toured_at", at)
	return result.RowsAffected == 1, result.Error
}

func (self *transaction) PutAgentPhone(phone *models.AgentPhone) (*models.AgentPhone, error) {
	existing, err := self.agentPhoneByClient(phone.AgentID, phone.ClientID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	model := &agentPhoneModel{
		ID: newID(), CreatedAt: now, ModifiedAt: now, AgentID: phone.AgentID, ClientID: phone.ClientID,
		PhoneName: phone.PhoneName, PhoneModel: phone.PhoneModel, PushRegistration: phone.PushRegistration,
		PayloadKey: phone.PayloadKey, LastSeenAt: &now,
	}
	if existing != nil {
		model.ID, model.CreatedAt = existing.ID, existing.CreatedAt
		model.NotificationKinds = strings.Join(existing.NotificationKinds, ",")
		model.PushedThrough = existing.PushedThrough
		// A name the person gave the phone on the dashboard is theirs to
		// keep; the phone's own name only fills an empty one.
		if existing.PhoneName != "" {
			model.PhoneName = existing.PhoneName
		}
	} else {
		through, err := self.newestMainAgentMessageID(phone.AgentID)
		if err != nil {
			return nil, err
		}
		if through == "" {
			through = newID()
		}
		model.PushedThrough = through
	}
	if err := self.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "agent_id"}, {Name: "client_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"modified_at", "phone_name", "phone_model", "push_registration", "payload_key", "last_seen_at"}),
	}).Create(model).Error; err != nil {
		return nil, err
	}
	return model.toModel(), nil
}

// signedInPhone keeps the phones whose client holds a token that works or can
// still be renewed.
const signedInPhone = `EXISTS (SELECT 1 FROM "token" JOIN "agent" ON "agent"."user_id" = "token"."user_id"
	WHERE "agent"."id" = "agent_phone"."agent_id" AND "token"."client_id" = "agent_phone"."client_id"
	  AND "token"."revoked_at" IS NULL AND ("token"."expires_at" IS NULL OR "token"."expires_at" > ?))`

func (self *transaction) findPhones(query string, arguments ...any) ([]*models.AgentPhone, error) {
	var found []agentPhoneModel
	if err := self.tx.Where(query, arguments...).Where(signedInPhone, time.Now().Add(-models.RefreshWindow)).
		Order("\"created_at\"").Find(&found).Error; err != nil {
		return nil, err
	}
	phones := make([]*models.AgentPhone, 0, len(found))
	for index := range found {
		phones = append(phones, found[index].toModel())
	}
	return phones, nil
}

func (self *transaction) GetAgentPhone(agentId, phoneId string) (*models.AgentPhone, error) {
	phones, err := self.findPhones("\"agent_id\" = ? AND \"id\" = ?", agentId, phoneId)
	if err != nil || len(phones) == 0 {
		return nil, err
	}
	return phones[0], nil
}

// agentPhoneByClient is a client's phone whether or not it is signed in: a
// phone that connects again takes up its old row, and its settings.
func (self *transaction) agentPhoneByClient(agentId, clientId string) (*models.AgentPhone, error) {
	var found []agentPhoneModel
	if err := self.tx.Where("\"agent_id\" = ? AND \"client_id\" = ?", agentId, clientId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

func (self *transaction) ListAgentPhones(agentId string) ([]*models.AgentPhone, error) {
	return self.findPhones("\"agent_id\" = ?", agentId)
}

func (self *transaction) ListNotifiableAgentPhones() ([]*models.AgentPhone, error) {
	return self.findPhones("\"push_registration\" <> '' AND \"payload_key\" <> ''")
}

func (self *transaction) UpdateAgentPhoneSettings(agentId, phoneId, phoneName string, notificationKinds []string) error {
	return self.tx.Model(&agentPhoneModel{}).Where("\"agent_id\" = ? AND \"id\" = ?", agentId, phoneId).
		Updates(map[string]any{"phone_name": phoneName, "notification_kinds": strings.Join(notificationKinds, ","), "modified_at": time.Now()}).Error
}

func (self *transaction) DeleteAgentPhone(agentId, phoneId string) error {
	return self.tx.Where("\"agent_id\" = ? AND \"id\" = ?", agentId, phoneId).Delete(&agentPhoneModel{}).Error
}

func (self *transaction) NoteAgentPhoneDelivery(phoneId string, at time.Time, deliveryError string, isGone bool) error {
	updates := map[string]any{"last_delivery_error": deliveryError}
	if deliveryError == "" {
		updates["last_delivered_at"] = at
	}
	if isGone {
		updates["push_registration"] = ""
	}
	return self.tx.Model(&agentPhoneModel{}).Where("\"id\" = ?", phoneId).Updates(updates).Error
}

func (self *transaction) AdvanceAgentPhonePush(phoneId, fromMessageId, throughMessageId string) (bool, error) {
	result := self.tx.Model(&agentPhoneModel{}).
		Where("\"id\" = ? AND \"pushed_through\" = ?", phoneId, fromMessageId).
		Update("pushed_through", throughMessageId)
	return result.RowsAffected > 0, result.Error
}
