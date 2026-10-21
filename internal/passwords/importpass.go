package passwords

import (
	"errors"
	"regexp"
	"strings"
)

// ImportedItem is an item read from another password manager's export, ready
// to be encrypted and saved. Exactly one of Login and Note is set, the one
// ItemKind names.
type ImportedItem struct {
	ItemKind ItemKind
	Login    *LoginFields
	Note     *NoteFields
}

// ErrEmptyEntry is returned for a pass entry with nothing in it.
var ErrEmptyEntry = errors.New("passwords: the entry is empty")

var (
	// domainPattern is a host name with at least one dot and a letters-only
	// last label, such as example.com.
	domainPattern = regexp.MustCompile(`(?i)^([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,}$`)
	// emailPattern is deliberately loose: something@something.something.
	emailPattern = regexp.MustCompile(`^[^@\s/]+@[^@\s/]+\.[^@\s/]+$`)
)

// ParsePassEntry reads one decrypted entry of a pass password store. The path
// is the entry's path inside the store, without the .gpg ending, such as
// web/example.com/ada@example.com. The text is what gpg printed.
//
// The first line is the password. After it, lines starting login:, user:,
// username: or email: (any case) set the user name, url: and website: lines
// add sites, an otpauth:// line sets the one-time code, and every other line
// is kept as a note. An entry whose first line is blank, or is one of those
// lines, has no password line: with any of those lines it is a login with an
// empty password, and with none of them it is a note holding the whole text.
//
// The path's directories become the folder and its last element the name. When
// that last element looks like a user name or an email and the directory
// above it looks like a domain, as in web/example.com/ada@example.com, the
// domain becomes the name and the site (https://example.com) and the last
// element the user name. A name that is itself a domain also becomes a site.
func ParsePassEntry(path, text string) (*ImportedItem, error) {
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".gpg")
	var elements []string
	for _, element := range strings.Split(path, "/") {
		if element != "" {
			elements = append(elements, element)
		}
	}
	if len(elements) == 0 {
		return nil, errors.New("passwords: the entry has no path")
	}
	name := elements[len(elements)-1]
	directories := elements[:len(elements)-1]

	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimRight(text, "\n")
	if strings.TrimSpace(text) == "" {
		return nil, ErrEmptyEntry
	}
	lines := strings.Split(text, "\n")

	login := &LoginFields{}
	hasPasswordLine := strings.TrimSpace(lines[0]) != "" && !isFieldLine(lines[0])
	remaining := lines
	if hasPasswordLine {
		login.Password = lines[0]
		remaining = lines[1:]
	}

	var noteLines []string
	hasField := false
	for _, line := range remaining {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(trimmed), "otpauth://") && login.OneTimeCodeURI == "" {
			if _, err := ParseOneTimeCodeURI(trimmed); err == nil {
				login.OneTimeCodeURI = trimmed
				hasField = true
				continue
			}
		}
		key, fieldValue, isField := splitFieldLine(line)
		switch {
		case isField && isUserNameKey(key) && login.UserName == "" && fieldValue != "":
			login.UserName = fieldValue
			hasField = true
		case isField && isSiteKey(key) && fieldValue != "":
			login.Sites = appendSite(login.Sites, fieldValue)
			hasField = true
		default:
			noteLines = append(noteLines, line)
		}
	}

	if !hasPasswordLine && !hasField {
		return &ImportedItem{
			ItemKind: ItemKindNote,
			Note:     &NoteFields{Name: name, Text: text, Folder: strings.Join(directories, "/")},
		}, nil
	}

	login.Notes = strings.Trim(strings.Join(noteLines, "\n"), "\n")

	if len(directories) > 0 && isUserNameOrEmail(name) && domainPattern.MatchString(directories[len(directories)-1]) {
		// web/example.com/ada@example.com
		domain := directories[len(directories)-1]
		if login.UserName == "" {
			login.UserName = name
		}
		login.Sites = appendSite(login.Sites, domain)
		name = domain
		directories = directories[:len(directories)-1]
	} else if domainPattern.MatchString(name) {
		// social/example.org
		login.Sites = appendSite(login.Sites, name)
	}
	login.Name = name
	login.Folder = strings.Join(directories, "/")
	return &ImportedItem{ItemKind: ItemKindLogin, Login: login}, nil
}

// splitFieldLine splits "key: value" at the first colon. A line whose key has
// a space or a slash in it is prose or a web address, not a field.
func splitFieldLine(line string) (key, fieldValue string, isField bool) {
	key, fieldValue, isField = strings.Cut(line, ":")
	if !isField {
		return "", "", false
	}
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" || strings.ContainsAny(key, " \t/") {
		return "", "", false
	}
	return key, strings.TrimSpace(fieldValue), true
}

func isUserNameKey(key string) bool {
	switch key {
	case "login", "user", "username", "email":
		return true
	}
	return false
}

func isSiteKey(key string) bool {
	return key == "url" || key == "website"
}

// isFieldLine reports whether a line is one the importer reads as a field, so
// a first line like that is not taken for the password.
func isFieldLine(line string) bool {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "otpauth://") {
		return true
	}
	key, _, isField := splitFieldLine(line)
	return isField && (isUserNameKey(key) || isSiteKey(key))
}

// isUserNameOrEmail reports whether a path element reads as the user name of
// an account: an email, or a single word that is not itself a domain.
func isUserNameOrEmail(element string) bool {
	if emailPattern.MatchString(element) {
		return true
	}
	return !strings.ContainsAny(element, " \t@") && !domainPattern.MatchString(element)
}

// appendSite adds a site once. An address without a scheme is taken to be
// https.
func appendSite(sites []string, site string) []string {
	site = strings.TrimSpace(site)
	if !strings.Contains(site, "://") {
		site = "https://" + site
	}
	for _, existing := range sites {
		if existing == site {
			return sites
		}
	}
	return append(sites, site)
}
