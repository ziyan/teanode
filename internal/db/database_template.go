package db

import (
	"time"

	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"
)

type TemplateOperation interface {
	// list templates by domain id
	ListTemplates(domainId string, options *Options) ([]*models.Template, error)

	// get template by id
	GetTemplate(templateId string, options *Options) (*models.Template, error)

	// get multiple templates
	GetTemplates(templateIds []string, options *Options) ([]*models.Template, error)

	// get template by name
	GetTemplateByName(domainId, name string, options *Options) (*models.Template, error)

	// create template
	CreateTemplate(template *models.Template, options *Options) (*models.Template, error)

	// create multiple templates
	CreateTemplates(templates []*models.Template, options *Options) ([]*models.Template, error)

	// save template
	ModifyTemplate(templateId string, modifier func(*models.Template) error, options *Options) (*models.Template, error)

	// save multiple templates
	ModifyTemplates(templateIds []string, modifier func([]*models.Template) error, options *Options) ([]*models.Template, error)

	// delete template
	DeleteTemplate(templateId string, options *Options) error
}

type templateModel struct {
	ID string `gorm:"primary_key:true;size:32"`

	CreatedAt  time.Time
	ModifiedAt time.Time

	DomainID string `gorm:"size:32"`

	LayoutID *string `gorm:"size:32"`

	Name string `gorm:"size:32"`

	Comment string `gorm:"type:text"`

	Locale string `gorm:"size:32"`

	Subject string `gorm:"size:256"`

	HTMLContent string `gorm:"type:text"`

	TextContent string `gorm:"type:text"`
}

func (self *templateModel) TableName() string {
	return "template"
}

func getTemplateFromTemplateModel(model templateModel, translations []translation) *models.Template {
	template := &models.Template{
		ID:          model.ID,
		CreatedAt:   model.CreatedAt.In(time.Local),
		ModifiedAt:  model.ModifiedAt.In(time.Local),
		DomainID:    model.DomainID,
		Name:        model.Name,
		Comment:     model.Comment,
		Locale:      model.Locale,
		Subject:     model.Subject,
		HTMLContent: model.HTMLContent,
		TextContent: model.TextContent,
	}
	if model.LayoutID != nil {
		template.LayoutID = *model.LayoutID
	}
	for _, each := range translations {
		template.Translations = append(template.Translations, &models.TemplateTranslation{
			Locale:      each.Locale,
			Subject:     each.Subject,
			HTMLContent: each.HTMLContent,
			TextContent: each.TextContent,
		})
	}
	return template
}

func getTranslationsFromTemplate(template *models.Template) []translation {
	translations := make([]translation, 0, len(template.Translations))
	for _, each := range template.Translations {
		if each == nil {
			continue
		}
		translations = append(translations, translation{
			Locale:      each.Locale,
			Subject:     each.Subject,
			HTMLContent: each.HTMLContent,
			TextContent: each.TextContent,
		})
	}
	return translations
}

func updateTemplateModelFromTemplate(model *templateModel, template *models.Template) bool {
	var dirty bool
	if model.DomainID != template.DomainID {
		model.DomainID = template.DomainID
		dirty = true
	}
	if !optionalReferencesAreEqual(model.LayoutID, template.LayoutID) {
		model.LayoutID = nil
		if layoutId := template.LayoutID; layoutId != "" {
			model.LayoutID = &layoutId
		}
		dirty = true
	}
	if model.Name != template.Name {
		model.Name = template.Name
		dirty = true
	}
	if model.Comment != template.Comment {
		model.Comment = template.Comment
		dirty = true
	}
	if model.Locale != template.Locale {
		model.Locale = template.Locale
		dirty = true
	}
	if model.Subject != template.Subject {
		model.Subject = template.Subject
		dirty = true
	}
	if model.HTMLContent != template.HTMLContent {
		model.HTMLContent = template.HTMLContent
		dirty = true
	}
	if model.TextContent != template.TextContent {
		model.TextContent = template.TextContent
		dirty = true
	}
	return dirty
}

func (self *transaction) queryTemplates(options *Options) *gorm.DB {
	return self.query(&templateModel{}, options)
}

// templatesFromModels converts rows to templates, with their translations
// fetched in one query rather than one per template.
func (self *transaction) templatesFromModels(existingModels []templateModel) ([]*models.Template, error) {
	templateIds := make([]string, 0, len(existingModels))
	for _, existingModel := range existingModels {
		templateIds = append(templateIds, existingModel.ID)
	}
	translations, err := self.loadTemplateTranslations(templateIds)
	if err != nil {
		return nil, err
	}
	templates := make([]*models.Template, 0, len(existingModels))
	for _, existingModel := range existingModels {
		templates = append(templates, getTemplateFromTemplateModel(existingModel, translations[existingModel.ID]))
	}
	return templates, nil
}

func (self *transaction) ListTemplates(domainId string, options *Options) ([]*models.Template, error) {
	var existingModels []templateModel
	if err := self.queryTemplates(options).Where("\"domain_id\" = ?", domainId).Order("\"name\"").Find(&existingModels).Error; err != nil {
		return nil, err
	}
	return self.templatesFromModels(existingModels)
}

