package computer

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"
)

// Cutting a conversation into units, for whoever read it.
//
// This was written against a Mattermost export and is the part of it
// that is not about Mattermost: a chat is posts, and a post is not worth
// a document of its own. The reader that knows the shape on disk hands
// its posts over as chatPost and gets back the units, so a Slack export
// written as records and a Mattermost export read from its own files
// produce the same kind of document rather than two that only look
// alike.

// The bounds of one unit.
const (
	// chatGap is how long a silence ends a window. Somebody answering
	// twenty minutes later is still in the same exchange; an hour later
	// is a new one.
	chatGap = 30 * time.Minute

	// chatWindowPosts and chatWindowCharacters bound a window, so that a
	// busy channel does not make one unit of a whole afternoon.
	chatWindowPosts      = 40
	chatWindowCharacters = 3000
)

// chatPost is one post of any chat, as the readers hand it to the
// grouping: the Mattermost reader from its export, the records reader
// from a record.
type chatPost struct {
	ID       string
	Thread   string // the root this replies to, or empty
	Replied  bool   // a root with replies elsewhere
	At       time.Time
	Author   string
	Text     string
	Metadata map[string]any
}

// chatUnits cuts a channel's posts into threads and windows and renders
// each as one entry. relative is the file the posts came from, channel
// its name; private marks every unit.
//
// The posts are expected in time order, which is how both readers hand
// them over: a window is consecutive posts, and consecutive means
// nothing in a file somebody shuffled.
func chatUnits(relative, channel string, posts []chatPost, private bool) []ScanEntry {
	var entries []ScanEntry
	add := func(id string, group []chatPost) {
		if len(group) == 0 {
			return
		}
		text, participants := renderChat(group)
		if strings.TrimSpace(text) == "" {
			return
		}
		if secret, _ := SecretContent(text); secret {
			return
		}
		sum := sha256.Sum256([]byte(text))
		// What the first post carried is the unit's, so a script can say
		// which space or which export a conversation came from; the
		// names the digest reads are set after it and win.
		metadata := map[string]any{}
		for name, value := range group[0].Metadata {
			metadata[name] = value
		}
		metadata["channel"] = channel
		metadata["participants"] = participants
		metadata["posts"] = len(group)
		entries = append(entries, ScanEntry{
			ExternalID: relative + "#" + id, Kind: "chat",
			Title: channel + " — " + group[0].At.Format("2 Jan 2006"),
			Hash:  hex.EncodeToString(sum[:]), Private: private,
			HappenedAt: pointerTo(group[0].At),
			Text:       text, Size: int64(len(text)),
			Metadata: metadata,
		})
	}

	// Threads first: a root and everything that answered it.
	replies := map[string][]chatPost{}
	var loose []chatPost
	roots := map[string]chatPost{}
	for _, post := range posts {
		if post.Thread != "" {
			replies[post.Thread] = append(replies[post.Thread], post)
			continue
		}
		if post.Replied {
			roots[post.ID] = post
			continue
		}
		loose = append(loose, post)
	}
	for id, group := range replies {
		thread := group
		if root, found := roots[id]; found {
			thread = append([]chatPost{root}, group...)
			delete(roots, id)
		}
		sort.SliceStable(thread, func(left, right int) bool {
			return thread[left].At.Before(thread[right].At)
		})
		add(id, thread)
	}
	// A root whose replies are not in this file is still a post.
	for id, root := range roots {
		add(id, []chatPost{root})
	}

	// Then windows of what is left: consecutive posts with no long gap.
	var window []chatPost
	characters := 0
	flush := func() {
		if len(window) > 0 {
			add(window[0].ID, window)
			window, characters = nil, 0
		}
	}
	var previous time.Time
	for _, post := range loose {
		if len(window) > 0 &&
			(post.At.Sub(previous) > chatGap ||
				len(window) >= chatWindowPosts ||
				characters+len(post.Text) > chatWindowCharacters) {
			flush()
		}
		window = append(window, post)
		characters += len(post.Text)
		previous = post.At
	}
	flush()

	sort.SliceStable(entries, func(left, right int) bool {
		if entries[left].HappenedAt == nil || entries[right].HappenedAt == nil {
			return false
		}
		return entries[left].HappenedAt.Before(*entries[right].HappenedAt)
	})
	return entries
}

// renderChat is a unit as it is read and embedded, and who was in it.
func renderChat(posts []chatPost) (string, []string) {
	var builder strings.Builder
	seen := map[string]bool{}
	var participants []string
	for _, post := range posts {
		who := post.Author
		if who == "" {
			who = "somebody"
		}
		if !seen[who] {
			seen[who] = true
			participants = append(participants, who)
		}
		builder.WriteString(post.At.Format("15:04") + " " + who + ": ")
		builder.WriteString(strings.TrimSpace(post.Text))
		builder.WriteByte('\n')
	}
	return strings.TrimSpace(builder.String()), participants
}
