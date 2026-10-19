package photo

import (
	"regexp"
	"strconv"
	"time"
)

// unixEpoch is the date a library reports for a picture that was saved from
// another app and never given a creation date. It is no capture time, and
// shown on a clock west of UTC it is the evening of 1969-12-31.
var unixEpoch = time.Unix(0, 0).UTC()

// isUnixEpoch says a capture time is exactly 1970-01-01T00:00:00Z, which is
// treated as unknown wherever it comes from: the file's metadata or the date
// the phone sent.
func isUnixEpoch(moment time.Time) bool {
	return moment.Equal(unixEpoch)
}

// The earliest year a file name's date may name. Cameras that write their date
// into the name do not predate it, and a run of digits that happens to look
// like a date usually does.
const earliestFilenameYear = 1990

// Two ways of writing a date and time into a file name: a dashed one that apps
// saving pictures use (2018-01-30_19-22-02, with an optional _787
// milliseconds part), and the compact one of cameras (IMG_20180130_192202,
// PXL_20180130_192202123, VID_20180130_192202, 20180130_192202). The compact
// one must not start in the middle of a longer run of digits.
var (
	dashedFilenameDate  = regexp.MustCompile(`(\d{4})-(\d{2})-(\d{2})_(\d{2})-(\d{2})-(\d{2})`)
	compactFilenameDate = regexp.MustCompile(`(?:^|\D)(\d{4})(\d{2})(\d{2})[_-](\d{2})(\d{2})(\d{2})`)
)

// takenAtFromFilename reads the date and time a file name carries. It is a
// wall clock reading of the camera, as a picture's EXIF date without an
// offset is, so it is returned held as if it were UTC. A date that does not
// exist, or whose year is before earliestFilenameYear or after the year
// following now, is not one.
func takenAtFromFilename(filename string, now time.Time) (time.Time, bool) {
	for _, pattern := range []*regexp.Regexp{dashedFilenameDate, compactFilenameDate} {
		match := pattern.FindStringSubmatch(filename)
		if match == nil {
			continue
		}
		var parts [6]int
		for index := range parts {
			parts[index], _ = strconv.Atoi(match[index+1])
		}
		wallClock := time.Date(parts[0], time.Month(parts[1]), parts[2], parts[3], parts[4], parts[5], 0, time.UTC)
		// time.Date carries a month 13 or a 31st of February over into the
		// next one, so a date that did not survive as written was not one.
		isExistingDate := wallClock.Year() == parts[0] && int(wallClock.Month()) == parts[1] && wallClock.Day() == parts[2] &&
			wallClock.Hour() == parts[3] && wallClock.Minute() == parts[4] && wallClock.Second() == parts[5]
		if isExistingDate && parts[0] >= earliestFilenameYear && parts[0] <= now.Year()+1 {
			return wallClock, true
		}
	}
	return time.Time{}, false
}

// settleTakenAt decides what the file says about when the photo was taken,
// before it is saved. A date of exactly the Unix epoch is no date. When there
// is none, and the photo has none either, the date and time in the original
// file name stand in, as a clock reading in the owner's zone like an EXIF date
// with no offset. A date the file or the uploader did give always wins over
// the name. What is left unknown falls through to the upload time, as before.
func (self *processedPhoto) settleTakenAt(now time.Time) {
	metadata := &self.metadata
	if metadata.TakenAt != nil && isUnixEpoch(*metadata.TakenAt) {
		metadata.TakenAt, metadata.TakenAtOffsetMinutes, metadata.IsTakenAtWallClock = nil, nil, false
	}
	if metadata.TakenAt != nil || self.photo.TakenAt != nil {
		return
	}
	if wallClock, isFound := takenAtFromFilename(self.photo.OriginalFilename, now); isFound {
		metadata.TakenAt, metadata.IsTakenAtWallClock = &wallClock, true
	}
}
