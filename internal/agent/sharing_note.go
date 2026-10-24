package agent

import (
	"strings"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/push"
)

// When somebody shares something with a person, or widens what they shared,
// the person is told the way an invitation to a circle is told: a note in the
// main conversation of their agent when it is on, written in the transaction
// that made the share, and a notification on their phones of kind circle after
// it commits. The same two tell an owner that somebody joined a circle their
// shares reach. Without them a share only shows the day the person opens the
// screen it appears on.

// NoteSharing writes the note into the person's main conversation when their
// agent is on, and says whose agent row holds their phones, or "" when they
// have none. noteKind is NoteShareReceived or NoteShareReached.
func NoteSharing(tx db.Transaction, toldUserId string, noteKind models.AgentNoteKind, detail string) (string, error) {
	person, err := tx.GetAgentByUser(toldUserId)
	if err != nil || person == nil {
		return "", err
	}
	if !person.Active() {
		return person.ID, nil
	}
	main, err := tx.MainAgentConversation(person.ID, "drawer")
	if err != nil {
		return "", err
	}
	if _, err := tx.AppendAgentMessage(models.NewAgentNote(main.ID, noteKind, detail)); err != nil {
		return "", err
	}
	return person.ID, nil
}

// NotifySharing tells the phones of an agent's person what the note says, in
// the background, in their language.
func (self *PhoneNotifier) NotifySharing(agentId string, noteKind models.AgentNoteKind, detail string) {
	self.NotifyPhones(agentId, models.NotificationCircle, &push.Content{
		NotificationKind: models.NotificationCircle,
		Title:            phraseSharing.English,
		Body:             PhoneBody(models.NoteText(noteKind, detail)),
		ThreadID:         "circle-sharing",
		Wording: func(content *push.Content, language string) {
			content.Title = phraseSharing.In(language)
			content.Body = PhoneBody(sharingText(noteKind, detail, language))
		},
	})
}

var phraseSharing = push.Phrase{English: "Sharing", Chinese: "共享", Japanese: "共有"}

// sharingText is what a share_received or share_reached note says, in a
// language: the note's own English, or the dashboard's wording of it in
// Chinese or Japanese.
func sharingText(noteKind models.AgentNoteKind, detail string, language string) string {
	if language == push.LanguageEnglish {
		return models.NoteText(noteKind, detail)
	}
	switch noteKind {
	case models.NoteShareReceived:
		if received := models.ParseShareReceivedNote(detail); received != nil {
			return shareReceivedText(received, language)
		}
	case models.NoteShareReached:
		if reached := models.ParseShareReachedNote(detail); reached != nil {
			nouns := make([]string, 0, len(reached.ShareKinds))
			for _, kind := range reached.ShareKinds {
				nouns = append(nouns, shareKindPhrases[kind].In(language))
			}
			listed := strings.Join(nouns, phraseListSeparator.In(language))
			if last := len(nouns) - 1; last > 0 {
				listed = strings.Join(nouns[:last], phraseListSeparator.In(language)) + phraseListLastSeparator.In(language) + nouns[last]
			}
			return phraseShareReached.In(language, reached.MemberName, reached.CircleName, listed)
		}
	}
	return models.NoteText(noteKind, detail)
}

// shareReceivedText is a share_received note in Chinese or Japanese, as
// models.ShareReceivedText says it in English.
func shareReceivedText(note *models.ShareReceivedNote, language string) string {
	var sentence string
	if phrase, ok := shareReceivedSubjectPhrases[note.ShareKind]; ok && note.ShareSubject != "" {
		sentence = phrase.In(language, note.SharerName, note.ShareSubject)
	} else if phrase, ok := shareReceivedPhrases[note.ShareKind]; ok {
		sentence = phrase.In(language, note.SharerName)
	} else {
		sentence = phraseSharedSomething.In(language, note.SharerName)
	}
	if level, ok := shareLevelPhrases[note.ShareLevel]; ok && shareKindsWithLevel[note.ShareKind] {
		sentence += phraseShareLevel.In(language, level.In(language))
	}
	return sentence
}

