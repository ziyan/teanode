package tools

// CharacterPage is one page of a text read in characters (runes), so a page
// never ends inside a multi-byte character.
type CharacterPage struct {
	// Text is the page itself.
	Text string
	// NextOffset is where the next page starts, in characters; it is zero
	// when this page reaches the end.
	NextOffset int
	// TotalCharacters is the length of the whole text, in characters.
	TotalCharacters int
	// HasMore is true when characters follow this page.
	HasMore bool
}

// PageCharacters is the page of text that starts at offset characters in and
// is at most limit characters long. A limit of zero or less is the rest of
// the text; an offset past the end is an empty last page.
func PageCharacters(text string, offset, limit int) CharacterPage {
	runes := []rune(text)
	page := CharacterPage{TotalCharacters: len(runes)}
	if offset < 0 {
		offset = 0
	}
	if offset > len(runes) {
		offset = len(runes)
	}
	end := len(runes)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	page.Text = string(runes[offset:end])
	if end < len(runes) {
		page.HasMore = true
		page.NextOffset = end
	}
	return page
}
