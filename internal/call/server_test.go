package call

import (
	"sync"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// device is a test's stand-in for a phone: a peer connection that offers a
// microphone, answers the server's offers, and notes whose audio reaches it.
type device struct {
	t          *testing.T
	server     *Server
	callId     string
	userId     string
	connection *webrtc.PeerConnection
	microphone *webrtc.TrackLocalStaticRTP

	mutex sync.Mutex
	heard map[string]int

	// signaling holds the join until the server's answer is applied, and
	// each offer after it in turn, as a real device must: an offer can
	// arrive before the answer to the join has been read.
	signaling sync.Mutex
	problems  []string
}

func newDevice(t *testing.T, server *Server, callId, userId string) *device {
	t.Helper()
	connection, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	microphone, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "audio", userId)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.AddTrack(microphone); err != nil {
		t.Fatal(err)
	}
	joined := &device{t: t, server: server, callId: callId, userId: userId, connection: connection, microphone: microphone, heard: map[string]int{}}
	connection.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		// The stream is named after whose it is.
		from := remote.StreamID()
		for {
			if _, _, err := remote.ReadRTP(); err != nil {
				return
			}
			joined.mutex.Lock()
			joined.heard[from]++
			joined.mutex.Unlock()
		}
	})
	return joined
}

// join offers, takes the server's answer, and answers each later offer.
func (self *device) join() {
	self.t.Helper()
	self.signaling.Lock()
	defer self.signaling.Unlock()
	offer, err := self.connection.CreateOffer(nil)
	if err != nil {
		self.t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(self.connection)
	if err := self.connection.SetLocalDescription(offer); err != nil {
		self.t.Fatal(err)
	}
	<-gathered
	answerSDP, err := self.server.Join(self.callId, self.userId, self.connection.LocalDescription().SDP, func(signal Signal) {
		go self.answerOffer(signal.OfferSDP)
	})
	if err != nil {
		self.t.Fatalf("Join: %s", err)
	}
	if err := self.connection.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answerSDP}); err != nil {
		self.t.Fatalf("the server's answer: %s", err)
	}
}

func (self *device) answerOffer(offerSDP string) {
	self.signaling.Lock()
	defer self.signaling.Unlock()
	if self.connection.ConnectionState() == webrtc.PeerConnectionStateClosed {
		return
	}
	if err := self.connection.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offerSDP}); err != nil {
		self.problem("the server's offer: " + err.Error())
		return
	}
	answer, err := self.connection.CreateAnswer(nil)
	if err != nil {
		self.problem("CreateAnswer: " + err.Error())
		return
	}
	gathered := webrtc.GatheringCompletePromise(self.connection)
	if err := self.connection.SetLocalDescription(answer); err != nil {
		self.problem("SetLocalDescription: " + err.Error())
		return
	}
	<-gathered
	if err := self.server.Answer(self.callId, self.userId, self.connection.LocalDescription().SDP); err != nil && err != ErrNotOnCall {
		self.problem("Answer: " + err.Error())
	}
}

// problem keeps what went wrong for the test to report, which a goroutine
// that outlives the test cannot.
func (self *device) problem(text string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.problems = append(self.problems, text)
}

func (self *device) reportProblems() {
	self.t.Helper()
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for _, text := range self.problems {
		self.t.Errorf("%s: %s", self.userId, text)
	}
}

// speak sends a few packets of silence, as a microphone would.
func (self *device) speak(stop <-chan struct{}) {
	sequence := uint16(0)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			sequence++
			_ = self.microphone.WriteRTP(&rtp.Packet{
				Header:  rtp.Header{Version: 2, PayloadType: 111, SequenceNumber: sequence, Timestamp: uint32(sequence) * 960, SSRC: 1234},
				Payload: []byte{0xf8, 0xff, 0xfe},
			})
		}
	}
}

func (self *device) heardFrom(userId string) int {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.heard[userId]
}

func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("gave up waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// What one person says reaches everybody else on the call, a person who
// joins late hears it too, and somebody who left is no longer sent anything.
func TestTheMediaServerForwardsEachVoiceToTheOthers(t *testing.T) {
	server, err := New(Settings{PublicAddress: "127.0.0.1", UDPPort: 0, TCPPort: 0})
	if err != nil {
		t.Fatalf("New: %s", err)
	}
	defer func() { _ = server.Close() }()
	recorded := map[string]int{}
	var recordedMutex sync.Mutex
	server.SetAudioRecorder(func(callId, userId string, _ []byte, codec webrtc.RTPCodecParameters) {
		if codec.MimeType != webrtc.MimeTypeOpus {
			t.Errorf("recorded %s", codec.MimeType)
		}
		recordedMutex.Lock()
		recorded[userId]++
		recordedMutex.Unlock()
	})

	mira := newDevice(t, server, "call-1", "mira")
	tobias := newDevice(t, server, "call-1", "tobias")
	mira.join()
	tobias.join()
	stop := make(chan struct{})
	defer close(stop)
	go mira.speak(stop)
	go tobias.speak(stop)
	waitFor(t, "Tobias to hear Mira", func() bool { return tobias.heardFrom("mira") > 5 })
	waitFor(t, "Mira to hear Tobias", func() bool { return mira.heardFrom("tobias") > 5 })
	if mira.heardFrom("mira") != 0 {
		t.Error("Mira was sent her own voice")
	}

	priya := newDevice(t, server, "call-1", "priya")
	priya.join()
	waitFor(t, "Priya, who joined late, to hear both", func() bool { return priya.heardFrom("mira") > 5 && priya.heardFrom("tobias") > 5 })
	if !server.IsOn("call-1", "priya") || server.IsOn("call-1", "sam") || server.IsOn("call-2", "priya") {
		t.Error("IsOn says the wrong people are on")
	}

	for _, each := range []*device{mira, tobias, priya} {
		each.reportProblems()
	}
	recordedMutex.Lock()
	if recorded["mira"] == 0 || recorded["tobias"] == 0 {
		t.Errorf("the recorder was told %v", recorded)
	}
	recordedMutex.Unlock()

	server.Leave("call-1", "tobias")
	if server.IsOn("call-1", "tobias") {
		t.Error("Tobias is still on after leaving")
	}
	if err := server.Answer("call-1", "tobias", "v=0"); err != ErrNotOnCall {
		t.Errorf("an answer from somebody not on the call: %v", err)
	}
	server.End("call-1")
	if server.IsOn("call-1", "mira") {
		t.Error("Mira is still on after the call ended")
	}
}
