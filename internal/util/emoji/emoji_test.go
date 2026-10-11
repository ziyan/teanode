package emoji_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/ziyan/teanode/internal/util/emoji"
)

// What a person reacts with is one emoji or two, however many code points
// each takes, and anything that is not an emoji is refused rather than
// trimmed.
func TestSplitReadsEmojiAndRefusesText(t *testing.T) {
	for _, fixture := range []struct {
		name     string
		text     string
		expected []string
	}{
		{"empty", "", nil},
		{"one with presentation of its own", "\U0001F44D", []string{"\U0001F44D"}},
		{"a heart, asked to be an emoji", "❤\ufe0f", []string{"❤\ufe0f"}},
		{"a skin tone", "\U0001F44D\U0001F3FD", []string{"\U0001F44D\U0001F3FD"}},
		{"a flag", "\U0001F1EF\U0001F1F5", []string{"\U0001F1EF\U0001F1F5"}},
		{"two flags", "\U0001F1EF\U0001F1F5\U0001F1FA\U0001F1F8", []string{"\U0001F1EF\U0001F1F5", "\U0001F1FA\U0001F1F8"}},
		{"a keycap", "1\ufe0f\u20e3", []string{"1\ufe0f\u20e3"}},
		{"a family", "\U0001F468\u200d\U0001F469\u200d\U0001F467", []string{"\U0001F468\u200d\U0001F469\u200d\U0001F467"}},
		{"a heart on fire", "❤\ufe0f\u200d\U0001F525", []string{"❤\ufe0f\u200d\U0001F525"}},
		{"a subdivision flag", "\U0001F3F4\U000E0067\U000E0062\U000E0073\U000E0063\U000E0074\U000E007F", []string{"\U0001F3F4\U000E0067\U000E0062\U000E0073\U000E0063\U000E0074\U000E007F"}},
		{"two side by side", "\U0001F525\U0001F389", []string{"\U0001F525", "\U0001F389"}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			found, err := emoji.Split(fixture.text)
			if err != nil {
				t.Fatalf("Split(%q): %s", fixture.text, err)
			}
			if !reflect.DeepEqual(found, fixture.expected) {
				t.Fatalf("Split(%q) = %q, want %q", fixture.text, found, fixture.expected)
			}
		})
	}

	for _, fixture := range []struct{ name, text string }{
		{"a letter", "a"},
		{"a word after an emoji", "\U0001F44Dok"},
		{"a digit alone", "1"},
		{"a copyright sign written plainly", "©"},
		{"an emoji asked to be text", "❤\ufe0e"},
		{"one regional indicator", "\U0001F1EF"},
		{"a joiner with nothing after it", "\U0001F468\u200d"},
		{"a joiner alone", "\u200d"},
		{"a selector alone", "\ufe0f"},
		{"tags that are never closed", "\U0001F3F4\U000E0067\U000E0062"},
		{"a space", " "},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			if found, err := emoji.Split(fixture.text); !errors.Is(err, emoji.ErrNotEmoji) {
				t.Fatalf("Split(%q) = %q, %v; want ErrNotEmoji", fixture.text, found, err)
			}
		})
	}
}
