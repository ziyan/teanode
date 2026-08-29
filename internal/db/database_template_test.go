package db_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A template's translations are rows of their own, and saving the template
// has to make the rows match the list every time: add what is new, change
// what changed, and drop what was removed.
func TestTemplateTranslationsRoundTrip(t *testing.T) {
	t.Parallel()
	dbtest.RunTransaction(t, func(tx db.Transaction) {
		layout, err := tx.CreateLayout(&models.Layout{
			DomainID:    "example.com",
			Locale:      "en",
			HTMLContent: "<html>{% block content %}{% endblock %}</html>",
			Translations: []*models.LayoutTranslation{
				{Locale: "zh", HTMLContent: "<html lang=zh>{% block content %}{% endblock %}</html>"},
			},
		}, nil)
		if err != nil {
			t.Fatalf("failed to create layout: %s", err)
		}

		template, err := tx.CreateTemplate(&models.Template{
			DomainID: "example.com",
			LayoutID: layout.ID,
			Name:     "welcome",
			Locale:   "en",
			Subject:  "Welcome, {{ name }}",
			Translations: []*models.TemplateTranslation{
				{Locale: "zh", Subject: "欢迎，{{ name }}"},
				{Locale: "ja", Subject: "ようこそ、{{ name }}"},
			},
		}, nil)
		if err != nil {
			t.Fatalf("failed to create template: %s", err)
		}

		loaded, err := tx.GetTemplateByName("example.com", "welcome", nil)
		if err != nil {
			t.Fatalf("failed to load template: %s", err)
		}
		if loaded == nil || loaded.ID != template.ID {
			t.Fatalf("expected the template back by name")
		}
		if loaded.Locale != "en" || len(loaded.Translations) != 2 {
			t.Fatalf("expected the default locale and two translations, got %q and %d", loaded.Locale, len(loaded.Translations))
		}
		// Sorted by locale, so a list is stable between reads.
		if loaded.Translations[0].Locale != "ja" || loaded.Translations[1].Locale != "zh" {
			t.Errorf("expected translations ordered by locale, got %q then %q", loaded.Translations[0].Locale, loaded.Translations[1].Locale)
		}
		if loaded.Translations[1].Subject != "欢迎，{{ name }}" {
			t.Errorf("the Chinese subject came back changed: %q", loaded.Translations[1].Subject)
		}

		// Change one, drop one, add one.
		modified, err := tx.ModifyTemplate(template.ID, func(template *models.Template) error {
			template.Translations = []*models.TemplateTranslation{
				{Locale: "zh", Subject: "你好，{{ name }}"},
				{Locale: "fr", Subject: "Bienvenue, {{ name }}"},
			}
			return nil
		}, nil)
		if err != nil {
			t.Fatalf("failed to modify template: %s", err)
		}
		if !modified.ModifiedAt.After(template.CreatedAt) && !modified.ModifiedAt.Equal(template.CreatedAt) {
			t.Errorf("expected the modification time to be set")
		}

		reloaded, err := tx.GetTemplate(template.ID, nil)
		if err != nil {
			t.Fatalf("failed to reload template: %s", err)
		}
		if len(reloaded.Translations) != 2 {
			t.Fatalf("expected two translations after the change, got %d", len(reloaded.Translations))
		}
		byLocale := map[string]string{}
		for _, translation := range reloaded.Translations {
			byLocale[translation.Locale] = translation.Subject
		}
		if byLocale["zh"] != "你好，{{ name }}" {
			t.Errorf("expected the Chinese subject to be updated, got %q", byLocale["zh"])
		}
		if byLocale["fr"] != "Bienvenue, {{ name }}" {
			t.Errorf("expected the French translation to be added, got %q", byLocale["fr"])
		}
		if _, still := byLocale["ja"]; still {
			t.Errorf("expected the Japanese translation to be removed")
		}

		// The layout's translation loads with it, and deleting the layout
		// leaves the template standing without one.
		loadedLayout, err := tx.GetLayout(layout.ID, nil)
		if err != nil {
			t.Fatalf("failed to load layout: %s", err)
		}
		if len(loadedLayout.Translations) != 1 || loadedLayout.Translations[0].Locale != "zh" {
			t.Fatalf("expected the layout's one translation back")
		}
		if err := tx.DeleteLayout(layout.ID, nil); err != nil {
			t.Fatalf("failed to delete layout: %s", err)
		}
		orphaned, err := tx.GetTemplate(template.ID, nil)
		if err != nil {
			t.Fatalf("failed to reload template after deleting its layout: %s", err)
		}
		if orphaned.LayoutID != "" {
			t.Errorf("expected the template to lose its layout reference, got %q", orphaned.LayoutID)
		}

		// Deleting the template takes its translations with it.
		if err := tx.DeleteTemplate(template.ID, nil); err != nil {
			t.Fatalf("failed to delete template: %s", err)
		}
		gone, err := tx.GetTemplate(template.ID, nil)
		if err != nil {
			t.Fatalf("failed to look for the deleted template: %s", err)
		}
		if gone != nil {
			t.Errorf("expected the template to be gone")
		}
	})
}
