import {
  ANSWER_CHAT_CALL_MEDIA,
  CHAT_CALL_SIGNALS,
  JOIN_CHAT_CALL_MEDIA,
  LEAVE_CHAT_CALL,
  SET_CHAT_CALL_MUTED,
  SET_CHAT_CALL_VIDEO,
} from './chatCallApi'

// One device on one call: the microphone and the camera it sends, and what it
// receives from the others, through the one peer connection it holds to the
// server's media server. docs/subsystems/chat-calls.md ("A device on a call")
// is the contract this follows step by step.
//
// Nothing here knows about React or the page: the GraphQL calls, the peer
// connection and the media devices are handed in, so the order in which
// offers are answered can be tested without a browser.

export type GraphqlRequest = <T>(query: string, variables?: Record<string, unknown>) => Promise<T>

export type GraphqlSubscribe = <T>(
  query: string,
  variables: Record<string, unknown>,
  onData: (data: T) => void,
  onEnd?: (error?: Error) => void,
) => () => void

// GATHER_TIMEOUT_MS is the longest a device waits for its own addresses to be
// gathered before sending what it has. The server is an ICE-lite agent and
// needs no candidates from a device, so waiting is for the sake of the
// browsers that put none in an offer until they are done; it must never hold
// a person back from a call.
export const GATHER_TIMEOUT_MS = 2000

// ChatCallParticipant is what has arrived from one other person: their
// microphone, and their camera while it is on. The stream a track arrives in
// is named for the person's user id by the server.
export interface ChatCallParticipant {
  audioStream: MediaStream | null
  videoStream: MediaStream | null
}

export type ChatCallPhase = 'joining' | 'connected' | 'closed'

export interface ChatCallSessionSnapshot {
  phase: ChatCallPhase
  // The microphone is on but sends silence.
  isMuted: boolean
  isVideoOn: boolean
  // More than one camera, and this device's is on: the switch is offered.
  canSwitchCamera: boolean
  // The connection to the media server dropped or failed. It comes back on its
  // own when the network does, or it does not and the person leaves.
  isConnectionLost: boolean
  // The microphone this device sends, as the browser names it; empty until
  // the microphone is open. The one marked in use in the audio menu.
  microphoneDeviceId: string
  // Where the sound of the others plays; empty is the system's default.
  // Held here, and not by each audio element, so that the audio element of a
  // person who arrives later plays where the others do.
  soundOutputDeviceId: string
  // What this device sends, for its own picture and its own level.
  localAudioStream: MediaStream | null
  localVideoStream: MediaStream | null
  // The others, by user id.
  participants: Record<string, ChatCallParticipant>
}

// ChatCallProblem is something that went wrong after the call began, told to
// whoever draws it.
export type ChatCallProblem = 'camera' | 'media'

export interface ChatCallSessionOptions {
  callId: string
  // Whether the camera is on from the start.
  isVideoOn: boolean
  graphql: GraphqlRequest
  subscribe: GraphqlSubscribe
  // A peer connection with no ICE servers: the media server is ICE-lite and
  // reachable on its public address.
  createPeerConnection: () => RTCPeerConnection
  mediaDevices: Pick<MediaDevices, 'getUserMedia' | 'enumerateDevices'> &
    Partial<Pick<MediaDevices, 'addEventListener' | 'removeEventListener'>>
  createMediaStream?: (tracks: MediaStreamTrack[]) => MediaStream
  gatherTimeoutMS?: number
  onProblem?: (problem: ChatCallProblem, caught: unknown) => void
}

// waitForGathering resolves when the connection has gathered its local
// addresses, or after the time given, whichever comes first.
export function waitForGathering(connection: RTCPeerConnection, timeoutMS: number): Promise<void> {
  if (connection.iceGatheringState === 'complete') return Promise.resolve()
  return new Promise((resolve) => {
    const done = () => {
      window.clearTimeout(timer)
      connection.removeEventListener('icegatheringstatechange', onChange)
      resolve()
    }
    const onChange = () => {
      if (connection.iceGatheringState === 'complete') done()
    }
    const timer = window.setTimeout(done, timeoutMS)
    connection.addEventListener('icegatheringstatechange', onChange)
  })
}

