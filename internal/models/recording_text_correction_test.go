package models

import "testing"

func TestReplaceWholePhrase(test *testing.T) {
	for _, example := range []struct {
		name, text, phrase, replacement, want string
		wantCount                             int
	}{
		{"every occurrence", "Tea node, Tea node.", "Tea node", "TeaNode", "TeaNode, TeaNode.", 2},
		{"not inside a longer word", "Tea nodes and Tea node", "Tea node", "TeaNode", "Tea nodes and TeaNode", 1},
		{"not after a word", "ExTea node", "Tea node", "TeaNode", "ExTea node", 0},
		{"case must match", "tea node", "Tea node", "TeaNode", "tea node", 0},
		{"white space runs match one space", "Tea \n  node ok", "Tea node", "TeaNode", "TeaNode ok", 1},
		{"punctuation edge needs no boundary", "a.b c", "a.", "x.", "x.b c", 1},
		{"digits are word characters", "node2 node", "node", "Node", "node2 Node", 1},
		{"unicode letters are word characters", "énode node", "node", "Node", "énode Node", 1},
		{"empty phrase", "text", " ", "x", "text", 0},
	} {
		got, count := ReplaceWholePhrase(example.text, example.phrase, example.replacement)
		if got != example.want || count != example.wantCount {
			test.Errorf("%s: got %q (%d), want %q (%d)", example.name, got, count, example.want, example.wantCount)
		}
	}
}
