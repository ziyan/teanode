package computer

import (
	"context"
	"encoding/json"
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
		about, ok := probeVideo(ctx, attachment.Path)
		if !ok {
			continue
		}
		if strings.TrimSpace(attachment.Text) == "" {
			attachment.Text = describeVideo(about, attachment.Name)
		}
		if sheet := self.sheetFor(ctx, attachment.Path, about); sheet != "" {
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

// sheetFor is the sheet of a video's frames, made once and kept, or ""
// where there is none and none can be made this page.
func (self *recordsFolder) sheetFor(ctx context.Context, path string, about videoFacts) string {
	hash, _, err := hashOfFile(path)
	if err != nil {
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
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return ""
	}
	refuse := func() string {
		_ = os.WriteFile(refused, nil, 0o600)
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
	run := func(arguments ...string) bool {
		commandContext, cancel := context.WithTimeout(ctx, sheetCommandTime)
		defer cancel()
		return exec.CommandContext(commandContext, "ffmpeg", arguments...).Run() == nil
	}
	for index := 0; index < 9; index++ {
		// Just inside each ninth of the clip: the very first and last
		// frames of a recording are usually a blank desktop.
		at := about.seconds * (float64(index) + 0.5) / 9
		frame := fmt.Sprintf("%s.%d.jpg", sheet, index)
		if run("-nostdin", "-v", "error", "-ss", fmt.Sprintf("%.3f", at), "-i", path, "-frames:v", "1", "-vf", "scale=480:-2", "-q:v", "4", "-y", frame) {
			if information, err := os.Stat(frame); err == nil && information.Size() > 0 {
				frames = append(frames, frame)
			}
		}
	}
	if len(frames) < 2 {
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
