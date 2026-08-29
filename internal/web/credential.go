package web

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/ziyan/teanode/internal/util/security"
)

// The two kinds of stored credential, and the prefix each carries.
//
// The prefix is outside the signature and is stripped before verifying. It is
// there so that a value pasted into a chat window or committed to a repository
// can be recognised for what it is — by a person, and by a secret scanner.
const (
	kindSession   = "session"
	kindToken     = "token"
	SessionPrefix = "tns_"
	TokenPrefix   = "tnt_"
)

// issue mints a credential: an identifier, the string to hand out, and the
// hash to store.
//
// The string is id, secret and a signature over both, which is what
// security.EncodeToken builds. Two things follow from that shape. A value that
// was not minted here fails the signature check without a database query, so
// somebody guessing at cookies does not get a row lookup per guess. And the
// kind is signed but not written down, so a session cookie cannot be presented
// as an API token however it is rearranged.
//
// Only the hash of the secret is stored. A copy of the database is then not a
// set of working sessions — which is the difference between somebody reading a
// backup and somebody being logged in as you.
func issue(kind, prefix string, secret []byte) (id, value, keyHash string) {
	id = security.NewULID()
	key := security.GenerateRandomString(16, security.LowerAlphaNumeric)
	return id, prefix + security.EncodeToken(kind, id, key, secret), hashKey(key)
}

// parse takes a credential apart and checks its signature, returning the
// identifier and the secret half.
func parse(kind, prefix, value string, secret []byte) (id, key string, ok bool) {
	rest, found := strings.CutPrefix(value, prefix)
	if !found {
		return "", "", false
	}
	id, key, err := security.DecodeToken(kind, rest, secret)
	if err != nil {
		return "", "", false
	}
	return id, key, true
}

// matches compares a presented secret with a stored hash, in constant time so
// that a wrong secret cannot be refined byte by byte.
func matches(keyHash, key string) bool {
	expected, err := hex.DecodeString(keyHash)
	if err != nil {
		return false
	}
	actual := sha256.Sum256([]byte(key))
	return subtle.ConstantTimeCompare(expected, actual[:]) == 1
}

func hashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// requestOrigin describes where a request came from, for the list a person
// reads when deciding whether a session is theirs.
func requestOrigin(request *http.Request) (ip, userAgent string) {
	if request == nil {
		return "", ""
	}
	agent := request.UserAgent()
	if len(agent) > maxUserAgent {
		agent = agent[:maxUserAgent]
	}
	return remoteAddress(request), agent
}

// maxUserAgent bounds what is stored. The column is text, but a header is
// whatever the client sent, and there is no reason to keep a kilobyte of it.
const maxUserAgent = 512
