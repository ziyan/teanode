package tools

import "testing"

func TestPageCharactersNeverSplitsACharacter(test *testing.T) {
	text := "héllo wörld"
	first := PageCharacters(text, 0, 2)
	if first.Text != "hé" || !first.HasMore || first.NextOffset != 2 || first.TotalCharacters != 11 {
		test.Fatalf("first page = %+v", first)
	}
	rest := PageCharacters(text, first.NextOffset, 0)
	if rest.Text != "llo wörld" || rest.HasMore || rest.NextOffset != 0 {
		test.Fatalf("rest = %+v", rest)
	}
	past := PageCharacters(text, 99, 5)
	if past.Text != "" || past.HasMore {
		test.Fatalf("past the end = %+v", past)
	}
}
