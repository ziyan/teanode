package config

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// LocalTokenPrefix begins a locally minted token. It exists so that a token
// pasted into a chat window or committed to a repository can be recognised
// for what it is by a secret scanner. See MintLocalToken.
const LocalTokenPrefix = "tnl_"

// LocalUsername is the operator a locally minted token acts as. It is not a
// configurable account, and the parentheses keep it clearly apart from one in
// the log.
const LocalUsername = "(local)"

// MintLocalToken issues a short lived token signed with the server secret,
// without storing anything.
//
// It is how the command line client authenticates when it is run on the server
// itself. That is not a privilege escalation: minting one requires reading the
// configuration file, and whoever can do that can already read the session
// key, the signing keys and every password hash, or simply edit the file. What
// it buys is that the client goes through the running server's API rather than
// writing the file underneath it, so there is one writer instead of two.
func (self *Configuration) MintLocalToken(lifetime time.Duration) (string, error) {
	secret := self.Secret()
	if len(secret) == 0 {
		return "", fmt.Errorf("config: no server secret, so a local token cannot be signed")
	}
	if lifetime <= 0 {
		lifetime = time.Minute
	}
	expiry := strconv.FormatInt(time.Now().Add(lifetime).Unix(), 10)
	signature := signLocalToken(secret, expiry)
	return LocalTokenPrefix + expiry + "_" + base64.RawURLEncoding.EncodeToString(signature), nil
}

// VerifyLocalToken reports whether a locally minted token is valid now.
func (self *Configuration) VerifyLocalToken(value string) bool {
	rest, ok := strings.CutPrefix(value, LocalTokenPrefix)
	if !ok {
		return false
	}
	expiry, encoded, ok := strings.Cut(rest, "_")
	if !ok {
		return false
	}
	signature, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return false
	}
	secret := self.Secret()
	if len(secret) == 0 {
		return false
	}
	if subtle.ConstantTimeCompare(signature, signLocalToken(secret, expiry)) != 1 {
		return false
	}
	seconds, err := strconv.ParseInt(expiry, 10, 64)
	if err != nil {
		return false
	}
	return time.Now().Before(time.Unix(seconds, 0))
}

// signLocalToken domain separates the local token signature from the other
// things the server secret signs, so that a bounce return path can never be
// replayed as a token or the other way around.
func signLocalToken(secret []byte, expiry string) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("teanode-local-token|" + expiry))
	return mac.Sum(nil)
}
