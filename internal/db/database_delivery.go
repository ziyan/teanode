package db

import (
	"bytes"
	"encoding/json"
	"io"
	"time"

	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"
)

type DeliveryOperation interface {
	// retry deliveries
	ListDeliveriesToRetry(options *Options) ([]*models.Delivery, error)

	// list deliveries that belong to a specific domain (via mail's domain_id)
	ListDeliveriesByDomainID(domainId string, options *Options) ([]*models.Delivery, error)

	// list delivieries that belongs to specific mails
	ListDeliveries(mailIds []string, options *Options) ([]*models.Delivery, error)

	// get delivery by id
	GetDelivery(deliveryId string, options *Options) (*models.Delivery, error)

	// get multiple deliveries
	GetDeliveries(deliveryIds []string, options *Options) ([]*models.Delivery, error)

	// create delivery
	CreateDelivery(delivery *models.Delivery, options *Options) (*models.Delivery, error)

	// create multiple deliveries
	CreateDeliveries(deliveries []*models.Delivery, options *Options) ([]*models.Delivery, error)

	// save delivery
	ModifyDelivery(deliveryId string, modifier func(*models.Delivery) error, options *Options) (*models.Delivery, error)

	// save multiple deliveries
	ModifyDeliveries(deliveryIds []string, modifier func([]*models.Delivery) error, options *Options) ([]*models.Delivery, error)

	// delete delivery
	DeleteDelivery(deliveryId string, options *Options) error
}

type deliveryModel struct {
	ID string `gorm:"primary_key:true;size:32"`

	CreatedAt  time.Time
	ModifiedAt time.Time

	MailID  string  `gorm:"size:32"`
	AliasID *string `gorm:"size:32"`

	Recipient string `gorm:"size:320"`

	Kind   string `gorm:"size:32"`
	Status string `gorm:"size:32"`

	Size uint64

	AttemptedAt *time.Time
	DeliveredAt *time.Time
	DroppedAt   *time.Time
	NotifiedAt  *time.Time
	RetryAt     *time.Time

	Attempts uint64

	Error string `gorm:"type:txt"`

	DeliveryStatuses []byte `gorm:"type:jsonb"`
}

func (self *deliveryModel) TableName() string {
	return "delivery"
}

func getDeliveryFromDeliveryModel(model deliveryModel) *models.Delivery {
	delivery := &models.Delivery{
		ID:         model.ID,
		CreatedAt:  model.CreatedAt.In(time.Local),
		ModifiedAt: model.ModifiedAt.In(time.Local),
		MailID:     model.MailID,
		Recipient:  model.Recipient,
		Kind:       models.GetDeliveryKind(model.Kind),
		Status:     models.GetDeliveryStatus(model.Status),
		Size:       model.Size,
		Attempts:   model.Attempts,
		Error:      model.Error,
	}
	if model.AliasID != nil {
		delivery.AliasID = *model.AliasID
	}
	if model.AttemptedAt != nil {
		deliveredAt := model.AttemptedAt.In(time.Local)
		delivery.AttemptedAt = &deliveredAt
	}
	if model.DeliveredAt != nil {
		deliveredAt := model.DeliveredAt.In(time.Local)
		delivery.DeliveredAt = &deliveredAt
	}
	if model.DroppedAt != nil {
		droppedAt := model.DroppedAt.In(time.Local)
		delivery.DroppedAt = &droppedAt
	}
	if model.NotifiedAt != nil {
		notifiedAt := model.NotifiedAt.In(time.Local)
		delivery.NotifiedAt = &notifiedAt
	}
	if model.RetryAt != nil {
		retryAt := model.RetryAt.In(time.Local)
		delivery.RetryAt = &retryAt
	}
	if len(model.DeliveryStatuses) > 0 {
		if err := json.Unmarshal(model.DeliveryStatuses, &delivery.DeliveryStatuses); err != nil && err != io.EOF {
			log.Warningf("failed to unmarshal delivery statuses in delivery %q: %s", model.ID, err)
		}
	}
	return delivery
}

