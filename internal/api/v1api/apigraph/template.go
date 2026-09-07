package apigraph

import (
	"context"
	"fmt"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/mailer"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/templating"
)

type TemplateQuery interface {
	// List Templates belonging to a Domain
	ListTemplates(ctx context.Context, arguments ListTemplatesArguments) ([]*models.Template, error)

	// Get a particular Template belonging to a Domain
	GetTemplate(ctx context.Context, arguments GetTemplateArguments) (*models.Template, error)

	// Render a Template with variables filled in, as a message would be, without sending it
	RenderTemplate(ctx context.Context, arguments RenderTemplateArguments) (*mailer.Rendered, error)
}

type TemplateMutation interface {
	// Create new Template
	CreateTemplate(ctx context.Context, arguments CreateTemplateArguments) (*CreateTemplateReturnValue, error)

	// Modify existing Template
	ModifyTemplate(ctx context.Context, arguments ModifyTemplateArguments) (*ModifyTemplateReturnValue, error)

	// Delete a Template
	DeleteTemplate(ctx context.Context, arguments DeleteTemplateArguments) error
}

type ListTemplatesArguments struct {
	// ID of the Domain
	DomainID string `json:"domainId"`

	*api.Pagination `json:"pagination"`
}

func (self *graph) ListTemplates(ctx context.Context, arguments ListTemplatesArguments) ([]*models.Template, error) {
	if _, err := self.requireDomainPermission(ctx, models.PermissionDomainManage, arguments.DomainID); err != nil {
		return nil, err
	}

	templates, err := api.ContextTransaction(ctx).ListTemplates(arguments.DomainID, nil)
	if err != nil {
		return nil, err
	}

	// The layouts once for the whole list, rather than once per template.
	layouts, err := api.ContextTransaction(ctx).ListLayouts(arguments.DomainID, nil)
	if err != nil {
		return nil, err
	}
	layoutsById := make(map[string]*models.Layout, len(layouts))
	for _, layout := range layouts {
		layoutsById[layout.ID] = layout
	}
	for _, template := range templates {
		describeTemplate(template, layoutsById[template.LayoutID])
	}
	return templates, nil
}

type GetTemplateArguments struct {
	// ID of the Template to look up
	TemplateID *string `json:"templateId"`

	// ID of the Domain where the template belongs, only needed if using Name to look up template
	DomainID *string `json:"domainId"`

	// Name of the template to look up, when using Name to look up, also need DomainID
	Name *string `json:"name"`
}

func (self *graph) GetTemplate(ctx context.Context, arguments GetTemplateArguments) (*models.Template, error) {
	if _, err := self.requireAnyPermission(ctx, models.PermissionDomainManage); err != nil {
		return nil, err
	}

	var template *models.Template
	var err error
	if arguments.TemplateID != nil {
		template, err = api.ContextTransaction(ctx).GetTemplate(*arguments.TemplateID, nil)
	} else if arguments.DomainID != nil && arguments.Name != nil {
		template, err = api.ContextTransaction(ctx).GetTemplateByName(*arguments.DomainID, *arguments.Name, nil)
	}
	if err != nil {
		return nil, err
	}
	if template == nil {
		return nil, api.ErrNotFound
	}

	// the domain has to still be configured
	if !self.domainStillExists(ctx, template.DomainID) {
		return nil, api.ErrNotFound
	}

	layout, err := self.layoutOfTemplate(api.ContextTransaction(ctx), template)
	if err != nil {
		return nil, err
	}
	describeTemplate(template, layout)
	return template, nil
}

// layoutOfTemplate loads the layout a template sits in, or nil when it has
// none or the layout is gone.
func (self *graph) layoutOfTemplate(tx db.Transaction, template *models.Template) (*models.Layout, error) {
	if template.LayoutID == "" {
		return nil, nil
	}
	return tx.GetLayout(template.LayoutID, nil)
}

