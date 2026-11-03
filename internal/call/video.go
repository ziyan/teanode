package call

import (
	"strings"
	"sync"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// Pictures go through the server in two sizes. A device sends its camera
// twice (simulcast), whole and as a small copy, each a layer named by its
// RTP stream id. Each person watching is sent one layer of each camera: the
// whole picture of the person they feature (whoever they chose, or else
// whoever is speaking) and the small copy of everybody else's, unless so few
// cameras are on that every picture can be whole. A device that sends one
// layer only, with no name, is sent as it is to everybody.

const (
	// layerWhole and layerSmall are the stream ids devices give the two
	// sizes of their camera.
	layerWhole = "f"
	layerSmall = "q"

	// frameTicks is a frame at 30 a second on the 90 kHz clock of video, the
	// gap left between the last packet of one layer and the first of the next
	// a person is switched to.
	frameTicks = 3000

	// pictureAskedEvery is how often a person waiting for the other layer
	// asks again for the whole picture it starts with, in case the request
	// was lost.
	pictureAskedEvery = time.Second
)

// wholePicturesMost is how many of the others' cameras a person is sent
// whole before only the featured one is. A variable for tests.
var wholePicturesMost = 3

// forwardedVideo is one person's camera, in the layers their device sends,
// going on to everybody else; each watcher has their own way out, since each
// may be sent another layer.
type forwardedVideo struct {
	fromUserId string
	publisher  *webrtc.PeerConnection
	codec      webrtc.RTPCodecCapability

	mutex    sync.RWMutex
	layers   map[string]*webrtc.TrackRemote
	watchers map[string]*videoWatcher
}

// videoWatcher is the way a camera goes to one person: the track the server
// sends them, the layer they should be sent, the layer they are sent, and
// what keeps the packets they get in one unbroken sequence when the layer
// changes under them.
type videoWatcher struct {
	userId string
	local  *webrtc.TrackLocalStaticRTP
	sender *webrtc.RTPSender

	mutex           sync.Mutex
	hasWanted       bool
	wantedLayer     string
	sentLayer       string
	isLayerSent     bool
	hasSent         bool
	lastSequence    uint16
	lastTimestamp   uint32
	sequenceOffset  uint16
	timestampOffset uint32
	pictureAskedAt  time.Time
}

// layerOf is the layer a layer's track carries: its stream id, or none for
// a device that sends one.
func layerOf(remote *webrtc.TrackRemote) string {
	return strings.TrimSpace(remote.RID())
}

// availableLayer is the layer to send of those the camera has, the one
// wanted when it has it; the video's mutex is held.
func (self *forwardedVideo) availableLayer(wanted string) (string, bool) {
	if _, ok := self.layers[wanted]; ok {
		return wanted, true
	}
	// The other named layer, or the one a device with a single layer sends.
	for _, each := range []string{layerSmall, layerWhole, ""} {
		if _, ok := self.layers[each]; ok {
			return each, true
		}
	}
	for each := range self.layers {
		return each, true
	}
	return "", false
}

// want sets the layer a watcher should be sent, and asks the camera for the
// whole picture that layer starts with when it changes.
func (self *forwardedVideo) want(watcher *videoWatcher, layer string) {
	self.mutex.RLock()
	available, ok := self.availableLayer(layer)
	remote := self.layers[available]
	self.mutex.RUnlock()
	if !ok {
		return
	}
	watcher.mutex.Lock()
	isNewlyWanted := !watcher.hasWanted || watcher.wantedLayer != available
	watcher.wantedLayer, watcher.hasWanted = available, true
	isWaiting := !watcher.isLayerSent || watcher.sentLayer != available
	shouldAsk := isNewlyWanted && isWaiting
	if shouldAsk {
		watcher.pictureAskedAt = time.Now()
	}
	watcher.mutex.Unlock()
	if shouldAsk && remote != nil {
		self.askPicture(remote)
	}
}

// askPicture asks the camera for a whole picture of one layer.
func (self *forwardedVideo) askPicture(remote *webrtc.TrackRemote) {
	_ = self.publisher.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(remote.SSRC())}})
}

// askPictureFor asks again for the whole picture a watcher waits for, or
// for the layer it is sent when it lost one.
func (self *forwardedVideo) askPictureFor(watcher *videoWatcher) {
	watcher.mutex.Lock()
	layer := watcher.wantedLayer
	watcher.pictureAskedAt = time.Now()
	watcher.mutex.Unlock()
	self.mutex.RLock()
	remote := self.layers[layer]
	self.mutex.RUnlock()
	if remote != nil {
		self.askPicture(remote)
	}
}

// askPicturesStillWaited asks again for the pictures watchers have waited
// for longer than pictureAskedEvery.
func (self *forwardedVideo) askPicturesStillWaited(now time.Time) {
	self.mutex.RLock()
	watchers := make([]*videoWatcher, 0, len(self.watchers))
	for _, watcher := range self.watchers {
		watchers = append(watchers, watcher)
	}
	self.mutex.RUnlock()
	for _, watcher := range watchers {
		watcher.mutex.Lock()
		isWaiting := (!watcher.isLayerSent || watcher.sentLayer != watcher.wantedLayer) && now.Sub(watcher.pictureAskedAt) >= pictureAskedEvery
		watcher.mutex.Unlock()
		if isWaiting {
			self.askPictureFor(watcher)
		}
	}
}

