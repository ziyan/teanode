// Package call is the media server of calls in chats: everybody on a call
// sends their microphone, and their camera when it is on, to the server once,
// and the server forwards each of those to everybody else on the call without
// mixing it (a selective forwarding unit). A phone sends its microphone once
// and its camera in two sizes however many people are on the call; each
// person is sent the whole picture of whoever they feature and the small one
// of the rest (video.go), and is told who is speaking (speaker.go).
//
// One port of each kind carries every call: UDP, and TCP for networks that
// let no UDP through. The server's public address is what it tells the
// devices to send to. The server is an ICE-lite agent: it is reachable at that
// address, so a device needs to tell it nothing about its own; the server
// answers whatever reaches it.
//
// Signaling is the caller's: Join answers a device's offer, and every later
// change (somebody's stream to add or take away) is an offer from the server,
// handed to the signal function given at Join, which the device answers with
// Answer. Only the server offers after the first exchange, so the two sides
// never offer at once.
package call

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/op/go-logging"
	"github.com/pion/interceptor"
	pionlogging "github.com/pion/logging"
	"github.com/pion/rtp"
	"github.com/pion/sdp/v3"
	"github.com/pion/webrtc/v4"
)

var log = logging.MustGetLogger("call")

var (
	// ErrNotOnCall is a device answering for a call it has not joined.
	ErrNotOnCall = errors.New("call: not on the call")

	// ErrClosed is the media server stopped.
	ErrClosed = errors.New("call: the media server is stopped")
)

// gatherWait bounds how long an answer or an offer waits for the server's
// own addresses: they are local, so this is a moment.
const gatherWait = 5 * time.Second

// Settings say where the media server listens and what address it gives.
type Settings struct {
	// PublicAddress is the address devices send to: the server's own, or the
	// address of the gateway that forwards the ports to it.
	PublicAddress string
	UDPPort       int
	TCPPort       int
}

// Signal is something the server tells one device on a call: an offer to
// answer, or who is speaking now (for a device that asked to be told). An
// offer can arrive before the device has read the server's answer to its
// join, so a device takes its join and then each offer one at a time, in that
// order.
type Signal struct {
	OfferSDP       string
	SpeakingUserID string
}

// Server is the media server.
type Server struct {
	api         *webrtc.API
	udpConn     net.PacketConn
	tcpListener net.Listener

	mutex  sync.Mutex
	rooms  map[string]*room
	closed bool

	// recordAudio, when set, is handed every RTP packet of a person's
	// microphone on a call, for a transcript; see SetAudioRecorder.
	recordAudio AudioRecorder

	// deviceLost, when set, is told of a device whose connection failed
	// (the tab closed, the phone lost its network for good); see
	// SetDeviceLost.
	deviceLost func(callId, userId string)
}

// AudioRecorder is told each packet of each person's microphone; it keeps
// those it has been asked to. The packet is the server's to reuse once the
// call returns.
type AudioRecorder func(callId, userId string, packet []byte, codec webrtc.RTPCodecParameters)

// New starts listening on the ports and is ready for calls.
func New(settings Settings) (*Server, error) {
	if settings.PublicAddress == "" {
		return nil, errors.New("call: no public address to give devices")
	}
	udpConn, err := net.ListenPacket("udp4", net.JoinHostPort("", strconv.Itoa(settings.UDPPort)))
	if err != nil {
		return nil, fmt.Errorf("call: cannot listen on UDP port %d: %w", settings.UDPPort, err)
	}
	tcpListener, err := net.Listen("tcp4", net.JoinHostPort("", strconv.Itoa(settings.TCPPort)))
	if err != nil {
		_ = udpConn.Close()
		return nil, fmt.Errorf("call: cannot listen on TCP port %d: %w", settings.TCPPort, err)
	}
	loggerFactory := pionlogging.NewDefaultLoggerFactory()
	engine := webrtc.SettingEngine{LoggerFactory: loggerFactory}
	engine.SetLite(true)
	engine.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4, webrtc.NetworkTypeTCP4})
	engine.SetICEUDPMux(webrtc.NewICEUDPMux(loggerFactory.NewLogger("ice"), udpConn))
	engine.SetICETCPMux(webrtc.NewICETCPMux(loggerFactory.NewLogger("ice"), tcpListener, 64))
	// The public address beside the server's own: a device on the same
	// network may reach it either way.
	if err := engine.SetICEAddressRewriteRules(webrtc.ICEAddressRewriteRule{
		External:        []string{settings.PublicAddress},
		AsCandidateType: webrtc.ICECandidateTypeHost,
		Mode:            webrtc.ICEAddressRewriteAppend,
	}); err != nil {
		_ = udpConn.Close()
		_ = tcpListener.Close()
		return nil, fmt.Errorf("call: cannot give the public address: %w", err)
	}
	media, err := newMediaEngine()
	if err != nil {
		_ = udpConn.Close()
		_ = tcpListener.Close()
		return nil, err
	}
	// What a sending device needs to hear back to send its whole picture:
	// which packets were lost (resent from what the server sent), reports of
	// what arrived, and how much got through when (transport-wide feedback),
	// from which it judges how much it can send.
	interceptors := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(media, interceptors); err != nil {
		_ = udpConn.Close()
		_ = tcpListener.Close()
		return nil, err
	}
	return &Server{
		api:         webrtc.NewAPI(webrtc.WithSettingEngine(engine), webrtc.WithMediaEngine(media), webrtc.WithInterceptorRegistry(interceptors)),
		udpConn:     udpConn,
		tcpListener: tcpListener,
		rooms:       map[string]*room{},
	}, nil
}

