package models

import (
	"slices"
	"testing"
)

// What an agent writes mentions a member by an @ before their name, and
// everybody by @all in any of the apps' languages; an address, a longer
// name and a member who left mention nobody.
func TestWordsMentionMembersByAnAtBeforeTheirName(t *testing.T) {
	t.Parallel()
	members := []*ChatMember{
		{UserID: "user-ada", Name: "Ada"},
		{UserID: "user-ada-lin", Name: "Ada Lin"},
		{UserID: "user-milo", Name: "Milo"},
		{UserID: "user-gone", Name: "Rosa", IsRemoved: true},
	}
	for _, testCase := range []struct {
		body        string
		wantUserIds []string
		wantAll     bool
	}{
		{"@Ada can you look?", []string{"user-ada"}, false},
		{"thanks @ada lin, and @Milo.", []string{"user-ada-lin", "user-milo"}, false},
		{"@Adam is not here", nil, false},
		{"write to ada@example.com", nil, false},
		{"@Rosa left", nil, false},
		{"@all dinner at six", nil, true},
		{"大家好@所有人", nil, true},
		{"@全員 よろしく", nil, true},
		{"@allison", nil, false},
		{"@Milo @Milo", []string{"user-milo"}, false},
	} {
		gotUserIds, gotAll := ChatMentionsIn(testCase.body, members)
		if !slices.Equal(gotUserIds, testCase.wantUserIds) || gotAll != testCase.wantAll {
			t.Errorf("ChatMentionsIn(%q) = %v, %v; want %v, %v", testCase.body, gotUserIds, gotAll, testCase.wantUserIds, testCase.wantAll)
		}
	}
}

// A message mentions a person by name or by mentioning everybody.
func TestAMessageMentionsAPersonByNameOrEverybody(t *testing.T) {
	t.Parallel()
	named := &ChatMessage{MentionedUserIDs: []string{"user-ada"}}
	if !named.MentionsUser("user-ada") || named.MentionsUser("user-milo") || named.MentionsUser("") {
		t.Error("a message naming one person mentions only them")
	}
	if everybody := (&ChatMessage{IsAllMentioned: true}); !everybody.MentionsUser("user-milo") {
		t.Error("a message mentioning everybody does not mention a member")
	}
}
