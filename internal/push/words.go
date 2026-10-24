package push

import (
	"fmt"
	"strings"
)

// The words a notification is written in.
//
// A notification is sealed on the server and the phone only decrypts it, so
// what it says is written here, in the language of the person the phone
// belongs to. What people wrote (a message, a name, what the agent said) is
// sent as it is; the fixed words around it are a Phrase, in each language the
// app is shown in.

// The languages a notification's fixed words are written in, the app's and
// the dashboard's: English, Simplified Chinese and Japanese.
const (
	LanguageEnglish  = "en"
	LanguageChinese  = "zh"
	LanguageJapanese = "ja"
)

// PhoneLanguage is the language a notification is written in for a language
// tag: Chinese or Japanese for a tag of either ("zh-Hans-CN" is Chinese),
// and English for anything else, a tag of no language included.
func PhoneLanguage(tag string) string {
	language, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(tag)), "-")
	language, _, _ = strings.Cut(language, "_")
	switch language {
	case LanguageChinese:
		return LanguageChinese
	case LanguageJapanese:
		return LanguageJapanese
	}
	return LanguageEnglish
}

// Phrase is fixed wording a notification shows, in each language. Each is a
// format for fmt: a language that orders the values its own way indexes its
// verbs (%[2]s).
type Phrase struct {
	English  string
	Chinese  string
	Japanese string
}

// In is the phrase in a language, as PhoneLanguage names it, with the values
// put in.
func (self Phrase) In(language string, values ...any) string {
	format := self.English
	switch language {
	case LanguageChinese:
		format = self.Chinese
	case LanguageJapanese:
		format = self.Japanese
	}
	if len(values) == 0 {
		return format
	}
	return fmt.Sprintf(format, values...)
}

// buttonPhrases are the labels of the buttons every kind of notification
// may carry. A button that answers a question is labeled with the answer,
// which is not one of these and is left as it is.
var buttonPhrases = map[string]Phrase{
	"reply":   {English: "Reply", Chinese: "回复", Japanese: "返信"},
	"open":    {English: "Open", Chinese: "打开", Japanese: "開く"},
	"approve": {English: "Approve", Chinese: "批准", Japanese: "承認"},
	"deny":    {English: "Deny", Chinese: "拒绝", Japanese: "拒否"},
}

// ForLanguage is the content as it is sent to a phone whose person reads a
// language: a copy, with its Wording applied and its buttons labeled in that
// language. The content itself is left as it is, since it may go to several
// phones at once.
func (self *Content) ForLanguage(language string) *Content {
	worded := *self
	worded.Buttons = make([]Button, len(self.Buttons))
	for index, button := range self.Buttons {
		if phrase, ok := buttonPhrases[button.ButtonAction]; ok {
			button.Label = phrase.In(language)
		}
		worded.Buttons[index] = button
	}
	if self.Buttons == nil {
		worded.Buttons = nil
	}
	if self.Wording != nil {
		self.Wording(&worded, language)
	}
	worded.Wording = nil
	return &worded
}
