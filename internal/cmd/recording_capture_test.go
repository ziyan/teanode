package cmd

import (
	"strings"
	"testing"
)

func TestRecordReadsAFileStandardInputOrTheMicrophone(test *testing.T) {
	for _, testCase := range []struct {
		input, device   string
		isStdinTerminal bool
		operatingSystem string
		wantSource      string
	}{
		{input: "meeting.m4a", isStdinTerminal: true, operatingSystem: "linux", wantSource: "-re -i meeting.m4a"},
		{input: "-", isStdinTerminal: true, operatingSystem: "linux", wantSource: "-i pipe:0"},
		{isStdinTerminal: false, operatingSystem: "linux", wantSource: "-i pipe:0"},
		{isStdinTerminal: true, operatingSystem: "linux", wantSource: "-f pulse -i default"},
		{isStdinTerminal: true, operatingSystem: "darwin", wantSource: "-f avfoundation -i :0"},
		{device: "fixture-microphone", isStdinTerminal: true, operatingSystem: "linux", wantSource: "-f pulse -i fixture-microphone"},
	} {
		arguments := strings.Join(recordingFfmpegArguments(testCase.input, testCase.device, testCase.isStdinTerminal, testCase.operatingSystem), " ")
		if !strings.Contains(arguments, testCase.wantSource) || !strings.HasSuffix(arguments, "-ac 1 -ar 16000 -f s16le pipe:1") {
			test.Errorf("%+v gave %q", testCase, arguments)
		}
	}
}
