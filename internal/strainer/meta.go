package strainer

import (
	"strconv"
	"strings"
)

// Meta rules: a rule that fires on a combination of other rules' outcomes.
//
// The expressions look like "A && (B || !C)" and "(A + B + C) > 1", so this
// is a small recursive-descent parser over the usual precedence, evaluated
// with each named rule standing for 1 when it fired and 0 when it did not.
// Written by hand because it is a few hundred lines and the alternative is a
// dependency that parses a language nobody else speaks.

// metaNode is one node of the expression.
type metaNode interface {
	// evaluate returns a number; anything other than zero counts as fired.
	evaluate(hits map[string]bool) float64

	// names lists the rules the expression refers to.
	names() []string
}

type metaNumber float64

func (self metaNumber) evaluate(map[string]bool) float64 { return float64(self) }
func (self metaNumber) names() []string                  { return nil }

// metaName stands for another rule.
type metaName string

func (self metaName) evaluate(hits map[string]bool) float64 {
	if hits[string(self)] {
		return 1
	}
	return 0
}

func (self metaName) names() []string { return []string{string(self)} }

type metaUnary struct {
	operator byte
	operand  metaNode
}

func (self *metaUnary) evaluate(hits map[string]bool) float64 {
	value := self.operand.evaluate(hits)
	if self.operator == '!' {
		if value == 0 {
			return 1
		}
		return 0
	}
	return -value
}

func (self *metaUnary) names() []string { return self.operand.names() }

type metaBinary struct {
	operator    string
	left, right metaNode
}

func (self *metaBinary) names() []string { return append(self.left.names(), self.right.names()...) }

func (self *metaBinary) evaluate(hits map[string]bool) float64 {
	left := self.left.evaluate(hits)

	// Short circuit, so that "A && B" does not evaluate B when A did not
	// fire — which matters only for cost, since nothing here has effects.
	switch self.operator {
	case "&&":
		if left == 0 {
			return 0
		}
		if self.right.evaluate(hits) != 0 {
			return 1
		}
		return 0
	case "||":
		if left != 0 {
			return 1
		}
		if self.right.evaluate(hits) != 0 {
			return 1
		}
		return 0
	}

	right := self.right.evaluate(hits)
	truth := func(value bool) float64 {
		if value {
			return 1
		}
		return 0
	}
	switch self.operator {
	case "+":
		return left + right
	case "-":
		return left - right
	case "*":
		return left * right
	case "/":
		if right == 0 {
			return 0
		}
		return left / right
	case ">":
		return truth(left > right)
	case "<":
		return truth(left < right)
	case ">=":
		return truth(left >= right)
	case "<=":
		return truth(left <= right)
	case "==":
		return truth(left == right)
	case "!=":
		return truth(left != right)
	}
	return 0
}

// metaParser walks the expression text.
type metaParser struct {
	tokens []string
	index  int
}

// parseMeta reads one meta expression.
func parseMeta(text string) (metaNode, error) {
	parser := &metaParser{tokens: tokenizeMeta(text)}
	node, err := parser.parseOr()
	if err != nil {
		return nil, err
	}
	if parser.index != len(parser.tokens) {
		return nil, errMetaSyntax
	}
	return node, nil
}

func tokenizeMeta(text string) []string {
	tokens := make([]string, 0, 16)
	for index := 0; index < len(text); {
		character := text[index]
		switch {
		case character == ' ' || character == '\t':
			index++
		case strings.ContainsRune("()+-*/!", rune(character)):
			// Two-character operators that start with one of these.
			if character == '!' && index+1 < len(text) && text[index+1] == '=' {
				tokens = append(tokens, "!=")
				index += 2
				continue
			}
			tokens = append(tokens, string(character))
			index++
		case character == '&' || character == '|':
			if index+1 < len(text) && text[index+1] == character {
				tokens = append(tokens, text[index:index+2])
				index += 2
				continue
			}
			// A single & or | is not something these expressions use.
			return nil
		case character == '<' || character == '>' || character == '=':
			if index+1 < len(text) && text[index+1] == '=' {
				tokens = append(tokens, text[index:index+2])
				index += 2
				continue
			}
			if character == '=' {
				return nil
			}
			tokens = append(tokens, string(character))
			index++
		default:
			start := index
			for index < len(text) && (isNameCharacter(text[index]) || text[index] == '.') {
				index++
			}
			if start == index {
				// Something this parser does not know; the rule is skipped
				// rather than guessed at.
				return nil
			}
			tokens = append(tokens, text[start:index])
		}
	}
	return tokens
}

