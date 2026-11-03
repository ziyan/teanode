// Package call is the media server of calls in chats: everybody on a call
// sends their microphone, and their camera when it is on, to the server once,
// and the server forwards each of those to everybody else on the call without
// mixing it (a selective forwarding unit). A phone sends one stream however
// many people are on the call.
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
	pionlogging "github.com/pion/logging"
	"github.com/pion/rtcp"
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
// answer. One can arrive before the device has read the server's answer to
// its join, so a device takes its join and then each offer one at a time, in
// that order.
type Signal struct {
	OfferSDP string
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
	media := &webrtc.MediaEngine{}
	if err := media.RegisterDefaultCodecs(); err != nil {
		_ = udpConn.Close()
		_ = tcpListener.Close()
		return nil, err
	}
	return &Server{
		api:         webrtc.NewAPI(webrtc.WithSettingEngine(engine), webrtc.WithMediaEngine(media)),
		udpConn:     udpConn,
		tcpListener: tcpListener,
		rooms:       map[string]*room{},
	}, nil
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
		found = &room{server: self, callId: callId, participants: map[string]*participant{}}
		self.rooms[callId] = found
	}
	return found, nil
}

// Join puts a device on a call with its offer, and returns the server's
// answer. A device already on it for the person is replaced: the person
// moved to another device, or this one connected again.
func (self *Server) Join(callId, userId, offerSDP string, signal func(Signal)) (string, error) {
	room, err := self.roomOf(callId, true)
	if err != nil {
		return "", err
	}
	return room.join(userId, offerSDP, signal)
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
}

// participant is one person's device on a call.
type participant struct {
	userId     string
	connection *webrtc.PeerConnection
	signal     func(Signal)

	// negotiating says an offer went out that has not been answered, and
	// isNegotiationOwed that another change came meanwhile, for one more
	// offer once it is.
	negotiationMutex  sync.Mutex
	negotiating       bool
	isNegotiationOwed bool
}

// forwardedTrack is one person's microphone or camera, sent on to the
// others; senders are the ways out, by whose device each goes to.
type forwardedTrack struct {
	fromUserId string
	remote     *webrtc.TrackRemote
	local      *webrtc.TrackLocalStaticRTP
	senders    map[string]*webrtc.RTPSender
	// publisher is the device it comes from, asked for a whole picture when
	// somebody starts to watch.
	publisher *webrtc.PeerConnection
}

func (self *room) join(userId, offerSDP string, signal func(Signal)) (string, error) {
	self.leave(userId)
	connection, err := self.server.api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return "", err
	}
	joined := &participant{userId: userId, connection: connection, signal: signal}
	connection.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		self.forward(joined, remote)
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
	self.mutex.Unlock()
	if added {
		go self.negotiate(joined)
	}
	return answerSDP, nil
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
	// What the receiver asks for (a whole picture, a resend) goes back to
	// whoever sends it.
	go self.relayFeedback(sender, track)
	if track.remote.Kind() == webrtc.RTPCodecTypeVideo {
		requestPicture(track)
	}
	return true
}

// relayFeedback reads what a receiving device says about a forwarded track,
// and asks the sending device for a whole picture when the receiver lost
// one.
func (self *room) relayFeedback(sender *webrtc.RTPSender, track *forwardedTrack) {
	for {
		packets, _, err := sender.ReadRTCP()
		if err != nil {
			return
		}
		for _, packet := range packets {
			switch packet.(type) {
			case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
				requestPicture(track)
			}
		}
	}
}

func requestPicture(track *forwardedTrack) {
	_ = track.publisher.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(track.remote.SSRC())}})
}

// forward starts sending a device's new track to everybody else, and reads
// it until it ends.
func (self *room) forward(from *participant, remote *webrtc.TrackRemote) {
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
	isAudio := remote.Kind() == webrtc.RTPCodecTypeAudio
	buffer := make([]byte, 1500)
	for {
		read, _, err := remote.Read(buffer)
		if err != nil {
			break
		}
		if _, err := local.Write(buffer[:read]); err != nil && !errors.Is(err, io.ErrClosedPipe) {
			break
		}
		if isAudio && recordAudio != nil {
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
	self.mutex.Unlock()
	if left != nil {
		// Its tracks end as their reads fail, which takes them from the
		// others.
		_ = left.connection.Close()
	}
}

func (self *room) close() {
	self.mutex.Lock()
	participants := self.participants
	self.participants = map[string]*participant{}
	self.mutex.Unlock()
	for _, each := range participants {
		_ = each.connection.Close()
	}
}
