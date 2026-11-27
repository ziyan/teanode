package tools

import (
	"context"
	"fmt"
	"unicode/utf8"

	"github.com/ziyan/teanode/internal/mcpserve"
)

// ResultMoreName is the tool that reads on in a result that was too long for
// one answer.
const ResultMoreName = "result_more"

// moreLineReserve is the room a page keeps for the line after it that says how
// to read on.
const moreLineReserve = 300

// PageResult is a tool's text as one answer carries it. A text within what the
// run keeps of a result (ResultCharactersOf) comes back whole. A longer one is
// held whole, and what comes back is its first page, cut at a character and
// preferably at a line break, and a line saying how long the whole result is
// and how to read on with result_more. Nothing is dropped.
//
// The holder is the agent, so that a result can be read on in a later turn of
// the same conversation and never by anybody else. When the result cannot be
// held (a database that cannot be written, a result larger than one holder
// may keep) the line says so, rather than promising a way on.
func PageResult(ctx context.Context, run Run, text string, isUntrusted, isError bool) string {
	pageCharacters := ResultCharactersOf(run)
	if len(text) <= pageCharacters {
		return text
	}
	end := mcpserve.PageEnd(text, 0, pageCharacters-moreLineReserve)
	totalCharacterCount := utf8.RuneCountInString(text)
	remainingCharacterCount := utf8.RuneCountInString(text[end:])
	id := mcpserve.NewResultStore(run.Database()).Hold(ctx, holderOf(run), text, isUntrusted, isError)
	if id == "" {
		return fmt.Sprintf("%s\n[the result is %d characters in all and %d more are not shown: too many to hold for later, so narrow the call to read them]", text[:end], totalCharacterCount, remainingCharacterCount)
	}
	return fmt.Sprintf("%s\n[the result is %d characters in all and %d more follow: call %s with result_id %q and offset %d]", text[:end], totalCharacterCount, remainingCharacterCount, ResultMoreName, id, end)
}

// ReadResultMore is the next page of a result PageResult held: from offset,
// as long as a page is, with the line saying how to read on when there is
// more. isUntrusted says whether the result came from outside, for the caller
// to mark.
func ReadResultMore(ctx context.Context, run Run, id string, offset int) (page string, isUntrusted bool, err error) {
	text, isUntrusted, _, ok, err := mcpserve.NewResultStore(run.Database()).Find(ctx, holderOf(run), id)
	if err != nil {
		return "", false, fmt.Errorf("cannot read result %q just now: try the call again", id)
	}
	if !ok {
		return "", false, fmt.Errorf("no result %q is held: results are held for 30 minutes, a few at a time; make the original call again", id)
	}
	if offset < 0 || offset > len(text) {
		return "", false, fmt.Errorf("offset %d is outside the result, whose offsets run from 0 to %d; pass the offset the result named", offset, len(text))
	}
	if offset == len(text) {
		return "nothing more: that offset is the end of the result", isUntrusted, nil
	}
	for offset > 0 && !utf8.RuneStart(text[offset]) {
		offset--
	}
	end := mcpserve.PageEnd(text, offset, ResultCharactersOf(run)-moreLineReserve)
	if end >= len(text) {
		return text[offset:], isUntrusted, nil
	}
	return fmt.Sprintf("%s\n[%d more characters: call %s with result_id %q and offset %d]", text[offset:end], utf8.RuneCountInString(text[end:]), ResultMoreName, id, end), isUntrusted, nil
}

// holderOf is who a held result belongs to: the agent that made the call, in
// the conversation it made it in, so one conversation's long results never
// push out another's.
func holderOf(run Run) string {
	if conversation := run.Conversation(); conversation != nil {
		return "agent/" + run.Agent().ID + "/" + conversation.ID
	}
	return "agent/" + run.Agent().ID
}
