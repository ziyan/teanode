package cmd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
)

// Every operation of the journal on the API has a command under the name
// below, and every command is an operation. The agent's journal tool is
// written with the dream that makes the narratives, and its parity is checked
// with it; this checks the API and the command line.
func TestJournalParity(test *testing.T) {
	test.Parallel()

	operations := map[string]bool{}
	for _, kind := range []reflect.Type{reflect.TypeFor[apigraph.JournalQuery](), reflect.TypeFor[apigraph.JournalMutation]()} {
		for index := 0; index < kind.NumMethod(); index++ {
			operations[kind.Method(index).Name] = true
		}
	}

	// The subcommand that does each operation.
	subcommands := map[string]string{
		"ListJournalDays":         "list",
		"ReadJournalDay":          "day",
		"ReadJournalPeriod":       "week",
		"SearchJournal":           "search",
		"ReadJournalSettings":     "settings",
		"SetJournalWords":         "write",
		"SetJournalMood":          "mood",
		"SetJournalLookingBack":   "look-back",
		"TouchUpJournalWords":     "touch-up",
		"StopJournalTouchUp":      "touch-up",
		"CorrectJournalNarrative": "correct",
		"RemoveJournalCorrection": "uncorrect",
		"WriteJournalNarrative":   "rewrite",
		"DeleteJournalNarrative":  "forget-narrative",
		"PinJournalRecord":        "pin",
		"RemoveJournalLink":       "unlink",
		"UpdateJournalSettings":   "settings",
		"EraseJournal":            "erase",
	}
	for operation := range operations {
		if _, isListed := subcommands[operation]; !isListed {
			test.Errorf("the table does not list the operation %s", operation)
		}
	}

	commands := map[string]*cli.Command{}
	for _, subcommand := range NewJournalCommand().Commands {
		commands[subcommand.Name] = subcommand
	}
	for operation, name := range subcommands {
		if !operations[operation] {
			test.Errorf("the API has no operation %s", operation)
		}
		if commands[name] == nil {
			test.Errorf("teanode journal has no %s, which does %s", name, operation)
		}
	}
	// The three periods are one operation with a kind.
	for _, name := range []string{"month", "year"} {
		if commands[name] == nil {
			test.Errorf("teanode journal has no %s", name)
		}
	}
	isOperationCommand := map[string]bool{"month": true, "year": true}
	for _, name := range subcommands {
		isOperationCommand[name] = true
	}
	for name := range commands {
		if !isOperationCommand[name] {
			test.Errorf("teanode journal %s is not an operation of the API", name)
		}
	}

	// Every argument of an operation is a flag of its subcommand, under the
	// name the command line gives it, or is exempt with the reason.
	flagNames := map[string]string{
		"fromDay": "from", "untilDay": "until", "offset": "page", "periodKind": "period", "how": "how",
		"words": "text", "lookingBack": "text", "moodWords": "words", "linkReason": "reason",
		"isWeatherOn": "weather", "isEveningQuestionOn": "evening-question", "eveningQuestionTime": "evening-question-time",
	}
	// "<Operation>.<argument>" is exempt for the reason given.
	exempt := map[string]string{
		"ListJournalDays.first":                   "a page is 14 days, and --page moves through them",
		"SearchJournal.first":                     "a page is 20 hits, and --page moves through them",
		"SearchJournal.query":                     "the text is the argument of the subcommand",
		"ReadJournalDay.day":                      "the day is the argument of the subcommand",
		"ReadJournalPeriod.periodKind":            "the subcommand is the kind: week, month or year",
		"ReadJournalPeriod.day":                   "the day is the argument of the subcommand",
		"SetJournalWords.day":                     "the day is the argument of the subcommand",
		"SetJournalWords.baseModifiedAt":          "the command reads the day first and sends the time it read",
		"SetJournalMood.day":                      "the day is the argument of the subcommand",
		"SetJournalMood.moodLevel":                "the mood is the argument of the subcommand",
		"SetJournalLookingBack.periodKind":        "the kind is the first argument of the subcommand",
		"SetJournalLookingBack.day":               "the day is the argument of the subcommand",
		"SetJournalLookingBack.baseModifiedAt":    "the command reads the period first and sends the time it read",
		"TouchUpJournalWords.day":                 "the day is the argument of the subcommand",
		"StopJournalTouchUp.day":                  "the day is the argument of the subcommand",
		"CorrectJournalNarrative.day":             "the day is the argument of the subcommand",
		"CorrectJournalNarrative.correctionText":  "the words are the argument of the subcommand",
		"RemoveJournalCorrection.day":             "the day is the argument of the subcommand",
		"RemoveJournalCorrection.correctionIndex": "the number is the argument of the subcommand, counted from 1 as the day lists them",
		"WriteJournalNarrative.day":               "the day is the argument of the subcommand",
		"DeleteJournalNarrative.day":              "the day is the argument of the subcommand",
		"PinJournalRecord.day":                    "the day is the argument of the subcommand",
		"PinJournalRecord.recordKind":             "the record is the kind:id argument of the subcommand",
		"PinJournalRecord.recordId":               "the record is the kind:id argument of the subcommand",
		"RemoveJournalLink.linkId":                "the link is the argument of the subcommand",
	}
	for key, reason := range exempt {
		if strings.TrimSpace(reason) == "" {
			test.Errorf("the exemption %s needs a reason", key)
		}
	}
	for _, kind := range []reflect.Type{reflect.TypeFor[apigraph.JournalQuery](), reflect.TypeFor[apigraph.JournalMutation]()} {
		for index := 0; index < kind.NumMethod(); index++ {
			method := kind.Method(index)
			if method.Type.NumIn() < 2 {
				continue
			}
			flags := map[string]bool{}
			for _, flag := range commands[subcommands[method.Name]].Flags {
				for _, name := range flag.Names() {
					flags[name] = true
				}
			}
			argumentsType := method.Type.In(1)
			for field := 0; field < argumentsType.NumField(); field++ {
				argument, _, _ := strings.Cut(argumentsType.Field(field).Tag.Get("json"), ",")
				if _, isExempt := exempt[method.Name+"."+argument]; !isExempt && !flags[flagNames[argument]] {
					test.Errorf("teanode journal %s has no flag for the argument %s of %s", subcommands[method.Name], argument, method.Name)
				}
			}
		}
	}

	// Every command takes --json.
	for name, subcommand := range commands {
		hasJSON := false
		for _, flag := range subcommand.Flags {
			hasJSON = hasJSON || flag.Names()[0] == "json"
		}
		if !hasJSON {
			test.Errorf("teanode journal %s has no --json", name)
		}
	}
}
