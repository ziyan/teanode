package db

import (
	"time"

	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"
)

type LayoutOperation interface {
	// list layouts by domain id
	ListLayouts(domainId string, options *Options) ([]*models.Layout, error)

	// get layout by id
	GetLayout(layoutId string, options *Options) (*models.Layout, error)

	// get multiple layouts
	GetLayouts(layoutIds []string, options *Options) ([]*models.Layout, error)

	// create layout
	CreateLayout(layout *models.Layout, options *Options) (*models.Layout, error)

	// create multiple layouts
	CreateLayouts(layouts []*models.Layout, options *Options) ([]*models.Layout, error)

	// save layout
	ModifyLayout(layoutId string, modifier func(*models.Layout) error, options *Options) (*models.Layout, error)

	// save multiple layouts
	ModifyLayouts(layoutIds []string, modifier func([]*models.Layout) error, options *Options) ([]*models.Layout, error)

	// delete layout
	DeleteLayout(layoutId string, options *Options) error
}

type layoutModel struct {
	ID string `gorm:"primary_key:true;size:32"`

	CreatedAt  time.Time
	ModifiedAt time.Time

	DomainID string `gorm:"size:32"`

	Comment string `gorm:"type:text"`

	Locale string `gorm:"size:32"`

	HTMLContent string `gorm:"type:text"`

	TextContent string `gorm:"type:text"`
}

func (self *layoutModel) TableName() string {
	return "layout"
}

func getLayoutFromLayoutModel(model layoutModel, translations []translation) *models.Layout {
	layout := &models.Layout{
		ID:          model.ID,
		CreatedAt:   model.CreatedAt.In(time.Local),
		ModifiedAt:  model.ModifiedAt.In(time.Local),
		DomainID:    model.DomainID,
		Comment:     model.Comment,
		Locale:      model.Locale,
		HTMLContent: model.HTMLContent,
		TextContent: model.TextContent,
	}
	for _, each := range translations {
		layout.Translations = append(layout.Translations, &models.LayoutTranslation{
			Locale:      each.Locale,
			HTMLContent: each.HTMLContent,
			TextContent: each.TextContent,
		})
	}
	return layout
}

func getTranslationsFromLayout(layout *models.Layout) []translation {
	translations := make([]translation, 0, len(layout.Translations))
	for _, each := range layout.Translations {
		if each == nil {
			continue
		}
		translations = append(translations, translation{
			Locale:      each.Locale,
			HTMLContent: each.HTMLContent,
			TextContent: each.TextContent,
		})
	}
	return translations
}

func updateLayoutModelFromLayout(model *layoutModel, layout *models.Layout) bool {
	var dirty bool
	if model.DomainID != layout.DomainID {
		model.DomainID = layout.DomainID
		dirty = true
	}
	if model.Comment != layout.Comment {
		model.Comment = layout.Comment
		dirty = true
	}
	if model.Locale != layout.Locale {
		model.Locale = layout.Locale
		dirty = true
	}
	if model.HTMLContent != layout.HTMLContent {
		model.HTMLContent = layout.HTMLContent
		dirty = true
	}
	if model.TextContent != layout.TextContent {
		model.TextContent = layout.TextContent
		dirty = true
	}
	return dirty
}

func (self *transaction) queryLayouts(options *Options) *gorm.DB {
	return self.query(&layoutModel{}, options)
}

func (self *transaction) layoutsFromModels(existingModels []layoutModel) ([]*models.Layout, error) {
	layoutIds := make([]string, 0, len(existingModels))
	for _, existingModel := range existingModels {
		layoutIds = append(layoutIds, existingModel.ID)
	}
	translations, err := self.loadLayoutTranslations(layoutIds)
	if err != nil {
		return nil, err
	}
	layouts := make([]*models.Layout, 0, len(existingModels))
	for _, existingModel := range existingModels {
		layouts = append(layouts, getLayoutFromLayoutModel(existingModel, translations[existingModel.ID]))
	}
	return layouts, nil
}

func (self *transaction) ListLayouts(domainId string, options *Options) ([]*models.Layout, error) {
	var existingModels []layoutModel
	if err := self.queryLayouts(options).Where("\"domain_id\" = ?", domainId).Order("\"id\"").Find(&existingModels).Error; err != nil {
		return nil, err
	}
	return self.layoutsFromModels(existingModels)
}

func (self *transaction) GetLayout(layoutId string, options *Options) (*models.Layout, error) {
	layouts, err := self.GetLayouts([]string{layoutId}, options)
	if err != nil {
		return nil, err
	}
	return layouts[0], nil
}

