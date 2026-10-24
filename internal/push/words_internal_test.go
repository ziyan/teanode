package push

import "testing"

// A language tag is reduced to a language a notification is written in, and
// anything else is English.
func TestALanguageTagIsOneANotificationIsWrittenIn(t *testing.T) {
	t.Parallel()
	for tag, want := range map[string]string{
		"zh": LanguageChinese, "zh-Hans-CN": LanguageChinese, "ZH_tw": LanguageChinese,
		"ja": LanguageJapanese, "ja-JP": LanguageJapanese,
		"en": LanguageEnglish, "fr": LanguageEnglish, "": LanguageEnglish,
	} {
		if got := PhoneLanguage(tag); got != want {
			t.Errorf("PhoneLanguage(%q) = %q, want %q", tag, got, want)
		}
	}
}

// The copy made for a phone is worded in its language and its standard
// buttons labeled in it; the content itself, which may go to other phones,
// is left as it was, and the wording is never sent.
func TestAContentIsWordedForOnePhoneOnACopy(t *testing.T) {
	t.Parallel()
	greeting := Phrase{English: "Hello %s", Chinese: "你好 %s", Japanese: "こんにちは %s"}
	content := &Content{
		Title: "Hello Ada", Body: "unchanged",
		Buttons: []Button{{Label: "Reply", ButtonAction: "reply"}, {Label: "At four", ButtonAction: "answer:1"}},
		Wording: func(content *Content, language string) { content.Title = greeting.In(language, "Ada") },
	}
	worded := content.ForLanguage(LanguageJapanese)
	if worded.Title != "こんにちは Ada" || worded.Body != "unchanged" || worded.Buttons[0].Label != "返信" || worded.Buttons[1].Label != "At four" {
		t.Errorf("the copy is %+v", worded)
	}
	if worded.Wording != nil {
		t.Error("the copy keeps its wording")
	}
	if content.Title != "Hello Ada" || content.Buttons[0].Label != "Reply" || content.Wording == nil {
		t.Errorf("the content itself changed: %+v", content)
	}
	if english := content.ForLanguage(LanguageEnglish); english.Title != "Hello Ada" || english.Buttons[0].Label != "Reply" {
		t.Errorf("in English: %+v", english)
	}
}
