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

// hasPicture says whether HTML shows an image.
func hasPicture(input string) bool {
	if !strings.Contains(strings.ToLower(input), "<img") {
		return false
	}
	root, err := html.Parse(strings.NewReader(input))
	if err != nil {
		return true
	}
	var isFound func(*html.Node) bool
	isFound = func(node *html.Node) bool {
		if node.Type == html.ElementNode && node.DataAtom == atom.Img {
			return true
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if isFound(child) {
				return true
			}
		}
		return false
	}
	return isFound(root)
}

// EditorHTML is a note's HTML made fit to put into the dashboard's own page,
// which is where its editor is. A message is shown in a frame of its own,
// and what is safe there is not safe here: a style block restyles the whole
// dashboard, and a class or an id borrows its rules. So the note keeps its
// text and its structure, and loses its style blocks, its classes and ids,
// and anything that could run: scripts and event handlers, which the input
// should not have left, are dropped here again rather than trusted.
func EditorHTML(input string) string {
	body := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := html.ParseFragment(strings.NewReader(input), body)
	if err != nil {
		return ""
	}
	for _, node := range nodes {
		body.AppendChild(node)
	}
	cleanForEditor(body)
	var builder strings.Builder
	for child := body.FirstChild; child != nil; child = child.NextSibling {
		if err := html.Render(&builder, child); err != nil {
			return ""
		}
	}
	return builder.String()
}

// cleanForEditor removes from a tree what EditorHTML does not keep.
func cleanForEditor(node *html.Node) {
	for child := node.FirstChild; child != nil; {
		next := child.NextSibling
		if child.Type == html.CommentNode || (child.Type == html.ElementNode && isRemovedFromEditor(child.DataAtom, child.Data)) {
			node.RemoveChild(child)
		} else {
			cleanForEditor(child)
		}
		child = next
	}
	if node.Type != html.ElementNode {
		return
	}
	kept := node.Attr[:0]
	for _, attribute := range node.Attr {
		key := strings.ToLower(attribute.Key)
		if key == "class" || key == "id" || strings.HasPrefix(key, "on") {
			continue
		}
		if (key == "href" || key == "src") && isScriptUrl(attribute.Val) {
			continue
		}
		kept = append(kept, attribute)
	}
	node.Attr = kept
}

func isRemovedFromEditor(element atom.Atom, name string) bool {
	switch element {
	case atom.Style, atom.Script, atom.Link, atom.Meta, atom.Title, atom.Base, atom.Noscript, atom.Template,
		atom.Iframe, atom.Object, atom.Embed, atom.Form:
		return true
	}
	return strings.EqualFold(name, "svg") || strings.EqualFold(name, "math")
}

// isScriptUrl says whether a link would run something rather than go
// somewhere.
func isScriptUrl(value string) bool {
	trimmed := strings.Map(func(character rune) rune {
		if character <= 0x20 || character == 0x7f {
			return -1
		}
		return character
	}, strings.ToLower(value))
	return strings.HasPrefix(trimmed, "javascript:") || strings.HasPrefix(trimmed, "vbscript:")
}
