package agent

import (
	"context"

	"github.com/ziyan/teanode/internal/agent/tools"
)

// The tool kit lives in internal/agent/tools; these are its names as this
// package has always used them, kept while the tools move out one family
// at a time (docs/planning/active/20260911-one-tool-one-package.md).

type (
	Tool    = tools.Tool
	Call    = tools.Call
	Result  = tools.Result
	Risk    = tools.Risk
	Family  = tools.Family
	Catalog = tools.Catalog
)

const (
	deferralThreshold = tools.DeferralThreshold

	RiskRead        = tools.RiskRead
	RiskWrite       = tools.RiskWrite
	RiskDestructive = tools.RiskDestructive
	RiskOutward     = tools.RiskOutward

	FamilyMailbox = tools.FamilyMailbox
	FamilyDomains = tools.FamilyDomains
	FamilyAudit   = tools.FamilyAudit
	FamilyPeople  = tools.FamilyPeople
	FamilyServer  = tools.FamilyServer
	FamilyAccount = tools.FamilyAccount
	FamilyGeneral = tools.FamilyGeneral
	FamilyServers = tools.FamilyServers
	FamilyBrowser = tools.FamilyBrowser
)

var (
	NewCatalog        = tools.NewCatalog
	NeedsConfirmation = tools.NeedsConfirmation
	Split             = tools.Split
	Search            = tools.Search
	listed            = tools.Listed

	object          = tools.Object
	stringProperty  = tools.StringProperty
	enumProperty    = tools.EnumProperty
	integerProperty = tools.IntegerProperty
	booleanProperty = tools.BooleanProperty
	arrayProperty   = tools.ArrayProperty
	jsonResult      = tools.JSONResult

	// Location and the time helpers moved to the kit with the datetime tool.
	Location = tools.Location

	// The text and cron helpers moved to the kit too.
	HTMLToText      = tools.HTMLToText
	normalizeText   = tools.NormalizeText
	stripQuoted     = tools.StripQuoted
	nextCron        = tools.NextCron
	resolveRelative = tools.ResolveRelative
)

func decodeArguments[T any](call *Call) (T, error) {
	return tools.DecodeArguments[T](call)
}

// runOf is the turn a tool was called in, as this package's own run. The
// tools still in this package reach it this way; a tool package reaches
// the same run through the kit's interface.
func runOf(ctx context.Context) *AskRun {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		panic(err)
	}
	return run.(*AskRun)
}

// Helpers that moved to the kit with the mailbox family.
var (
	threadSubject   = tools.ThreadSubject
	replySubject    = tools.ReplySubject
	permissionWords = tools.PermissionWords
)
