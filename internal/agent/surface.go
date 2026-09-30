package agent

import "strings"

// A surface is where a turn's words reach the person: the dashboard, a
// phone, a terminal, a mail, a chat app. Everything the prompt says because
// of it is here, in one entry: the situation's line on where the turn is
// happening, the block after the history on how to write for it, and
// whether it draws suggested replies. A new surface is one entry.
type surface struct {
	// situationLine is what the situation says of where the turn is;
	// empty says nothing.
	situationLine string

	// overlay is the block after the history on how to write for it;
	// empty adds none.
	overlay string

	// hasSuggestedReplies says the surface draws the hidden suggested
	// replies as buttons; anywhere else they are taken off
	// (models.StripSuggestedReplies).
	hasSuggestedReplies bool
}

// dashboardOverlay is how to write for the dashboard, which renders
// Markdown.
const dashboardOverlay = "<surface>\nThe dashboard: Markdown renders, and a table suits a list of like things. " + picturesLine + "\n</surface>"

// picturesLine is how a picture is shown where the dashboard draws one.
const picturesLine = "A picture shows in the answer when written as ![what it shows](address), with an address a tool showed you (an image on a page you read, a search result's picture) and never one you put together; [![what it shows](address)](page) links it to its page. Show a picture when it helps choose or recognize something, not as a link to one."

// surfaces is every surface the prompt says something particular about.
// A surface not listed is named in the situation and nothing more.
var surfaces = map[string]surface{
	// The drawer says "phone" on a narrow screen and "extension" in the
	// browser extension.
	"drawer":    {situationLine: talkingThrough("drawer"), overlay: dashboardOverlay, hasSuggestedReplies: true},
	"extension": {situationLine: talkingThrough("extension"), overlay: dashboardOverlay, hasSuggestedReplies: true},
	"page":      {situationLine: talkingThrough("page"), overlay: dashboardOverlay, hasSuggestedReplies: true},
	"phone": {
		situationLine:       talkingThrough("phone"),
		overlay:             "<surface>\nA phone: keep it short, no tables. " + picturesLine + "\n</surface>",
		hasSuggestedReplies: true,
	},
	"cli": {
		situationLine: talkingThrough("cli"),
		overlay:       terminalOverlay,
	},
	"api": {
		situationLine: talkingThrough("api"),
		overlay:       terminalOverlay,
	},
	"mail": {
		situationLine: talkingThrough("mail"),
		overlay:       "<surface>\nThe answer goes out as a mail message: plain paragraphs, and the first line is its subject.\n</surface>",
	},
	// A chat app is sent through its own Markdown: Telegram's older one,
	// which the bot converts to, and Discord's, which is the common one. A
	// mail: or memory: link is made an address of the dashboard there, or
	// its words alone when the server has no name.
	"telegram": {
		situationLine: "You are talking through Telegram.",
		overlay:       "<surface>\nTelegram on a phone: short, plain paragraphs, no tables, no headings; a list is one item per line. Bold, italics, `code` and web links show. A mail: or memory: link opens in the dashboard, so name what it is as well. Something you make (a page, a chart) reaches them as a file.\n</surface>",
	},
	"discord": {
		situationLine: "You are talking through Discord.",
		overlay:       "<surface>\nDiscord: short paragraphs, no tables; a list is one item per line. Bold, italics, `code`, code blocks and web links show. A mail: or memory: link opens in the dashboard, so name what it is as well. Something you make (a page, a chart) reaches them as a file.\n</surface>",
	},
	"mcp": {
		situationLine: "You are answering a program that reached you through the Model Context Protocol, for the person.",
	},
	backgroundSurface: {
		situationLine: "This turn was woken by a command you left running in the background, not by the person; what you say is read in the conversation when they look.",
	},
	speakFirstSurfacePrefix: {
		situationLine:       "You are writing first, before the person has said anything; they read it in the dashboard.",
		overlay:             dashboardOverlay,
		hasSuggestedReplies: true,
	},
}

// terminalOverlay is how to write for a terminal: the CLI and the API.
const terminalOverlay = "<surface>\nA terminal: plain text, no markdown tables wider than eighty columns, no suggestions of what to click.\n</surface>"

// talkingThrough is the situation's plain line for a surface.
func talkingThrough(name string) string {
	return "You are talking through the " + name + "."
}

// surfaceOf is what the prompt says for a turn on the named surface. A
// spoken-first turn is named by its reason after the prefix; an empty name
// says nothing at all.
func surfaceOf(name string) surface {
	if found, ok := surfaces[name]; ok {
		return found
	}
	if strings.HasPrefix(name, speakFirstSurfacePrefix) {
		return surfaces[speakFirstSurfacePrefix]
	}
	if name == "" {
		return surface{}
	}
	return surface{situationLine: talkingThrough(name)}
}
