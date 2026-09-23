package computer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// A video is not a picture, so nothing downstream can read one: the night
// looks at the kind, sees video/mp4 and passes it over. What it can read
// is a picture, so a typed source's video is named again as one -- nine
// frames spread across the clip, tiled into a single sheet -- beside the
// video, as an ordinary attachment, and the video itself carries what it
// says about itself in words: how long, how large, which codec. From there
// nothing new is needed: the night decides whether the sheet is worth
// opening exactly as it decides about a screenshot.
//
// ffmpeg runs here, on this computer, as the person, and only where it is
// installed. A sheet is made once for each video, by the hash of its
// bytes, and kept in the person's cache, so the same clip posted twice is
// one sheet; a video ffmpeg cannot read leaves a marker, so the next pass
// does not pay to find that out again.

// How long one page may spend making sheets, and how many: the page has
// its own deadline, and what is not made this pass is made the next.
const (
	sheetsTimeAPage  = 3 * time.Minute
	sheetsAPage      = 200
	sheetCommandTime = 30 * time.Second
)

// videoBudget is what a page has left for making sheets.
type videoBudget struct {
	until time.Time
	made  int
}

// withVideoSheets adds, beside each video a record came with, a sheet of
// its frames, and gives the video its description where it has no text.
func (self *recordsFolder) withVideoSheets(ctx context.Context, one *record) {
	var added []recordAttachment
	for index := range one.Attachments {
		attachment := &one.Attachments[index]
		kind := strings.TrimSpace(attachment.ContentType)
		if kind == "" {
			kind = mime.TypeByExtension(strings.ToLower(filepath.Ext(attachment.Name)))
		}
		if !strings.HasPrefix(kind, "video/") || !filepath.IsAbs(attachment.Path) {
			continue
		}
		if sheet := self.sheetFor(ctx, attachment); sheet != "" {
			added = append(added, recordAttachment{Path: sheet, Name: attachment.Name + " (frames).jpg", ContentType: "image/jpeg"})
		}
	}
	one.Attachments = append(one.Attachments, added...)
}

// videoFacts is what a video says about itself, for nothing: no frame is
// decoded to learn it.
type videoFacts struct {
	seconds float64
	width   int
	height  int
	codec   string
}

func probeVideo(ctx context.Context, path string) (videoFacts, bool) {
	ctx, cancel := context.WithTimeout(ctx, sheetCommandTime)
	defer cancel()
	output, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-print_format", "json", "-show_format", "-show_streams", path).Output()
	if err != nil {
		return videoFacts{}, false
	}
	var probed struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if json.Unmarshal(output, &probed) != nil {
		return videoFacts{}, false
	}
	for _, stream := range probed.Streams {
		if stream.CodecType != "video" {
			continue
		}
		seconds, _ := strconv.ParseFloat(probed.Format.Duration, 64)
		return videoFacts{seconds: seconds, width: stream.Width, height: stream.Height, codec: stream.CodecName}, true
	}
	return videoFacts{}, false
}

// describeVideo is the words a video carries even where no frame is ever
// looked at: a fifteen-second clip at phone resolution and a four-minute
// screen recording are different things, and a search for either can only
// find them if somebody wrote it down.
func describeVideo(about videoFacts, name string) string {
	minutes, seconds := int(about.seconds)/60, int(about.seconds)%60
	length := fmt.Sprintf("%ds", seconds)
	if minutes > 0 {
		length = fmt.Sprintf("%dm%02ds", minutes, seconds)
	}
	size := "unknown size"
	if about.width > 0 {
		size = fmt.Sprintf("%dx%d", about.width, about.height)
	}
	codec := about.codec
	if codec == "" {
		codec = "unknown codec"
	}
	return fmt.Sprintf("A video, %s: %s long, %s, %s. The sheet of frames beside it is what it looks like.", name, length, size, codec)
}

// framesDirectory is where sheets are kept, by the hash of their video.
func (self *recordsFolder) framesDirectory() string {
	return filepath.Join(self.options.Home, ".cache", "teanode", "frames")
}