func updateDeliveryModelFromDelivery(model *deliveryModel, delivery *models.Delivery) bool {
	var dirty bool
	if model.MailID != delivery.MailID {
		model.MailID = delivery.MailID
		dirty = true
	}
	if !optionalReferencesAreEqual(model.AliasID, delivery.AliasID) {
		model.AliasID = nil
		if aliasId := delivery.AliasID; aliasId != "" {
			model.AliasID = &aliasId
		}
		dirty = true
	}
	if model.Recipient != delivery.Recipient {
		model.Recipient = delivery.Recipient
		dirty = true
	}
	if model.Kind != delivery.Kind.String() {
		model.Kind = delivery.Kind.String()
		dirty = true
	}
	if model.Status != delivery.Status.String() {
		model.Status = delivery.Status.String()
		dirty = true
	}
	if model.Size != delivery.Size {
		model.Size = delivery.Size
		dirty = true
	}
	if !optionalTimesAreEqual(model.AttemptedAt, delivery.AttemptedAt) {
		model.AttemptedAt = delivery.AttemptedAt
		dirty = true
	}
	if !optionalTimesAreEqual(model.DeliveredAt, delivery.DeliveredAt) {
		model.DeliveredAt = delivery.DeliveredAt
		dirty = true
	}
	if !optionalTimesAreEqual(model.DroppedAt, delivery.DroppedAt) {
		model.DroppedAt = delivery.DroppedAt
		dirty = true
	}
	if !optionalTimesAreEqual(model.NotifiedAt, delivery.NotifiedAt) {
		model.NotifiedAt = delivery.NotifiedAt
		dirty = true
	}
	if !optionalTimesAreEqual(model.RetryAt, delivery.RetryAt) {
		model.RetryAt = delivery.RetryAt
		dirty = true
	}
	if model.Attempts != delivery.Attempts {
		model.Attempts = delivery.Attempts
		dirty = true
	}
	if model.Error != delivery.Error {
		model.Error = delivery.Error
		dirty = true
	}
	if rawDeliveryStatuses, err := json.Marshal(delivery.DeliveryStatuses); err == nil {
		if !bytes.Equal(model.DeliveryStatuses, rawDeliveryStatuses) {
			model.DeliveryStatuses = rawDeliveryStatuses
			dirty = true
		}
	} else {
		log.Warningf("failed to marshal delivery statuses in delivery %q: %s", model.ID, err)
	}

	// initialize
	if model.Status == "" {
		model.Status = models.DeliveryStatusQueued.String()
		delivery.Status = models.DeliveryStatusQueued
	}
	return dirty
}

func (self *transaction) queryDeliveries(options *Options) *gorm.DB {
	return self.query(&deliveryModel{}, options)
}

func (self *transaction) ListDeliveriesToRetry(options *Options) ([]*models.Delivery, error) {
	var existingModels []deliveryModel
	if err := self.tx.Raw(`UPDATE "delivery" SET "retry_at" = ? WHERE "id" IN (SELECT "id" FROM "delivery" WHERE "retry_at" < ? ORDER BY "retry_at" ASC LIMIT 8) RETURNING *`, time.Now().In(time.Local).Add(2*time.Hour), time.Now().In(time.Local)).Scan(&existingModels).Error; err != nil {
		return nil, err
	}
	deliveries := make([]*models.Delivery, 0, len(existingModels))
	for _, existingModel := range existingModels {
		deliveries = append(deliveries, getDeliveryFromDeliveryModel(existingModel))
	}
	return deliveries, nil
}

func (self *transaction) ListDeliveriesByDomainID(domainId string, options *Options) ([]*models.Delivery, error) {
	var existingModels []deliveryModel
	if err := self.queryDeliveries(options).Where("\"mail_id\" IN (SELECT \"id\" FROM \"mail\" WHERE \"domain_id\" = ?)", domainId).Find(&existingModels).Error; err != nil {
		return nil, err
	}
	deliveries := make([]*models.Delivery, 0, len(existingModels))
	for _, existingModel := range existingModels {
		delivery := getDeliveryFromDeliveryModel(existingModel)
		// Every row came back through the domain filter above, so saying which
		// domain it belongs to is reading the query back, not guessing.
		delivery.DomainID = domainId
		deliveries = append(deliveries, delivery)
	}
	return deliveries, nil
}

func (self *transaction) ListDeliveries(mailIds []string, options *Options) ([]*models.Delivery, error) {
	if len(mailIds) == 0 {
		return nil, nil
	}
	var existingModels []deliveryModel
	if err := self.queryDeliveries(options).Where("\"mail_id\" IN (?)", mailIds).Find(&existingModels).Error; err != nil {
		return nil, err
	}
	deliveries := make([]*models.Delivery, 0, len(existingModels))
	for _, existingModel := range existingModels {
		deliveries = append(deliveries, getDeliveryFromDeliveryModel(existingModel))
	}
	return deliveries, nil
}