// videoFeedback is what a device and the server say to each other about a
// picture: a lost one asked for again, and a whole one asked for.
var videoFeedback = []webrtc.RTCPFeedback{{Type: "goog-remb"}, {Type: "ccm", Parameter: "fir"}, {Type: "nack"}, {Type: "nack", Parameter: "pli"}}

// h264Profiles are the H.264 profiles a device may send: constrained
// baseline, baseline, main and high.
var h264Profiles = []string{"42001f", "42e01f", "4d001f", "64001f"}

// newMediaEngine is what devices and the server may send: Opus for sound,
// VP8 and H.264 for pictures, whose whole pictures the server can find to
// switch a watcher between sizes (video.go). There is no resending (RTX): a
// lost picture is asked for whole again. The header extensions are those a
// camera in two sizes needs, and the loudness of each packet of sound, to
// tell who is speaking.
func newMediaEngine() (*webrtc.MediaEngine, error) {
	media := &webrtc.MediaEngine{}
	opus := webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2, SDPFmtpLine: "minptime=10;useinbandfec=1"},
		PayloadType:        111,
	}
	if err := media.RegisterCodec(opus, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, err
	}
	videoCodecs := []webrtc.RTPCodecParameters{{
		RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000, RTCPFeedback: videoFeedback},
		PayloadType:        96,
	}}
	for index, profile := range h264Profiles {
		videoCodecs = append(videoCodecs, webrtc.RTPCodecParameters{
			RTPCodecCapability: webrtc.RTPCodecCapability{
				MimeType: webrtc.MimeTypeH264, ClockRate: 90000, RTCPFeedback: videoFeedback,
				SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=" + profile,
			},
			PayloadType: webrtc.PayloadType(102 + 2*index),
		})
	}
	for _, codec := range videoCodecs {
		if err := media.RegisterCodec(codec, webrtc.RTPCodecTypeVideo); err != nil {
			return nil, err
		}
	}
	if err := webrtc.ConfigureSimulcastExtensionHeaders(media); err != nil {
		return nil, err
	}
	if err := media.RegisterHeaderExtension(webrtc.RTPHeaderExtensionCapability{URI: sdp.AudioLevelURI}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, err
	}
	return media, nil
}

// SetAudioRecorder sets what is told each microphone packet.
func (self *Server) SetAudioRecorder(recorder AudioRecorder) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.recordAudio = recorder
}

// SetDeviceLost sets what is told when a device's connection fails, after
// it has been taken off the call: the person is no longer on it, though
// nothing said so. A device the server takes off itself (it joined again, it
// left) is not lost.
func (self *Server) SetDeviceLost(deviceLost func(callId, userId string)) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.deviceLost = deviceLost
}

// Close ends every call and stops listening.
func (self *Server) Close() error {
	self.mutex.Lock()
	rooms := self.rooms
	self.rooms = map[string]*room{}
	self.closed = true
	self.mutex.Unlock()
	for _, room := range rooms {
		room.close()
	}
	return errors.Join(self.udpConn.Close(), self.tcpListener.Close())
}

func (self *Server) roomOf(callId string, isMade bool) (*room, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.closed {
		return nil, ErrClosed
	}
	found := self.rooms[callId]
	if found == nil && isMade {
		found = &room{server: self, callId: callId, participants: map[string]*participant{}, done: make(chan struct{})}
		self.rooms[callId] = found
		go found.followSpeakers()
	}
	return found, nil
}