// describeTemplate fills in what is derived rather than stored: the names
// the template reads, in every locale it has, and those of its layout.
func describeTemplate(template *models.Template, layout *models.Layout) {
	sources := templateSources(template)
	if layout != nil {
		sources = append(sources, layoutSources(layout)...)
	}
	template.Variables = templating.Variables(sources...)
}

func templateSources(template *models.Template) []string {
	sources := []string{template.Subject, template.HTMLContent, template.TextContent}
	for _, translation := range template.Translations {
		sources = append(sources, translation.Subject, translation.HTMLContent, translation.TextContent)
	}
	return sources
}

func layoutSources(layout *models.Layout) []string {
	sources := []string{layout.HTMLContent, layout.TextContent}
	for _, translation := range layout.Translations {
		sources = append(sources, translation.HTMLContent, translation.TextContent)
	}
	return sources
}

// validateLocales checks a default locale and the locales of a list of
// translations: each has to be shaped like a language tag, none may repeat,
// and none may be the default's — that content belongs on the parent.
func validateLocales(defaultLocale string, locales []string) error {
	if defaultLocale != "" && !templating.ValidLocale(defaultLocale) {
		return fmt.Errorf("%w: %q is not a language tag such as en or zh-CN", api.ErrInvalidArguments, defaultLocale)
	}
	seen := map[string]bool{templating.NormalizeLocale(defaultLocale): defaultLocale != ""}
	for _, locale := range locales {
		if !templating.ValidLocale(locale) {
			return fmt.Errorf("%w: %q is not a language tag such as en or zh-CN", api.ErrInvalidArguments, locale)
		}
		normalized := templating.NormalizeLocale(locale)
		if seen[normalized] {
			return fmt.Errorf("%w: %q appears twice", api.ErrInvalidArguments, locale)
		}
		seen[normalized] = true
	}
	return nil
}

// TemplateTranslationParameters is a Template's subject and content in one locale.
type TemplateTranslationParameters struct {
	// Locale this translation is in, such as "zh-CN"
	Locale string `json:"locale"`

	// Subject line
	Subject string `json:"subject" graphapi:"nullable"`

	// HTML content
	HTMLContent string `json:"htmlContent" graphapi:"nullable"`

	// Text content
	TextContent string `json:"textContent" graphapi:"nullable"`
}

// TemplateParameters represents template properties that can be modified.
type TemplateParameters struct {
	// Layout that the Template uses
	LayoutID string `json:"layoutId"`

	// Name of the Template, must be unique within the Domain
	Name string `json:"name"`

	// Comment about this Template
	Comment string `json:"comment"`

	// Locale of the default Subject, HTMLContent and TextContent, such as "en"; optional
	Locale string `json:"locale" graphapi:"nullable"`

	// Subject line
	Subject string `json:"subject"`

	// HTML content
	HTMLContent string `json:"htmlContent"`

	// Text content
	TextContent string `json:"textContent"`

	// Translations into other locales; the whole list, replacing what was stored
	Translations []*TemplateTranslationParameters `json:"translations" graphapi:"nullable"`
}

// validate checks the parameters, and returns the translations as the model
// carries them.
func (self *TemplateParameters) validate() ([]*models.TemplateTranslation, error) {
	if self.Name == "" {
		return nil, fmt.Errorf("%w: a template needs a name", api.ErrInvalidArguments)
	}
	locales := make([]string, 0, len(self.Translations))
	translations := make([]*models.TemplateTranslation, 0, len(self.Translations))
	for _, translation := range self.Translations {
		if translation == nil {
			continue
		}
		locales = append(locales, translation.Locale)
		translations = append(translations, &models.TemplateTranslation{
			Locale:      translation.Locale,
			Subject:     translation.Subject,
			HTMLContent: translation.HTMLContent,
			TextContent: translation.TextContent,
		})
	}
	if err := validateLocales(self.Locale, locales); err != nil {
		return nil, err
	}
	return translations, nil
}

