package photo

import (
	"crypto/hmac"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/util/security"
)

// A signed playback address: a way to fetch one preview or one file of one photo
// without signing in, for a player, which fetches by itself and cannot send
// the person's token. The signature covers the owner, the reader it was made
// for, the photo, which preview or file, and when it expires, so it opens
// exactly that and nothing else; it is made with the server's secret, as an
// attachment's is. It is not the whole credential: the route asks again
// whether the reader may still open the photo, so an address does not outlive
// the share that let them in.

// PlaybackFor is how long a signed playback address stays good: time enough to watch
// a video through, short because it is made afresh each time it is needed.
const PlaybackFor = 6 * time.Hour

// Which kind of thing a playback address names, so that a signature for a
// preview named "original" could never open a file of that name.
const (
	PlaybackKindPreview = "preview"
	PlaybackKindFile    = "file"
)

func playbackSubject(ownerId, readerId, photoId, playbackKind, name, expires string) string {
	return strings.Join([]string{"photo-playback", ownerId, readerId, photoId, playbackKind, name, expires}, "\n")
}

// SignPlayback is the query value that opens a photo's preview or file, which
// the owner owns, to the reader, until the time given, or "" when there is no
// secret to sign with. The reader is the owner for a photo of their own.
func SignPlayback(secret []byte, ownerId, readerId, photoId, playbackKind, name string, until time.Time) string {
	if len(secret) == 0 {
		return ""
	}
	expires := strconv.FormatInt(until.Unix(), 10)
	return expires + "." + hex.EncodeToString(security.SignString(playbackSubject(ownerId, readerId, photoId, playbackKind, name, expires), secret))
}

// VerifyPlayback says whether the query value opens the named preview or file
// now.
func VerifyPlayback(secret []byte, signature, ownerId, readerId, photoId, playbackKind, name string, now time.Time) bool {
	expires, signature, isSplit := strings.Cut(signature, ".")
	if !isSplit || len(secret) == 0 {
		return false
	}
	until, err := strconv.ParseInt(expires, 10, 64)
	if err != nil || now.Unix() > until {
		return false
	}
	given, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	return hmac.Equal(given, security.SignString(playbackSubject(ownerId, readerId, photoId, playbackKind, name, expires), secret))
}