export class ChatCallSession {
  private readonly options: ChatCallSessionOptions
  private snapshot: ChatCallSessionSnapshot = {
    phase: 'joining',
    isMuted: false,
    isVideoOn: false,
    canSwitchCamera: false,
    isConnectionLost: false,
    microphoneDeviceId: '',
    soundOutputDeviceId: '',
    localAudioStream: null,
    localVideoStream: null,
    participants: {},
  }
  private readonly listeners = new Set<() => void>()

  private connection: RTCPeerConnection | null = null
  private unsubscribeSignals: (() => void) | null = null
  private audioTrack: MediaStreamTrack | null = null
  private audioSender: RTCRtpSender | null = null
  private videoTrack: MediaStreamTrack | null = null
  private videoSender: RTCRtpSender | null = null
  private videoInputCount = 0

  // The server's offers, in the order they came. An offer is answered only
  // once the answer to the join has been set, and each only after the one
  // before it was answered: a second offer made over a first that is still
  // open would be refused by the connection.
  private readonly offers: string[] = []
  private isJoinApplied = false
  private isAnsweringOffers = false

  private isClosed = false
  private isLeft = false
  private isChangingVideo = false
  private isChangingMicrophone = false
  private readonly receivedTracks = new Map<
    string,
    { audio: MediaStreamTrack | null; video: MediaStreamTrack | null }
  >()

  constructor(options: ChatCallSessionOptions) {
    this.options = options
  }