// The wording of the sharing notes in Chinese and Japanese, as the dashboard
// has it. A phrase takes the sharer's name, and a subject phrase then what
// was shared.
var (
	shareReceivedPhrases = map[models.ShareKind]push.Phrase{
		models.ShareKindCalendar:  {Chinese: "%s与你共享了TA的日历", Japanese: "%sさんがカレンダーを共有しました"},
		models.ShareKindLocation:  {Chinese: "%s与你共享了TA的位置", Japanese: "%sさんが位置情報を共有しました"},
		models.ShareKindHealth:    {Chinese: "%s与你共享了TA的健康数据", Japanese: "%sさんがヘルスケアのデータを共有しました"},
		models.ShareKindRecording: {Chinese: "%s与你共享了一段录音", Japanese: "%sさんが録音を共有しました"},
		models.ShareKindFinance:   {Chinese: "%s与你共享了一个财务账户", Japanese: "%sさんが金融口座を共有しました"},
	}
	shareReceivedSubjectPhrases = map[models.ShareKind]push.Phrase{
		models.ShareKindMemoryPage: {Chinese: "%s与你共享了记忆页面“%s”", Japanese: "%sさんがメモリーページ「%s」を共有しました"},
		models.ShareKindSkill:      {Chinese: "%s与你共享了TA的“%s”技能", Japanese: "%sさんがスキル「%s」を共有しました"},
		models.ShareKindPhotoAlbum: {Chinese: "%s与你共享了相簿“%s”", Japanese: "%sさんがアルバム「%s」を共有しました"},
		models.ShareKindRecording:  {Chinese: "%s与你共享了录音“%s”", Japanese: "%sさんが録音「%s」を共有しました"},
	}
	phraseSharedSomething = push.Phrase{Chinese: "%s与你共享了内容", Japanese: "%sさんが共有しました"}

	// shareKindsWithLevel are the kinds whose note says how much was shared.
	shareKindsWithLevel = map[models.ShareKind]bool{
		models.ShareKindCalendar: true, models.ShareKindLocation: true, models.ShareKindPhotoAlbum: true,
		models.ShareKindRecording: true, models.ShareKindFinance: true,
	}
	phraseShareLevel  = push.Phrase{Chinese: "（%s）", Japanese: "（%s）"}
	shareLevelPhrases = map[string]push.Phrase{
		"busy":         {Chinese: "仅显示忙碌时段", Japanese: "予定の有無のみ"},
		"titles":       {Chinese: "标题", Japanese: "タイトル"},
		"details":      {Chinese: "详情", Japanese: "詳細"},
		"edit":         {Chinese: "你可以编辑", Japanese: "編集できます"},
		"current":      {Chinese: "TA现在的位置", Japanese: "現在地"},
		"history":      {Chinese: "TA现在和去过的位置", Japanese: "現在地と訪れた場所"},
		"view":         {Chinese: "你可以查看", Japanese: "閲覧できます"},
		"contribute":   {Chinese: "你可以添加照片", Japanese: "写真を追加できます"},
		"summary":      {Chinese: "标题和摘要", Japanese: "タイトルと要約"},
		"transcript":   {Chinese: "摘要和转写文本", Japanese: "要約と文字起こし"},
		"balance":      {Chinese: "仅余额", Japanese: "残高のみ"},
		"spending":     {Chinese: "余额和支出", Japanese: "残高と支出"},
		"transactions": {Chinese: "余额、支出和交易", Japanese: "残高、支出、取引"},
	}

	phraseShareReached = push.Phrase{
		Chinese:  "%s加入了“%s”，现在可以看到你的%s",
		Japanese: "%sさんが「%s」に参加し、あなたの%sを見られるようになりました",
	}
	shareKindPhrases = map[models.ShareKind]push.Phrase{
		models.ShareKindCalendar:   {Chinese: "日历", Japanese: "カレンダー"},
		models.ShareKindLocation:   {Chinese: "位置", Japanese: "位置情報"},
		models.ShareKindHealth:     {Chinese: "健康数据", Japanese: "ヘルスケアのデータ"},
		models.ShareKindMemoryPage: {Chinese: "记忆页面", Japanese: "メモリーページ"},
		models.ShareKindPhotoAlbum: {Chinese: "相簿", Japanese: "アルバム"},
		models.ShareKindRecording:  {Chinese: "录音", Japanese: "録音"},
		models.ShareKindFinance:    {Chinese: "财务账户", Japanese: "金融口座"},
		models.ShareKindSkill:      {Chinese: "技能", Japanese: "スキル"},
		models.ShareKindPasswords:  {Chinese: "密码", Japanese: "パスワード"},
	}
	phraseListSeparator     = push.Phrase{English: ", ", Chinese: "、", Japanese: "、"}
	phraseListLastSeparator = push.Phrase{English: " and ", Chinese: "和", Japanese: "と"}
)
