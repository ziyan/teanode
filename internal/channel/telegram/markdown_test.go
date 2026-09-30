package telegram

import "testing"

func TestTelegramMarkdown(t *testing.T) {
	for _, testCase := range []struct{ name, text, want string }{
		{"bold", "The **regatta** is on the 21st.", "The *regatta* is on the 21st."},
		{"heading", "## The mooring\nPaid in March.", "*The mooring*\nPaid in March."},
		{"bold heading", "### **Fees**", "*Fees*"},
		{"star bullets", "* one\n  * two", "• one\n  • two"},
		{"dash bullets stay", "- one\n- two", "- one\n- two"},
		{"strike", "It was ~~Tuesday~~ Thursday.", "It was Tuesday Thursday."},
		{"italic stays", "an _early_ start", "an _early_ start"},
		{"cited message", "See [Mooring invoice](mail:item42) for the fee.", "See Mooring invoice for the fee."},
		{"memory page", "Ask [Some Person](memory:people/some-person).", "Ask Some Person."},
		{"dashboard link stays", "[Some Person](https://mail.example.com/settings/knowledge/people/some-person)", "[Some Person](https://mail.example.com/settings/knowledge/people/some-person)"},
		{"web link stays", "[the club](https://example.org/club)", "[the club](https://example.org/club)"},
		{"code span untouched", "Run `**not bold**` then **bold**.", "Run `**not bold**` then *bold*."},
		{"fence untouched", "```\n# not a heading\n**x**\n```\n# Heading", "```\n# not a heading\n**x**\n```\n*Heading*"},
	} {
		if got := telegramMarkdown(testCase.text); got != testCase.want {
			t.Errorf("%s: got %q, want %q", testCase.name, got, testCase.want)
		}
	}
}
