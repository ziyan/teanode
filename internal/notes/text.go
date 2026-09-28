package notes

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// HTMLToText reads a note's HTML as the lines a person sees: a block a line,
// a line break a line, an empty block an empty line, and a list item a line
// starting "- ", which is what TextToHTML turns back into a list.
//
// Not the reduction mail is read with, which surrounds every block with
// blank lines and then collapses them: a note's empty lines are the ones its
// writer typed, and the text an agent edits has to write back as the same
// note.
func HTMLToText(input string) string {
	root, err := html.Parse(strings.NewReader(input))
	if err != nil {
		return strings.TrimSpace(input)
	}
	reader := &textReader{}
	reader.walk(root)
	reader.endLine(false)
	lines := reader.lines
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	return strings.Join(lines, "\n")
}

// textReader collects the lines of a document as it is walked.
type textReader struct {
	lines   []string
	current strings.Builder

	// hasText says the current line holds something more than a list
	// item's mark.
	hasText bool
}

// endLine finishes the line being written. A block ending with nothing in
// it adds no line; a line break always does, which is how an empty line is
// written.
func (self *textReader) endLine(isBreak bool) {
	if self.hasText || isBreak {
		self.lines = append(self.lines, strings.TrimRight(self.current.String(), " "))
	}
	self.current.Reset()
	self.hasText = false
}

func (self *textReader) walk(node *html.Node) {
	switch node.Type {
	case html.TextNode:
		text := strings.ReplaceAll(node.Data, " ", " ")
		text = strings.Join(strings.FieldsFunc(text, func(character rune) bool {
			return character == '\n' || character == '\r' || character == '\t'
		}), " ")
		if strings.TrimSpace(text) == "" && !self.hasText {
			return
		}
		self.current.WriteString(text)
		self.hasText = true
		return
	case html.ElementNode:
		switch node.DataAtom {
		case atom.Head, atom.Script, atom.Style, atom.Title:
			return
		case atom.Br:
			self.endLine(true)
			return
		}
	}
	isBlock := node.Type == html.ElementNode && isBlockElement(node.DataAtom)
	if isBlock {
		self.endLine(false)
		if node.DataAtom == atom.Li {
			self.current.WriteString("- ")
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		self.walk(child)
	}
	if isBlock {
		self.endLine(false)
	}
}

func isBlockElement(element atom.Atom) bool {
	switch element {
	case atom.Div, atom.P, atom.Li, atom.Ul, atom.Ol, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6,
		atom.Blockquote, atom.Pre, atom.Table, atom.Tr, atom.Section, atom.Article, atom.Header, atom.Footer:
		return true
	}
	return false
}
