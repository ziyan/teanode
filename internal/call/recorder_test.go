package call

import (
	"os"
	"testing"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

var opus = webrtc.RTPCodecParameters{RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}}

func packetOf(t *testing.T, ssrc uint32, sequence uint16) []byte {
	t.Helper()
	packet := &rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 111, SequenceNumber: sequence, Timestamp: uint32(sequence) * 960, SSRC: ssrc}, Payload: []byte{0xf8, 0xff, 0xfe}}
	raw, err := packet.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// Every voice on a call being transcribed is kept, each stretch in a file of
// its own, and nothing of a call that is not; what is kept of a call is
// handed over once and removed with Discard.
func TestTheRecorderKeepsEveryVoiceOfACallWanted(t *testing.T) {
	recorder := NewRecorder(t.TempDir())
	recorder.Want("call-1", true)
	for sequence := uint16(1); sequence <= 10; sequence++ {
		recorder.Record("call-1", "mira", packetOf(t, 1, sequence), opus)
		recorder.Record("call-1", "tobias", packetOf(t, 2, sequence), opus)
		recorder.Record("call-elsewhere", "priya", packetOf(t, 9, sequence), opus)
	}
	// Mira joined again: a new stream is a new stretch.
	for sequence := uint16(1); sequence <= 5; sequence++ {
		recorder.Record("call-1", "mira", packetOf(t, 3, sequence), opus)
	}
	recorder.Record("call-1", "mira", packetOf(t, 3, 6), webrtc.RTPCodecParameters{RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}})

	kept := recorder.Finish("call-1")
	voicesOf := map[string]int{}
	for _, voice := range kept {
		voicesOf[voice.UserID]++
	}
	if len(kept) != 3 || voicesOf["mira"] != 2 || voicesOf["tobias"] != 1 {
		t.Fatalf("kept %+v", kept)
	}
	if elsewhere := recorder.Finish("call-elsewhere"); len(elsewhere) != 0 {
		t.Errorf("a call nobody wanted transcribed was kept: %+v", elsewhere)
	}
	for _, voice := range kept {
		if info, err := os.Stat(voice.Path); err != nil || info.Size() == 0 {
			t.Errorf("%s: %v", voice.Path, err)
		}
	}
	// What is on disk is handed over until it is discarded.
	if again := recorder.Finish("call-1"); len(again) != 3 {
		t.Errorf("read again: %+v", again)
	}
	if err := recorder.Discard("call-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(kept[0].Path); !os.IsNotExist(err) {
		t.Errorf("the file is still there: %v", err)
	}
	if err := recorder.Discard("../elsewhere"); err == nil {
		t.Error("a path outside the recordings was discarded")
	}

	// A server that stops finds what it kept of a call when it starts again.
	directory := t.TempDir()
	stopped := NewRecorder(directory)
	stopped.Want("call-3", true)
	for sequence := uint16(1); sequence <= 5; sequence++ {
		stopped.Record("call-3", "mira", packetOf(t, 5, sequence), opus)
	}
	started := NewRecorder(directory)
	if found := started.Finish("call-3"); len(found) != 1 || found[0].UserID != "mira" || found[0].StartedAt.IsZero() {
		t.Errorf("after a restart the recorder found %+v", found)
	}

	// A call no longer wanted stops being kept.
	recorder.Want("call-2", true)
	recorder.Record("call-2", "mira", packetOf(t, 4, 1), opus)
	recorder.Want("call-2", false)
	recorder.Record("call-2", "mira", packetOf(t, 4, 2), opus)
	if kept := recorder.Finish("call-2"); len(kept) != 1 {
		t.Errorf("after the call was no longer wanted: %+v", kept)
	}
}
