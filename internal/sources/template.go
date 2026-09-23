package sources

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Scope is what a template can refer to: settings, container, item, each,
// parent, folder, response, detail, output and pass, each a value, and
// secrets, read as secret:key.
type Scope struct {
	Values  map[string]any
	Secrets map[string]string
}

// with is the scope with one more name in it.
func (self Scope) with(name string, value any) Scope {
	values := make(map[string]any, len(self.Values)+1)
	for key, each := range self.Values {
		values[key] = each
	}
	values[name] = value
	return Scope{Values: values, Secrets: self.Secrets}
}

// piece is one part of a template: literal text, or an expression.
type piece struct {
	literal    string
	expression *expression
}

// expression is an operand and the filters it goes through.
type expression struct {
	operand operand
	filters []filter
}

// operand is a path into the scope, a quoted string, or a secret.
type operand struct {
	path    []step
	literal *string
	secret  string
}

// step is one part of a path: a name, "*" for every value, or a name
// indexed by the value of another path, name[other.path].
type step struct {
	name  string
	index []step
}

type filter struct {
	name      string
	arguments []operand
}

// compiled is a parsed template.
type compiled []piece

// compileTemplate parses a template once.
func compileTemplate(text string) (compiled, error) {
	var pieces compiled
	for text != "" {
		start := strings.Index(text, "{{")
		if start < 0 {
			pieces = append(pieces, piece{literal: text})
			break
		}
		if start > 0 {
			pieces = append(pieces, piece{literal: text[:start]})
		}
		end := strings.Index(text[start:], "}}")
		if end < 0 {
			return nil, fmt.Errorf("%q opens {{ and never closes it", text)
		}
		parsed, err := parseExpression(strings.TrimSpace(text[start+2 : start+end]))
		if err != nil {
			return nil, err
		}
		pieces = append(pieces, piece{expression: parsed})
		text = text[start+end+2:]
	}
	return pieces, nil
}

// words splits an expression into words, keeping quoted strings whole.
func words(text string) ([]string, error) {
	var result []string
	for text = strings.TrimSpace(text); text != ""; text = strings.TrimSpace(text) {
		if text[0] == '"' || text[0] == '\'' {
			quote := text[0]
			end := strings.IndexByte(text[1:], quote)
			if end < 0 {
				return nil, fmt.Errorf("a quoted string in %q never closes", text)
			}
			result = append(result, text[:end+2])
			text = text[end+2:]
			continue
		}
		end := strings.IndexAny(text, " \t")
		if end < 0 {
			end = len(text)
		}
		result = append(result, text[:end])
		text = text[end:]
	}
	return result, nil
}

func parseExpression(text string) (*expression, error) {
	parts := strings.Split(text, "|")
	head, err := parseOperand(strings.TrimSpace(parts[0]))
	if err != nil {
		return nil, err
	}
	parsed := &expression{operand: head}
	for _, part := range parts[1:] {
		split, err := words(part)
		if err != nil {
			return nil, err
		}
		if len(split) == 0 {
			return nil, fmt.Errorf("%q has an empty filter", text)
		}
		if !knownFilters[split[0]] {
			return nil, fmt.Errorf("%q is not a filter", split[0])
		}
		one := filter{name: split[0]}
		for _, argument := range split[1:] {
			operand, err := parseOperand(argument)
			if err != nil {
				return nil, err
			}
			one.arguments = append(one.arguments, operand)
		}
		parsed.filters = append(parsed.filters, one)
	}
	return parsed, nil
}

var knownFilters = map[string]bool{
	"epoch-ms": true, "date": true, "time": true, "join": true, "replace": true, "urlencode": true,
	"html-text": true, "or": true, "empty": true, "present": true, "flag": true,
}

func parseOperand(text string) (operand, error) {
	if text == "" {
		return operand{}, fmt.Errorf("an expression is empty")
	}
	if text[0] == '"' || text[0] == '\'' {
		unquoted := text[1 : len(text)-1]
		if text[0] == '"' {
			if decoded, err := strconv.Unquote(text); err == nil {
				unquoted = decoded
			}
		}
		return operand{literal: &unquoted}, nil
	}
	if key, isSecret := strings.CutPrefix(text, "secret:"); isSecret {
		return operand{secret: key}, nil
	}
	path, err := parsePath(text)
	if err != nil {
		return operand{}, err
	}
	return operand{path: path}, nil
}

