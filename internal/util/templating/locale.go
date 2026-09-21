package templating

import (
	"regexp"
	"strings"
)

// localePattern is the shape of a language tag: a language of two or three
// letters, which is every language ISO 639 names, then subtags of one to
// eight letters or digits, dash separated. It is deliberately looser than
// BCP 47, which has a grammar for what each subtag may be; what matters here
// is that "zh-CN" and "pt-BR" are accepted and "English" or "zh CN" are not.
var localePattern = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{1,8})*$`)

// ValidLocale reports whether a string is shaped like a language tag.
func ValidLocale(locale string) bool {
	return localePattern.MatchString(locale)
}

// NormalizeLocale makes two spellings of the same tag compare equal: case
// is insignificant in a language tag, and an underscore is how POSIX writes
// the separator that the web writes as a dash.
func NormalizeLocale(locale string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(locale), "_", "-"))
}

func primaryLanguage(locale string) string {
	return strings.SplitN(locale, "-", 2)[0]
}

// MatchLocale picks, from the locales available, the one a request for a
// locale should be answered with: the same tag, else the bare language
// without a region, else anything in the same language. Reports false when
// nothing is close, which is when the caller falls back to its default.
//
// The candidate is returned as it was given, so the caller can find it
// again in whatever it was matching over.
func MatchLocale(requested string, available []string) (string, bool) {
	wanted := NormalizeLocale(requested)
	if wanted == "" {
		return "", false
	}
	for _, candidate := range available {
		if NormalizeLocale(candidate) == wanted {
			return candidate, true
		}
	}
	language := primaryLanguage(wanted)
	for _, candidate := range available {
		if NormalizeLocale(candidate) == language {
			return candidate, true
		}
	}
	for _, candidate := range available {
		if primaryLanguage(NormalizeLocale(candidate)) == language {
			return candidate, true
		}
	}
	return "", false
}
