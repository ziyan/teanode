package sources

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// A condition decides whether an item is skipped, a listing runs, a
// folder is walked into, or a container is read. It compares operands --
// templates, quoted strings, bare words, [lists] -- with ==, !=, <, <=, >,
// >=, in and matches, and joins comparisons with &&, || and !, in that
// order of binding, with parentheses where that is not what is meant.

type condition interface {
	holds(scope Scope) (bool, error)
}

// compileCondition parses a condition once. An empty one never holds.
func compileCondition(text string) (condition, error) {
	tokens, err := conditionTokens(text)
	if err != nil {
		return nil, err
	}
	parser := &conditionParser{tokens: tokens}
	parsed, err := parser.or()
	if err != nil {
		return nil, fmt.Errorf("%q: %w", text, err)
	}
	if parser.position != len(tokens) {
		return nil, fmt.Errorf("%q: %q is left over", text, tokens[parser.position].text)
	}
	return parsed, nil
}

type tokenKind int

const (
	tokenOperand tokenKind = iota
	tokenOperator
	tokenOpen
	tokenClose
	tokenNot
)

type token struct {
	kind tokenKind
	text string
}

var conditionOperators = []string{"&&", "||", "==", "!=", "<=", ">=", "<", ">"}

// conditionTokens splits a condition into operands and operators. A
// template is one operand however many spaces are inside it, and so is a
// list and a quoted string.
func conditionTokens(text string) ([]token, error) {
	var tokens []token
	for position := 0; position < len(text); {
		rest := text[position:]
		switch {
		case rest[0] == ' ' || rest[0] == '\t':
			position++
			continue
		case strings.HasPrefix(rest, "{{"):
			end := strings.Index(rest, "}}")
			if end < 0 {
				return nil, fmt.Errorf("a {{ never closes")
			}
			tokens = append(tokens, token{kind: tokenOperand, text: rest[:end+2]})
			position += end + 2
			continue
		case rest[0] == '[':
			end := strings.IndexByte(rest, ']')
			if end < 0 {
				return nil, fmt.Errorf("a [ never closes")
			}
			tokens = append(tokens, token{kind: tokenOperand, text: rest[:end+1]})
			position += end + 1
			continue
		case len(tokens) > 0 && tokens[len(tokens)-1].kind == tokenOperator && tokens[len(tokens)-1].text == "matches" && rest[0] != '"' && rest[0] != '\'':
			// A pattern is one word to the next space, parentheses and
			// all: they are the pattern's, not the condition's.
			end := strings.IndexAny(rest, " \t")
			if end < 0 {
				end = len(rest)
			}
			tokens = append(tokens, token{kind: tokenOperand, text: rest[:end]})
			position += end
			continue
		case rest[0] == '"' || rest[0] == '\'':
			end := strings.IndexByte(rest[1:], rest[0])
			if end < 0 {
				return nil, fmt.Errorf("a quoted string never closes")
			}
			tokens = append(tokens, token{kind: tokenOperand, text: rest[:end+2]})
			position += end + 2
			continue
		case rest[0] == '(':
			tokens = append(tokens, token{kind: tokenOpen})
			position++
			continue
		case rest[0] == ')':
			tokens = append(tokens, token{kind: tokenClose})
			position++
			continue
		}
		matched := false
		for _, operator := range conditionOperators {
			if strings.HasPrefix(rest, operator) {
				tokens = append(tokens, token{kind: tokenOperator, text: operator})
				position += len(operator)
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		if rest[0] == '!' {
			tokens = append(tokens, token{kind: tokenNot})
			position++
			continue
		}
		end := strings.IndexAny(rest, " \t()")
		if end < 0 {
			end = len(rest)
		}
		word := rest[:end]
		if word == "in" || word == "matches" {
			tokens = append(tokens, token{kind: tokenOperator, text: word})
		} else {
			tokens = append(tokens, token{kind: tokenOperand, text: word})
		}
		position += end
	}
	return tokens, nil
}

type conditionParser struct {
	tokens   []token
	position int
}

func (self *conditionParser) peek() *token {
	if self.position >= len(self.tokens) {
		return nil
	}
	return &self.tokens[self.position]
}

func (self *conditionParser) or() (condition, error) {
	left, err := self.and()
	if err != nil {
		return nil, err
	}
	for next := self.peek(); next != nil && next.kind == tokenOperator && next.text == "||"; next = self.peek() {
		self.position++
		right, err := self.and()
		if err != nil {
			return nil, err
		}
		left = either{left, right}
	}
	return left, nil
}

func (self *conditionParser) and() (condition, error) {
	left, err := self.unary()
	if err != nil {
		return nil, err
	}
	for next := self.peek(); next != nil && next.kind == tokenOperator && next.text == "&&"; next = self.peek() {
		self.position++
		right, err := self.unary()
		if err != nil {
			return nil, err
		}
		left = both{left, right}
	}
	return left, nil
}

func (self *conditionParser) unary() (condition, error) {
	next := self.peek()
	if next == nil {
		return nil, fmt.Errorf("it ends where something was expected")
	}
	switch next.kind {
	case tokenNot:
		self.position++
		inner, err := self.unary()
		if err != nil {
			return nil, err
		}
		return not{inner}, nil
	case tokenOpen:
		self.position++
		inner, err := self.or()
		if err != nil {
			return nil, err
		}
		if closing := self.peek(); closing == nil || closing.kind != tokenClose {
			return nil, fmt.Errorf("a ( never closes")
		}
		self.position++
		return inner, nil
	case tokenOperand:
		return self.comparison()
	}
	return nil, fmt.Errorf("%q is not where it can be", next.text)
}

func (self *conditionParser) comparison() (condition, error) {
	left, err := conditionOperand(self.tokens[self.position].text)
	if err != nil {
		return nil, err
	}
	self.position++
	next := self.peek()
	if next == nil || next.kind != tokenOperator || next.text == "&&" || next.text == "||" {
		return truth{left}, nil
	}
	operator := next.text
	self.position++
	if right := self.peek(); right == nil || right.kind != tokenOperand {
		return nil, fmt.Errorf("%s has nothing on its right", operator)
	}
	rightText := self.tokens[self.position].text
	self.position++
	if operator == "matches" {
		pattern, err := regexp.Compile(strings.Trim(rightText, "\"'"))
		if err != nil {
			return nil, err
		}
		return matching{left, pattern}, nil
	}
	right, err := conditionOperand(rightText)
	if err != nil {
		return nil, err
	}
	return comparing{left, operator, right}, nil
}

// conditionValue is one operand of a condition.
type conditionValue struct {
	template compiled
	list     []string
	constant *string
}

func conditionOperand(text string) (conditionValue, error) {
	switch {
	case strings.HasPrefix(text, "{{"):
		template, err := compileTemplate(text)
		return conditionValue{template: template}, err
	case strings.HasPrefix(text, "["):
		var list []string
		for _, each := range strings.Split(strings.Trim(text, "[]"), ",") {
			if each = strings.Trim(strings.TrimSpace(each), "\"'"); each != "" {
				list = append(list, each)
			}
		}
		return conditionValue{list: list}, nil
	}
	constant := strings.Trim(text, "\"'")
	return conditionValue{constant: &constant}, nil
}

func (self conditionValue) value(scope Scope) (any, error) {
	switch {
	case self.template != nil:
		return self.template.value(scope)
	case self.list != nil:
		list := make([]any, len(self.list))
		for index, each := range self.list {
			list[index] = each
		}
		return list, nil
	}
	return *self.constant, nil
}

type truth struct{ operand conditionValue }

func (self truth) holds(scope Scope) (bool, error) {
	value, err := self.operand.value(scope)
	return truthy(value), err
}

type not struct{ inner condition }

func (self not) holds(scope Scope) (bool, error) {
	held, err := self.inner.holds(scope)
	return !held, err
}

type both struct{ left, right condition }

func (self both) holds(scope Scope) (bool, error) {
	held, err := self.left.holds(scope)
	if err != nil || !held {
		return false, err
	}
	return self.right.holds(scope)
}

type either struct{ left, right condition }

func (self either) holds(scope Scope) (bool, error) {
	held, err := self.left.holds(scope)
	if err != nil || held {
		return held, err
	}
	return self.right.holds(scope)
}

type matching struct {
	operand conditionValue
	pattern *regexp.Regexp
}

func (self matching) holds(scope Scope) (bool, error) {
	value, err := self.operand.value(scope)
	return self.pattern.MatchString(text(value)), err
}

type comparing struct {
	left     conditionValue
	operator string
	right    conditionValue
}

func (self comparing) holds(scope Scope) (bool, error) {
	left, err := self.left.value(scope)
	if err != nil {
		return false, err
	}
	right, err := self.right.value(scope)
	if err != nil {
		return false, err
	}
	if self.operator == "in" {
		for _, each := range asList(right) {
			if text(each) == text(left) {
				return true, nil
			}
		}
		return false, nil
	}
	order := compare(left, right)
	switch self.operator {
	case "==":
		return order == 0, nil
	case "!=":
		return order != 0, nil
	case "<":
		return order < 0, nil
	case "<=":
		return order <= 0, nil
	case ">":
		return order > 0, nil
	case ">=":
		return order >= 0, nil
	}
	return false, fmt.Errorf("%q is not a comparison", self.operator)
}

// compare orders two values as times where both are times, as numbers
// where both are numbers, and as text otherwise. A boolean compares with
// the text true or false.
func compare(left, right any) int {
	if leftTime, ok := asTime(left); ok {
		if rightTime, ok := asTime(right); ok {
			return leftTime.Compare(rightTime)
		}
	}
	leftNumber, leftError := strconv.ParseFloat(text(left), 64)
	rightNumber, rightError := strconv.ParseFloat(text(right), 64)
	if leftError == nil && rightError == nil {
		switch {
		case leftNumber < rightNumber:
			return -1
		case leftNumber > rightNumber:
			return 1
		}
		return 0
	}
	return strings.Compare(text(left), text(right))
}

func asList(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []string:
		list := make([]any, len(typed))
		for index, each := range typed {
			list[index] = each
		}
		return list
	case nil:
		return nil
	}
	return []any{value}
}