func (self *transaction) GetLayouts(layoutIds []string, options *Options) ([]*models.Layout, error) {
	if len(layoutIds) == 0 {
		return nil, nil
	}
	var existingModels []layoutModel
	if err := self.queryLayouts(options).Where(layoutIds).Find(&existingModels).Error; err != nil {
		return nil, err
	}
	found, err := self.layoutsFromModels(existingModels)
	if err != nil {
		return nil, err
	}
	foundMap := make(map[string]*models.Layout, len(found))
	for _, layout := range found {
		foundMap[layout.ID] = layout
	}
	layouts := make([]*models.Layout, 0, len(layoutIds))
	for _, layoutId := range layoutIds {
		layouts = append(layouts, foundMap[layoutId])
	}
	return layouts, nil
}

func (self *transaction) CreateLayout(layout *models.Layout, options *Options) (*models.Layout, error) {
	if _, err := self.CreateLayouts([]*models.Layout{layout}, options); err != nil {
		return nil, err
	}
	return layout, nil
}

func (self *transaction) CreateLayouts(layouts []*models.Layout, options *Options) ([]*models.Layout, error) {
	now := time.Now().In(time.Local)
	for _, layout := range layouts {
		id := security.NewULID()
		newModel := layoutModel{
			ID:         id,
			CreatedAt:  now,
			ModifiedAt: now,
		}
		updateLayoutModelFromLayout(&newModel, layout)
		if err := self.tx.Create(&newModel).Error; err != nil {
			return nil, err
		}
		if _, err := self.saveLayoutTranslations(id, getTranslationsFromLayout(layout), now); err != nil {
			return nil, err
		}
		layout.ID = id
		layout.CreatedAt = now
		layout.ModifiedAt = now
	}
	return layouts, nil
}

func (self *transaction) ModifyLayout(layoutId string, modifier func(*models.Layout) error, options *Options) (*models.Layout, error) {
	layouts, err := self.ModifyLayouts([]string{layoutId}, func(layouts []*models.Layout) error {
		return modifier(layouts[0])
	}, options)
	if err != nil {
		return nil, err
	}
	return layouts[0], nil
}

func (self *transaction) ModifyLayouts(layoutIds []string, modifier func([]*models.Layout) error, options *Options) ([]*models.Layout, error) {
	if len(layoutIds) == 0 {
		return nil, nil
	}
	var existingModels []layoutModel
	if err := self.tx.Model(&layoutModel{}).Where(layoutIds).Find(&existingModels).Error; err != nil {
		return nil, err
	}
	existingModelsMap := make(map[string]layoutModel)
	for _, existingModel := range existingModels {
		existingModelsMap[existingModel.ID] = existingModel
	}
	found, err := self.layoutsFromModels(existingModels)
	if err != nil {
		return nil, err
	}
	foundMap := make(map[string]*models.Layout, len(found))
	for _, layout := range found {
		foundMap[layout.ID] = layout
	}
	layouts := make([]*models.Layout, 0, len(layoutIds))
	for _, layoutId := range layoutIds {
		if layout, ok := foundMap[layoutId]; ok {
			layouts = append(layouts, layout)
		} else {
			layouts = append(layouts, &models.Layout{ID: layoutId})
		}
	}
	if err := modifier(layouts); err != nil {
		return nil, err
	}
	now := time.Now().In(time.Local)
	for _, layout := range layouts {
		existingModel, ok := existingModelsMap[layout.ID]
		if ok {
			translationsDirty, err := self.saveLayoutTranslations(layout.ID, getTranslationsFromLayout(layout), now)
			if err != nil {
				return nil, err
			}
			if updateLayoutModelFromLayout(&existingModel, layout) || translationsDirty {
				existingModel.ModifiedAt = now
				if err := self.tx.Save(&existingModel).Error; err != nil {
					return nil, err
				}
				layout.ModifiedAt = now
			}
		} else {
			newModel := layoutModel{
				ID:         layout.ID,
				CreatedAt:  now,
				ModifiedAt: now,
			}
			updateLayoutModelFromLayout(&newModel, layout)
			if err := self.tx.Create(&newModel).Error; err != nil {
				return nil, err
			}
			if _, err := self.saveLayoutTranslations(layout.ID, getTranslationsFromLayout(layout), now); err != nil {
				return nil, err
			}
			layout.CreatedAt = now
			layout.ModifiedAt = now
		}
	}
	return layouts, nil
}

func (self *transaction) DeleteLayout(layoutId string, options *Options) error {
	// The translations go with it: the foreign key cascades. A template
	// using it keeps working without a layout: that key sets null.
	if err := self.tx.Where([]string{layoutId}).Delete(&layoutModel{}).Error; err != nil {
		return err
	}
	return nil
}