// send passes a packet of one layer to everybody who is sent that layer, and
// moves anybody who wants it over to it when the packet starts a whole
// picture.
func (self *forwardedVideo) send(layer string, packet *rtp.Packet) {
	isPictureStart := isWholePictureStart(self.codec.MimeType, packet.Payload)
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	for _, watcher := range self.watchers {
		outgoing, ok := watcher.rewrite(layer, packet, isPictureStart)
		if !ok {
			continue
		}
		_ = watcher.local.WriteRTP(outgoing)
	}
}

// rewrite is the packet as the watcher is sent it, or false when it is not
// sent this layer. The sequence numbers and times carry on from the last
// packet sent, whichever layer that came from, so a switch looks to the
// watcher's device like the next frame.
func (self *videoWatcher) rewrite(layer string, packet *rtp.Packet, isPictureStart bool) (*rtp.Packet, bool) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if !self.isLayerSent || self.sentLayer != layer {
		if self.wantedLayer != layer || !isPictureStart {
			return nil, false
		}
		if self.hasSent {
			self.sequenceOffset = self.lastSequence + 1 - packet.SequenceNumber
			self.timestampOffset = self.lastTimestamp + frameTicks - packet.Timestamp
		} else {
			self.sequenceOffset, self.timestampOffset = 0, 0
		}
		self.sentLayer, self.isLayerSent = layer, true
	}
	header := packet.Header
	header.SequenceNumber = packet.SequenceNumber + self.sequenceOffset
	header.Timestamp = packet.Timestamp + self.timestampOffset
	// The sending device's header extensions are numbered for its own
	// connection, not the watcher's.
	header.Extension, header.Extensions, header.ExtensionProfile = false, nil, 0
	self.lastSequence, self.lastTimestamp, self.hasSent = header.SequenceNumber, header.Timestamp, true
	return &rtp.Packet{Header: header, Payload: packet.Payload}, true
}

// isWholePictureStart says whether a packet of video begins a picture that
// needs none before it (a key frame), where a watcher can be switched to its
// layer. The codecs a device may send are VP8 and H.264 (see New).
func isWholePictureStart(mimeType string, payload []byte) bool {
	switch {
	case strings.EqualFold(mimeType, webrtc.MimeTypeVP8):
		return isVP8KeyFrameStart(payload)
	case strings.EqualFold(mimeType, webrtc.MimeTypeH264):
		return isH264KeyFrameStart(payload)
	}
	return false
}

// isVP8KeyFrameStart reads the VP8 payload descriptor (RFC 7741): the first
// packet of a frame has S set and partition 0, and the frame header after
// the descriptor has the inverse key frame bit clear.
func isVP8KeyFrameStart(payload []byte) bool {
	if len(payload) < 1 {
		return false
	}
	descriptor := payload[0]
	isStart := descriptor&0x10 != 0
	partition := descriptor & 0x07
	if !isStart || partition != 0 {
		return false
	}
	index := 1
	if descriptor&0x80 != 0 {
		if len(payload) <= index {
			return false
		}
		extension := payload[index]
		index++
		if extension&0x80 != 0 {
			if len(payload) <= index {
				return false
			}
			if payload[index]&0x80 != 0 {
				index += 2
			} else {
				index++
			}
		}
		if extension&0x40 != 0 {
			index++
		}
		if extension&0x30 != 0 {
			index++
		}
	}
	if len(payload) <= index {
		return false
	}
	return payload[index]&0x01 == 0
}

// H.264 NAL unit types (RFC 6184) that matter here.
const (
	h264TypeIDR           = 5
	h264TypeSPS           = 7
	h264TypeAggregated    = 24
	h264TypeFragmentation = 28
)

// isH264KeyFrameStart says whether a packet starts an IDR picture: the
// sequence parameters before it, or the IDR slice itself, alone, in an
// aggregation packet, or as the first fragment.
func isH264KeyFrameStart(payload []byte) bool {
	if len(payload) < 1 {
		return false
	}
	switch payload[0] & 0x1f {
	case h264TypeIDR, h264TypeSPS:
		return true
	case h264TypeAggregated:
		for index := 1; index+2 < len(payload); {
			size := int(payload[index])<<8 | int(payload[index+1])
			index += 2
			if index >= len(payload) {
				return false
			}
			switch payload[index] & 0x1f {
			case h264TypeIDR, h264TypeSPS:
				return true
			}
			index += size
		}
	case h264TypeFragmentation:
		if len(payload) < 2 {
			return false
		}
		isFirst := payload[1]&0x80 != 0
		nalType := payload[1] & 0x1f
		return isFirst && (nalType == h264TypeIDR || nalType == h264TypeSPS)
	}
	return false
}