// Join puts a device on a call with its offer, and returns the server's
// answer. A device already on it for the person is replaced: the person
// moved to another device, or this one connected again. shouldTellSpeaker
// says the device is to be told who is speaking; a device that does not read
// such signals is not sent them.
func (self *Server) Join(callId, userId, offerSDP string, shouldTellSpeaker bool, signal func(Signal)) (string, error) {
	room, err := self.roomOf(callId, true)
	if err != nil {
		return "", err
	}
	return room.join(userId, offerSDP, shouldTellSpeaker, signal)
}

// Feature sets whose camera the person's device is sent whole: the person
// chosen, or whoever is speaking when featuredUserId is empty. It is the
// device's own, and goes when it leaves.
func (self *Server) Feature(callId, userId, featuredUserId string) error {
	room, err := self.roomOf(callId, false)
	if err != nil {
		return err
	}
	if room == nil {
		return ErrNotOnCall
	}
	room.mutex.Lock()
	defer room.mutex.Unlock()
	watching := room.participants[userId]
	if watching == nil {
		return ErrNotOnCall
	}
	watching.featuredUserId = featuredUserId
	room.chooseLayers()
	return nil
}

// SpeakingUserID is who is speaking on a call, as far as the server can
// tell; empty when nobody has yet.
func (self *Server) SpeakingUserID(callId string) string {
	room, _ := self.roomOf(callId, false)
	if room == nil {
		return ""
	}
	room.mutex.Lock()
	defer room.mutex.Unlock()
	return room.speakingUserId
}

// Answer is a device's answer to the server's latest offer.
func (self *Server) Answer(callId, userId, answerSDP string) error {
	room, err := self.roomOf(callId, false)
	if err != nil {
		return err
	}
	if room == nil {
		return ErrNotOnCall
	}
	return room.answer(userId, answerSDP)
}

// Leave takes the person's device off a call.
func (self *Server) Leave(callId, userId string) {
	room, _ := self.roomOf(callId, false)
	if room != nil {
		room.leave(userId)
	}
}

// End takes everybody off a call.
func (self *Server) End(callId string) {
	self.mutex.Lock()
	room := self.rooms[callId]
	delete(self.rooms, callId)
	self.mutex.Unlock()
	if room != nil {
		room.close()
	}
}

// IsOn says whether the person has a device on the call on this server.
func (self *Server) IsOn(callId, userId string) bool {
	room, _ := self.roomOf(callId, false)
	if room == nil {
		return false
	}
	room.mutex.Lock()
	defer room.mutex.Unlock()
	return room.participants[userId] != nil
}

// room is one call on this server.
type room struct {
	server *Server
	callId string

	mutex        sync.Mutex
	participants map[string]*participant
	forwarded    []*forwardedTrack
	videos       []*forwardedVideo

	// Who is speaking, who spoke before them, and since when (speaker.go);
	// done closes when the room does.
	speakingUserId         string
	previousSpeakingUserId string
	speakerChangedAt       time.Time
	voiceMutex             sync.Mutex
	voices                 map[string]*voiceActivity
	done                   chan struct{}
	closeOnce              sync.Once
}

// participant is one person's device on a call.
type participant struct {
	userId     string
	connection *webrtc.PeerConnection
	signal     func(Signal)

	// featuredUserId is whose camera the device is sent whole, empty for
	// whoever is speaking; shouldTellSpeaker says it is told who that is.
	featuredUserId    string
	shouldTellSpeaker bool

	// negotiating says an offer went out that has not been answered, and
	// isNegotiationOwed that another change came meanwhile, for one more
	// offer once it is.
	negotiationMutex  sync.Mutex
	negotiating       bool
	isNegotiationOwed bool
}

// forwardedTrack is one person's microphone, sent on to the others; senders
// are the ways out, by whose device each goes to. A camera is a
// forwardedVideo (video.go).
type forwardedTrack struct {
	fromUserId string
	remote     *webrtc.TrackRemote
	local      *webrtc.TrackLocalStaticRTP
	senders    map[string]*webrtc.RTPSender
	// publisher is the device it comes from, asked for a whole picture when
	// somebody starts to watch.
	publisher *webrtc.PeerConnection
}

