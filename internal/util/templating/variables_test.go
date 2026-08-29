package templating_test

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/util/templating"
)

func TestVariables(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		templates []string
		expected  []string
	}{
		"plain": {
			[]string{`Hello {{ name }}, welcome to {{site}}.`},
			[]string{"name", "site"},
		},
		"dottedPathReadsItsRoot": {
			[]string{`{{ user.name }} at {{ user.address.city }}`},
			[]string{"user"},
		},
		"filterIsNotAVariableButItsArgumentMayBe": {
			[]string{`{{ price|floatformat:2 }} {{ title|default:fallback|upper }}`},
			[]string{"fallback", "price", "title"},
		},
		"loopVariableIsDefinedAndTheIterableIsRead": {
			[]string{`{% for item in items %}{{ item.name }} {{ forloop.Counter }}{% endfor %}`},
			[]string{"items"},
		},
		"conditionReadsItsOperands": {
			[]string{`{% if count > limit and not disabled %}x{% elif other %}y{% else %}z{% endif %}`},
			[]string{"count", "disabled", "limit", "other"},
		},
		"layoutBlockNamesAreNotVariables": {
			[]string{
				`<h1>{{ heading }}</h1>{% block content %}{% endblock %}<p>{{ footer }}</p>`,
				`{% block content %}Dear {{ name }}{% endblock %}`,
			},
			[]string{"footer", "heading", "name"},
		},
		"stringsAreNotVariables": {
			[]string{`{{ greeting|default:"hello there" }} {% if kind == 'welcome' %}{{ kind }}{% endif %}`},
			[]string{"greeting", "kind"},
		},
		"setAndWithDefine": {
			[]string{`{% set total = price * quantity %}{% with label=title %}{{ label }} {{ total }}{% endwith %}`},
			[]string{"price", "quantity", "title"},
		},
		"nothing": {
			[]string{`no variables here`, ``},
			[]string{},
		},
	} {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			actual := templating.Variables(testCase.templates...)
			if strings.Join(actual, ",") != strings.Join(testCase.expected, ",") {
				t.Fatalf("expected %v, got %v", testCase.expected, actual)
			}
		})
	}
}
