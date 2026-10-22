package picture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	probeTimeout   = 60 * time.Second
	posterTimeout  = 2 * time.Minute
	previewTimeout = 30 * time.Minute

	maximumProbeBytes  = 4 << 20
	maximumPosterBytes = 64 << 20

	// VideoPreviewLongSidePixels is the longest side of a video preview.
	VideoPreviewLongSidePixels = 1280

	// encodeThreadCount is how many threads one ffmpeg run may use. Photos are
	// processed two at a time on a server that does other work, and a run that
	// takes every core to itself starves it.
	encodeThreadCount = "2"

	// The container formats a camera or a phone makes. As in the recording
	// decoder, nothing else is opened, so a playlist cannot name another file.
	supportedVideoFormats = "mov,mp4,m4a,3gp,3g2,mj2,matroska,webm,avi,mpegts,mpeg,asf,flv,ogg"
)

// VideoMetadata is what a video file says about itself. TakenAt follows the
// rule of Metadata: with an offset it is a moment, without one it is UTC.
type VideoMetadata struct {
	DurationMS int64
	// WidthPixels and HeightPixels are the size as the video is shown, after
	// the rotation its file asks for.
	WidthPixels          int
	HeightPixels         int
	TakenAt              *time.Time
	TakenAtOffsetMinutes *int
	Latitude             *float64
	Longitude            *float64
}

// CheckSize refuses a video whose shown size is more than a picture may be,
// as ReadSize does for one. The size is a claim in the file's header, and
// ffmpeg allocates for it when it decodes, so it is checked before any frame
// is cut.
func (self *VideoMetadata) CheckSize() error {
	if self.WidthPixels > MaximumSidePixels || self.HeightPixels > MaximumSidePixels ||
		int64(self.WidthPixels)*int64(self.HeightPixels) > MaximumPixels {
		return fmt.Errorf("%w: %d by %d pixels", ErrTooLarge, self.WidthPixels, self.HeightPixels)
	}
	return nil
}

type videoProbe struct {
	Streams []struct {
		CodecType   string            `json:"codec_type"`
		Width       int               `json:"width"`
		Height      int               `json:"height"`
		Duration    string            `json:"duration"`
		Tags        map[string]string `json:"tags"`
		Disposition struct {
			AttachedPicture int `json:"attached_pic"`
		} `json:"disposition"`
		SideDataList []struct {
			SideDataType string   `json:"side_data_type"`
			Rotation     *float64 `json:"rotation"`
		} `json:"side_data_list"`
	} `json:"streams"`
	Format struct {
		Duration string            `json:"duration"`
		Tags     map[string]string `json:"tags"`
	} `json:"format"`
}

