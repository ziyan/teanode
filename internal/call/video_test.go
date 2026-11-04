package call

import (
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/sdp/v3"
	"github.com/pion/webrtc/v4"
)

// testCamera is a camera that sends two layers, whole and small, as a phone
// does; each packet ends with the layer's mark, F or Q.
type testCamera struct {
	connection   *webrtc.PeerConnection
	whole, small *webrtc.TrackLocalStaticRTP
	sender       *webrtc.RTPSender
	// isWholePaused stops the whole layer, as a device short of bandwidth
	// does, while the small one goes on.
	isWholePaused atomic.Bool
}

func (self *device) camera() *testCamera {
	self.t.Helper()
	vp8 := webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000}
	whole, err := webrtc.NewTrackLocalStaticRTP(vp8, "video", self.userId, webrtc.WithRTPStreamID(layerWhole))
	if err != nil {
		self.t.Fatal(err)
	}
	small, err := webrtc.NewTrackLocalStaticRTP(vp8, "video", self.userId, webrtc.WithRTPStreamID(layerSmall))
	if err != nil {
		self.t.Fatal(err)
	}
	sender, err := self.connection.AddTrack(whole)
	if err != nil {
		self.t.Fatal(err)
	}
	if err := sender.AddEncoding(small); err != nil {
		self.t.Fatal(err)
	}
	return &testCamera{connection: self.connection, whole: whole, small: small, sender: sender}
}

// streamExtensions are the numbers the connection gave the header extensions
// that name a packet's media section and layer, and the section's id: a
// browser writes both on every packet of a camera in layers, and the test
// writes them itself.
func (self *testCamera) streamExtensions() (midId, ridId uint8, mid string) {
	for _, extension := range self.sender.GetParameters().HeaderExtensions {
		switch extension.URI {
		case sdp.SDESMidURI:
			midId = uint8(extension.ID)
		case sdp.SDESRTPStreamIDURI:
			ridId = uint8(extension.ID)
		}
	}
	for _, transceiver := range self.connection.GetTransceivers() {
		if transceiver.Sender() == self.sender {
			mid = transceiver.Mid()
		}
	}
	return midId, ridId, mid
}

// film sends frames on both layers, every tenth a key frame, each layer
// with sequence numbers and times of its own, as an encoder's are.
func (self *testCamera) film(stop <-chan struct{}) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	frame := 0
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			frame++
			frameHeader := byte(0x01)
			if frame%10 == 1 {
				frameHeader = 0x00
			}
			midId, ridId, mid := self.streamExtensions()
			for _, layer := range []struct {
				track         *webrtc.TrackLocalStaticRTP
				sequenceBase  uint16
				timestampBase uint32
				mark          byte
			}{{self.whole, 1000, 70000, 'F'}, {self.small, 50000, 9000000, 'Q'}} {
				if layer.mark == 'F' && self.isWholePaused.Load() {
					continue
				}
				packet := &rtp.Packet{
					Header: rtp.Header{
						Version: 2, PayloadType: 96, Marker: true,
						SequenceNumber: layer.sequenceBase + uint16(frame), Timestamp: layer.timestampBase + uint32(frame)*frameTicks,
					},
					Payload: []byte{0x10, frameHeader, 0x9d, 0x01, 0x2a, layer.mark},
				}
				if midId != 0 && ridId != 0 {
					_ = packet.SetExtension(midId, []byte(mid))
					_ = packet.SetExtension(ridId, []byte(layer.track.RID()))
				}
				_ = layer.track.WriteRTP(packet)
			}
		}
	}
}

func (self *device) layerSeenFrom(userId string) byte {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.seenLayer[userId]
}

// sequencesFrom are the sequence numbers of the pictures from a person, from
// the first given on.
func (self *device) sequencesFrom(userId string) []uint16 {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return slices.Clone(self.seenSequences[userId])
}

func (self *device) speakersTold() []string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return slices.Clone(self.speakers)
}

