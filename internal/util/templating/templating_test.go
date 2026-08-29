package templating_test

import (
	"bytes"
	"testing"

	"github.com/ziyan/teanode/internal/util/templating"
)

type renderTestCase struct {
	variables map[string]interface{}
	templates []string
	expected  string
}

var renderTestCases = map[string]renderTestCase{
	"simple": {
		nil,
		[]string{
			`content`,
		},
		`content`,
	},
	"inherit": {
		nil,
		[]string{
			`header {% block content %}{% endblock %} footer`,
			`{% block content %}body{% endblock %}`,
		},
		`header body footer`,
	},
	"withVariables": {
		map[string]interface{}{
			"name": "John",
			"ip":   "127.0.0.1",
		},
		[]string{
			`Hi {{name}}! {% block content %}{% endblock %}`,
			`{% block content %}You are from {{ip}}.{% endblock %}`,
		},
		`Hi John! You are from 127.0.0.1.`,
	},
}

func TestRender(t *testing.T) {
	t.Parallel()

	for name, renderTestCase := range renderTestCases { //nolint:paralleltest
		name, renderTestCase := name, renderTestCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var buffer bytes.Buffer
			if err := templating.Render(&buffer, renderTestCase.variables, renderTestCase.templates...); err != nil {
				t.Fatalf("failed to render %q: %s", name, err)
			}
			if buffer.String() != renderTestCase.expected {
				t.Fatalf("test case %q expected:\n%s\nactual:\n%s", name, renderTestCase.expected, buffer.String())
			}
		})
	}
}

func BenchmarkRender(b *testing.B) {
	var buffer bytes.Buffer
	for i := 0; i < b.N; {
		for _, renderTestCase := range renderTestCases {
			if i++; i == b.N {
				break
			}
			buffer.Reset()
			_ = templating.Render(&buffer, renderTestCase.variables, renderTestCase.templates...)
		}
	}
}