// videoHash is a video's hash, remembered by its path, size and time, so
// a video already seen is not read again on every page.
func (self *recordsFolder) videoHash(path string) (string, bool) {
	information, err := os.Stat(path)
	if err != nil || !information.Mode().IsRegular() || information.Size() > self.maxAttachmentBytes {
		return "", false
	}
	seen := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%d", path, information.Size(), information.ModTime().UnixNano())))
	remembered := filepath.Join(self.framesDirectory(), "seen", hex.EncodeToString(seen[:16]))
	if content, err := os.ReadFile(remembered); err == nil && len(strings.TrimSpace(string(content))) == 64 {
		return strings.TrimSpace(string(content)), true
	}
	hash, _, err := hashOfFile(path)
	if err != nil {
		return "", false
	}
	if os.MkdirAll(filepath.Dir(remembered), 0o700) == nil {
		_ = os.WriteFile(remembered, []byte(hash+"\n"), 0o600)
	}
	return hash, true
}

// sheetFor is the sheet of a video's frames, made once and kept, or ""
// where there is none and none can be made this page. The video is given
// its description where it has no text, when anything is learned of it.
func (self *recordsFolder) sheetFor(ctx context.Context, attachment *recordAttachment) string {
	hash, ok := self.videoHash(attachment.Path)
	if !ok {
		return ""
	}
	directory := self.framesDirectory()
	sheet := filepath.Join(directory, hash+".jpg")
	refused := filepath.Join(directory, hash+".no")
	if _, err := os.Stat(sheet); err == nil {
		return sheet
	}
	if _, err := os.Stat(refused); err == nil {
		return ""
	}
	if self.videos.until.IsZero() {
		self.videos.until = time.Now().Add(sheetsTimeAPage)
	}
	if self.videos.made >= sheetsAPage || time.Now().After(self.videos.until) {
		return ""
	}
	about, ok := probeVideo(ctx, attachment.Path)
	if !ok {
		return ""
	}
	if strings.TrimSpace(attachment.Text) == "" {
		attachment.Text = describeVideo(about, attachment.Name)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return ""
	}
	// A video is marked as one ffmpeg cannot read only when ffmpeg said
	// so: a page cut short says nothing about the video.
	refuse := func() string {
		if ctx.Err() == nil {
			_ = os.WriteFile(refused, nil, 0o600)
		}
		return ""
	}
	if about.seconds <= 0 || about.width == 0 {
		return refuse()
	}
	self.videos.made++
	// Named .jpg while it is built: ffmpeg picks the format from the name.
	working := sheet + ".building.jpg"
	listing := sheet + ".list.txt"
	var frames []string
	defer func() {
		for _, leftover := range append(frames, listing, working) {
			_ = os.Remove(leftover)
		}
	}()
	timedOut := false
	run := func(arguments ...string) bool {
		commandContext, cancel := context.WithTimeout(ctx, sheetCommandTime)
		defer cancel()
		err := exec.CommandContext(commandContext, "ffmpeg", arguments...).Run()
		if errors.Is(commandContext.Err(), context.DeadlineExceeded) {
			timedOut = true
		}
		return err == nil
	}
	for index := 0; index < 9; index++ {
		// Just inside each ninth of the clip: the very first and last
		// frames of a recording are usually a blank desktop.
		at := about.seconds * (float64(index) + 0.5) / 9
		frame := fmt.Sprintf("%s.%d.jpg", sheet, index)
		if run("-nostdin", "-v", "error", "-ss", fmt.Sprintf("%.3f", at), "-i", attachment.Path, "-frames:v", "1", "-vf", "scale=480:-2", "-q:v", "4", "-y", frame) {
			if information, err := os.Stat(frame); err == nil && information.Size() > 0 {
				frames = append(frames, frame)
			}
		}
	}
	if len(frames) < 2 {
		if timedOut {
			return ""
		}
		return refuse()
	}
	var list strings.Builder
	for _, frame := range frames {
		list.WriteString("file '" + strings.ReplaceAll(frame, "'", `'\''`) + "'\n")
	}
	if os.WriteFile(listing, []byte(list.String()), 0o600) != nil {
		return ""
	}
	rows := (len(frames) + 2) / 3
	if !run("-nostdin", "-v", "error", "-f", "concat", "-safe", "0", "-i", listing, "-vf", fmt.Sprintf("tile=3x%d", rows), "-frames:v", "1", "-q:v", "4", "-y", working) {
		if timedOut {
			return ""
		}
		return refuse()
	}
	if information, err := os.Stat(working); err != nil || information.Size() == 0 {
		return refuse()
	}
	if os.Rename(working, sheet) != nil {
		return ""
	}
	return sheet
}
