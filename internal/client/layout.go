package client

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Layout is what goes around a template: the frame the message is rendered
// into.
type Layout struct {
	ID           string               `json:"id"`
	CreatedAt    time.Time            `json:"createdAt"`
	ModifiedAt   time.Time            `json:"modifiedAt"`
	DomainID     string               `json:"domainId"`
	Comment      string               `json:"comment"`
	Locale       string               `json:"locale"`
	HTMLContent  string               `json:"htmlContent"`
	TextContent  string               `json:"textContent"`
	Translations []*LayoutTranslation `json:"translations"`
}

// LayoutTranslation is a layout's content in one locale.
type LayoutTranslation struct {
	Locale      string `json:"locale"`
	HTMLContent string `json:"htmlContent"`
	TextContent string `json:"textContent"`
}

// LayoutParameters is everything that can be set on a layout. The server
// replaces the whole layout with them.
type LayoutParameters struct {
	Comment      string               `json:"comment"`
	Locale       string               `json:"locale,omitempty"`
	HTMLContent  string               `json:"htmlContent"`
	TextContent  string               `json:"textContent"`
	Translations []*LayoutTranslation `json:"translations,omitempty"`
}

// Parameters returns a layout's settings, so that a change can start from
// what is stored.
func (self *Layout) Parameters() *LayoutParameters {
	return &LayoutParameters{
		Comment:      self.Comment,
		Locale:       self.Locale,
		HTMLContent:  self.HTMLContent,
		TextContent:  self.TextContent,
		Translations: self.Translations,
	}
}

const layoutFields = `{
	id createdAt modifiedAt domainId comment locale htmlContent textContent
	translations { locale htmlContent textContent }
}`

// ListLayouts returns a domain's layouts.
func ListLayouts(ctx context.Context, connection *Client, domainId string) ([]*Layout, error) {
	var result struct {
		ListLayouts []*Layout `json:"ListLayouts"`
	}
	query := `query ($domainId: String!) { ListLayouts(domainId: $domainId) ` + layoutFields + ` }`
	if err := connection.Execute(ctx, query, map[string]any{"domainId": domainId}, &result); err != nil {
		return nil, err
	}
	return result.ListLayouts, nil
}

// GetLayout returns one layout.
func GetLayout(ctx context.Context, connection *Client, layoutId string) (*Layout, error) {
	var result struct {
		GetLayout *Layout `json:"GetLayout"`
	}
	query := `query ($layoutId: String!) { GetLayout(layoutId: $layoutId) ` + layoutFields + ` }`
	if err := connection.Execute(ctx, query, map[string]any{"layoutId": layoutId}, &result); err != nil {
		return nil, err
	}
	return result.GetLayout, nil
}

// CreateLayout adds a layout to a domain.
func CreateLayout(ctx context.Context, connection *Client, domainId string, parameters *LayoutParameters) (*Layout, error) {
	var result struct {
		CreateLayout struct {
			Layout *Layout `json:"layout"`
		} `json:"CreateLayout"`
	}
	query := `mutation ($domainId: String!, $layoutParameters: LayoutParametersInput!) {
		CreateLayout(domainId: $domainId, layoutParameters: $layoutParameters) { layout ` + layoutFields + ` }
	}`
	variables := map[string]any{"domainId": domainId, "layoutParameters": parameters}
	if err := connection.Execute(ctx, query, variables, &result); err != nil {
		return nil, err
	}
	return result.CreateLayout.Layout, nil
}

// ModifyLayout replaces a layout's settings.
func ModifyLayout(ctx context.Context, connection *Client, layoutId string, parameters *LayoutParameters) (*Layout, error) {
	var result struct {
		ModifyLayout struct {
			Layout *Layout `json:"layout"`
		} `json:"ModifyLayout"`
	}
	query := `mutation ($layoutId: String!, $layoutParameters: LayoutParametersInput!) {
		ModifyLayout(layoutId: $layoutId, layoutParameters: $layoutParameters) { layout ` + layoutFields + ` }
	}`
	variables := map[string]any{"layoutId": layoutId, "layoutParameters": parameters}
	if err := connection.Execute(ctx, query, variables, &result); err != nil {
		return nil, err
	}
	return result.ModifyLayout.Layout, nil
}

// DeleteLayout removes a layout.
func DeleteLayout(ctx context.Context, connection *Client, layoutId string) error {
	query := `mutation ($layoutId: String!) { DeleteLayout(layoutId: $layoutId) }`
	return connection.Execute(ctx, query, map[string]any{"layoutId": layoutId}, nil)
}

// RenderLayout renders a layout by itself, its blocks showing their default
// content, for a preview.
func RenderLayout(ctx context.Context, connection *Client, domainId string, parameters *LayoutParameters, locale string, variables map[string]any) (*Rendered, error) {
	var result struct {
		RenderLayout *Rendered `json:"RenderLayout"`
	}
	query := `query ($domainId: String!, $layoutParameters: LayoutParametersInput!, $locale: String, $variables: Any) {
		RenderLayout(domainId: $domainId, layoutParameters: $layoutParameters, locale: $locale, variables: $variables) ` + renderedFields + `
	}`
	arguments := map[string]any{"domainId": domainId, "layoutParameters": parameters}
	if locale != "" {
		arguments["locale"] = locale
	}
	if variables != nil {
		arguments["variables"] = variables
	}
	if err := connection.Execute(ctx, query, arguments, &result); err != nil {
		return nil, err
	}
	return result.RenderLayout, nil
}

// isNotFound reports whether the server answered "not found", which a
// lookup by name turns into a nil result rather than an error.
func isNotFound(err error) bool {
	if errors.Is(err, ErrNotFound) {
		return true
	}
	var reported Errors
	if errors.As(err, &reported) {
		for _, candidate := range reported {
			if strings.Contains(candidate.Message, "not found") {
				return true
			}
		}
	}
	return false
}
