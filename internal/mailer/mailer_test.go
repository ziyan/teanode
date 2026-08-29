package mailer_test

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/mailer"
	"github.com/ziyan/teanode/internal/models"
)

// A template sent in a locale renders the translation closest to it, and
// falls back to its own content when it has nothing close. The layout is
// chosen on its own, so a translated template still renders inside a layout
// that was never translated.
func TestRenderChoosesTheTranslation(t *testing.T) {
	t.Parallel()

	layout := &models.Layout{
		Locale:      "en",
		HTMLContent: `<style>p{color:red}</style><p>{% block content %}{% endblock %}</p>`,
		TextContent: `== {% block content %}{% endblock %} ==`,
		Translations: []*models.LayoutTranslation{
			{Locale: "ja", HTMLContent: `<p lang="ja">{% block content %}{% endblock %}</p>`, TextContent: `【{% block content %}{% endblock %}】`},
		},
	}
	template := &models.Template{
		Locale:      "en",
		Subject:     "Welcome, {{ name }}",
		HTMLContent: `{% block content %}Hello {{ name }}{% endblock %}`,
		TextContent: `{% block content %}Hello {{ name }}{% endblock %}`,
		Translations: []*models.TemplateTranslation{
			{Locale: "zh", Subject: "欢迎，{{ name }}", HTMLContent: `{% block content %}你好 {{ name }}{% endblock %}`, TextContent: `{% block content %}你好 {{ name }}{% endblock %}`},
		},
	}
	variables := map[string]interface{}{"name": "Ada"}

	for name, testCase := range map[string]struct {
		locale  string
		subject string
		html    string
		text    string
		chosen  string
	}{
		"exact":    {"zh", "欢迎，Ada", `<p style="color: red;">你好 Ada</p>`, "== 你好 Ada ==", "zh"},
		"region":   {"zh-CN", "欢迎，Ada", `<p style="color: red;">你好 Ada</p>`, "== 你好 Ada ==", "zh"},
		"fallback": {"fr", "Welcome, Ada", `<p style="color: red;">Hello Ada</p>`, "== Hello Ada ==", "en"},
		// The layout has Japanese and the template does not: Japanese
		// furniture around English words is what was written.
		"layoutOnly": {"ja", "Welcome, Ada", `<p lang="ja">Hello Ada</p>`, "【Hello Ada】", "en"},
		"none":       {"", "Welcome, Ada", `<p style="color: red;">Hello Ada</p>`, "== Hello Ada ==", "en"},
	} {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rendered, err := mailer.Render(template, layout, testCase.locale, variables)
			if err != nil {
				t.Fatalf("render failed: %s", err)
			}
			if rendered.Subject != testCase.subject {
				t.Errorf("expected subject %q, got %q", testCase.subject, rendered.Subject)
			}
			if !strings.Contains(rendered.HTMLContent, testCase.html) {
				t.Errorf("expected html containing %q, got %q", testCase.html, rendered.HTMLContent)
			}
			if rendered.TextContent != testCase.text {
				t.Errorf("expected text %q, got %q", testCase.text, rendered.TextContent)
			}
			if rendered.Locale != testCase.chosen {
				t.Errorf("expected locale %q, got %q", testCase.chosen, rendered.Locale)
			}
		})
	}
}

// A template with no layout renders on its own, and so does one whose
// layout has nothing in it — a block with no parent to place it would
// otherwise render to nothing.
func TestRenderWithoutALayout(t *testing.T) {
	t.Parallel()

	template := &models.Template{
		Subject:     "{{ subject }}",
		HTMLContent: `{% block content %}<b>{{ body }}</b>{% endblock %}`,
		TextContent: `{{ body }}`,
	}
	variables := map[string]interface{}{"subject": "s", "body": "b"}

	for name, layout := range map[string]*models.Layout{
		"none":  nil,
		"empty": {HTMLContent: "  ", TextContent: ""},
	} {
		layout := layout
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rendered, err := mailer.Render(template, layout, "", variables)
			if err != nil {
				t.Fatalf("render failed: %s", err)
			}
			if rendered.Subject != "s" || rendered.TextContent != "b" || !strings.Contains(rendered.HTMLContent, "<b>b</b>") {
				t.Errorf("unexpected rendering: %+v", rendered)
			}
		})
	}
}

func TestRenderReportsATemplateError(t *testing.T) {
	t.Parallel()
	_, err := mailer.Render(&models.Template{Subject: "{{ unclosed"}, nil, "", nil)
	if err == nil {
		t.Fatalf("expected a syntax error to be reported")
	}
}
