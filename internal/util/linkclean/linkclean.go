// Package linkclean shortens a link for people: it takes the tracking
// parameters out of a link's query string, and writes the short text shown
// for a link that has no words of its own.
//
// Both are done on the text of the link rather than on a parsed URL. The
// dashboard and the iPhone app do the same work, and each language's URL
// parser normalizes differently (host case, percent escapes, dot segments),
// so a parser would make three answers to one question. The shared cases in
// testdata/links.json are read by the tests of all three, and a change here
// is a change there.
package linkclean

import (
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	// DisplayLength is the longest text shown for a link, in characters.
	DisplayLength = 48

	// DisplayQueryLength is the longest cleaned query, question mark
	// included, that is shown with the link. A longer one is left out.
	DisplayQueryLength = 32

	ellipsis = "…"
)

// trackingParameters are query parameters that say where a visitor came from
// and carry nothing about what the page is. utm_ is a family and is matched
// by prefix.
var trackingParameters = map[string]bool{
	"gclid": true, "gclsrc": true, "gbraid": true, "wbraid": true, "dclid": true,
	"fbclid": true, "msclkid": true, "yclid": true, "twclid": true, "ttclid": true,
	"li_fat_id": true, "igshid": true, "mc_cid": true, "mc_eid": true,
	"_gl": true, "_ga": true, "_hsenc": true, "_hsmi": true, "mkt_tok": true,
	"oly_anon_id": true, "oly_enc_id": true, "vero_id": true, "rb_clickid": true,
	"s_cid": true, "ref_src": true,
}

// linkPattern splits a web link into its scheme, authority, path, query
// (question mark included) and fragment (number sign included).
var linkPattern = regexp.MustCompile(`^(?i:(https?))://([^/?#]*)([^?#]*)(\?[^#]*)?(#(?s:.*))?$`)

// Clean is raw without its tracking parameters. Every other parameter stays,
// as written and in its order. Anything that is not an http or https link,
// and a link with nothing to remove, comes back exactly as it was given.
func Clean(raw string) string {
	parts := linkPattern.FindStringSubmatch(raw)
	if parts == nil || parts[4] == "" {
		return raw
	}
	kept, isChanged := keptParameters(parts[4])
	if !isChanged {
		return raw
	}
	cleaned := parts[1] + "://" + parts[2] + parts[3]
	if len(kept) > 0 {
		cleaned += "?" + strings.Join(kept, "&")
	}
	return cleaned + parts[5]
}

// keptParameters are the parameters of a query (question mark included) that
// are not tracking, and whether any was dropped. A query with nothing to
// drop is not rewritten.
func keptParameters(query string) ([]string, bool) {
	var kept []string
	isChanged := false
	for _, parameter := range strings.Split(query[1:], "&") {
		name, _, _ := strings.Cut(parameter, "=")
		name = strings.ToLower(name)
		if trackingParameters[name] || strings.HasPrefix(name, "utm_") {
			isChanged = true
			continue
		}
		kept = append(kept, parameter)
	}
	if !isChanged {
		return nil, false
	}
	// Dropping a tracking parameter rewrites the query, and the empty
	// parameters it leaves (a doubled ampersand) go with it.
	return slices.DeleteFunc(kept, func(parameter string) bool { return parameter == "" }), true
}

// DisplayText is the short text for a link that has no words of its own: the
// host without "www.", the path, and the cleaned query only when it is short.
// There is no scheme, no credentials and no fragment, and past DisplayLength
// characters the middle is cut and replaced with an ellipsis. Text that is
// not an http or https link is cut the same way and otherwise left alone.
func DisplayText(raw string) string {
	parts := linkPattern.FindStringSubmatch(Clean(raw))
	if parts == nil {
		return shorten(raw)
	}
	host := strings.ToLower(parts[2])
	if at := strings.LastIndex(host, "@"); at >= 0 {
		host = host[at+1:]
	}
	host = strings.TrimPrefix(host, "www.")
	path := parts[3]
	if decoded, err := url.PathUnescape(path); err == nil && utf8.ValidString(decoded) {
		path = decoded
	}
	query := parts[4]
	if utf8.RuneCountInString(query) > DisplayQueryLength {
		query = ""
	}
	if path == "/" && query == "" {
		path = ""
	}
	return shorten(host + path + query)
}

// shorten cuts the middle out of text longer than DisplayLength characters,
// so that both the start, which says where the link goes, and the end, which
// says what it is, stay.
func shorten(text string) string {
	characters := []rune(text)
	if len(characters) <= DisplayLength {
		return text
	}
	headLength := DisplayLength / 2
	tailLength := DisplayLength - headLength - 1
	return string(characters[:headLength]) + ellipsis + string(characters[len(characters)-tailLength:])
}
