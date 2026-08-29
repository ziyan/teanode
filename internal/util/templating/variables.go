package templating

import (
	"regexp"
	"sort"
	"strings"
)

// Variables reports the names a set of templates reads from their context,
// sorted, without duplicates. It is what tells a dashboard which inputs to
// draw for a template, and a script which keys its variables need.
//
// It reads the source rather than asking pongo2, which does not expose its
// syntax tree. That makes it a heuristic: every {{ … }} and {% … %} is
// scanned for identifiers, the ones the language itself defines are left
// out, and a name a tag introduces — a loop variable, a block name, the
// target of set — is treated as defined rather than read. Scoping is
// ignored, so a loop variable is excluded everywhere rather than only
// inside its loop; a template that reads a context variable of the same
// name as one of its own loop variables is doing something a reader would
// also find confusing.
func Variables(templates ...string) []string {
	read := map[string]bool{}
	defined := map[string]bool{}

	for _, template := range templates {
		for _, match := range blockPattern.FindAllStringSubmatch(template, -1) {
			if match[1] != "" {
				scanExpression(match[1], read, defined)
			} else {
				scanTag(match[2], read, defined)
			}
		}
	}

	names := make([]string, 0, len(read))
	for name := range read {
		if !defined[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// blockPattern finds the two kinds of block. The first group is the inside
// of a {{ }} and the second the inside of a {% %}; exactly one is set.
var blockPattern = regexp.MustCompile(`(?s)\{\{(.*?)\}\}|\{%(.*?)%\}`)

// stringPattern finds a quoted string, so its contents are not read as
// names.
var stringPattern = regexp.MustCompile(`"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'`)

// tokenPattern splits what remains into names — a dotted path counts as one
// — and single characters of punctuation. Numbers fall through as
// punctuation and are ignored.
var tokenPattern = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z0-9_]+)*|[^\sA-Za-z_]`)

// keywords are the words the language uses, in every case pongo2 accepts
// them, and the names it defines inside every template.
var keywords = map[string]bool{
	"and": true, "or": true, "not": true, "in": true, "is": true, "as": true,
	"true": true, "false": true, "none": true, "nil": true,
	"forloop": true, "loop": true, "super": true,
}

// tags are the block tags pongo2 ships. Each may appear as its name or as
// end followed by its name.
var tags = []string{
	"autoescape", "block", "comment", "cycle", "extends", "filter", "firstof",
	"for", "if", "elif", "else", "ifchanged", "ifequal", "ifnotequal",
	"import", "include", "lorem", "macro", "now", "set", "spaceless", "ssi",
	"templatetag", "widthratio", "with", "empty",
}

func isKeyword(name string) bool {
	lowered := strings.ToLower(name)
	if keywords[lowered] {
		return true
	}
	for _, tag := range tags {
		if lowered == tag || lowered == "end"+tag {
			return true
		}
	}
	return false
}

func tokens(source string) []string {
	return tokenPattern.FindAllString(stringPattern.ReplaceAllString(source, `""`), -1)
}

// root is the first segment of a dotted path: what user.name reads from the
// context is user.
func root(name string) string {
	return strings.SplitN(name, ".", 2)[0]
}

func isName(token string) bool {
	first := token[0]
	return first == '_' || (first >= 'A' && first <= 'Z') || (first >= 'a' && first <= 'z')
}

// scanExpression reads the names out of an expression. The name after a |
// is a filter, not a variable; anything after the filter's colon is its
// argument and may be one.
func scanExpression(source string, read, defined map[string]bool) {
	skipFilter := false
	for _, token := range tokens(source) {
		if token == "|" {
			skipFilter = true
			continue
		}
		if !isName(token) {
			continue
		}
		if skipFilter {
			skipFilter = false
			continue
		}
		if !isKeyword(root(token)) {
			read[root(token)] = true
		}
	}
}

// scanTag reads the names out of a tag, knowing which ones the tag defines
// rather than reads.
func scanTag(source string, read, defined map[string]bool) {
	all := tokens(source)
	if len(all) == 0 || !isName(all[0]) {
		return
	}
	tag := strings.ToLower(all[0])
	rest := all[1:]

	switch tag {
	case "for":
		// Loop variables up to "in" are defined; the rest is the iterable,
		// which may carry filters.
		position := 0
		for position < len(rest) && strings.ToLower(rest[position]) != "in" {
			if isName(rest[position]) {
				defined[root(rest[position])] = true
			}
			position++
		}
		if position < len(rest) {
			scanExpression(strings.Join(rest[position+1:], " "), read, defined)
		}
	case "block", "extends", "include", "import", "from", "filter", "autoescape",
		"templatetag", "lorem", "now", "ssi", "comment", "endblock":
		// Names of blocks, files, filters and modes rather than variables.
	case "set":
		if len(rest) > 0 && isName(rest[0]) {
			defined[root(rest[0])] = true
		}
		if len(rest) > 1 {
			scanExpression(strings.Join(rest[1:], " "), read, defined)
		}
	case "with":
		// Each "name=expression" defines a name; anything else is read.
		for index, token := range rest {
			if !isName(token) {
				continue
			}
			if index+1 < len(rest) && rest[index+1] == "=" {
				defined[root(token)] = true
			} else if !isKeyword(root(token)) {
				read[root(token)] = true
			}
		}
	case "macro":
		// The macro's name and its parameters are all its own.
		for _, token := range rest {
			if isName(token) {
				defined[root(token)] = true
			}
		}
	default:
		scanExpression(strings.Join(rest, " "), read, defined)
	}
}