func isNameCharacter(character byte) bool {
	return character == '_' ||
		(character >= 'a' && character <= 'z') ||
		(character >= 'A' && character <= 'Z') ||
		(character >= '0' && character <= '9')
}

func (self *metaParser) peek() string {
	if self.index >= len(self.tokens) {
		return ""
	}
	return self.tokens[self.index]
}

func (self *metaParser) parseOr() (metaNode, error) {
	left, err := self.parseAnd()
	if err != nil {
		return nil, err
	}
	for self.peek() == "||" {
		self.index++
		right, err := self.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &metaBinary{operator: "||", left: left, right: right}
	}
	return left, nil
}

func (self *metaParser) parseAnd() (metaNode, error) {
	left, err := self.parseComparison()
	if err != nil {
		return nil, err
	}
	for self.peek() == "&&" {
		self.index++
		right, err := self.parseComparison()
		if err != nil {
			return nil, err
		}
		left = &metaBinary{operator: "&&", left: left, right: right}
	}
	return left, nil
}

func (self *metaParser) parseComparison() (metaNode, error) {
	left, err := self.parseSum()
	if err != nil {
		return nil, err
	}
	for {
		operator := self.peek()
		switch operator {
		case ">", "<", ">=", "<=", "==", "!=":
			self.index++
			right, err := self.parseSum()
			if err != nil {
				return nil, err
			}
			left = &metaBinary{operator: operator, left: left, right: right}
		default:
			return left, nil
		}
	}
}

func (self *metaParser) parseSum() (metaNode, error) {
	left, err := self.parseProduct()
	if err != nil {
		return nil, err
	}
	for {
		operator := self.peek()
		if operator != "+" && operator != "-" {
			return left, nil
		}
		self.index++
		right, err := self.parseProduct()
		if err != nil {
			return nil, err
		}
		left = &metaBinary{operator: operator, left: left, right: right}
	}
}

func (self *metaParser) parseProduct() (metaNode, error) {
	left, err := self.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		operator := self.peek()
		if operator != "*" && operator != "/" {
			return left, nil
		}
		self.index++
		right, err := self.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &metaBinary{operator: operator, left: left, right: right}
	}
}

func (self *metaParser) parseUnary() (metaNode, error) {
	switch self.peek() {
	case "!":
		self.index++
		operand, err := self.parseUnary()
		if err != nil {
			return nil, err
		}
		return &metaUnary{operator: '!', operand: operand}, nil
	case "-":
		self.index++
		operand, err := self.parseUnary()
		if err != nil {
			return nil, err
		}
		return &metaUnary{operator: '-', operand: operand}, nil
	}
	return self.parseAtom()
}

func (self *metaParser) parseAtom() (metaNode, error) {
	token := self.peek()
	if token == "" {
		return nil, errMetaSyntax
	}
	if token == "(" {
		self.index++
		inner, err := self.parseOr()
		if err != nil {
			return nil, err
		}
		if self.peek() != ")" {
			return nil, errMetaSyntax
		}
		self.index++
		return inner, nil
	}
	self.index++

	if number, err := strconv.ParseFloat(token, 64); err == nil {
		return metaNumber(number), nil
	}
	if !isNameCharacter(token[0]) {
		return nil, errMetaSyntax
	}
	return metaName(token), nil
}
