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

// Only the voices wanted are kept, each stretch in a file of its own; what
// is kept of a call is handed over once and removed with Discard.
func TestTheRecorderKeepsOnlyTheVoicesWanted(t *testing.T) {
	recorder := NewRecorder(t.TempDir())
	recorder.Want("call-1", "mira", true)
	for sequence := uint16(1); sequence <= 10; sequence++ {
		recorder.Record("call-1", "mira", packetOf(t, 1, sequence), opus)
		recorder.Record("call-1", "tobias", packetOf(t, 2, sequence), opus)
	}
	// Mira joined again: a new stream is a new stretch.
	for sequence := uint16(1); sequence <= 5; sequence++ {
		recorder.Record("call-1", "mira", packetOf(t, 3, sequence), opus)
	}
	recorder.Record("call-1", "mira", packetOf(t, 3, 6), webrtc.RTPCodecParameters{RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}})

	kept := recorder.Finish("call-1")
	if len(kept) != 2 || kept[0].UserID != "mira" || kept[1].UserID != "mira" {
		t.Fatalf("kept %+v", kept)
	}
	for _, voice := range kept {
		if info, err := os.Stat(voice.Path); err != nil || info.Size() == 0 {
			t.Errorf("%s: %v", voice.Path, err)
		}
	}
	if again := recorder.Finish("call-1"); len(again) != 0 {
		t.Errorf("handed over twice: %+v", again)
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

	// A voice no longer wanted stops being kept, and a stretch nothing was
	// written to leaves no file.
	recorder.Want("call-2", "mira", true)
	recorder.Record("call-2", "mira", packetOf(t, 4, 1), opus)
	recorder.Want("call-2", "mira", false)
	recorder.Record("call-2", "mira", packetOf(t, 4, 2), opus)
	if kept := recorder.Finish("call-2"); len(kept) != 1 {
		t.Errorf("after the voice was no longer wanted: %+v", kept)
	}
}
