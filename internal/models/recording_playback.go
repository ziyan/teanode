package models

// RecordingPlayback describes an owned audio preview, never an original receipt.
type RecordingPlayback struct {
	RecordingMediaID    string `json:"recordingMediaId"`
	PlaybackPath        string `json:"playbackPath"`
	PlaybackContentType string `json:"playbackContentType"`
	PlaybackSizeBytes   int64  `json:"playbackSizeBytes"`
	PlaybackSHA256      string `json:"playbackSha256"`
	PlaybackDurationMS  int64  `json:"playbackDurationMS"`
	TimelineStartMS     int64  `json:"timelineStartMS"`
}
