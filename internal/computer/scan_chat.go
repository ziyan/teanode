package computer

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
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

// chatBotShare is the share of a channel's posts that being written by
// an integration makes it a bot channel: searchable on request, never
// embedded. A build server posting every commit is not knowledge.
//
// The bounds of one unit are in chat_units.go, with the code that cuts
// them.
const chatBotShare = 0.8

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

// scanMattermost reads an export a page at a time.
//
// A channel file at a time where it fits, because a unit needs the posts
// around it and a channel file is the natural boundary; but a page is
// bounded within a file too. A support channel with years of posts came
// back as one answer of tens of megabytes, the server refused it and
// closed the socket, every scan open on that computer failed with it,
// and the next pass tried the same file again. The cursor is the
// channel's path, or the path and the last unit sent -- "posts/x.jsonl"
// or "posts/x.jsonl#p123" -- so a pass goes on from the next channel or
// from the next unit of the same one.
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

	afterFile, afterUnit := arguments.After, ""
	if cut := strings.Index(arguments.After, "#"); cut >= 0 {
		afterFile, afterUnit = arguments.After[:cut], arguments.After
	}
	started := arguments.After == ""
	// One channel file becomes many threads, so a page here fills by
	// bytes long before it fills by count.
	carried := 0
	for index, relative := range files {
		if !started {
			if relative != afterFile {
				continue
			}
			started = true
			// Stopped at the end of this file last time: on to the next.
			if afterUnit == "" {
				continue
			}
		}
		if len(result.Entries) >= most || carried >= scanPageBytes {
			// The last channel file sent in full, which the next page
			// begins after; see scanFiles.
			result.Next = files[index-1]
			break
		}
		entries, err := channelEntries(root, relative, users, channels, arguments)
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
		skipping := afterUnit != "" && relative == afterFile
		afterUnit = ""
		for _, entry := range entries {
			if skipping {
				if entry.ExternalID == afterUnit || entry.ExternalID == arguments.After {
					skipping = false
				}
				continue
			}
			if len(result.Entries) >= most || carried >= scanPageBytes {
				// Mid-file: the cursor names the last unit sent, and the
				// next page starts with the one after it.
				result.Next = result.Entries[len(result.Entries)-1].ExternalID
				return result, nil
			}
			// What the server already holds is named and not sent again.
			if arguments.Known[entry.ExternalID] == entry.Hash {
				entry.Unchanged = true
				entry.Text = ""
			}
			carried += len(entry.Text)
			result.Entries = append(result.Entries, entry)
		}
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

	// The cutting is the same for every chat, so it lives in
	// chat_units.go and this reader only says what the export means: who
	// a user id is, and that a post with replies is a thread's root.
	group := make([]chatPost, 0, len(posts))
	for _, post := range posts {
		who := users[post.UserID].Username
		if who == "" {
			who = "somebody"
		}
		group = append(group, chatPost{
			ID: post.ID, Thread: post.RootID, Replied: millis(post.ReplyCount) > 0,
			At: whenOf(post), Author: who, Text: post.Message,
		})
	}
	entries := chatUnits(relative, channelName, group, private)
	for index := range entries {
		// The team and what the channel is for are Mattermost's own, and
		// no other chat has them to give.
		entries[index].Metadata["team"] = team
		entries[index].Metadata["purpose"] = channel.Purpose
	}
	return entries, nil
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

// channelEntries is readChannelFile through a cache of one file: the
// last channel read, in the fixed order the pages walk it.
//
// A page mid-file used to read and cut the whole file again -- twenty
// seconds a page for a monitor channel of a hundred megabytes, and a run
// of pages over it ran past its deadline. The units are the same on
// every page until the file changes, so they are kept, and the pages of
// a file after the first cost nothing.
func channelEntries(root, relative string, users map[string]mattermostUser, channels map[string]mattermostChannel, arguments *ScanArguments) ([]ScanEntry, error) {
	path := filepath.Join(root, relative)
	information, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	channelCache.mutex.Lock()
	defer channelCache.mutex.Unlock()
	if channelCache.path == path && channelCache.modified.Equal(information.ModTime()) && channelCache.size == information.Size() {
		return channelCache.entries, nil
	}
	entries, err := readChannelFile(root, relative, users, channels, arguments)
	if err != nil {
		return nil, err
	}
	// In a fixed order, so that "the units after this one" means the same
	// thing on the next request as it did on this one.
	sort.SliceStable(entries, func(left, right int) bool {
		if entries[left].HappenedAt != nil && entries[right].HappenedAt != nil && !entries[left].HappenedAt.Equal(*entries[right].HappenedAt) {
			return entries[left].HappenedAt.Before(*entries[right].HappenedAt)
		}
		return entries[left].ExternalID < entries[right].ExternalID
	})
	channelCache.path, channelCache.modified, channelCache.size, channelCache.entries = path, information.ModTime(), information.Size(), entries
	return entries, nil
}

var channelCache struct {
	mutex    sync.Mutex
	path     string
	modified time.Time
	size     int64
	entries  []ScanEntry
}