  // The two halves of useSyncExternalStore.
  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener)
    return () => {
      this.listeners.delete(listener)
    }
  }

  getSnapshot = (): ChatCallSessionSnapshot => this.snapshot

  private update(patch: Partial<ChatCallSessionSnapshot>) {
    this.snapshot = { ...this.snapshot, ...patch }
    this.listeners.forEach((listener) => listener())
  }

  private makeStream(tracks: MediaStreamTrack[]): MediaStream {
    return this.options.createMediaStream ? this.options.createMediaStream(tracks) : new MediaStream(tracks)
  }

  // start joins the call's media: the signals first, then the microphone, the
  // offer and the join. It throws when the device cannot be connected; the
  // session is then closed, and the caller says so and leaves the call.
  async start(): Promise<void> {
    const { callId, graphql, subscribe, mediaDevices } = this.options
    try {
      // The server's offers come on the signals, so they are opened before
      // anything is sent.
      this.unsubscribeSignals = subscribe<{ ChatCallSignals: { offerSdp: string } }>(
        CHAT_CALL_SIGNALS,
        { callId },
        (data) => this.queueOffer(data.ChatCallSignals.offerSdp),
        (error) => {
          if (error && !this.isClosed) this.options.onProblem?.('media', error)
        },
      )

      const microphone = await mediaDevices.getUserMedia({ audio: true })
      if (this.isClosed) {
        microphone.getTracks().forEach((track) => track.stop())
        return
      }
      this.audioTrack = microphone.getAudioTracks()[0] ?? null
      this.update({
        microphoneDeviceId: this.audioTrack?.getSettings().deviceId ?? '',
        localAudioStream: microphone,
      })

      const connection = this.options.createPeerConnection()
      this.connection = connection
      connection.addEventListener('track', (event) => this.receiveTrack(event as RTCTrackEvent))
      connection.addEventListener('connectionstatechange', () => {
        if (this.isClosed) return
        const state = connection.connectionState
        this.update({ isConnectionLost: state === 'failed' || state === 'disconnected' })
      })
      // The sender is kept: choosing another microphone swaps the track in
      // it, which needs no new offer.
      if (this.audioTrack) this.audioSender = connection.addTrack(this.audioTrack, microphone)
      // The camera has a seat from the start, empty while it is off, so
      // turning it on later is a track swapped in and not a new offer.
      this.videoSender = connection.addTransceiver('video', { direction: 'sendrecv' }).sender
      if (this.options.isVideoOn) {
        try {
          await this.openCameraTrack()
        } catch (caught) {
          // A call with a camera that will not open is still a call.
          this.options.onProblem?.('camera', caught)
        }
        if (this.isClosed) return
      }

      const offer = await connection.createOffer()
      await connection.setLocalDescription(offer)
      await waitForGathering(connection, this.options.gatherTimeoutMS ?? GATHER_TIMEOUT_MS)
      if (this.isClosed) return
      const joined = await graphql<{ JoinChatCallMedia: { answerSdp: string } }>(JOIN_CHAT_CALL_MEDIA, {
        callId,
        offerSdp: connection.localDescription?.sdp ?? offer.sdp ?? '',
      })
      if (this.isClosed) return
      await connection.setRemoteDescription({ type: 'answer', sdp: joined.JoinChatCallMedia.answerSdp })
      this.isJoinApplied = true
      this.update({ phase: 'connected' })

      mediaDevices.addEventListener?.('devicechange', this.onDeviceChange)
      void this.refreshCameraCount()
      // The others are told of a camera that is on from the first moment.
      if (this.videoTrack) await graphql(SET_CHAT_CALL_VIDEO, { callId, isVideoOn: true })
      if (this.snapshot.isMuted) await graphql(SET_CHAT_CALL_MUTED, { callId, isMuted: true })
      void this.answerOffers()
    } catch (caught) {
      this.close()
      throw caught
    }
  }

  private queueOffer(offerSdp: string) {
    if (this.isClosed) return
    this.offers.push(offerSdp)
    void this.answerOffers()
  }

  // answerOffers answers what is queued, one offer at a time and in order,
  // once the join's answer is set. Called again whenever either condition may
  // have changed; a second call while one is running leaves the work to it.
  private async answerOffers() {
    if (this.isAnsweringOffers || !this.isJoinApplied) return
    this.isAnsweringOffers = true
    try {
      while (!this.isClosed && this.offers.length > 0) {
        const offerSdp = this.offers.shift() as string
        try {
          await this.answerOffer(offerSdp)
        } catch (caught) {
          // The next offer is the server's way of trying again.
          if (!this.isClosed) this.options.onProblem?.('media', caught)
        }
      }
    } finally {
      this.isAnsweringOffers = false
    }
  }

  private async answerOffer(offerSdp: string) {
    const connection = this.connection
    if (!connection) return
    await connection.setRemoteDescription({ type: 'offer', sdp: offerSdp })
    const answer = await connection.createAnswer()
    await connection.setLocalDescription(answer)
    await waitForGathering(connection, this.options.gatherTimeoutMS ?? GATHER_TIMEOUT_MS)
    if (this.isClosed) return
    await this.options.graphql(ANSWER_CHAT_CALL_MEDIA, {
      callId: this.options.callId,
      answerSdp: connection.localDescription?.sdp ?? answer.sdp ?? '',
    })
  }

  // receiveTrack files a track under the person it belongs to: the stream it
  // arrives in is named for their user id, and its kind says audio or video.
  private receiveTrack(event: RTCTrackEvent) {
    const stream = event.streams[0]
    if (!stream || this.isClosed) return
    const userId = stream.id
    const track = event.track
    const received = this.receivedTracks.get(userId) ?? { audio: null, video: null }
    this.receivedTracks.set(userId, received)
    const current = this.snapshot.participants[userId] ?? { audioStream: null, videoStream: null }
    if (track.kind === 'audio') {
      received.audio = track
      this.setParticipant(userId, { ...current, audioStream: stream })
    } else {
      received.video = track
      this.setParticipant(userId, { ...current, videoStream: this.makeStream([track]) })
    }
    const drop = () => this.dropTrack(userId, track)
    track.addEventListener('ended', drop)
    stream.addEventListener('removetrack', (removed) => {
      if ((removed as MediaStreamTrackEvent).track === track) drop()
    })
  }

  private dropTrack(userId: string, track: MediaStreamTrack) {
    const received = this.receivedTracks.get(userId)
    const current = this.snapshot.participants[userId]
    if (!received || !current || this.isClosed) return
    if (received.audio === track) {
      received.audio = null
      this.setParticipant(userId, { ...current, audioStream: null })
    } else if (received.video === track) {
      received.video = null
      this.setParticipant(userId, { ...current, videoStream: null })
    }
  }

  private setParticipant(userId: string, participant: ChatCallParticipant) {
    this.update({ participants: { ...this.snapshot.participants, [userId]: participant } })
  }

  // setMuted stops the microphone's sound without closing it, so that being
  // heard again is instant and the others' connection is not renegotiated.
  // The others are told, to show it beside the name; the sound stops either
  // way, so a failure to tell them is not the person's to handle.
  setMuted(isMuted: boolean) {
    if (this.audioTrack) this.audioTrack.enabled = !isMuted
    this.update({ isMuted })
    if (this.isJoinApplied && !this.isClosed) {
      void this.options.graphql(SET_CHAT_CALL_MUTED, { callId: this.options.callId, isMuted }).catch(() => undefined)
    }
  }

  // setMicrophone moves the call to another microphone: the one chosen is
  // opened before the old one is let go of, so a microphone that will not open
  // leaves the call as it was. The track is swapped in the sender, which needs
  // no new offer, and it keeps whether the person is muted. An empty id is the
  // system's default microphone.
  async setMicrophone(deviceId: string): Promise<void> {
    if (this.isClosed || this.isChangingMicrophone || !this.audioSender) return
    this.isChangingMicrophone = true
    try {
      const stream = await this.options.mediaDevices.getUserMedia({
        audio: deviceId ? { deviceId: { exact: deviceId } } : true,
      })
      const track = stream.getAudioTracks()[0]
      if (!track) {
        stream.getTracks().forEach((each) => each.stop())
        throw new Error('no audio track')
      }
      if (this.isClosed) {
        track.stop()
        return
      }
      track.enabled = !this.snapshot.isMuted
      try {
        await this.audioSender.replaceTrack(track)
      } catch (caught) {
        track.stop()
        throw caught
      }
      const previous = this.audioTrack
      this.audioTrack = track
      previous?.stop()
      this.update({ microphoneDeviceId: track.getSettings().deviceId ?? deviceId, localAudioStream: stream })
    } finally {
      this.isChangingMicrophone = false
    }
  }

  // setSoundOutput chooses where the sound of the others plays. The session
  // only keeps the choice; each audio element of the call applies it.
  setSoundOutput(deviceId: string) {
    this.update({ soundOutputDeviceId: deviceId })
  }

  // listAudioDevices is the microphones and the loudspeakers of this device.
  // The browser names them only once the page has been allowed a microphone,
  // which a call has.
  async listAudioDevices(): Promise<{ microphones: MediaDeviceInfo[]; speakers: MediaDeviceInfo[] }> {
    const devices = await this.options.mediaDevices.enumerateDevices()
    return {
      microphones: devices.filter((device) => device.kind === 'audioinput'),
      speakers: devices.filter((device) => device.kind === 'audiooutput'),
    }
  }

  // watchDeviceChanges tells of a device plugged in or taken away, until the
  // function returned is called.
  watchDeviceChanges(onChange: () => void): () => void {
    const { mediaDevices } = this.options
    mediaDevices.addEventListener?.('devicechange', onChange)
    return () => mediaDevices.removeEventListener?.('devicechange', onChange)
  }

  // setVideo turns the camera on or off: the track in the video seat is
  // swapped, which needs no new offer, and the others are told.
  async setVideo(isVideoOn: boolean): Promise<void> {
    if (this.isClosed || this.isChangingVideo || isVideoOn === Boolean(this.videoTrack)) return
    this.isChangingVideo = true
    try {
      if (isVideoOn) {
        await this.openCameraTrack()
      } else {
        await this.closeCameraTrack()
      }
      try {
        await this.options.graphql(SET_CHAT_CALL_VIDEO, { callId: this.options.callId, isVideoOn })
      } catch (caught) {
        // The others still see what they saw, so this device does too.
        if (isVideoOn) await this.closeCameraTrack()
        throw caught
      }
      void this.refreshCameraCount()
    } finally {
      this.isChangingVideo = false
    }
  }

  // switchCamera moves to the next camera of the device. A phone that cannot
  // open two cameras at once has the first one let go before the second opens.
  async switchCamera(): Promise<void> {
    if (this.isClosed || this.isChangingVideo || !this.videoTrack) return
    this.isChangingVideo = true
    try {
      const cameras = (await this.options.mediaDevices.enumerateDevices()).filter(
        (device) => device.kind === 'videoinput' && device.deviceId,
      )
      if (cameras.length < 2) return
      const currentId = this.videoTrack.getSettings().deviceId
      const next = cameras[(cameras.findIndex((camera) => camera.deviceId === currentId) + 1) % cameras.length]
      this.videoTrack.stop()
      try {
        await this.openCameraTrack(next.deviceId)
      } catch (caught) {
        await this.openCameraTrack(currentId).catch(() => this.closeCameraTrack())
        throw caught
      }
    } finally {
      this.isChangingVideo = false
    }
  }

  private async openCameraTrack(deviceId?: string) {
    const stream = await this.options.mediaDevices.getUserMedia({
      video: deviceId ? { deviceId: { exact: deviceId } } : true,
    })
    const track = stream.getVideoTracks()[0]
    if (!track) throw new Error('no video track')
    if (this.isClosed) {
      track.stop()
      return
    }
    await this.videoSender?.replaceTrack(track)
    this.videoTrack = track
    // A camera taken away under the call (unplugged, or another program
    // took it) turns the camera off rather than leaving a frozen picture.
    track.addEventListener('ended', () => {
      if (this.videoTrack === track) {
        void this.setVideo(false).catch((caught) => this.options.onProblem?.('camera', caught))
      }
    })
    this.update({ isVideoOn: true, localVideoStream: this.makeStream([track]) })
  }

  private async closeCameraTrack() {
    const track = this.videoTrack
    this.videoTrack = null
    track?.stop()
    await this.videoSender?.replaceTrack(null)
    this.update({ isVideoOn: false, localVideoStream: null, canSwitchCamera: false })
  }

  private onDeviceChange = () => {
    void this.refreshCameraCount()
  }

  private async refreshCameraCount() {
    try {
      const devices = await this.options.mediaDevices.enumerateDevices()
      if (this.isClosed) return
      this.videoInputCount = devices.filter((device) => device.kind === 'videoinput').length
    } catch {
      this.videoInputCount = 0
    }
    this.update({ canSwitchCamera: this.videoInputCount > 1 && Boolean(this.videoTrack) })
  }

  // close lets go of everything this device holds, and does not tell the
  // server: for a call that the server has already ended.
  close() {
    if (this.isClosed) return
    this.isClosed = true
    this.unsubscribeSignals?.()
    this.unsubscribeSignals = null
    this.options.mediaDevices.removeEventListener?.('devicechange', this.onDeviceChange)
    this.audioTrack?.stop()
    this.videoTrack?.stop()
    this.audioTrack = null
    this.audioSender = null
    this.videoTrack = null
    this.connection?.close()
    this.offers.length = 0
    this.receivedTracks.clear()
    this.update({
      phase: 'closed',
      isVideoOn: false,
      canSwitchCamera: false,
      microphoneDeviceId: '',
      localAudioStream: null,
      localVideoStream: null,
      participants: {},
    })
  }

  // leave closes everything and takes the person off the call. Said once,
  // however many times it is asked.
  async leave(): Promise<void> {
    this.close()
    if (this.isLeft) return
    this.isLeft = true
    await this.options.graphql(LEAVE_CHAT_CALL, { callId: this.options.callId })
  }
}
