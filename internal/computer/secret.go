package computer

import (
	"math"
	"path/filepath"
	"regexp"
	"strings"
)

// The secret filter, which runs here and not on the server.
//
// A scan reads a person's whole checkout. One of the maintainer's has two
// thousand eight hundred tracked `.pem` files in it. Whatever else is
// true of the server, it should not be possible for a mistake there to
// pull a private key across the socket -- so the refusal happens on the
// machine the files are on, before anything is sent, and what was refused
// is reported as a count and a name rather than as content.
//
// It is deliberately over-eager. A false positive costs one file out of
// a hundred thousand and is listed so the person can see it; a false
// negative is a key in somebody's embedding provider.

// secretNames are file names that are a secret whatever is in them.
var secretNames = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\.(pem|key|p12|pfx|jks|keystore|kdbx|ppk|asc|gpg)$`),
	regexp.MustCompile(`(?i)(^|/)id_(rsa|dsa|ecdsa|ed25519)(\.pub)?$`),
	regexp.MustCompile(`(?i)(^|/)\.env(\.|$)`),
	regexp.MustCompile(`(?i)(^|/)(credentials|secrets?|passwords?)(\.|$)`),
	regexp.MustCompile(`(?i)(^|/)\.(netrc|pgpass|htpasswd)$`),
	regexp.MustCompile(`(?i)(^|/)(service.?account|serviceaccount).*\.json$`),
	regexp.MustCompile(`(?i)(^|/)\.aws/`),
	regexp.MustCompile(`(?i)(^|/)\.ssh/`),
	regexp.MustCompile(`(?i)(^|/)\.gnupg/`),
}

// secretContent is what a file may not carry, whatever it is called.
var secretContent = []*regexp.Regexp{
	// Covers RSA, EC, DSA, ENCRYPTED and OPENSSH headers alike.
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bASIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`),
	regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}\b`),
	regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}\b`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9]{32,}\b`),
	regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}\b`),
	regexp.MustCompile(`(?i)\b(api[_-]?key|secret|password|passwd|token)\b\s*[:=]\s*["'][^"']{16,}["']`),
}

// assignment is a value written beside a word that says it is a secret,
// without quotes around it: "password: hunter2", which is how somebody
// writes one in a note to themselves and how a .env file is shaped.
//
// The quoted form above does not catch it, and a note to oneself is
// exactly where a password gets written down.
var assignment = regexp.MustCompile(`(?i)\b(api[_-]?keys?|secrets?|passwords?|passwd|tokens?|credentials?|passphrases?)\b\s*[:=]\s*(\S{10,})`)

// reference is a value that names where the secret is rather than being
// it: an environment variable, a template, a field of a settings object.
// Refusing these would refuse half the configuration files in a checkout.
var reference = regexp.MustCompile(`^(\$|\{\{|\$\{|<|%|["']?(process\.env|os\.environ|env|config|settings|options|self|this)\.)`)

// assignedSecret finds a secret written beside a word that says what it
// is, and answers with the word rather than the value.
func assignedSecret(text string) (string, bool) {
	for _, match := range assignment.FindAllStringSubmatch(text, 32) {
		if len(match) < 3 {
			continue
		}
		value := strings.Trim(match[2], `"'`+"`,;")
		if len(value) < 10 || reference.MatchString(value) {
			continue
		}
		// A type annotation ("password: string") or a placeholder is not
		// a secret; something with no spaces and some variety in it is.
		if isPlaceholder(value) {
			continue
		}
		return strings.ToLower(match[1]), true
	}
	return "", false
}

// isPlaceholder says whether a value is standing in for a secret rather
// than being one: all one character, a word of plain letters, or one of
// the words people write when they mean "fill this in".
func isPlaceholder(value string) bool {
	lowered := strings.ToLower(value)
	for _, word := range []string{"string", "changeme", "example", "redacted", "hidden", "none", "null", "true", "false", "xxxxx", "todo", "placeholder"} {
		if strings.Contains(lowered, word) {
			return true
		}
	}
	// A plain lowercase word with no digits and no punctuation is prose,
	// not a key.
	plain := true
	for _, character := range value {
		if character < 'a' || character > 'z' {
			plain = false
			break
		}
	}
	return plain
}

