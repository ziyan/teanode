package client

import (
	"context"
	"time"
)

// Template is a message to be sent with variables filled in.
type Template struct {
	ID           string                 `json:"id"`
	CreatedAt    time.Time              `json:"createdAt"`
	ModifiedAt   time.Time              `json:"modifiedAt"`
	DomainID     string                 `json:"domainId"`
	LayoutID     string                 `json:"layoutId"`
	Name         string                 `json:"name"`
	Comment      string                 `json:"comment"`
	Locale       string                 `json:"locale"`
	Subject      string                 `json:"subject"`
	HTMLContent  string                 `json:"htmlContent"`
	TextContent  string                 `json:"textContent"`
	Translations []*TemplateTranslation `json:"translations"`
	Variables    []string               `json:"variables"`
}

// TemplateTranslation is a template's subject and content in one locale.
type TemplateTranslation struct {
	Locale      string `json:"locale"`
	Subject     string `json:"subject"`
	HTMLContent string `json:"htmlContent"`
	TextContent string `json:"textContent"`
}

// TemplateParameters is everything that can be set on a template. The server
// replaces the whole template with them, so an update sends every field.
type TemplateParameters struct {
	LayoutID     string                 `json:"layoutId"`
	Name         string                 `json:"name"`
	Comment      string                 `json:"comment"`
	Locale       string                 `json:"locale,omitempty"`
	Subject      string                 `json:"subject"`
	HTMLContent  string                 `json:"htmlContent"`
	TextContent  string                 `json:"textContent"`
	Translations []*TemplateTranslation `json:"translations,omitempty"`
}

// Parameters returns a template's settings, so that a change can start from
// what is stored.
func (self *Template) Parameters() *TemplateParameters {
	return &TemplateParameters{
		LayoutID:     self.LayoutID,
		Name:         self.Name,
		Comment:      self.Comment,
		Locale:       self.Locale,
		Subject:      self.Subject,
		HTMLContent:  self.HTMLContent,
		TextContent:  self.TextContent,
		Translations: self.Translations,
	}
}

const templateFields = `{
	id createdAt modifiedAt domainId layoutId name comment locale subject htmlContent textContent
	translations { locale subject htmlContent textContent }
	variables
}`

// ListTemplates returns a domain's templates.
func ListTemplates(ctx context.Context, connection *Client, domainId string) ([]*Template, error) {
	var result struct {
		ListTemplates []*Template `json:"ListTemplates"`
	}
	query := `query ($domainId: String!) { ListTemplates(domainId: $domainId) ` + templateFields + ` }`
	if err := connection.Execute(ctx, query, map[string]any{"domainId": domainId}, &result); err != nil {
		return nil, err
	}
	return result.ListTemplates, nil
}

// GetTemplateByName returns a domain's template with this name, or nil.
func GetTemplateByName(ctx context.Context, connection *Client, domainId, name string) (*Template, error) {
	var result struct {
		GetTemplate *Template `json:"GetTemplate"`
	}
	query := `query ($domainId: String, $name: String) { GetTemplate(domainId: $domainId, name: $name) ` + templateFields + ` }`
	if err := connection.Execute(ctx, query, map[string]any{"domainId": domainId, "name": name}, &result); err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return result.GetTemplate, nil
}

// CreateTemplate adds a template to a domain.
func CreateTemplate(ctx context.Context, connection *Client, domainId string, parameters *TemplateParameters) (*Template, error) {
	var result struct {
		CreateTemplate struct {
			Template *Template `json:"template"`
		} `json:"CreateTemplate"`
	}
	query := `mutation ($domainId: String!, $templateParameters: TemplateParametersInput!) {
		CreateTemplate(domainId: $domainId, templateParameters: $templateParameters) { template ` + templateFields + ` }
	}`
	variables := map[string]any{"domainId": domainId, "templateParameters": parameters}
	if err := connection.Execute(ctx, query, variables, &result); err != nil {
		return nil, err
	}
	return result.CreateTemplate.Template, nil
}

// ModifyTemplate replaces a template's settings.
func ModifyTemplate(ctx context.Context, connection *Client, templateId string, parameters *TemplateParameters) (*Template, error) {
	var result struct {
		ModifyTemplate struct {
			Template *Template `json:"template"`
		} `json:"ModifyTemplate"`
	}
	query := `mutation ($templateId: String!, $templateParameters: TemplateParametersInput!) {
		ModifyTemplate(templateId: $templateId, templateParameters: $templateParameters) { template ` + templateFields + ` }
	}`
	variables := map[string]any{"templateId": templateId, "templateParameters": parameters}
	if err := connection.Execute(ctx, query, variables, &result); err != nil {
		return nil, err
	}
	return result.ModifyTemplate.Template, nil
}

// DeleteTemplate removes a template.
func DeleteTemplate(ctx context.Context, connection *Client, templateId string) error {
	query := `mutation ($templateId: String!) { DeleteTemplate(templateId: $templateId) }`
	return connection.Execute(ctx, query, map[string]any{"templateId": templateId}, nil)
}

// Rendered is a template or layout with its variables filled in.
type Rendered struct {
	Subject     string   `json:"subject"`
	HTMLContent string   `json:"htmlContent"`
	TextContent string   `json:"textContent"`
	Locale      string   `json:"locale"`
	Variables   []string `json:"variables"`
}

const renderedFields = `{ subject htmlContent textContent locale variables }`

// RenderTemplate renders a stored template as a message would be, without
// sending it.
func RenderTemplate(ctx context.Context, connection *Client, domainId, templateId, locale string, variables map[string]any) (*Rendered, error) {
	var result struct {
		RenderTemplate *Rendered `json:"RenderTemplate"`
	}
	query := `query ($domainId: String!, $templateId: String, $locale: String, $variables: Any) {
		RenderTemplate(domainId: $domainId, templateId: $templateId, locale: $locale, variables: $variables) ` + renderedFields + `
	}`
	arguments := map[string]any{"domainId": domainId, "templateId": templateId}
	if locale != "" {
		arguments["locale"] = locale
	}
	if variables != nil {
		arguments["variables"] = variables
	}
	if err := connection.Execute(ctx, query, arguments, &result); err != nil {
		return nil, err
	}
	return result.RenderTemplate, nil
}