// Each person is sent the small picture of everybody, the whole picture of
// whoever they feature, and of whoever is speaking when they feature nobody,
// and is told who that is. A switch of layers keeps the sequence unbroken.
func TestEachPersonIsSentTheWholePictureOfWhoTheyFeature(t *testing.T) {
	before := wholePicturesMost
	wholePicturesMost = 0
	defer func() { wholePicturesMost = before }()
	server, err := New(Settings{PublicAddress: "127.0.0.1", UDPPort: 0, TCPPort: 0})
	if err != nil {
		t.Fatalf("New: %s", err)
	}
	defer func() { _ = server.Close() }()

	mira := newDevice(t, server, "call-1", "mira")
	miraCamera := mira.camera()
	tobias := newDevice(t, server, "call-1", "tobias")
	tobiasCamera := tobias.camera()
	priya := newDevice(t, server, "call-1", "priya")
	priya.shouldTellSpeaker = true
	mira.join()
	tobias.join()
	priya.join()
	stop := make(chan struct{})
	defer close(stop)
	go miraCamera.film(stop)
	go tobiasCamera.film(stop)
	go mira.speak(stop)
	tobiasQuiet := make(chan struct{})
	go tobias.speak(tobiasQuiet)

	waitFor(t, "Priya to see both small pictures", func() bool {
		return priya.layerSeenFrom("mira") == 'Q' && priya.layerSeenFrom("tobias") == 'Q'
	})

	if err := server.Feature("call-1", "priya", "mira"); err != nil {
		t.Fatalf("Feature: %s", err)
	}
	waitFor(t, "Priya to see Mira whole", func() bool { return priya.layerSeenFrom("mira") == 'F' })
	time.Sleep(200 * time.Millisecond)
	if priya.layerSeenFrom("tobias") != 'Q' {
		t.Error("Tobias, not featured, is sent whole")
	}
	sequences := priya.sequencesFrom("mira")
	for index := 1; index < len(sequences); index++ {
		if sequences[index] != sequences[index-1]+1 {
			t.Fatalf("Mira's pictures jump from %d to %d at the switch", sequences[index-1], sequences[index])
		}
	}
	// Mira's whole picture stops, as on a weak connection: Priya is sent
	// the small one meanwhile, and the whole one again when it comes back.
	miraCamera.isWholePaused.Store(true)
	waitFor(t, "Priya to fall back to Mira's small picture", func() bool { return priya.layerSeenFrom("mira") == 'Q' })
	miraCamera.isWholePaused.Store(false)
	waitFor(t, "Priya to see Mira whole again", func() bool { return priya.layerSeenFrom("mira") == 'F' })
	if err := server.Feature("call-1", "sam", "mira"); err != ErrNotOnCall {
		t.Errorf("somebody not on the call featured somebody: %v", err)
	}

	// Featuring nobody follows whoever speaks: Tobias.
	if err := server.Feature("call-1", "priya", ""); err != nil {
		t.Fatalf("Feature: %s", err)
	}
	close(tobiasQuiet)
	loud := make(chan struct{})
	defer close(loud)
	go tobias.speakAt(loud, 10)
	waitFor(t, "Priya to be told Tobias is speaking", func() bool { return slices.Contains(priya.speakersTold(), "tobias") })
	waitFor(t, "Priya to see Tobias whole", func() bool { return priya.layerSeenFrom("tobias") == 'F' })
	waitFor(t, "Priya to see Mira small again", func() bool { return priya.layerSeenFrom("mira") == 'Q' })
	if server.SpeakingUserID("call-1") != "tobias" {
		t.Errorf("the server says %q is speaking", server.SpeakingUserID("call-1"))
	}
	if told := mira.speakersTold(); len(told) != 0 {
		t.Errorf("Mira, who did not ask, was told %v", told)
	}
	for _, each := range []*device{mira, tobias, priya} {
		each.reportProblems()
	}
}

// A late or repeated packet does not move back where a switch carries on
// from, and a switch after a wait leaves a gap in time as long as the wait.
func TestASwitchCarriesOnFromTheNewestPacket(t *testing.T) {
	watcher := &videoWatcher{hasWanted: true, wantedLayer: layerSmall}
	start := time.Now()
	keyFrame := []byte{0x10, 0x00, 0x9d}
	frame := []byte{0x10, 0x01, 0x9d}
	sent := func(layer string, sequence uint16, timestamp uint32, payload []byte, at time.Time) *rtp.Packet {
		packet, _ := watcher.rewrite(layer, &rtp.Packet{Header: rtp.Header{SequenceNumber: sequence, Timestamp: timestamp}, Payload: payload},
			isWholePictureStart(webrtc.MimeTypeVP8, payload), at)
		return packet
	}
	sent(layerSmall, 10, 1000, keyFrame, start)
	sent(layerSmall, 11, 4000, frame, start)
	// A packet that comes late, after 11.
	sent(layerSmall, 9, 0, frame, start)
	watcher.wantedLayer = layerWhole
	if packet := sent(layerWhole, 500, 777, frame, start); packet != nil {
		t.Fatal("the other layer was sent before its whole picture")
	}
	later := start.Add(time.Second)
	switched := sent(layerWhole, 501, 900, keyFrame, later)
	if switched == nil || switched.SequenceNumber != 12 {
		t.Fatalf("the switch went on from %v, not 11", switched)
	}
	if gap := switched.Timestamp - 4000; gap < 80000 || gap > 100000 {
		t.Errorf("a second's wait left a gap of %d ticks", gap)
	}
}

func TestWholePicturesAreFoundInVP8AndH264(t *testing.T) {
	for _, each := range []struct {
		name     string
		mimeType string
		payload  []byte
		isWhole  bool
	}{
		{"a VP8 key frame", webrtc.MimeTypeVP8, []byte{0x10, 0x00, 0x9d}, true},
		{"a VP8 key frame after a picture id", webrtc.MimeTypeVP8, []byte{0x90, 0x80, 0x81, 0x23, 0x00}, true},
		{"a VP8 frame that needs the one before", webrtc.MimeTypeVP8, []byte{0x10, 0x01, 0x9d}, false},
		{"the middle of a VP8 frame", webrtc.MimeTypeVP8, []byte{0x00, 0x00}, false},
		{"an H.264 IDR slice", webrtc.MimeTypeH264, []byte{0x65, 0x88}, true},
		{"H.264 sequence parameters", webrtc.MimeTypeH264, []byte{0x67, 0x42}, true},
		{"H.264 parameters in an aggregation", webrtc.MimeTypeH264, []byte{0x18, 0x00, 0x02, 0x67, 0x42, 0x00, 0x02, 0x68, 0xce}, true},
		{"the first fragment of an IDR slice", webrtc.MimeTypeH264, []byte{0x7c, 0x85, 0x00}, true},
		{"a later fragment of an IDR slice", webrtc.MimeTypeH264, []byte{0x7c, 0x05, 0x00}, false},
		{"an H.264 slice that needs the one before", webrtc.MimeTypeH264, []byte{0x41, 0x9a}, false},
		{"nothing", webrtc.MimeTypeVP8, nil, false},
	} {
		if got := isWholePictureStart(each.mimeType, each.payload); got != each.isWhole {
			t.Errorf("%s: %v", each.name, got)
		}
	}
}