// sensitiveDirectories are names that suggest what is inside is about
// *other people* rather than about the person whose agent this is.
//
// The distinction matters and it is easy to get backwards. This is a
// personal agent: its whole purpose is to know the person's own life, so
// their tax returns, their contracts, their medical letters, their bank
// statements and their will are exactly what it should read. Holding
// those back would make it useless for the questions people most want
// answered -- what did I pay last year, when does the lease end, what did
// the consultant say.
//
// What is held back is the other thing a person's disk holds: records
// they keep in a work capacity about named colleagues and candidates. An
// evaluation of somebody else is that person's business, they did not
// consent to it being embedded, and it is the one category where the
// person reading this file would not want the default to be yes.
//
// Nothing here is a refusal, only a pause: the directory is named on the
// source's page with a switch beside it.
var sensitiveDirectories = []*regexp.Regexp{
	// Judgments of named people.
	regexp.MustCompile(`(?i)^(perf|performance|peer|self)?[-_]?(eval|evals|evaluation|evaluations|review|reviews|appraisal|appraisals|feedback)s?$`),
	// People who applied for something.
	regexp.MustCompile(`(?i)^(recruit|recruiting|recruitment|candidates?|applicants?|interviews?|hiring|resumes?|cvs?)$`),
	// What other people are paid, and the files that say so.
	regexp.MustCompile(`(?i)^(salary|salaries|compensation|payroll|headcount)$`),
	// The department whose filing cabinet is other people's records.
	regexp.MustCompile(`(?i)^(hr|people[-_]?ops|personnel|employee[-_]?records?)$`),
	// Discipline and complaints, which are about somebody by name.
	regexp.MustCompile(`(?i)^(disciplinary|grievances?|investigations?|incidents?[-_]?hr)$`),
}

// SecretName says whether a path is a secret by its name alone.
func SecretName(path string) bool {
	cleaned := filepath.ToSlash(path)
	for _, pattern := range secretNames {
		if pattern.MatchString(cleaned) {
			return true
		}
	}
	return false
}

// SecretContent says whether text carries something that must not leave
// the machine, and what kind it was.
func SecretContent(text string) (bool, string) {
	for _, pattern := range secretContent {
		if pattern.MatchString(text) {
			return true, "a credential"
		}
	}
	if word, found := assignedSecret(text); found {
		return true, "a " + word + " written beside the word that names it"
	}
	if line, found := highEntropyLine(text); found {
		return true, "a high-entropy value (" + line + ")"
	}
	return false, ""
}

// SensitiveDirectory says whether a directory's name suggests it is about
// named people.
func SensitiveDirectory(name string) bool {
	for _, pattern := range sensitiveDirectories {
		if pattern.MatchString(name) {
			return true
		}
	}
	return false
}

// highEntropyRun is the shortest run of credential-looking characters
// worth measuring, and highEntropyBits the bits per character above which
// a run of them is more likely a key than a word.
//
// Four and a half bits is above English prose (around four) and below
// base64 of random bytes (six). Hexadecimal sits at four, which is why a
// hash does not trip this and a base64 key does.
const (
	highEntropyRun  = 40
	highEntropyBits = 4.5
)

var entropyCandidate = regexp.MustCompile(`[A-Za-z0-9+/=_-]{40,}`)

// highEntropyLine finds a run of characters that looks like a key rather
// than like words, and says what it found -- the first few characters
// only, because the point is to name it, not to carry it.
func highEntropyLine(text string) (string, bool) {
	for _, candidate := range entropyCandidate.FindAllString(text, 32) {
		if len(candidate) < highEntropyRun {
			continue
		}
		// A run of one repeated character, or of digits, is not a key
		// however long it is: a line of dashes, a serial number, the
		// output of a test fixture.
		if shannonBits(candidate) < highEntropyBits {
			continue
		}
		if looksLikeAWord(candidate) {
			continue
		}
		shown := candidate
		if len(shown) > 8 {
			shown = shown[:8] + "…"
		}
		return shown, true
	}
	return "", false
}

// shannonBits is the entropy of a string in bits per character.
func shannonBits(value string) float64 {
	counts := map[rune]int{}
	for _, character := range value {
		counts[character]++
	}
	length := float64(len([]rune(value)))
	if length == 0 {
		return 0
	}
	var bits float64
	for _, count := range counts {
		probability := float64(count) / length
		bits -= probability * math.Log2(probability)
	}
	return bits
}

// looksLikeAWord says whether a long run is prose rather than a key:
// mostly letters, with vowels where words have them. A Go identifier
// eighty characters long is not a secret.
func looksLikeAWord(value string) bool {
	letters, vowels := 0, 0
	for _, character := range strings.ToLower(value) {
		if character >= 'a' && character <= 'z' {
			letters++
			if strings.ContainsRune("aeiou", character) {
				vowels++
			}
		}
	}
	if letters == 0 {
		return false
	}
	// Prose is almost all letters and about two fifths vowels; a key is
	// neither.
	return float64(letters)/float64(len(value)) > 0.85 && float64(vowels)/float64(letters) > 0.25
}
