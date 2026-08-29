package models

import (
	"time"

	"github.com/ziyan/teanode/internal/config"
)

// Template is a mail template.
type Template struct {
	// ID of the Template
	ID string `json:"id,omitempty"`

	// Timestamp when the Template was created
	CreatedAt time.Time `json:"createdAt,omitempty"`

	// Timestamp when the Template was last modified
	ModifiedAt time.Time `json:"modifiedAt,omitempty"`

	// Domain that the Template belongs to
	DomainID string         `json:"domainId,omitempty"`
	Domain   *config.Domain `json:"-"`

	// Layout that the Template uses
	LayoutID string  `json:"layoutId,omitempty"`
	Layout   *Layout `json:"-"`

	// Name of the Template, must be unique within the Domain
	Name string `json:"name,omitempty"`

	// Comment about this Template
	Comment string `json:"comment,omitempty"`

	// Locale of the default Subject, HTMLContent and TextContent, such as "en" or "zh-CN"; optional
	Locale string `json:"locale,omitempty"`

	// Subject line
	Subject string `json:"subject,omitempty"`

	// HTML content
	HTMLContent string `json:"htmlContent,omitempty"`

	// Text content
	TextContent string `json:"textContent,omitempty"`

	// Translations of the subject and content into other locales
	Translations []*TemplateTranslation `json:"translations,omitempty"`

	// Names of the variables the Template and its Layout read when rendered, in every locale.
	// Derived when the Template is read rather than stored.
	Variables []string `json:"variables,omitempty"`
}

// TemplateTranslation is a Template's subject and content in one locale.
type TemplateTranslation struct {
	// Locale this translation is in, such as "zh-CN"
	Locale string `json:"locale"`

	// Subject line
	Subject string `json:"subject,omitempty"`

	// HTML content
	HTMLContent string `json:"htmlContent,omitempty"`

	// Text content
	TextContent string `json:"textContent,omitempty"`
}
