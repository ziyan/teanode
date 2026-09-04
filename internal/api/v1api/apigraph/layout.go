package apigraph

import (
	"context"
	"fmt"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/mailer"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/templating"
)

type LayoutQuery interface {
	// List Layouts belonging to a Domain
	ListLayouts(ctx context.Context, arguments ListLayoutsArguments) ([]*models.Layout, error)

	// Get a particular Layout belonging to a Domain
	GetLayout(ctx context.Context, arguments GetLayoutArguments) (*models.Layout, error)

	// Render a Layout on its own, its blocks showing their default content, for a preview
	RenderLayout(ctx context.Context, arguments RenderLayoutArguments) (*mailer.Rendered, error)
}

type LayoutMutation interface {
	// Create new Layout
	CreateLayout(ctx context.Context, arguments CreateLayoutArguments) (*CreateLayoutReturnValue, error)

	// Modify existing Layout
	ModifyLayout(ctx context.Context, arguments ModifyLayoutArguments) (*ModifyLayoutReturnValue, error)

	// Delete a Layout
	DeleteLayout(ctx context.Context, arguments DeleteLayoutArguments) error
}

type ListLayoutsArguments struct {
	// ID of the Domain
	DomainID string `json:"domainId"`

	*api.Pagination `json:"pagination"`
}

func (self *graph) ListLayouts(ctx context.Context, arguments ListLayoutsArguments) ([]*models.Layout, error) {
	if _, err := self.requireDomain(ctx, arguments.DomainID); err != nil {
		return nil, err
	}

	layouts, err := api.ContextTransaction(ctx).ListLayouts(arguments.DomainID, nil)
	if err != nil {
		return nil, err
	}

	return layouts, nil
}

type GetLayoutArguments struct {
	// ID of the Layout to look up
	LayoutID string `json:"layoutId"`
}

func (self *graph) GetLayout(ctx context.Context, arguments GetLayoutArguments) (*models.Layout, error) {
	if err := self.requireOperator(ctx); err != nil {
		return nil, err
	}

	layout, err := api.ContextTransaction(ctx).GetLayout(arguments.LayoutID, nil)
	if err != nil {
		return nil, err
	}
	if layout == nil {
		return nil, api.ErrNotFound
	}

	// the domain has to still be configured
	if self.config.Current().FindDomainByID(layout.DomainID) == nil {
		return nil, api.ErrNotFound
	}

	return layout, nil
}

// LayoutTranslationParameters is a Layout's content in one locale.
type LayoutTranslationParameters struct {
	// Locale this translation is in, such as "zh-CN"
	Locale string `json:"locale"`

	// HTML content
	HTMLContent string `json:"htmlContent" graphapi:"nullable"`

	// Text content
	TextContent string `json:"textContent" graphapi:"nullable"`
}

// LayoutParameters represents layout properties that can be modified.
type LayoutParameters struct {
	// Comment about this Layout
	Comment string `json:"comment"`

	// Locale of the default HTMLContent and TextContent, such as "en"; optional
	Locale string `json:"locale" graphapi:"nullable"`

	// HTML content
	HTMLContent string `json:"htmlContent"`

	// Text content
	TextContent string `json:"textContent"`

	// Translations into other locales; the whole list, replacing what was stored
	Translations []*LayoutTranslationParameters `json:"translations" graphapi:"nullable"`
}

func (self *LayoutParameters) validate() ([]*models.LayoutTranslation, error) {
	locales := make([]string, 0, len(self.Translations))
	translations := make([]*models.LayoutTranslation, 0, len(self.Translations))
	for _, translation := range self.Translations {
		if translation == nil {
			continue
		}
		locales = append(locales, translation.Locale)
		translations = append(translations, &models.LayoutTranslation{
			Locale:      translation.Locale,
			HTMLContent: translation.HTMLContent,
			TextContent: translation.TextContent,
		})
	}
	if err := validateLocales(self.Locale, locales); err != nil {
		return nil, err
	}
	return translations, nil
}

type CreateLayoutArguments struct {
	// ID of the Domain that the Layout belongs to
	DomainID string `json:"domainId"`

	LayoutParameters LayoutParameters `json:"layoutParameters"`
}

type CreateLayoutReturnValue struct {
	Layout *models.Layout `json:"layout"`
}