// requireLayoutOfDomain checks a layout exists and belongs to the domain.
// An empty identifier means no layout and is fine.
func (self *graph) requireLayoutOfDomain(tx db.Transaction, domainId, layoutId string) (*models.Layout, error) {
	if layoutId == "" {
		return nil, nil
	}
	layout, err := tx.GetLayout(layoutId, nil)
	if err != nil {
		return nil, err
	}
	if layout == nil || layout.DomainID != domainId {
		return nil, fmt.Errorf("%w: no such layout", api.ErrNotFound)
	}
	return layout, nil
}

type CreateTemplateArguments struct {
	// ID of the Domain that the Template belongs to
	DomainID string `json:"domainId"`

	TemplateParameters TemplateParameters `json:"templateParameters"`
}

type CreateTemplateReturnValue struct {
	Template *models.Template `json:"template"`
}

func (self *graph) CreateTemplate(ctx context.Context, arguments CreateTemplateArguments) (*CreateTemplateReturnValue, error) {
	domain, err := self.requireDomainPermission(ctx, models.PermissionDomainManage, arguments.DomainID)
	if err != nil {
		return nil, err
	}
	tx := api.ContextTransaction(ctx)

	translations, err := arguments.TemplateParameters.validate()
	if err != nil {
		return nil, err
	}
	layout, err := self.requireLayoutOfDomain(tx, domain.ID, arguments.TemplateParameters.LayoutID)
	if err != nil {
		return nil, err
	}

	// check that name is unique
	existingTemplate, err := tx.GetTemplateByName(domain.ID, arguments.TemplateParameters.Name, nil)
	if err != nil {
		return nil, err
	}
	if existingTemplate != nil {
		return nil, fmt.Errorf("%w: a template named %q", api.ErrAlreadyExists, arguments.TemplateParameters.Name)
	}

	template, err := tx.CreateTemplate(&models.Template{
		DomainID:     domain.ID,
		LayoutID:     arguments.TemplateParameters.LayoutID,
		Name:         arguments.TemplateParameters.Name,
		Comment:      arguments.TemplateParameters.Comment,
		Locale:       arguments.TemplateParameters.Locale,
		Subject:      arguments.TemplateParameters.Subject,
		HTMLContent:  arguments.TemplateParameters.HTMLContent,
		TextContent:  arguments.TemplateParameters.TextContent,
		Translations: translations,
	}, nil)
	if err != nil {
		return nil, err
	}
	describeTemplate(template, layout)

	return &CreateTemplateReturnValue{
		Template: template,
	}, nil
}

type ModifyTemplateArguments struct {
	// ID of the Template
	TemplateID string `json:"templateId"`

	TemplateParameters TemplateParameters `json:"templateParameters"`
}

type ModifyTemplateReturnValue struct {
	Template *models.Template `json:"template"`
}

func (self *graph) ModifyTemplate(ctx context.Context, arguments ModifyTemplateArguments) (*ModifyTemplateReturnValue, error) {
	if _, err := self.requireAnyPermission(ctx, models.PermissionDomainManage); err != nil {
		return nil, err
	}
	tx := api.ContextTransaction(ctx)

	template, err := tx.GetTemplate(arguments.TemplateID, nil)
	if err != nil {
		return nil, err
	}
	if template == nil {
		return nil, api.ErrNotFound
	}

	domain, err := self.requireDomainPermission(ctx, models.PermissionDomainManage, template.DomainID)
	if err != nil {
		return nil, err
	}

	translations, err := arguments.TemplateParameters.validate()
	if err != nil {
		return nil, err
	}
	layout, err := self.requireLayoutOfDomain(tx, domain.ID, arguments.TemplateParameters.LayoutID)
	if err != nil {
		return nil, err
	}

	// check that name is unique
	existingTemplate, err := tx.GetTemplateByName(domain.ID, arguments.TemplateParameters.Name, nil)
	if err != nil {
		return nil, err
	}
	if existingTemplate != nil && existingTemplate.ID != template.ID {
		return nil, fmt.Errorf("%w: a template named %q", api.ErrAlreadyExists, arguments.TemplateParameters.Name)
	}

	template, err = tx.ModifyTemplate(template.ID, func(template *models.Template) error {
		if template.CreatedAt.IsZero() {
			return api.ErrNotFound
		}
		template.LayoutID = arguments.TemplateParameters.LayoutID
		template.Name = arguments.TemplateParameters.Name
		template.Comment = arguments.TemplateParameters.Comment
		template.Locale = arguments.TemplateParameters.Locale
		template.Subject = arguments.TemplateParameters.Subject
		template.HTMLContent = arguments.TemplateParameters.HTMLContent
		template.TextContent = arguments.TemplateParameters.TextContent
		template.Translations = translations
		return nil
	}, nil)
	if err != nil {
		return nil, err
	}
	describeTemplate(template, layout)

	return &ModifyTemplateReturnValue{
		Template: template,
	}, nil
}

