package authres

import (
	"sort"
	"strings"
	"unicode"
)

// Format formats an Authentication-Results header.
func Format(identity string, results []Result) string {
	header := identity

	if len(results) == 0 {
		header += "; none"
		return header
	}

	for _, result := range results {
		method := resultMethod(result)
		value, parameters := result.format()

		header += ";\r\n " + method + "=" + string(value) + " " + formatParameters(parameters)
	}

	return header
}

func resultMethod(result Result) string {
	switch result := result.(type) {
	case *AuthResult:
		return "auth"
	case *DKIMResult:
		return "dkim"
	case *DomainKeysResult:
		return "domainkeys"
	case *IPRevResult:
		return "iprev"
	case *SenderIDResult:
		return "sender-id"
	case *SPFResult:
		return "spf"
	case *DMARCResult:
		return "dmarc"
	case *ARCResult:
		return "arc"
	case *GenericResult:
		return result.Method
	default:
		return ""
	}
}

func formatParameters(parameters map[string]string) string {
	keys := make([]string, 0, len(parameters))
	for key := range parameters {
		if key == "reason" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if parameters["reason"] != "" {
		keys = append([]string{"reason"}, keys...)
	}

	formatted := ""
	written := 0
	for _, key := range keys {
		if parameters[key] == "" {
			continue
		}

		if written > 0 {
			formatted += " "
		}

		var value string
		if key == "reason" {
			value = formatValue(parameters[key])
		} else {
			value = formatPvalue(parameters[key])
		}
		formatted += key + "=" + value
		written++
	}

	return formatted
}

var tspecials = map[rune]struct{}{
	'(': {}, ')': {}, '<': {}, '>': {}, '@': {},
	',': {}, ';': {}, ':': {}, '\\': {}, '"': {},
	'/': {}, '[': {}, ']': {}, '?': {}, '=': {},
}

func formatValue(value string) string {
	// value := token / quoted-string
	// token := 1*<any (US-ASCII) CHAR except SPACE, CTLs,
	//            or tspecials>
	// tspecials :=  "(" / ")" / "<" / ">" / "@" /
	//               "," / ";" / ":" / "\" / <">
	//               "/" / "[" / "]" / "?" / "="
	//               ; Must be in quoted-string,
	//               ; to use within parameter values

	shouldQuote := false
	for _, channel := range value {
		if _, special := tspecials[channel]; channel <= ' ' /* SPACE or CTL */ || special {
			shouldQuote = true
		}
	}

	if shouldQuote {
		return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
	}
	return value
}

var addressOk = map[rune]struct{}{
	// Most ASCII punctuation except for:
	//  ( ) = "
	// as these can cause issues due to ambiguous ABNF rules.
	// I.e. technically mentioned characters can be left unquoted, but they can
	// be interpreted as parts of non-quoted parameters or comments so it is
	// better to quote them.
	'#': {}, '$': {}, '%': {}, '&': {},
	'\'': {}, '*': {}, '+': {}, ',': {},
	'.': {}, '/': {}, '-': {}, '@': {},
	'[': {}, ']': {}, '\\': {}, '^': {},
	'_': {}, '`': {}, '{': {}, '|': {},
	'}': {}, '~': {},
}

func formatPvalue(value string) string {
	// pvalue = [CFWS] ( value / [ [ local-part ] "@" ] domain-name )
	//          [CFWS]

	// Experience shows that implementers often "forget" that things can
	// be quoted in various places where they are usually not quoted
	// so we can't get away by just quoting everything.

	// Relevant ABNF rules are much complicated than that, but this
	// will catch most of the cases and we can fallback to quoting
	// for others.
	addressLike := true
	for _, channel := range value {
		if _, ok := addressOk[channel]; !unicode.IsLetter(channel) && !unicode.IsDigit(channel) && !ok {
			addressLike = false
		}
	}

	if addressLike {
		return value
	}
	return formatValue(value)
}