func (self *transaction) GetTemplate(templateId string, options *Options) (*models.Template, error) {
	templates, err := self.GetTemplates([]string{templateId}, options)
	if err != nil {
		return nil, err
	}
	return templates[0], nil
}

func (self *transaction) GetTemplates(templateIds []string, options *Options) ([]*models.Template, error) {
	if len(templateIds) == 0 {
		return nil, nil
	}
	var existingModels []templateModel
	if err := self.queryTemplates(options).Where(templateIds).Find(&existingModels).Error; err != nil {
		return nil, err
	}
	found, err := self.templatesFromModels(existingModels)
	if err != nil {
		return nil, err
	}
	foundMap := make(map[string]*models.Template, len(found))
	for _, template := range found {
		foundMap[template.ID] = template
	}
	templates := make([]*models.Template, 0, len(templateIds))
	for _, templateId := range templateIds {
		templates = append(templates, foundMap[templateId])
	}
	return templates, nil
}

func (self *transaction) GetTemplateByName(domainId, name string, options *Options) (*models.Template, error) {
	var existingModels []templateModel
	if err := self.queryTemplates(options).Where("\"domain_id\" = ? AND \"name\" = ?", domainId, name).Find(&existingModels).Error; err != nil {
		return nil, err
	}
	if len(existingModels) == 0 {
		return nil, nil
	}
	templates, err := self.templatesFromModels(existingModels[:1])
	if err != nil {
		return nil, err
	}
	return templates[0], nil
}

func (self *transaction) CreateTemplate(template *models.Template, options *Options) (*models.Template, error) {
	if _, err := self.CreateTemplates([]*models.Template{template}, options); err != nil {
		return nil, err
	}
	return template, nil
}

func (self *transaction) CreateTemplates(templates []*models.Template, options *Options) ([]*models.Template, error) {
	now := time.Now().In(time.Local)
	for _, template := range templates {
		id := security.NewULID()
		newModel := templateModel{
			ID:         id,
			CreatedAt:  now,
			ModifiedAt: now,
		}
		updateTemplateModelFromTemplate(&newModel, template)
		if err := self.tx.Create(&newModel).Error; err != nil {
			return nil, err
		}
		if _, err := self.saveTemplateTranslations(id, getTranslationsFromTemplate(template), now); err != nil {
			return nil, err
		}
		template.ID = id
		template.CreatedAt = now
		template.ModifiedAt = now
	}
	return templates, nil
}

func (self *transaction) ModifyTemplate(templateId string, modifier func(*models.Template) error, options *Options) (*models.Template, error) {
	templates, err := self.ModifyTemplates([]string{templateId}, func(templates []*models.Template) error {
		return modifier(templates[0])
	}, options)
	if err != nil {
		return nil, err
	}
	return templates[0], nil
}

func (self *transaction) ModifyTemplates(templateIds []string, modifier func([]*models.Template) error, options *Options) ([]*models.Template, error) {
	if len(templateIds) == 0 {
		return nil, nil
	}
	var existingModels []templateModel
	if err := self.tx.Model(&templateModel{}).Where(templateIds).Find(&existingModels).Error; err != nil {
		return nil, err
	}
	existingModelsMap := make(map[string]templateModel)
	for _, existingModel := range existingModels {
		existingModelsMap[existingModel.ID] = existingModel
	}
	found, err := self.templatesFromModels(existingModels)
	if err != nil {
		return nil, err
	}
	foundMap := make(map[string]*models.Template, len(found))
	for _, template := range found {
		foundMap[template.ID] = template
	}
	templates := make([]*models.Template, 0, len(templateIds))
	for _, templateId := range templateIds {
		if template, ok := foundMap[templateId]; ok {
			templates = append(templates, template)
		} else {
			templates = append(templates, &models.Template{ID: templateId})
		}
	}
	if err := modifier(templates); err != nil {
		return nil, err
	}
	now := time.Now().In(time.Local)
	for _, template := range templates {
		existingModel, ok := existingModelsMap[template.ID]
		if ok {
			translationsDirty, err := self.saveTemplateTranslations(template.ID, getTranslationsFromTemplate(template), now)
			if err != nil {
				return nil, err
			}
			if updateTemplateModelFromTemplate(&existingModel, template) || translationsDirty {
				existingModel.ModifiedAt = now
				if err := self.tx.Save(&existingModel).Error; err != nil {
					return nil, err
				}
				template.ModifiedAt = now
			}
		} else {
			newModel := templateModel{
				ID:         template.ID,
				CreatedAt:  now,
				ModifiedAt: now,
			}
			updateTemplateModelFromTemplate(&newModel, template)
			if err := self.tx.Create(&newModel).Error; err != nil {
				return nil, err
			}
			if _, err := self.saveTemplateTranslations(template.ID, getTranslationsFromTemplate(template), now); err != nil {
				return nil, err
			}
			template.CreatedAt = now
			template.ModifiedAt = now
		}
	}
	return templates, nil
}

func (self *transaction) DeleteTemplate(templateId string, options *Options) error {
	// The translations go with it: the foreign key cascades.
	if err := self.tx.Where([]string{templateId}).Delete(&templateModel{}).Error; err != nil {
		return err
	}
	return nil
}