type DeleteTemplateArguments struct {
	// ID of the Template
	TemplateID string `json:"templateId"`
}

func (self *graph) DeleteTemplate(ctx context.Context, arguments DeleteTemplateArguments) error {
	if _, err := self.requireAnyPermission(ctx, models.PermissionDomainManage); err != nil {
		return err
	}

	template, err := api.ContextTransaction(ctx).GetTemplate(arguments.TemplateID, nil)
	if err != nil {
		return err
	}
	if template == nil {
		return api.ErrNotFound
	}

	// the domain has to still be configured
	if !self.domainStillExists(ctx, template.DomainID) {
		return api.ErrNotFound
	}

	return api.ContextTransaction(ctx).DeleteTemplate(template.ID, nil)
}

type RenderTemplateArguments struct {
	// ID of the Domain
	DomainID string `json:"domainId"`

	// ID of a stored Template to render; or leave unset and give templateParameters
	TemplateID string `json:"templateId" graphapi:"nullable"`

	// Content to render that has not been stored, for a preview while editing
	TemplateParameters *TemplateParameters `json:"templateParameters"`

	// Locale to render in; the closest translation is used, the default when none is close
	Locale string `json:"locale" graphapi:"nullable"`

	// Values for the Template's variables
	Variables map[string]interface{} `json:"variables" graphapi:"nullable"`
}

// RenderTemplate is the preview: what a message from this template would
// say, rendered by the same code that sends it.
func (self *graph) RenderTemplate(ctx context.Context, arguments RenderTemplateArguments) (*mailer.Rendered, error) {
	domain, err := self.requireDomainPermission(ctx, models.PermissionDomainManage, arguments.DomainID)
	if err != nil {
		return nil, err
	}
	tx := api.ContextTransaction(ctx)

	var template *models.Template
	switch {
	case arguments.TemplateID != "":
		template, err = tx.GetTemplate(arguments.TemplateID, nil)
		if err != nil {
			return nil, err
		}
		if template == nil || template.DomainID != domain.ID {
			return nil, api.ErrNotFound
		}
	case arguments.TemplateParameters != nil:
		parameters := arguments.TemplateParameters
		translations, err := parameters.validate()
		if err != nil {
			return nil, err
		}
		template = &models.Template{
			DomainID:     domain.ID,
			LayoutID:     parameters.LayoutID,
			Locale:       parameters.Locale,
			Subject:      parameters.Subject,
			HTMLContent:  parameters.HTMLContent,
			TextContent:  parameters.TextContent,
			Translations: translations,
		}
	default:
		return nil, fmt.Errorf("%w: give a templateId or templateParameters", api.ErrInvalidArguments)
	}

	layout, err := self.requireLayoutOfDomain(tx, domain.ID, template.LayoutID)
	if err != nil {
		return nil, err
	}

	rendered, err := mailer.Render(template, layout, arguments.Locale, arguments.Variables)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	return rendered, nil
}
