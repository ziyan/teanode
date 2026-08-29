package db

import (
	"time"

	"github.com/ziyan/teanode/internal/util/security"
)

// A translation is a child row of a template or a layout: the same content
// in another locale. The two tables have the same shape apart from the
// subject, so one set of loading and saving code serves both, keyed by the
// column that names the parent.

type templateTranslationModel struct {
	ID string `gorm:"primary_key:true;size:32"`

	CreatedAt  time.Time
	ModifiedAt time.Time

	TemplateID string `gorm:"size:32"`

	Locale string `gorm:"size:32"`

	Subject string `gorm:"size:256"`

	HTMLContent string `gorm:"type:text"`

	TextContent string `gorm:"type:text"`
}

func (self *templateTranslationModel) TableName() string {
	return "template_translation"
}

type layoutTranslationModel struct {
	ID string `gorm:"primary_key:true;size:32"`

	CreatedAt  time.Time
	ModifiedAt time.Time

	LayoutID string `gorm:"size:32"`

	Locale string `gorm:"size:32"`

	HTMLContent string `gorm:"type:text"`

	TextContent string `gorm:"type:text"`
}

func (self *layoutTranslationModel) TableName() string {
	return "layout_translation"
}

// translation is what the two models have in common, so saving can diff a
// parent's list against its rows without knowing which table it is.
type translation struct {
	Locale      string
	Subject     string
	HTMLContent string
	TextContent string
}

func (self *transaction) loadTemplateTranslations(templateIds []string) (map[string][]translation, error) {
	if len(templateIds) == 0 {
		return map[string][]translation{}, nil
	}
	var existingModels []templateTranslationModel
	if err := self.tx.Where("\"template_id\" IN ?", templateIds).Order("\"locale\"").Find(&existingModels).Error; err != nil {
		return nil, err
	}
	byParent := make(map[string][]translation)
	for _, existingModel := range existingModels {
		byParent[existingModel.TemplateID] = append(byParent[existingModel.TemplateID], translation{
			Locale:      existingModel.Locale,
			Subject:     existingModel.Subject,
			HTMLContent: existingModel.HTMLContent,
			TextContent: existingModel.TextContent,
		})
	}
	return byParent, nil
}

func (self *transaction) loadLayoutTranslations(layoutIds []string) (map[string][]translation, error) {
	if len(layoutIds) == 0 {
		return map[string][]translation{}, nil
	}
	var existingModels []layoutTranslationModel
	if err := self.tx.Where("\"layout_id\" IN ?", layoutIds).Order("\"locale\"").Find(&existingModels).Error; err != nil {
		return nil, err
	}
	byParent := make(map[string][]translation)
	for _, existingModel := range existingModels {
		byParent[existingModel.LayoutID] = append(byParent[existingModel.LayoutID], translation{
			Locale:      existingModel.Locale,
			HTMLContent: existingModel.HTMLContent,
			TextContent: existingModel.TextContent,
		})
	}
	return byParent, nil
}

// saveTemplateTranslations makes the rows match the list: a locale in both
// is updated when it changed, one only in the list is inserted, one only in
// the rows is deleted. Reports whether anything was written, so the parent's
// modification time moves when only a translation did.
func (self *transaction) saveTemplateTranslations(templateId string, wanted []translation, now time.Time) (bool, error) {
	var existingModels []templateTranslationModel
	if err := self.tx.Where("\"template_id\" = ?", templateId).Find(&existingModels).Error; err != nil {
		return false, err
	}
	existingByLocale := make(map[string]templateTranslationModel, len(existingModels))
	for _, existingModel := range existingModels {
		existingByLocale[existingModel.Locale] = existingModel
	}

	var dirty bool
	kept := make(map[string]bool, len(wanted))
	for _, want := range wanted {
		kept[want.Locale] = true
		existingModel, ok := existingByLocale[want.Locale]
		if !ok {
			if err := self.tx.Create(&templateTranslationModel{
				ID:          security.NewULID(),
				CreatedAt:   now,
				ModifiedAt:  now,
				TemplateID:  templateId,
				Locale:      want.Locale,
				Subject:     want.Subject,
				HTMLContent: want.HTMLContent,
				TextContent: want.TextContent,
			}).Error; err != nil {
				return false, err
			}
			dirty = true
			continue
		}
		if existingModel.Subject == want.Subject && existingModel.HTMLContent == want.HTMLContent && existingModel.TextContent == want.TextContent {
			continue
		}
		existingModel.Subject = want.Subject
		existingModel.HTMLContent = want.HTMLContent
		existingModel.TextContent = want.TextContent
		existingModel.ModifiedAt = now
		if err := self.tx.Save(&existingModel).Error; err != nil {
			return false, err
		}
		dirty = true
	}
	for locale, existingModel := range existingByLocale {
		if kept[locale] {
			continue
		}
		if err := self.tx.Delete(&existingModel).Error; err != nil {
			return false, err
		}
		dirty = true
	}
	return dirty, nil
}

func (self *transaction) saveLayoutTranslations(layoutId string, wanted []translation, now time.Time) (bool, error) {
	var existingModels []layoutTranslationModel
	if err := self.tx.Where("\"layout_id\" = ?", layoutId).Find(&existingModels).Error; err != nil {
		return false, err
	}
	existingByLocale := make(map[string]layoutTranslationModel, len(existingModels))
	for _, existingModel := range existingModels {
		existingByLocale[existingModel.Locale] = existingModel
	}

	var dirty bool
	kept := make(map[string]bool, len(wanted))
	for _, want := range wanted {
		kept[want.Locale] = true
		existingModel, ok := existingByLocale[want.Locale]
		if !ok {
			if err := self.tx.Create(&layoutTranslationModel{
				ID:          security.NewULID(),
				CreatedAt:   now,
				ModifiedAt:  now,
				LayoutID:    layoutId,
				Locale:      want.Locale,
				HTMLContent: want.HTMLContent,
				TextContent: want.TextContent,
			}).Error; err != nil {
				return false, err
			}
			dirty = true
			continue
		}
		if existingModel.HTMLContent == want.HTMLContent && existingModel.TextContent == want.TextContent {
			continue
		}
		existingModel.HTMLContent = want.HTMLContent
		existingModel.TextContent = want.TextContent
		existingModel.ModifiedAt = now
		if err := self.tx.Save(&existingModel).Error; err != nil {
			return false, err
		}
		dirty = true
	}
	for locale, existingModel := range existingByLocale {
		if kept[locale] {
			continue
		}
		if err := self.tx.Delete(&existingModel).Error; err != nil {
			return false, err
		}
		dirty = true
	}
	return dirty, nil
}