// ProbeVideo reads a video's length, shown size, date and place with ffprobe.
// path is a file the caller wrote in a private temporary directory.
func ProbeVideo(ctx context.Context, path string) (*VideoMetadata, error) {
	program, err := findVideoProgram("ffprobe")
	if err != nil {
		return nil, err
	}
	file, err := openVideoFile(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	arguments := append([]string{"-v", "error"}, videoInputArguments()...)
	arguments = append(arguments, "-show_format", "-show_streams", "-of", "json")
	output := &boundedWriter{maximumBytes: maximumProbeBytes}
	if err := runVideoProgram(ctx, program, arguments, file, output); err != nil {
		return nil, err
	}
	var probe videoProbe
	if err := json.Unmarshal(output.buffer.Bytes(), &probe); err != nil {
		return nil, errors.New("picture: the video could not be understood")
	}

	metadata := &VideoMetadata{}
	var streamDuration string
	var streamTags map[string]string
	hasVideo := false
	for _, stream := range probe.Streams {
		if stream.CodecType != "video" || stream.Disposition.AttachedPicture != 0 || stream.Width <= 0 || stream.Height <= 0 {
			continue
		}
		hasVideo = true
		metadata.WidthPixels, metadata.HeightPixels = stream.Width, stream.Height
		// A phone films with the sensor's long side across and records a turn
		// to make it upright; old files say it in a tag, new ones in the
		// display matrix.
		rotationDegrees := 0.0
		if tag, isTagged := stream.Tags["rotate"]; isTagged {
			rotationDegrees, _ = strconv.ParseFloat(tag, 64)
		}
		for _, sideData := range stream.SideDataList {
			if sideData.Rotation != nil {
				rotationDegrees = *sideData.Rotation
			}
		}
		if turns := int(math.Round(math.Abs(rotationDegrees) / 90)); turns%2 == 1 {
			metadata.WidthPixels, metadata.HeightPixels = stream.Height, stream.Width
		}
		streamDuration, streamTags = stream.Duration, stream.Tags
		break
	}
	if !hasVideo {
		return nil, errors.New("picture: the file has no video")
	}
	metadata.DurationMS = parseDurationMS(probe.Format.Duration)
	if metadata.DurationMS == 0 {
		metadata.DurationMS = parseDurationMS(streamDuration)
	}

	// The QuickTime tags of an iPhone carry the date with its offset and the
	// place, and are more exact than the container's creation time, which is
	// UTC.
	tagOf := func(names ...string) string {
		for _, name := range names {
			for _, tags := range []map[string]string{probe.Format.Tags, streamTags} {
				for key, value := range tags {
					if strings.EqualFold(key, name) {
						return value
					}
				}
			}
		}
		return ""
	}
	metadata.TakenAt, metadata.TakenAtOffsetMinutes = parseVideoDate(tagOf("com.apple.quicktime.creationdate"), tagOf("creation_time"))
	metadata.Latitude, metadata.Longitude = parseIso6709(tagOf("com.apple.quicktime.location.ISO6709", "location", "location-eng"))
	return metadata, nil
}

func parseDurationMS(seconds string) int64 {
	value, err := strconv.ParseFloat(seconds, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 || value > 365*24*3600 {
		return 0
	}
	return int64(math.Round(value * 1000))
}

// parseVideoDate prefers the QuickTime creation date, which has an offset,
// and falls back to the container's creation time. A container with no date
// at all often says the start of the QuickTime epoch, 1904, which is nothing;
// a date that isPlausibleDate refuses, as that one and one far ahead, is none.
func parseVideoDate(quickTimeDate, containerDate string) (*time.Time, *int) {
	if moment, err := time.Parse("2006-01-02T15:04:05-0700", quickTimeDate); err == nil && isPlausibleDate(moment) {
		_, offsetSeconds := moment.Zone()
		offsetMinutes := offsetSeconds / 60
		return &moment, &offsetMinutes
	}
	if moment, err := time.Parse("2006-01-02T15:04:05-07:00", quickTimeDate); err == nil && isPlausibleDate(moment) {
		_, offsetSeconds := moment.Zone()
		offsetMinutes := offsetSeconds / 60
		return &moment, &offsetMinutes
	}
	if moment, err := time.Parse(time.RFC3339Nano, containerDate); err == nil && isPlausibleDate(moment) {
		moment = moment.UTC()
		return &moment, nil
	}
	return nil, nil
}

// iso6709 matches "+10.0000+020.0000+012.345/": a signed latitude, a signed
// longitude, an optional altitude, an optional coordinate system, and a slash.
var iso6709 = regexp.MustCompile(`^([+-][0-9]+(?:\.[0-9]+)?)([+-][0-9]+(?:\.[0-9]+)?)(?:[+-][0-9]+(?:\.[0-9]+)?)?(?:CRS[A-Za-z0-9_]+)?/?$`)

// parseIso6709 reads latitude and longitude in degrees, or in degrees and
// minutes (+DDMM.MM) or degrees, minutes and seconds (+DDMMSS.SS), as the
// number of digits before the point says.
func parseIso6709(text string) (*float64, *float64) {
	match := iso6709.FindStringSubmatch(strings.TrimSpace(text))
	if match == nil {
		return nil, nil
	}
	latitude, isLatitudeRead := parseIso6709Part(match[1], 2, 90)
	longitude, isLongitudeRead := parseIso6709Part(match[2], 3, 180)
	if !isLatitudeRead || !isLongitudeRead {
		return nil, nil
	}
	return &latitude, &longitude
}

func parseIso6709Part(text string, degreeDigits int, limitDegrees float64) (float64, bool) {
	sign := 1.0
	if text[0] == '-' {
		sign = -1
	}
	digits := text[1:]
	whole, _, _ := strings.Cut(digits, ".")
	value, err := strconv.ParseFloat(digits, 64)
	if err != nil {
		return 0, false
	}
	degrees := value
	switch len(whole) {
	case degreeDigits:
	case degreeDigits + 2:
		degrees = math.Floor(value/100) + math.Mod(value, 100)/60
	case degreeDigits + 4:
		degrees = math.Floor(value/10000) + math.Floor(math.Mod(value, 10000)/100)/60 + math.Mod(value, 100)/3600
	default:
		return 0, false
	}
	degrees *= sign
	if math.IsNaN(degrees) || math.Abs(degrees) > limitDegrees {
		return 0, false
	}
	return degrees, true
}

// VideoPoster returns a JPEG of the frame about one second in, or the first
// frame of a video shorter than that, the right way up: ffmpeg applies the
// rotation a phone records.
func VideoPoster(ctx context.Context, path string) ([]byte, error) {
	program, err := findVideoProgram("ffmpeg")
	if err != nil {
		return nil, err
	}
	file, err := openVideoFile(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	ctx, cancel := context.WithTimeout(ctx, posterTimeout)
	defer cancel()
	// Seeking past the end of a short video is not always an error, and
	// ffmpeg either writes nothing or says it wrote nothing, so a second
	// attempt from the start follows either.
	var lastErr error
	for _, seekSeconds := range []string{"1", "0"} {
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		arguments := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-threads", encodeThreadCount, "-ss", seekSeconds}
		arguments = append(arguments, videoInputArguments()...)
		arguments = append(arguments, "-map", "0:v:0", "-frames:v", "1", "-an", "-sn", "-dn",
			"-threads", encodeThreadCount, "-f", "image2pipe", "-c:v", "mjpeg", "-q:v", "2", "pipe:1")
		output := &boundedWriter{maximumBytes: maximumPosterBytes}
		if lastErr = runVideoProgram(ctx, program, arguments, file, output); lastErr != nil {
			if ctx.Err() != nil || errors.Is(lastErr, errOutputTooLarge) {
				return nil, lastErr
			}
			continue
		}
		if output.buffer.Len() > 0 {
			return output.buffer.Bytes(), nil
		}
		lastErr = errors.New("picture: the video has no frame to show")
	}
	return nil, lastErr
}

// VideoPreview writes an H.264 and AAC MP4 of the video at inputPath to
// outputPath, at most VideoPreviewLongSidePixels on the long side, the right
// way up, with the moov box in front so that a browser starts playing before
// it has the whole file. It is the one copy that every browser plays.
// outputPath is replaced, and removed again if the conversion fails.
func VideoPreview(ctx context.Context, inputPath, outputPath string) error {
	program, err := findVideoProgram("ffmpeg")
	if err != nil {
		return err
	}
	if !filepath.IsAbs(outputPath) {
		return errors.New("picture: a video preview is written to an absolute path")
	}
	file, err := openVideoFile(inputPath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	ctx, cancel := context.WithTimeout(ctx, previewTimeout)
	defer cancel()
	// The longest side is the limit, so the box is square; the rotation is
	// applied by ffmpeg before the scale, so the limit is on the shown shape.
	// Dimensions are kept even because yuv420p cannot be anything else, and
	// nothing is made larger than it was.
	limit := strconv.Itoa(VideoPreviewLongSidePixels)
	scale := fmt.Sprintf("scale=w='min(%s,iw)':h='min(%s,ih)':force_original_aspect_ratio=decrease:force_divisible_by=2", limit, limit)
	arguments := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-threads", encodeThreadCount}
	arguments = append(arguments, videoInputArguments()...)
	// The metadata is dropped so a preview does not carry the place it was
	// filmed to whoever it is shown to.
	arguments = append(arguments, "-map", "0:v:0", "-map", "0:a:0?", "-sn", "-dn",
		"-map_metadata", "-1", "-map_chapters", "-1",
		"-vf", scale,
		"-threads", encodeThreadCount, "-c:v", "libx264", "-preset", "veryfast", "-crf", "26", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "128k",
		"-max_muxing_queue_size", "1024",
		"-movflags", "+faststart", "-f", "mp4", outputPath)
	if err := runVideoProgram(ctx, program, arguments, file, io.Discard); err != nil {
		_ = os.Remove(outputPath)
		return err
	}
	return nil
}

// CheckVideoPrograms says whether ffmpeg and ffprobe, which videos are read
// with, are installed, so that a caller can ask before it copies a large file
// to hand to them. The error is ErrProgramMissing.
func CheckVideoPrograms() error {
	for _, name := range []string{"ffprobe", "ffmpeg"} {
		if _, err := findVideoProgram(name); err != nil {
			return err
		}
	}
	return nil
}

func findVideoProgram(name string) (string, error) {
	program, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%w: videos cannot be read because %s was not found; install FFmpeg", ErrProgramMissing, name)
	}
	return program, nil
}

// openVideoFile opens the video for the child to read through a descriptor,
// which is how the recording decoder runs ffmpeg too: the child never opens
// a path or a URL of its own.
func openVideoFile(path string) (*os.File, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("picture: the video could not be opened: %w", err)
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, errors.New("picture: the video is not a file")
	}
	return file, nil
}

// videoInputArguments makes ffmpeg and ffprobe read descriptor 3 as a video
// and nothing else: no network, no playlist that names another file.
func videoInputArguments() []string {
	return []string{"-protocol_whitelist", "fd,pipe", "-format_whitelist", supportedVideoFormats, "-fd", "3", "-i", "fd:"}
}

func runVideoProgram(ctx context.Context, program string, arguments []string, input *os.File, output io.Writer) error {
	stderr := &tailWriter{maximumBytes: 4 << 10}
	command := exec.CommandContext(ctx, program, arguments...)
	// The file is in a private directory of the caller's, which is the home
	// the program is given.
	command.Env = ProgramEnvironment(filepath.Dir(input.Name()))
	command.ExtraFiles = []*os.File{input}
	command.Stdout = output
	command.Stderr = stderr
	command.WaitDelay = waitDelay
	if err := command.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("picture: %s did not finish: %w", filepath.Base(program), ctxErr)
		}
		// The child dies of the broken pipe, so its exit status is all Run
		// reports; the writer knows why.
		if limited, isLimited := output.(*boundedWriter); isLimited && limited.isExceeded {
			return errOutputTooLarge
		}
		return fmt.Errorf("picture: the video could not be read: %s", stderr.lastLine())
	}
	return nil
}
