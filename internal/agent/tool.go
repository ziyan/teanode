package agent

import (
	"github.com/ziyan/teanode/internal/agent/tools"
)

// The tool kit lives in internal/agent/tools; these are its names as this
// package has always used them, kept while the tools move out one family
// at a time.

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
	RiskGranting    = tools.RiskGranting

	FamilyMailbox  = tools.FamilyMailbox
	FamilyDomains  = tools.FamilyDomains
	FamilyAudit    = tools.FamilyAudit
	FamilyPeople   = tools.FamilyPeople
	FamilyServer   = tools.FamilyServer
	FamilyAccount  = tools.FamilyAccount
	FamilyGeneral  = tools.FamilyGeneral
	FamilyServers  = tools.FamilyServers
	FamilySkills   = tools.FamilySkills
	FamilyBrowser  = tools.FamilyBrowser
	FamilyComputer = tools.FamilyComputer
)

var (
	NewCatalog        = tools.NewCatalog
	RenameTools       = tools.Rename
	ActionsOf         = tools.ActionsOf
	NeedsConfirmation = tools.NeedsConfirmation
	Split             = tools.Split
	Search            = tools.Search
	listed            = tools.Listed

	// Location and the time helpers moved to the kit with the datetime tool.
	Location = tools.Location

	// The text and cron helpers moved to the kit too.
	HTMLToText      = tools.HTMLToText
	normalizeText   = tools.NormalizeText
	stripQuoted     = tools.StripQuoted
	nextCron        = tools.NextCron
	resolveRelative = tools.ResolveRelative
)

// Helpers that moved to the kit with the mailbox family.
var (
	threadSubject   = tools.ThreadSubject
	replySubject    = tools.ReplySubject
	permissionWords = tools.PermissionWords
)