func (self *transaction) GetDelivery(deliveryId string, options *Options) (*models.Delivery, error) {
	deliveries, err := self.GetDeliveries([]string{deliveryId}, options)
	if err != nil {
		return nil, err
	}
	return deliveries[0], nil
}

func (self *transaction) GetDeliveries(deliveryIds []string, options *Options) ([]*models.Delivery, error) {
	if len(deliveryIds) == 0 {
		return nil, nil
	}
	var existingModels []deliveryModel
	if err := self.queryDeliveries(options).Where(deliveryIds).Find(&existingModels).Error; err != nil {
		return nil, err
	}
	existingModelsMap := make(map[string]deliveryModel)
	for _, existingModel := range existingModels {
		existingModelsMap[existingModel.ID] = existingModel
	}
	deliveries := make([]*models.Delivery, 0, len(deliveryIds))
	for _, deliveryId := range deliveryIds {
		existingModel, ok := existingModelsMap[deliveryId]
		if ok {
			deliveries = append(deliveries, getDeliveryFromDeliveryModel(existingModel))
		} else {
			deliveries = append(deliveries, nil)
		}
	}
	return deliveries, nil
}

func (self *transaction) CreateDelivery(delivery *models.Delivery, options *Options) (*models.Delivery, error) {
	if _, err := self.CreateDeliveries([]*models.Delivery{delivery}, options); err != nil {
		return nil, err
	}
	return delivery, nil
}

func (self *transaction) CreateDeliveries(deliveries []*models.Delivery, options *Options) ([]*models.Delivery, error) {
	now := time.Now().In(time.Local)
	for _, delivery := range deliveries {
		id := security.NewULID()
		newModel := deliveryModel{
			ID:         id,
			CreatedAt:  now,
			ModifiedAt: now,
		}
		updateDeliveryModelFromDelivery(&newModel, delivery)
		if err := self.tx.Create(&newModel).Error; err != nil {
			return nil, err
		}
		delivery.ID = id
		delivery.CreatedAt = now
		delivery.ModifiedAt = now
	}
	return deliveries, nil
}

func (self *transaction) ModifyDelivery(deliveryId string, modifier func(*models.Delivery) error, options *Options) (*models.Delivery, error) {
	deliveries, err := self.ModifyDeliveries([]string{deliveryId}, func(deliveries []*models.Delivery) error {
		return modifier(deliveries[0])
	}, options)
	if err != nil {
		return nil, err
	}
	return deliveries[0], nil
}

func (self *transaction) ModifyDeliveries(deliveryIds []string, modifier func([]*models.Delivery) error, options *Options) ([]*models.Delivery, error) {
	if len(deliveryIds) == 0 {
		return nil, nil
	}
	var existingModels []deliveryModel
	if err := self.tx.Model(&deliveryModel{}).Where(deliveryIds).Find(&existingModels).Error; err != nil {
		return nil, err
	}
	existingModelsMap := make(map[string]deliveryModel)
	for _, existingModel := range existingModels {
		existingModelsMap[existingModel.ID] = existingModel
	}
	deliveries := make([]*models.Delivery, 0, len(deliveryIds))
	for _, deliveryId := range deliveryIds {
		existingModel, ok := existingModelsMap[deliveryId]
		if ok {
			deliveries = append(deliveries, getDeliveryFromDeliveryModel(existingModel))
		} else {
			deliveries = append(deliveries, &models.Delivery{ID: deliveryId})
		}
	}
	if err := modifier(deliveries); err != nil {
		return nil, err
	}
	now := time.Now().In(time.Local)
	for _, delivery := range deliveries {
		existingModel, ok := existingModelsMap[delivery.ID]
		if ok {
			if updateDeliveryModelFromDelivery(&existingModel, delivery) {
				existingModel.ModifiedAt = now
				if err := self.tx.Save(&existingModel).Error; err != nil {
					return nil, err
				}
				delivery.ModifiedAt = now
			}
		} else {
			newModel := deliveryModel{
				ID:         delivery.ID,
				CreatedAt:  now,
				ModifiedAt: now,
			}
			updateDeliveryModelFromDelivery(&newModel, delivery)
			if err := self.tx.Create(&newModel).Error; err != nil {
				return nil, err
			}
			delivery.CreatedAt = now
			delivery.ModifiedAt = now
		}
	}
	return deliveries, nil
}

func (self *transaction) DeleteDelivery(deliveryId string, options *Options) error {
	if err := self.tx.Where([]string{deliveryId}).Delete(&deliveryModel{}).Error; err != nil {
		return err
	}
	return nil
}