func (self *graph) CreateLayout(ctx context.Context, arguments CreateLayoutArguments) (*CreateLayoutReturnValue, error) {
	domain, err := self.requireDomain(ctx, arguments.DomainID)
	if err != nil {
		return nil, err
	}
	translations, err := arguments.LayoutParameters.validate()
	if err != nil {
		return nil, err
	}

	layout, err := api.ContextTransaction(ctx).CreateLayout(&models.Layout{
		DomainID:     domain.ID,
		Comment:      arguments.LayoutParameters.Comment,
		Locale:       arguments.LayoutParameters.Locale,
		HTMLContent:  arguments.LayoutParameters.HTMLContent,
		TextContent:  arguments.LayoutParameters.TextContent,
		Translations: translations,
	}, nil)
	if err != nil {
		return nil, err
	}

	return &CreateLayoutReturnValue{
		Layout: layout,
	}, nil
}

type ModifyLayoutArguments struct {
	// ID of the Layout
	LayoutID string `json:"layoutId"`

	LayoutParameters LayoutParameters `json:"layoutParameters"`
}

type ModifyLayoutReturnValue struct {
	Layout *models.Layout `json:"layout"`
}

func (self *graph) ModifyLayout(ctx context.Context, arguments ModifyLayoutArguments) (*ModifyLayoutReturnValue, error) {
	if err := self.requireOperator(ctx); err != nil {
		return nil, err
	}

	layout, err := api.ContextTransaction(ctx).GetLayout(arguments.LayoutID, nil)
	if err != nil {
		return nil, err
	}
	if layout == nil {
		return nil, api.ErrNotFound
	}

	// the domain has to still be configured
	if self.config.Current().FindDomainByID(layout.DomainID) == nil {
		return nil, api.ErrNotFound
	}

	translations, err := arguments.LayoutParameters.validate()
	if err != nil {
		return nil, err
	}

	layout, err = api.ContextTransaction(ctx).ModifyLayout(layout.ID, func(layout *models.Layout) error {
		if layout.CreatedAt.IsZero() {
			return api.ErrNotFound
		}
		layout.Comment = arguments.LayoutParameters.Comment
		layout.Locale = arguments.LayoutParameters.Locale
		layout.HTMLContent = arguments.LayoutParameters.HTMLContent
		layout.TextContent = arguments.LayoutParameters.TextContent
		layout.Translations = translations
		return nil
	}, nil)
	if err != nil {
		return nil, err
	}

	return &ModifyLayoutReturnValue{
		Layout: layout,
	}, nil
}

type DeleteLayoutArguments struct {
	// ID of the Layout
	LayoutID string `json:"layoutId"`
}

func (self *graph) DeleteLayout(ctx context.Context, arguments DeleteLayoutArguments) error {
	if err := self.requireOperator(ctx); err != nil {
		return err
	}

	layout, err := api.ContextTransaction(ctx).GetLayout(arguments.LayoutID, nil)
	if err != nil {
		return err
	}
	if layout == nil {
		return api.ErrNotFound
	}

	// the domain has to still be configured
	if self.config.Current().FindDomainByID(layout.DomainID) == nil {
		return api.ErrNotFound
	}

	return api.ContextTransaction(ctx).DeleteLayout(layout.ID, nil)
}

type RenderLayoutArguments struct {
	// ID of the Domain
	DomainID string `json:"domainId"`

	// The layout to render, stored or not
	LayoutParameters LayoutParameters `json:"layoutParameters"`

	// Locale to render in; the closest translation is used, the default when none is close
	Locale string `json:"locale" graphapi:"nullable"`

	// Values for the Layout's variables
	Variables map[string]interface{} `json:"variables" graphapi:"nullable"`
}

// RenderLayout previews a layout by itself. Rendered with an empty template
// inside it, so each block shows whatever the layout put there as a default.
func (self *graph) RenderLayout(ctx context.Context, arguments RenderLayoutArguments) (*mailer.Rendered, error) {
	domain, err := self.requireDomain(ctx, arguments.DomainID)
	if err != nil {
		return nil, err
	}
	translations, err := arguments.LayoutParameters.validate()
	if err != nil {
		return nil, err
	}
	layout := &models.Layout{
		DomainID:     domain.ID,
		Locale:       arguments.LayoutParameters.Locale,
		HTMLContent:  arguments.LayoutParameters.HTMLContent,
		TextContent:  arguments.LayoutParameters.TextContent,
		Translations: translations,
	}

	rendered, err := mailer.Render(&models.Template{DomainID: domain.ID}, layout, arguments.Locale, arguments.Variables)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}

	// The locale reported is the layout's choice: the template here is
	// empty and has none of its own to report.
	locales := make([]string, 0, len(layout.Translations))
	for _, translation := range layout.Translations {
		locales = append(locales, translation.Locale)
	}
	if chosen, ok := templating.MatchLocale(arguments.Locale, locales); ok {
		rendered.Locale = chosen
	} else {
		rendered.Locale = layout.Locale
	}
	return rendered, nil
}
