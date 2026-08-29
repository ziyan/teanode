// Package templating provides template rendering with layout inheritance.
package templating

import (
	"fmt"
	"io"

	"github.com/flosch/pongo2/v4"

	"github.com/ziyan/teanode/internal/util/bufferpool"
)

type templateMap map[string]io.Reader

// Abs calculates the path to a given template. Whenever a path must be resolved
// due to an import from another template, the base equals the parent template's path.
func (self templateMap) Abs(base, name string) string {
	return name
}

// Get returns an io.Reader where the template's content can be read from.
func (self templateMap) Get(name string) (io.Reader, error) {
	reader, ok := self[name]
	if !ok {
		return nil, fmt.Errorf("templating: invalid template path %q", name)
	}
	return reader, nil
}

func Render(writer io.Writer, variables map[string]interface{}, templates ...string) error {
	templateMap := make(templateMap)
	var templateName string
	for templateIndex, templateContent := range templates {
		buffer, releaseBuffer := bufferpool.AcquireBuffer()
		defer releaseBuffer()
		if templateName != "" {
			fmt.Fprintf(buffer, "{%% extends %q %%}", templateName)
		}
		buffer.WriteString(templateContent)
		templateName = fmt.Sprintf("__t%d__", templateIndex)
		templateMap[templateName] = buffer
	}
	templateSet := pongo2.NewSet("", templateMap)
	template, err := templateSet.FromFile(templateName)
	if err != nil {
		return err
	}
	return template.ExecuteWriterUnbuffered(pongo2.Context(variables), writer)
}
