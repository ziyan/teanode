package authres

import (
	"sort"
	"strings"
	"unicode"
)

// Format formats an Authentication-Results header.
func Format(identity string, results []Result) string {
	s := identity

	if len(results) == 0 {
		s += "; none"
		return s
	}

	for _, r := range results {
		method := resultMethod(r)
		value, parameters := r.format()

		s += ";\r\n " + method + "=" + string(value) + " " + formatParameters(parameters)
	}

	return s
}

func resultMethod(r Result) string {
	switch r := r.(type) {
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
		return r.Method
	default:
		return ""
	}
}

func formatParameters(parameters map[string]string) string {
	keys := make([]string, 0, len(parameters))
	for k := range parameters {
		if k == "reason" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if parameters["reason"] != "" {
		keys = append([]string{"reason"}, keys...)
	}

	s := ""
	i := 0
	for _, k := range keys {
		if parameters[k] == "" {
			continue
		}

		if i > 0 {
			s += " "
		}

		var value string
		if k == "reason" {
			value = formatValue(parameters[k])
		} else {
			value = formatPvalue(parameters[k])
		}
		s += k + "=" + value
		i++
	}

	return s
}

var tspecials = map[rune]struct{}{
	'(': {}, ')': {}, '<': {}, '>': {}, '@': {},
	',': {}, ';': {}, ':': {}, '\\': {}, '"': {},
	'/': {}, '[': {}, ']': {}, '?': {}, '=': {},
}

func formatValue(s string) string {
	// value := token / quoted-string
	// token := 1*<any (US-ASCII) CHAR except SPACE, CTLs,
	//            or tspecials>
	// tspecials :=  "(" / ")" / "<" / ">" / "@" /
	//               "," / ";" / ":" / "\" / <">
	//               "/" / "[" / "]" / "?" / "="
	//               ; Must be in quoted-string,
	//               ; to use within parameter values

	shouldQuote := false
	for _, channel := range s {
		if _, special := tspecials[channel]; channel <= ' ' /* SPACE or CTL */ || special {
			shouldQuote = true
		}
	}

	if shouldQuote {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
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

func formatPvalue(s string) string {
	// pvalue = [CFWS] ( value / [ [ local-part ] "@" ] domain-name )
	//          [CFWS]

	// Experience shows that implementers often "forget" that things can
	// be quoted in various places where they are usually not quoted
	// so we can't get away by just quoting everything.

	// Relevant ABNF rules are much complicated than that, but this
	// will catch most of the cases and we can fallback to quoting
	// for others.
	addressLike := true
	for _, channel := range s {
		if _, ok := addressOk[channel]; !unicode.IsLetter(channel) && !unicode.IsDigit(channel) && !ok {
			addressLike = false
		}
	}

	if addressLike {
		return s
	}
	return formatValue(s)
}
