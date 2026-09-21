package models

import (
	"time"
)

// Layout is a base layout for Template.
type Layout struct {
	// ID of the Layout
	ID string `json:"id,omitempty"`

	// Timestamp when the Layout was created
	CreatedAt time.Time `json:"createdAt,omitempty"`

	// Timestamp when the Layout was last modified
	ModifiedAt time.Time `json:"modifiedAt,omitempty"`

	// Domain that the Layout belongs to
	DomainID string  `json:"domainId,omitempty"`
	Domain   *Domain `json:"-"`

	// Comment about this Layout
	Comment string `json:"comment,omitempty"`

	// Locale of the default HTMLContent and TextContent, such as "en" or "zh-CN"; optional
	Locale string `json:"locale,omitempty"`

	// HTML content
	HTMLContent string `json:"htmlContent,omitempty"`

	// Text content
	TextContent string `json:"textContent,omitempty"`

	// Translations of the content into other locales
	Translations []*LayoutTranslation `json:"translations,omitempty"`
}

// LayoutTranslation is a Layout's content in one locale.
type LayoutTranslation struct {
	// Locale this translation is in, such as "zh-CN"
	Locale string `json:"locale"`

	// HTML content
	HTMLContent string `json:"htmlContent,omitempty"`

	// Text content
	TextContent string `json:"textContent,omitempty"`
}
