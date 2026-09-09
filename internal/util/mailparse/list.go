package mailparse

import (
	"net/mail"
	"strings"
)

// What a message says about the mailing list it came from.
//
// Bulk mail carries headers that ordinary mail does not. List-Unsubscribe
// (RFC 2369) says how to leave: one or more URLs in angle brackets, an
// https: page or a mailto: address or both. List-Id (RFC 2919) names the list
// itself — "Example Weekly <weekly.news.example.com>" — which is the identity
// a sender publishes for exactly this purpose and which survives a change of
// sending address. List-Unsubscribe-Post (RFC 8058) is the sender undertaking
// that one POST to the https: URL, with no further interaction, unsubscribes
// the reader.
//
// A message carrying none of them is ordinary mail. Precedence: bulk is not
// enough on its own: bounce notices and automatic replies carry it, and
// nobody subscribed to those.
//
// List-Unsubscribe-Post on its own is the exception, and is not a guess. It
// says nothing by itself — it exists only to promise that the address in
// List-Unsubscribe answers a POST — so a sender never emits it alone. Finding
// it alone means the address was removed on the way here, which is what the
// relays that hide a reader's real address do: Apple's rewrites the sender and
// drops List-Unsubscribe, leaving this behind. The message is still a
// newsletter, and the reader still wants it grouped; what they cannot be
// offered is a way out, so that is what the message is marked with.

// ListInfo is what a message says about its list, or the zero value when it
// says nothing.
type ListInfo struct {
	// Key identifies the subscription: the list's own identifier when it
	// publishes one, and the sending address otherwise.
	Key string

	// Name is what to call it in front of a person.
	Name string

	// Unsubscribe are the addresses to leave by, in the order the sender put
	// them, each an https:, http: or mailto: URL.
	Unsubscribe []string

	// OneClick reports that the sender promised RFC 8058: a POST to the
	// https: address leaves the list, with nothing else to do.
	OneClick bool

	// Stripped reports that the sender said how to leave and something
	// between them and here removed it. The subscription is real; the way out
	// is not missing because the sender withheld it.
	Stripped bool
}

// Subscription reports whether the message belongs to a list at all.
func (self ListInfo) Subscription() bool {
	return self.Key != ""
}

// ParseList reads what a message says about its list. from is the From header
// as it arrived, undecoded; the address and the display name are taken from
// it when the list publishes no identity of its own.
func ParseList(headers []string, from string) ListInfo {
	unsubscribe := unfold(FindHeaderValue(headers, "List-Unsubscribe"))
	listID := unfold(FindHeaderValue(headers, "List-Id"))
	post := unfold(FindHeaderValue(headers, "List-Unsubscribe-Post"))

	// The third way in: the promise about an address, with no address. See
	// the note above — a sender does not write this on its own.
	stripped := unsubscribe == "" && post != ""
	if unsubscribe == "" && listID == "" && !stripped {
		return ListInfo{}
	}

	info := ListInfo{Unsubscribe: unsubscribeURLs(unsubscribe), Stripped: stripped}
	info.Key, info.Name = listIdentity(listID)
	if info.Key == "" {
		// No identity of its own, so the sender is the subscription. The
		// address rather than the display name, which changes with every
		// issue on some newsletters.
		address, name := addressAndName(from)
		info.Key = strings.ToLower(address)
		info.Name = name
		if info.Name == "" {
			info.Name = address
		}
	}
	if info.Key == "" {
		return ListInfo{}
	}

	// One-click over plain http would be readable by anyone on the path, and
	// RFC 8058 is written for https. A sender that promises one-click and
	// offers only a mailto: address has promised nothing this can act on.
	if hasOneClick(post) {
		for _, candidate := range info.Unsubscribe {
			if strings.HasPrefix(strings.ToLower(candidate), "https://") {
				info.OneClick = true
				break
			}
		}
	}
	return info
}

// HTTPSUnsubscribe is the address to POST to, or the empty string.
func (self ListInfo) HTTPSUnsubscribe() string {
	for _, candidate := range self.Unsubscribe {
		if strings.HasPrefix(strings.ToLower(candidate), "https://") {
			return candidate
		}
	}
	return ""
}

// WebUnsubscribe is a page a person can open, http as well as https, or the
// empty string.
func (self ListInfo) WebUnsubscribe() string {
	for _, candidate := range self.Unsubscribe {
		lowered := strings.ToLower(candidate)
		if strings.HasPrefix(lowered, "https://") || strings.HasPrefix(lowered, "http://") {
			return candidate
		}
	}
	return ""
}

// MailUnsubscribe is the address to write to, or the empty string.
func (self ListInfo) MailUnsubscribe() string {
	for _, candidate := range self.Unsubscribe {
		if strings.HasPrefix(strings.ToLower(candidate), "mailto:") {
			return candidate
		}
	}
	return ""
}

// listIdentity reads "Example Weekly <weekly.news.example.com>" as the
// identifier and the description. The identifier is what is inside the
// brackets; a sender that writes only that is common enough to allow.
func listIdentity(value string) (key, name string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ""
	}
	open := strings.Index(value, "<")
	close := strings.LastIndex(value, ">")
	if open >= 0 && close > open {
		key = strings.TrimSpace(value[open+1 : close])
		name = strings.TrimSpace(strings.Trim(strings.TrimSpace(value[:open]), `"`))
	} else {
		key = value
	}
	key = strings.ToLower(strings.TrimSpace(key))
	if name == "" {
		name = key
	}
	return key, DecodeHeaderValue(name)
}

// unsubscribeURLs reads the angle-bracketed list. Anything that is not a way
// to leave — a javascript: URL, a bare word — is dropped rather than stored
// for something later to act on.
func unsubscribeURLs(value string) []string {
	urls := make([]string, 0, 2)
	for index := 0; index < len(value); {
		open := strings.Index(value[index:], "<")
		if open < 0 {
			break
		}
		open += index
		close := strings.Index(value[open:], ">")
		if close < 0 {
			break
		}
		close += open
		candidate := strings.TrimSpace(value[open+1 : close])
		lowered := strings.ToLower(candidate)
		if strings.HasPrefix(lowered, "https://") || strings.HasPrefix(lowered, "http://") ||
			strings.HasPrefix(lowered, "mailto:") {
			urls = append(urls, candidate)
		}
		index = close + 1
	}
	return urls
}

// hasOneClick reads the undertaking, which is written with spaces in some
// places and not others.
func hasOneClick(value string) bool {
	compact := strings.ToLower(strings.Join(strings.Fields(value), ""))
	return strings.Contains(compact, "list-unsubscribe=one-click")
}

// addressAndName splits a From header. A header this cannot parse still has
// an address in it often enough to be worth taking as it stands.
func addressAndName(from string) (address, name string) {
	from = strings.TrimSpace(DecodeHeaderValue(unfold(from)))
	if from == "" {
		return "", ""
	}
	if parsed, err := mail.ParseAddress(from); err == nil {
		return strings.TrimSpace(parsed.Address), strings.TrimSpace(parsed.Name)
	}
	if open := strings.LastIndex(from, "<"); open >= 0 {
		if close := strings.Index(from[open:], ">"); close > 0 {
			return strings.TrimSpace(from[open+1 : open+close]), strings.TrimSpace(strings.Trim(from[:open], `" `))
		}
	}
	return from, ""
}

// unfold puts a header that was written over several lines back onto one.
func unfold(value string) string {
	if !strings.ContainsAny(value, "\r\n") {
		return strings.TrimSpace(value)
	}
	lines := strings.FieldsFunc(value, func(letter rune) bool { return letter == '\r' || letter == '\n' })
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, " ")
}
