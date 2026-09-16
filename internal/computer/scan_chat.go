package computer

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Reading a chat export.
//
// The unit of chat is not the post. An archive of a company's Mattermost
// holds two million of them, and most are "ok" and "thanks": embedding
// each would cost two million vectors that find nothing. So a channel is
// cut into units -- a thread where somebody replied, otherwise a window
// of consecutive posts with no long gap in it -- and a unit is one
// document. On the maintainer's archive that turns 1,963,356 posts into
// about 269,000 units, which is a dollar or two of embedding rather than
// a week of it.
//
// The shape read here is what the Mattermost API gives and what the
// person's own archiver writes beside it: channels.json, users.json,
// me.json, state.json, and posts/<team>/<channel>.jsonl.

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

	// chatBotShare is the share of a channel's posts that being written
	// by an integration makes it a bot channel: searchable on request,
	// never embedded. A build server posting every commit is not
	// knowledge.
	chatBotShare = 0.8
)

// mattermostPost is what one line of a channel's file holds. The archive
// writes numbers as strings in places, which is why the times are read
// leniently.
type mattermostPost struct {
	ID         string          `json:"id"`
	CreateAt   json.RawMessage `json:"create_at"`
	UserID     string          `json:"user_id"`
	ChannelID  string          `json:"channel_id"`
	RootID     string          `json:"root_id"`
	Message    string          `json:"message"`
	Type       string          `json:"type"`
	ReplyCount json.RawMessage `json:"reply_count"`
}

type mattermostUser struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Position  string `json:"position"`
}

type mattermostChannel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Purpose     string `json:"purpose"`
	Header      string `json:"header"`
	Type        string `json:"type"`
	TeamID      string `json:"team_id"`
}