func (self *room) join(userId, offerSDP string, shouldTellSpeaker bool, signal func(Signal)) (string, error) {
	self.leave(userId)
	connection, err := self.server.api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return "", err
	}
	joined := &participant{userId: userId, connection: connection, signal: signal, shouldTellSpeaker: shouldTellSpeaker}
	connection.OnTrack(func(remote *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		if remote.Kind() == webrtc.RTPCodecTypeVideo {
			self.forwardVideo(joined, remote)
			return
		}
		self.forward(joined, remote, audioLevelID(receiver))
	})
	connection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			self.mutex.Lock()
			isCurrent := self.participants[userId] == joined
			self.mutex.Unlock()
			if isCurrent && state == webrtc.PeerConnectionStateFailed {
				log.Infof("the device of %q on call %q lost its connection", userId, self.callId)
				self.leave(userId)
				self.server.mutex.Lock()
				deviceLost := self.server.deviceLost
				self.server.mutex.Unlock()
				if deviceLost != nil {
					go deviceLost(self.callId, userId)
				}
			}
		}
	})
	if err := connection.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offerSDP}); err != nil {
		_ = connection.Close()
		return "", fmt.Errorf("call: cannot read the offer: %w", err)
	}
	answer, err := connection.CreateAnswer(nil)
	if err != nil {
		_ = connection.Close()
		return "", err
	}
	answerSDP, err := setLocalAndGather(connection, answer)
	if err != nil {
		_ = connection.Close()
		return "", err
	}
	self.mutex.Lock()
	self.participants[userId] = joined
	// What the others already send goes to the new device too.
	var added bool
	for _, track := range self.forwarded {
		if track.fromUserId == userId {
			continue
		}
		if self.subscribe(joined, track) {
			added = true
		}
	}
	for _, video := range self.videos {
		if video.fromUserId != userId && self.watch(joined, video) {
			added = true
		}
	}
	self.chooseLayers()
	speaker := self.speakingUserId
	self.mutex.Unlock()
	if added {
		go self.negotiate(joined)
	}
	if shouldTellSpeaker && speaker != "" {
		go signal(Signal{SpeakingUserID: speaker})
	}
	return answerSDP, nil
}

// audioLevelID is the number the device gave the loudness header extension
// on its microphone, or 0 when it sends none.
func audioLevelID(receiver *webrtc.RTPReceiver) uint8 {
	if receiver == nil {
		return 0
	}
	for _, extension := range receiver.GetParameters().HeaderExtensions {
		if extension.URI == sdp.AudioLevelURI {
			return uint8(extension.ID)
		}
	}
	return 0
}

// setLocalAndGather sets the server's side and waits for its addresses, so
// the description it returns has them and nothing is sent after.
func setLocalAndGather(connection *webrtc.PeerConnection, description webrtc.SessionDescription) (string, error) {
	gathered := webrtc.GatheringCompletePromise(connection)
	if err := connection.SetLocalDescription(description); err != nil {
		return "", err
	}
	select {
	case <-gathered:
	case <-time.After(gatherWait):
	}
	local := connection.LocalDescription()
	if local == nil {
		return "", errors.New("call: no local description")
	}
	return local.SDP, nil
}

// subscribe adds a way out for a track to a device; the room's mutex is
// held. It says whether one was added.
func (self *room) subscribe(to *participant, track *forwardedTrack) bool {
	if _, ok := track.senders[to.userId]; ok {
		return false
	}
	sender, err := to.connection.AddTrack(track.local)
	if err != nil {
		log.Warningf("cannot send %q's %s to %q on call %q: %s", track.fromUserId, track.remote.Kind(), to.userId, self.callId, err)
		return false
	}
	track.senders[to.userId] = sender
	// Reports from the receiver are read so they do not pile up.
	go func() {
		for {
			if _, _, err := sender.ReadRTCP(); err != nil {
				return
			}
		}
	}()
	return true
}

