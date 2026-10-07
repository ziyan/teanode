package skills

import (
	"strings"
	"testing"
	"time"
)

// watchingSkill is a skill with one tool that lists and one that reads,
// and the watches given.
func watchingSkill(watches string) string {
	return `---
name: notes
description: "Notes on the person's own computer"
tools:
  - name: notes_new
    description: The notes written since a moment.
    type: shell
    command: [notes, list, --since, "{{since_epoch}}", --json]
    parameters:
      type: object
      properties:
        since_epoch: {type: string}
        folder: {type: string}
      required: ["since_epoch"]
  - name: notes_read
    description: One note in full.
    type: shell
    command: [notes, show, "{{id}}"]
    parameters:
      type: object
      properties:
        id: {type: string}
      required: ["id"]
` + watches + `---
`
}

func TestWatchesAreParsedAndChecked(t *testing.T) {
	skill, err := Parse([]byte(watchingSkill(`watches:
  - name: new_notes
    description: notes written since the last look
    kind: item
    guidance: A note that names a deadline is worth telling.
    every: 30m
    overlap: 1h
    list: {tool: notes_new, arguments: {folder: inbox}}
    read: {tool: notes_read}
`)))
	if err != nil {
		t.Fatalf("Parse: %s", err)
	}
	watch := skill.Watch("new_notes")
	if watch == nil || watch.EveryDuration() != 30*time.Minute || watch.OverlapDuration() != time.Hour {
		t.Fatalf("the watch: %+v", watch)
	}
	since := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	arguments := skill.ListArguments(watch, since)
	if arguments["folder"] != "inbox" || arguments["since_epoch"] != "1772600767" || arguments["since"] != nil || arguments["since_date"] != nil {
		t.Fatalf("the list is given its own arguments and the forms of the moment it declares: %v", arguments)
	}
	if read := skill.ReadArguments(watch, &WatchedItem{ID: "n1"}); read["id"] != "n1" {
		t.Fatalf("the read is given the id: %v", read)
	}
	if every := (&Watch{Every: "5s"}).EveryDuration(); every != time.Minute {
		t.Fatalf("no faster than a minute: %s", every)
	}
	if (&Watch{}).EveryDuration() != 10*time.Minute || (&Watch{}).OverlapDuration() != 10*time.Minute {
		t.Fatal("ten minutes each by default")
	}

	for broken, why := range map[string]string{
		"    kind: item\n    list: {tool: notes_new}\n":                              "no guidance",
		"    kind: news\n    list: {tool: notes_new}\n":                              "not mail or item",
		"    kind: mail\n    list: {tool: notes_gone}\n":                             "does not have",
		"    kind: mail\n    list: {tool: notes_new}\n    read: {tool: notes_new}\n": "takes no id",
		"    kind: mail\n    every: often\n    list: {tool: notes_new}\n":            "not a duration",
	} {
		_, err := Parse([]byte(watchingSkill("watches:\n  - name: new_notes\n    description: notes\n" + broken)))
		if err == nil || !strings.Contains(err.Error(), why) {
			t.Errorf("%q: %v, want %q", broken, err, why)
		}
	}
}

func TestWatchedItemsAreReadFromTheListsAnswer(t *testing.T) {
	items, err := ParseWatchedItems(map[string]any{"text": `[{"id":"a","version":"2","at":"2026-03-04T05:06:07Z","from":"someone","title":"t","text":"x","url":"https://example.com/a"},{"title":"no id"}]`})
	if err != nil || len(items) != 1 || items[0].ID != "a" || items[0].Version != "2" || items[0].URL != "https://example.com/a" {
		t.Fatalf("an array, the item with no id left out: %+v %v", items, err)
	}
	if items, err := ParseWatchedItems(map[string]any{"text": `{"items":[{"id":"b"}]}`}); err != nil || len(items) != 1 || items[0].ID != "b" {
		t.Fatalf("or under items: %+v %v", items, err)
	}
	if items, err := ParseWatchedItems(map[string]any{"text": "  "}); err != nil || len(items) != 0 {
		t.Fatalf("nothing printed is nothing new: %+v %v", items, err)
	}
	if _, err := ParseWatchedItems(map[string]any{"text": "[ended 1 on laptop]\nnot signed in"}); err == nil || !strings.Contains(err.Error(), "not signed in") {
		t.Fatalf("a complaint is said: %v", err)
	}
}