// forwardVideo takes one layer of a person's camera: the first starts
// sending the camera to everybody else, and each is read until it ends.
func (self *room) forwardVideo(from *participant, remote *webrtc.TrackRemote) {
	layer := layerOf(remote)
	self.mutex.Lock()
	var video *forwardedVideo
	for _, each := range self.videos {
		if each.fromUserId == from.userId && each.publisher == from.connection {
			video = each
		}
	}
	var owed []*participant
	if video == nil {
		video = &forwardedVideo{
			fromUserId: from.userId, publisher: from.connection, codec: remote.Codec().RTPCodecCapability,
			layers: map[string]*webrtc.TrackRemote{}, watchers: map[string]*videoWatcher{},
		}
		self.videos = append(self.videos, video)
		for userId, other := range self.participants {
			if userId != from.userId && self.watch(other, video) {
				owed = append(owed, other)
			}
		}
	}
	video.mutex.Lock()
	video.layers[layer] = remote
	video.mutex.Unlock()
	self.chooseLayers()
	self.mutex.Unlock()
	for _, other := range owed {
		go self.negotiate(other)
	}

	for {
		packet, _, err := remote.ReadRTP()
		if err != nil {
			break
		}
		video.send(layer, packet)
	}

	self.mutex.Lock()
	video.mutex.Lock()
	if video.layers[layer] == remote {
		delete(video.layers, layer)
	}
	isGone := len(video.layers) == 0
	video.mutex.Unlock()
	self.mutex.Unlock()
	if isGone {
		self.unforwardVideo(video)
	} else {
		self.mutex.Lock()
		self.chooseLayers()
		self.mutex.Unlock()
	}
}

// watch adds a way out for a camera to a person; the room's mutex is held.
// It says whether one was added, for an offer to the person.
func (self *room) watch(to *participant, video *forwardedVideo) bool {
	video.mutex.Lock()
	if _, ok := video.watchers[to.userId]; ok {
		video.mutex.Unlock()
		return false
	}
	video.mutex.Unlock()
	local, err := webrtc.NewTrackLocalStaticRTP(video.codec, "video", video.fromUserId)
	if err != nil {
		log.Warningf("cannot send %q's camera to %q on call %q: %s", video.fromUserId, to.userId, self.callId, err)
		return false
	}
	sender, err := to.connection.AddTrack(local)
	if err != nil {
		log.Warningf("cannot send %q's camera to %q on call %q: %s", video.fromUserId, to.userId, self.callId, err)
		return false
	}
	watcher := &videoWatcher{userId: to.userId, local: local, sender: sender}
	video.mutex.Lock()
	video.watchers[to.userId] = watcher
	video.mutex.Unlock()
	// A picture the watcher's device lost is asked for again.
	go func() {
		for {
			packets, _, err := sender.ReadRTCP()
			if err != nil {
				return
			}
			for _, packet := range packets {
				switch packet.(type) {
				case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
					video.askPictureFor(watcher)
				}
			}
		}
	}()
	return true
}

// unforwardVideo takes an ended camera away from everybody it went to.
func (self *room) unforwardVideo(video *forwardedVideo) {
	self.mutex.Lock()
	for index, each := range self.videos {
		if each == video {
			self.videos = append(self.videos[:index], self.videos[index+1:]...)
			break
		}
	}
	video.mutex.Lock()
	watchers := video.watchers
	video.watchers = map[string]*videoWatcher{}
	video.mutex.Unlock()
	var owed []*participant
	for userId, watcher := range watchers {
		other := self.participants[userId]
		if other == nil {
			continue
		}
		if err := other.connection.RemoveTrack(watcher.sender); err == nil {
			owed = append(owed, other)
		}
	}
	self.chooseLayers()
	self.mutex.Unlock()
	for _, other := range owed {
		go self.negotiate(other)
	}
}

// chooseLayers sets the layer of every camera each person is sent: whole
// for the person they feature, and for everybody while few cameras are on;
// the small copy otherwise. The room's mutex is held.
func (self *room) chooseLayers() {
	for _, watching := range self.participants {
		featured := self.featuredFor(watching)
		cameras := 0
		for _, video := range self.videos {
			if video.fromUserId != watching.userId {
				cameras++
			}
		}
		for _, video := range self.videos {
			video.mutex.RLock()
			watcher := video.watchers[watching.userId]
			video.mutex.RUnlock()
			if watcher == nil {
				continue
			}
			layer := layerSmall
			if cameras <= wholePicturesMost || video.fromUserId == featured {
				layer = layerWhole
			}
			video.want(watcher, layer)
		}
	}
}

// featuredFor is whose camera a person is sent whole: the one they chose,
// or else whoever is speaking, or spoke last before them when it is them.
// The room's mutex is held.
func (self *room) featuredFor(watching *participant) string {
	if watching.featuredUserId != "" {
		return watching.featuredUserId
	}
	if self.speakingUserId != "" && self.speakingUserId != watching.userId {
		return self.speakingUserId
	}
	if self.previousSpeakingUserId != watching.userId {
		return self.previousSpeakingUserId
	}
	return ""
}