var pathName = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_-]*|\*)$`)

// parsePath reads a.b.*.c and a[b.c].d.
func parsePath(text string) ([]step, error) {
	var path []step
	for text != "" {
		end := 0
		depth := 0
		for end < len(text) {
			if text[end] == '[' {
				depth++
			} else if text[end] == ']' {
				depth--
			} else if text[end] == '.' && depth == 0 {
				break
			}
			end++
		}
		segment := text[:end]
		one := step{}
		if open := strings.IndexByte(segment, '['); open >= 0 {
			if !strings.HasSuffix(segment, "]") {
				return nil, fmt.Errorf("%q opens [ and never closes it", segment)
			}
			index, err := parsePath(segment[open+1 : len(segment)-1])
			if err != nil {
				return nil, err
			}
			one.index = index
			segment = segment[:open]
		}
		if !pathName.MatchString(segment) {
			return nil, fmt.Errorf("%q is not a name", segment)
		}
		one.name = segment
		path = append(path, one)
		if end < len(text) {
			end++
		}
		text = text[end:]
	}
	return path, nil
}

// roots is every name a template's paths begin with, and every secret it
// reads, for the check at install that each is one there will be.
func (self compiled) roots() (names []string, secrets []string) {
	var visit func(operand)
	visit = func(one operand) {
		if one.secret != "" {
			secrets = append(secrets, one.secret)
		}
		if len(one.path) > 0 {
			names = append(names, one.path[0].name)
			if len(one.path) > 1 && one.path[0].name == "settings" {
				names[len(names)-1] = "settings." + one.path[1].name
			}
			for _, each := range one.path {
				if each.index != nil {
					visit(operand{path: each.index})
				}
			}
		}
	}
	for _, each := range self {
		if each.expression == nil {
			continue
		}
		visit(each.expression.operand)
		for _, filter := range each.expression.filters {
			for _, argument := range filter.arguments {
				visit(argument)
			}
		}
	}
	return names, secrets
}

// value is the template's value: the value itself for a template that is
// one expression and nothing else, so a list stays a list; otherwise the
// text of every piece joined.
func (self compiled) value(scope Scope) (any, error) {
	if len(self) == 1 && self[0].expression != nil {
		return self[0].expression.evaluate(scope)
	}
	var built strings.Builder
	for _, each := range self {
		if each.expression == nil {
			built.WriteString(each.literal)
			continue
		}
		value, err := each.expression.evaluate(scope)
		if err != nil {
			return nil, err
		}
		built.WriteString(text(value))
	}
	return built.String(), nil
}

// render is the template's text.
func (self compiled) render(scope Scope) (string, error) {
	value, err := self.value(scope)
	if err != nil {
		return "", err
	}
	return text(value), nil
}

func (self *expression) evaluate(scope Scope) (any, error) {
	value, err := self.operand.evaluate(scope)
	if err != nil {
		return nil, err
	}
	for _, each := range self.filters {
		if value, err = each.apply(value, scope); err != nil {
			return nil, err
		}
	}
	return value, nil
}

func (self operand) evaluate(scope Scope) (any, error) {
	switch {
	case self.literal != nil:
		return *self.literal, nil
	case self.secret != "":
		return scope.Secrets[self.secret], nil
	}
	return lookup(scope.Values, self.path, scope), nil
}

// lookup follows a path through maps and lists. Anything missing is nil,
// never an error: a field one item has and another lacks is ordinary.
func lookup(root any, path []step, scope Scope) any {
	current := root
	for position, each := range path {
		if each.name == "*" {
			var values []any
			switch typed := current.(type) {
			case []any:
				values = typed
			case map[string]any:
				keys := make([]string, 0, len(typed))
				for key := range typed {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				for _, key := range keys {
					values = append(values, typed[key])
				}
			default:
				return nil
			}
			rest := path[position+1:]
			if len(rest) == 0 {
				return values
			}
			var picked []any
			for _, value := range values {
				if found := lookup(value, rest, scope); found != nil {
					picked = append(picked, found)
				}
			}
			return picked
		}
		asMap, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = asMap[each.name]
		if each.index != nil {
			key := text(lookup(scope.Values, each.index, scope))
			asMap, ok := current.(map[string]any)
			if !ok {
				return nil
			}
			current = asMap[key]
		}
	}
	return current
}

func (self filter) apply(value any, scope Scope) (any, error) {
	argument := func(index int) (any, error) {
		if index >= len(self.arguments) {
			return nil, fmt.Errorf("%s needs %d arguments", self.name, index+1)
		}
		return self.arguments[index].evaluate(scope)
	}
	switch self.name {
	case "epoch-ms":
		milliseconds, err := strconv.ParseFloat(text(value), 64)
		if err != nil || milliseconds == 0 {
			return "", nil
		}
		return time.UnixMilli(int64(milliseconds)).UTC().Format(time.RFC3339), nil
	case "date", "time":
		when, ok := asTime(value)
		if !ok {
			return "", nil
		}
		if self.name == "date" {
			return when.Format("2006-01-02"), nil
		}
		return when.UTC().Format(time.RFC3339), nil
	case "join":
		separator, err := argument(0)
		if err != nil {
			return nil, err
		}
		list, _ := value.([]any)
		parts := make([]string, 0, len(list))
		for _, each := range list {
			if part := text(each); part != "" {
				parts = append(parts, part)
			}
		}
		return strings.Join(parts, text(separator)), nil
	case "replace":
		old, err := argument(0)
		if err != nil {
			return nil, err
		}
		replacement, err := argument(1)
		if err != nil {
			return nil, err
		}
		return strings.ReplaceAll(text(value), text(old), text(replacement)), nil
	case "urlencode":
		return url.PathEscape(text(value)), nil
	case "html-text":
		return htmlText(text(value)), nil
	case "or":
		if !isEmpty(value) {
			return value, nil
		}
		return argument(0)
	case "empty":
		return isEmpty(value), nil
	case "present":
		return !isEmpty(value), nil
	case "flag":
		whenTrue, err := argument(0)
		if err != nil {
			return nil, err
		}
		whenFalse, err := argument(1)
		if err != nil {
			return nil, err
		}
		if truthy(value) {
			return whenTrue, nil
		}
		return whenFalse, nil
	}
	return nil, fmt.Errorf("%q is not a filter", self.name)
}

// text is a value as the text a command or a record carries.
func text(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool:
		return strconv.FormatBool(typed)
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case time.Time:
		return typed.UTC().Format(time.RFC3339)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, each := range typed {
			parts = append(parts, text(each))
		}
		return strings.Join(parts, ",")
	case []string:
		return strings.Join(typed, ",")
	case map[string]any:
		// An XML element with attributes and text: its text.
		if inner, ok := typed["#text"]; ok {
			return text(inner)
		}
		encoded, _ := json.Marshal(typed)
		return string(encoded)
	}
	return fmt.Sprint(value)
}

func isEmpty(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(typed) == ""
	case []any:
		return len(typed) == 0
	case []string:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	}
	return false
}

// truthy is a value as a condition reads it.
func truthy(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		lowered := strings.ToLower(strings.TrimSpace(typed))
		return lowered != "" && lowered != "false" && lowered != "0"
	case json.Number:
		return typed.String() != "0"
	case float64:
		return typed != 0
	case int:
		return typed != 0
	}
	return !isEmpty(value)
}

// timeLayouts are the ways the tools say when.
var timeLayouts = []string{
	time.RFC3339Nano, time.RFC3339, time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822,
	"Mon, 2 Jan 2006 15:04:05 -0700", "Mon, 2 Jan 2006 15:04:05 MST", "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02",
}

func asTime(value any) (time.Time, bool) {
	switch typed := value.(type) {
	case time.Time:
		return typed, !typed.IsZero()
	case nil:
		return time.Time{}, false
	}
	said := strings.TrimSpace(text(value))
	for _, layout := range timeLayouts {
		if parsed, err := time.Parse(layout, said); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

var (
	htmlTags   = regexp.MustCompile(`(?s)<(script|style)[^>]*>.*?</(script|style)>|<[^>]+>`)
	htmlBreaks = regexp.MustCompile(`(?i)<(br|/p|/div|/li|/h[1-6])[^>]*>`)
	blankRuns  = regexp.MustCompile(`[ \t]+`)
	lineRuns   = regexp.MustCompile(`\n{3,}`)
)

// htmlText is HTML as plain text: tags gone, entities read, paragraphs kept.
func htmlText(value string) string {
	value = htmlBreaks.ReplaceAllString(value, "\n")
	value = htmlTags.ReplaceAllString(value, "")
	value = html.UnescapeString(value)
	value = blankRuns.ReplaceAllString(value, " ")
	value = lineRuns.ReplaceAllString(value, "\n\n")
	return strings.TrimSpace(value)
}