// forward starts sending a device's microphone to everybody else, and reads
// it until it ends, noting how loud each packet is (by the header extension
// numbered levelId, 0 for none) to tell who is speaking.
func (self *room) forward(from *participant, remote *webrtc.TrackRemote, levelId uint8) {
	local, err := webrtc.NewTrackLocalStaticRTP(remote.Codec().RTPCodecCapability, remote.Kind().String(), from.userId)
	if err != nil {
		log.Warningf("cannot forward %q's %s on call %q: %s", from.userId, remote.Kind(), self.callId, err)
		return
	}
	track := &forwardedTrack{fromUserId: from.userId, remote: remote, local: local, senders: map[string]*webrtc.RTPSender{}, publisher: from.connection}
	self.mutex.Lock()
	self.forwarded = append(self.forwarded, track)
	var owed []*participant
	for userId, other := range self.participants {
		if userId == from.userId {
			continue
		}
		if self.subscribe(other, track) {
			owed = append(owed, other)
		}
	}
	self.mutex.Unlock()
	for _, other := range owed {
		go self.negotiate(other)
	}

	self.server.mutex.Lock()
	recordAudio := self.server.recordAudio
	self.server.mutex.Unlock()
	buffer := make([]byte, 1500)
	packet := &rtp.Packet{}
	for {
		read, _, err := remote.Read(buffer)
		if err != nil {
			break
		}
		if err := packet.Unmarshal(buffer[:read]); err != nil {
			continue
		}
		if levelId != 0 {
			if level := packet.GetExtension(levelId); len(level) > 0 {
				self.hearLevel(from.userId, level[0]&0x7f, time.Now())
			}
		}
		// The sending device's header extensions are numbered for its own
		// connection, not the receivers'.
		packet.Extension, packet.Extensions, packet.ExtensionProfile = false, nil, 0
		if err := local.WriteRTP(packet); err != nil && !errors.Is(err, io.ErrClosedPipe) {
			break
		}
		if recordAudio != nil {
			recordAudio(self.callId, from.userId, buffer[:read], remote.Codec())
		}
	}
	self.unforward(track)
}

// unforward takes an ended track away from everybody it went to.
func (self *room) unforward(track *forwardedTrack) {
	self.mutex.Lock()
	for index, each := range self.forwarded {
		if each == track {
			self.forwarded = append(self.forwarded[:index], self.forwarded[index+1:]...)
			break
		}
	}
	var owed []*participant
	for userId, sender := range track.senders {
		other := self.participants[userId]
		if other == nil {
			continue
		}
		if err := other.connection.RemoveTrack(sender); err == nil {
			owed = append(owed, other)
		}
	}
	self.mutex.Unlock()
	for _, other := range owed {
		go self.negotiate(other)
	}
}

// negotiate sends a device an offer for what changed, or notes that one is
// owed when an offer is already out.
func (self *room) negotiate(to *participant) {
	to.negotiationMutex.Lock()
	if to.negotiating {
		to.isNegotiationOwed = true
		to.negotiationMutex.Unlock()
		return
	}
	to.negotiating = true
	to.isNegotiationOwed = false
	to.negotiationMutex.Unlock()

	offer, err := to.connection.CreateOffer(nil)
	if err == nil {
		var offerSDP string
		if offerSDP, err = setLocalAndGather(to.connection, offer); err == nil {
			to.signal(Signal{OfferSDP: offerSDP})
			return
		}
	}
	log.Warningf("cannot offer the changes of call %q to %q: %s", self.callId, to.userId, err)
	to.negotiationMutex.Lock()
	to.negotiating = false
	to.negotiationMutex.Unlock()
}

func (self *room) answer(userId, answerSDP string) error {
	self.mutex.Lock()
	to := self.participants[userId]
	self.mutex.Unlock()
	if to == nil {
		return ErrNotOnCall
	}
	if err := to.connection.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answerSDP}); err != nil {
		return fmt.Errorf("call: cannot read the answer: %w", err)
	}
	to.negotiationMutex.Lock()
	to.negotiating = false
	isOwed := to.isNegotiationOwed
	to.negotiationMutex.Unlock()
	if isOwed {
		go self.negotiate(to)
	}
	return nil
}

func (self *room) leave(userId string) {
	self.mutex.Lock()
	left := self.participants[userId]
	delete(self.participants, userId)
	// What was sent to the device goes with it, so a device that joins again
	// is sent everything anew.
	for _, track := range self.forwarded {
		delete(track.senders, userId)
	}
	for _, video := range self.videos {
		video.mutex.Lock()
		delete(video.watchers, userId)
		video.listWatchers()
		video.mutex.Unlock()
	}
	if left != nil {
		if self.speakingUserId == userId {
			self.speakingUserId = ""
		}
		self.chooseLayers()
	}
	self.mutex.Unlock()
	if left != nil {
		self.forgetVoice(userId)
	}
	if left != nil {
		// Its tracks end as their reads fail, which takes them from the
		// others.
		_ = left.connection.Close()
	}
}

func (self *room) close() {
	self.closeOnce.Do(func() { close(self.done) })
	self.mutex.Lock()
	participants := self.participants
	self.participants = map[string]*participant{}
	self.mutex.Unlock()
	for _, each := range participants {
		_ = each.connection.Close()
	}
}
