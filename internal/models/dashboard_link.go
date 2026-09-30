package models

import (
	"net/url"
	"regexp"
	"strings"
)

// The agent links to the dashboard in schemes of its own rather than in
// addresses, because it does not know the address the person reads it at:
// a message it cites is [subject](mail:ITEM_ID), a page of its memory is
// [name](memory:PATH), and one fact of that page [name](memory:PATH#N). The
// dashboard draws them as links to its own pages; anywhere else they are
// made whole from the dashboard's address, or left as their words.

// dashboardLinkPattern is a link in one of those schemes.
var dashboardLinkPattern = regexp.MustCompile(`\[([^\]\n]+)\]\((mail|memory):([^)\s]+)\)`)

// mailItemPattern is an item id as a mail: link may carry one.
var mailItemPattern = regexp.MustCompile(`^[A-Za-z0-9]+$`)

// factNumberPattern is the fact a memory: link may point at.
var factNumberPattern = regexp.MustCompile(`^[0-9]{1,6}$`)

// DashboardPath is the path in the dashboard a link of its own schemes
// opens, or "" when what follows the scheme is not one it could be. It is
// strict because the text is a model's: a memory: path is only a path the
// graph could hold, so no query, no "..", and nothing else rides along.
//
// A fact's number is accepted and not carried: the Knowledge page opens a
// page, not a place in it.
func DashboardPath(scheme, target string) string {
	switch scheme {
	case "mail":
		if !mailItemPattern.MatchString(target) {
			return ""
		}
		// Starred opens any item by id, whichever folder it is in.
		return "/mailbox/starred/" + target
	case "memory":
		path, fact, hasFact := strings.Cut(target, "#")
		if hasFact && !factNumberPattern.MatchString(fact) {
			return ""
		}
		if ValidPath(path) != nil {
			return ""
		}
		segments := strings.Split(path, "/")
		for index, segment := range segments {
			segments[index] = url.PathEscape(segment)
		}
		return "/settings/knowledge/" + strings.Join(segments, "/")
	}
	return ""
}

// LinkDashboardLinks makes each link in the dashboard's own schemes whole
// from base, the dashboard's address (config.DashboardBase), for somewhere
// that opens only addresses: a chat app. With no base, or a link that is not
// one, what is left is its words, which is what a chat app would otherwise
// show as a raw address that opens nothing.
func LinkDashboardLinks(text, base string) string {
	return dashboardLinkPattern.ReplaceAllStringFunc(text, func(link string) string {
		parts := dashboardLinkPattern.FindStringSubmatch(link)
		words, scheme, target := parts[1], parts[2], parts[3]
		path := DashboardPath(scheme, target)
		if base == "" || path == "" {
			return words
		}
		return "[" + words + "](" + base + path + ")"
	})
}

// UnlinkDashboardLinks leaves each link in the dashboard's own schemes as
// its words.
func UnlinkDashboardLinks(text string) string {
	return LinkDashboardLinks(text, "")
}