// scanMattermost reads an export, one channel file per page.
//
// A channel at a time rather than a post at a time, because a unit needs
// the posts around it and a channel file is the natural boundary. The
// cursor is the channel's own path, so a pass that stops halfway through
// an archive goes on from the next channel.
func scanMattermost(root string, arguments *ScanArguments, most int) (*ScanResult, error) {
	result := &ScanResult{}
	users, err := readUsers(filepath.Join(root, "users.json"))
	if err != nil {
		return nil, fmt.Errorf("this does not look like a chat export: %w", err)
	}
	channels := readChannels(filepath.Join(root, "channels.json"))

	var files []string
	if err := filepath.WalkDir(filepath.Join(root, "posts"), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		relative, _ := filepath.Rel(root, path)
		files = append(files, filepath.ToSlash(relative))
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Strings(files)

	started := arguments.After == ""
	// One channel file becomes many threads, so a page here fills by
	// bytes long before it fills by count.
	carried := 0
	for _, relative := range files {
		if !started {
			if relative == arguments.After {
				started = true
			}
			continue
		}
		if len(result.Entries) >= most || carried >= scanPageBytes {
			result.Next = relative
			break
		}
		entries, err := readChannelFile(root, relative, users, channels, arguments)
		if err != nil {
			// A channel file this program cannot read is reported as one
			// entry saying so, rather than silently missing from the
			// archive the person thinks they indexed.
			result.Entries = append(result.Entries, ScanEntry{
				ExternalID: relative, Kind: "chat",
				Refused: "could not be read: " + err.Error(),
			})
			result.Refused++
			continue
		}
		for _, entry := range entries {
			carried += len(entry.Text)
		}
		result.Entries = append(result.Entries, entries...)
	}
	return result, nil
}

func readUsers(path string) (map[string]mattermostUser, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var list []mattermostUser
	if err := json.Unmarshal(content, &list); err != nil {
		return nil, err
	}
	byId := make(map[string]mattermostUser, len(list))
	for _, user := range list {
		byId[user.ID] = user
	}
	return byId, nil
}

func readChannels(path string) map[string]mattermostChannel {
	byId := map[string]mattermostChannel{}
	content, err := os.ReadFile(path)
	if err != nil {
		return byId
	}
	var list []mattermostChannel
	if err := json.Unmarshal(content, &list); err != nil {
		return byId
	}
	for _, channel := range list {
		byId[channel.ID] = channel
	}
	return byId
}

// readChannelFile cuts one channel into units.
func readChannelFile(root, relative string, users map[string]mattermostUser, channels map[string]mattermostChannel, arguments *ScanArguments) ([]ScanEntry, error) {
	file, err := os.Open(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	team, channelName := teamAndChannel(relative)
	var posts []mattermostPost
	bots, total := 0, 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1<<20), 8<<20)
	for scanner.Scan() {
		var post mattermostPost
		if err := json.Unmarshal(scanner.Bytes(), &post); err != nil {
			continue
		}
		total++
		// System posts say who joined a channel, which is not knowledge.
		if strings.HasPrefix(post.Type, "system_") {
			continue
		}
		// An integration's post -- a build result, an alert -- is not
		// somebody talking.
		if post.Type == "slack_attachment" {
			bots++
			continue
		}
		if strings.TrimSpace(post.Message) == "" {
			continue
		}
		posts = append(posts, post)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if total > 0 && float64(bots)/float64(total) > chatBotShare {
		// A channel that is mostly an integration talking to itself. The
		// entry says so and carries nothing, so the source's page can
		// show it and the person can decide.
		return []ScanEntry{{
			ExternalID: relative, Kind: "chat", Title: channelName,
			Refused:  "mostly written by an integration, so nothing here was read",
			Metadata: map[string]any{"team": team, "channel": channelName, "posts": total},
		}}, nil
	}
	sort.SliceStable(posts, func(left, right int) bool {
		return millis(posts[left].CreateAt) < millis(posts[right].CreateAt)
	})

	var channel mattermostChannel
	if len(posts) > 0 {
		channel = channels[posts[0].ChannelID]
	}
	private := channel.Type == "P" || channel.Type == "D" || channel.Type == "G"

	var entries []ScanEntry
	add := func(id string, group []mattermostPost) {
		if len(group) == 0 {
			return
		}
		text, participants := renderChat(group, users)
		if strings.TrimSpace(text) == "" {
			return
		}
		if secret, _ := SecretContent(text); secret {
			return
		}
		sum := sha256.Sum256([]byte(text))
		hash := hex.EncodeToString(sum[:])
		external := relative + "#" + id
		entry := ScanEntry{
			ExternalID: external, Kind: "chat",
			Title: channelName + " — " + whenOf(group[0]).Format("2 Jan 2006"),
			Hash:  hash, Private: private,
			HappenedAt: pointerTo(whenOf(group[0])),
			Text:       text, Size: int64(len(text)),
			Metadata: map[string]any{
				"team": team, "channel": channelName, "participants": participants,
				"posts": len(group), "purpose": channel.Purpose,
			},
		}
		if arguments.Known[external] == hash {
			entry.Unchanged = true
			entry.Text = ""
		}
		entries = append(entries, entry)
	}

	// Threads first: a root and everything that answered it.
	replies := map[string][]mattermostPost{}
	var loose []mattermostPost
	roots := map[string]mattermostPost{}
	for _, post := range posts {
		if post.RootID != "" {
			replies[post.RootID] = append(replies[post.RootID], post)
			continue
		}
		if millis(post.ReplyCount) > 0 {
			roots[post.ID] = post
			continue
		}
		loose = append(loose, post)
	}
	for id, group := range replies {
		thread := group
		if root, found := roots[id]; found {
			thread = append([]mattermostPost{root}, group...)
			delete(roots, id)
		}
		sort.SliceStable(thread, func(left, right int) bool {
			return millis(thread[left].CreateAt) < millis(thread[right].CreateAt)
		})
		add(id, thread)
	}
	// A root whose replies are not in this file is still a post.
	for id, root := range roots {
		add(id, []mattermostPost{root})
	}

	// Then windows of what is left: consecutive posts with no long gap.
	var window []mattermostPost
	characters := 0
	flush := func() {
		if len(window) > 0 {
			add(window[0].ID, window)
			window, characters = nil, 0
		}
	}
	var previous time.Time
	for _, post := range loose {
		when := whenOf(post)
		if len(window) > 0 &&
			(when.Sub(previous) > chatGap ||
				len(window) >= chatWindowPosts ||
				characters+len(post.Message) > chatWindowCharacters) {
			flush()
		}
		window = append(window, post)
		characters += len(post.Message)
		previous = when
	}
	flush()

	sort.SliceStable(entries, func(left, right int) bool {
		if entries[left].HappenedAt == nil || entries[right].HappenedAt == nil {
			return false
		}
		return entries[left].HappenedAt.Before(*entries[right].HappenedAt)
	})
	return entries, nil
}

// renderChat is a unit as it is read and embedded, and who was in it.
func renderChat(posts []mattermostPost, users map[string]mattermostUser) (string, []string) {
	var builder strings.Builder
	seen := map[string]bool{}
	var participants []string
	for _, post := range posts {
		user := users[post.UserID]
		who := user.Username
		if who == "" {
			who = "somebody"
		}
		if !seen[who] {
			seen[who] = true
			participants = append(participants, who)
		}
		builder.WriteString(whenOf(post).Format("15:04") + " " + who + ": ")
		builder.WriteString(strings.TrimSpace(post.Message))
		builder.WriteByte('\n')
	}
	return strings.TrimSpace(builder.String()), participants
}

// teamAndChannel reads the two names out of posts/<team>/<channel>.jsonl.
func teamAndChannel(relative string) (string, string) {
	parts := strings.Split(strings.TrimSuffix(relative, ".jsonl"), "/")
	if len(parts) >= 3 {
		return parts[len(parts)-2], parts[len(parts)-1]
	}
	if len(parts) == 2 {
		return "", parts[1]
	}
	return "", relative
}

// millis reads a number the archive may have written as a string.
func millis(raw json.RawMessage) int64 {
	trimmed := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if trimmed == "" || trimmed == "null" {
		return 0
	}
	value, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return 0
	}
	return value
}

func whenOf(post mattermostPost) time.Time {
	return time.UnixMilli(millis(post.CreateAt))
}

func pointerTo(when time.Time) *time.Time { return &when }
