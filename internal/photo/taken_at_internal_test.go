package photo

import (
	"net/http"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

func TestADateAndTimeInAFileNameIsReadAsAClockReading(test *testing.T) {
	test.Parallel()
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	const prefix = "0a1b2c3d-0000-4000-8000-000000000001"
	for name, testCase := range map[string]struct {
		filename string
		expected time.Time
		isFound  bool
	}{
		"an app's name with milliseconds":   {prefix + "2018-01-30_19-22-02_787.jpeg", time.Date(2018, 1, 30, 19, 22, 2, 0, time.UTC), true},
		"an app's name without them":        {prefix + "2018-01-30_19-22-02.jpeg", time.Date(2018, 1, 30, 19, 22, 2, 0, time.UTC), true},
		"a camera's IMG name":               {"IMG_20190304_050607.jpg", time.Date(2019, 3, 4, 5, 6, 7, 0, time.UTC), true},
		"a Pixel name with milliseconds":    {"PXL_20200229_235959123.jpg", time.Date(2020, 2, 29, 23, 59, 59, 0, time.UTC), true},
		"a VID name":                        {"VID_20210102_030405.mp4", time.Date(2021, 1, 2, 3, 4, 5, 0, time.UTC), true},
		"a bare date and time":              {"20220506_070809.jpg", time.Date(2022, 5, 6, 7, 8, 9, 0, time.UTC), true},
		"next year is the latest":           {"IMG_20270101_000000.jpg", time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), true},
		"month 13":                          {"IMG_20181301_192202.jpg", time.Time{}, false},
		"the 31st of February":              {prefix + "2018-02-31_19-22-02_787.jpeg", time.Time{}, false},
		"hour 25":                           {"IMG_20180130_252202.jpg", time.Time{}, false},
		"before 1990":                       {"IMG_19891231_235959.jpg", time.Time{}, false},
		"after next year":                   {"IMG_20280101_000000.jpg", time.Time{}, false},
		"no date in the name":               {"holiday-beach.jpeg", time.Time{}, false},
		"a date without a time":             {"2018-01-30.jpeg", time.Time{}, false},
		"an empty name":                     {"", time.Time{}, false},
		"a longer run of digits is no date": {"123420180130_192202.jpg", time.Time{}, false},
	} {
		test.Run(name, func(test *testing.T) {
			actual, isFound := takenAtFromFilename(testCase.filename, now)
			if isFound != testCase.isFound || !actual.Equal(testCase.expected) {
				test.Fatalf("%q read as %s (found %v), expected %s (found %v)", testCase.filename, actual, isFound, testCase.expected, testCase.isFound)
			}
		})
	}
}

func TestAnEpochDateIsUnknownAndTheFileNameStandsInForIt(test *testing.T) {
	test.Parallel()
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	const prefix = "0a1b2c3d-0000-4000-8000-000000000001"
	epoch := time.Unix(0, 0).UTC()
	realDate := time.Date(2023, 7, 12, 14, 30, 0, 0, time.UTC)
	offsetMinutes := 120
	for name, testCase := range map[string]struct {
		filename string
		// photoTakenAt is the date the row already has, from the uploader.
		photoTakenAt *time.Time
		metadata     db.PhotoMetadata

		expectedTakenAt     *time.Time
		expectedIsWallClock bool
	}{
		"epoch metadata, an app's name":      {filename: prefix + "2018-01-30_19-22-02_787.jpeg", metadata: db.PhotoMetadata{TakenAt: &epoch, IsTakenAtWallClock: true}, expectedTakenAt: new(time.Date(2018, 1, 30, 19, 22, 2, 0, time.UTC)), expectedIsWallClock: true},
		"epoch metadata, an IMG name":        {filename: "IMG_20190304_050607.jpg", metadata: db.PhotoMetadata{TakenAt: &epoch}, expectedTakenAt: new(time.Date(2019, 3, 4, 5, 6, 7, 0, time.UTC)), expectedIsWallClock: true},
		"epoch metadata, a Pixel name":       {filename: "PXL_20200229_235959123.mp4", metadata: db.PhotoMetadata{TakenAt: &epoch, TakenAtOffsetMinutes: &offsetMinutes}, expectedTakenAt: new(time.Date(2020, 2, 29, 23, 59, 59, 0, time.UTC)), expectedIsWallClock: true},
		"epoch metadata, a VID name":         {filename: "VID_20210102_030405.mp4", metadata: db.PhotoMetadata{TakenAt: &epoch}, expectedTakenAt: new(time.Date(2021, 1, 2, 3, 4, 5, 0, time.UTC)), expectedIsWallClock: true},
		"epoch metadata, a bare name":        {filename: "20220506_070809.jpg", metadata: db.PhotoMetadata{TakenAt: &epoch}, expectedTakenAt: new(time.Date(2022, 5, 6, 7, 8, 9, 0, time.UTC)), expectedIsWallClock: true},
		"no metadata date, an app's name":    {filename: prefix + "2018-01-30_19-22-02_787.jpeg", expectedTakenAt: new(time.Date(2018, 1, 30, 19, 22, 2, 0, time.UTC)), expectedIsWallClock: true},
		"epoch metadata, no date in name":    {filename: "holiday-beach.jpeg", metadata: db.PhotoMetadata{TakenAt: &epoch, IsTakenAtWallClock: true}},
		"epoch metadata, an impossible date": {filename: "IMG_20181301_192202.jpg", metadata: db.PhotoMetadata{TakenAt: &epoch}},
		"real metadata beats the name":       {filename: "IMG_20190304_050607.jpg", metadata: db.PhotoMetadata{TakenAt: &realDate, IsTakenAtWallClock: true}, expectedTakenAt: &realDate, expectedIsWallClock: true},
		"the uploader's date beats the name": {filename: "IMG_20190304_050607.jpg", photoTakenAt: &realDate},
	} {
		test.Run(name, func(test *testing.T) {
			processed := &processedPhoto{
				photo:    &models.Photo{OriginalFilename: testCase.filename, TakenAt: testCase.photoTakenAt},
				metadata: testCase.metadata,
			}
			processed.settleTakenAt(now)
			actual := processed.metadata
			if (actual.TakenAt == nil) != (testCase.expectedTakenAt == nil) || (actual.TakenAt != nil && !actual.TakenAt.Equal(*testCase.expectedTakenAt)) {
				test.Fatalf("the date is %v, expected %v", actual.TakenAt, testCase.expectedTakenAt)
			}
			if actual.IsTakenAtWallClock != testCase.expectedIsWallClock {
				test.Fatalf("IsTakenAtWallClock is %v, expected %v", actual.IsTakenAtWallClock, testCase.expectedIsWallClock)
			}
			if actual.TakenAtOffsetMinutes != nil {
				test.Fatalf("the offset is %d, expected none: a clock reading has no offset", *actual.TakenAtOffsetMinutes)
			}
		})
	}
}

func TestAnUploadDatedAtTheEpochCountsAsUndated(test *testing.T) {
	test.Parallel()
	for name, testCase := range map[string]struct {
		value    string
		expected *time.Time
	}{
		"the epoch in UTC":              {"1970-01-01T00:00:00Z", nil},
		"the epoch written with offset": {"1970-01-01T09:00:00+09:00", nil},
		"a real date":                   {"2018-01-30T19:22:02Z", new(time.Date(2018, 1, 30, 19, 22, 2, 0, time.UTC))},
	} {
		test.Run(name, func(test *testing.T) {
			request, err := http.NewRequest(http.MethodPost, "/", nil)
			if err != nil {
				test.Fatal(err)
			}
			request.Header.Set(headerClientId, "client-photo-1")
			request.Header.Set(headerTakenAt, testCase.value)
			headers, headerErr := parseUploadHeaders(request)
			if headerErr != nil {
				test.Fatal(headerErr)
			}
			if (headers.TakenAt == nil) != (testCase.expected == nil) || (headers.TakenAt != nil && !headers.TakenAt.Equal(*testCase.expected)) {
				test.Fatalf("the date is %v, expected %v", headers.TakenAt, testCase.expected)
			}
			if testCase.expected == nil && headers.TakenAtOffsetMinutes != nil {
				test.Fatalf("an unknown date has offset %d", *headers.TakenAtOffsetMinutes)
			}
		})
	}
}
