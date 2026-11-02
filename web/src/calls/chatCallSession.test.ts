import { afterEach, describe, expect, it, vi } from 'vitest'

import { ChatCallSession, waitForGathering, type ChatCallSessionOptions } from './chatCallSession'

// A deferred answer: the test decides when the server replies.
function deferred<T>() {
  let resolve: (value: T) => void = () => {}
  let reject: (caught: unknown) => void = () => {}
  const promise = new Promise<T>((resolveWith, rejectWith) => {
    resolve = resolveWith
    reject = rejectWith
  })
  return { promise, resolve, reject }
}

function fakeTrack(kind: 'audio' | 'video', deviceId = 'camera-front') {
  const listeners: Record<string, (() => void)[]> = {}
  return {
    kind,
    enabled: true,
    stop: vi.fn(),
    getSettings: () => ({ deviceId }),
    addEventListener: (name: string, listener: () => void) => {
      listeners[name] = [...(listeners[name] ?? []), listener]
    },
    fire: (name: string) => (listeners[name] ?? []).forEach((listener) => listener()),
  }
}

type FakeTrack = ReturnType<typeof fakeTrack>

// A peer connection that keeps a log of what was done to it, in order, and
// takes as long over setting a description as the test lets it.
class FakePeerConnection {
  iceGatheringState = 'complete'
  connectionState = 'new'
  localDescription: { type: string; sdp: string } | null = null
  log: string[] = []
  listeners: Record<string, ((event: unknown) => void)[]> = {}
  sender = { replaceTrack: vi.fn(async () => {}) }
  audioSender = { replaceTrack: vi.fn(async () => {}) }
  answerCount = 0
  close = vi.fn()

  addEventListener(name: string, listener: (event: unknown) => void) {
    this.listeners[name] = [...(this.listeners[name] ?? []), listener]
  }
  removeEventListener() {}
  fire(name: string, event: unknown) {
    ;(this.listeners[name] ?? []).forEach((listener) => listener(event))
  }
  addTrack() {
    this.log.push('addTrack')
    return this.audioSender
  }
  addTransceiver(kind: string, init: { direction: string }) {
    this.log.push(`addTransceiver ${kind} ${init.direction}`)
    return { sender: this.sender }
  }
  async createOffer() {
    return { type: 'offer', sdp: 'device-offer' }
  }
  async createAnswer() {
    this.answerCount += 1
    return { type: 'answer', sdp: `device-answer-${this.answerCount}` }
  }
  async setLocalDescription(description: { type: string; sdp: string }) {
    this.log.push(`local ${description.sdp}`)
    this.localDescription = description
  }
  async setRemoteDescription(description: { type: string; sdp: string }) {
    this.log.push(`remote ${description.type} ${description.sdp}`)
  }
}

interface Harness {
  session: ChatCallSession
  connection: FakePeerConnection
  graphql: ReturnType<typeof vi.fn>
  subscribe: ReturnType<typeof vi.fn>
  joinAnswer: ReturnType<typeof deferred<{ JoinChatCallMedia: { answerSdp: string } }>>
  answers: ReturnType<typeof deferred<boolean>>[]
  signal: (offerSdp: string) => void
  microphone: FakeTrack
  // The microphones opened by choosing one, in order.
  chosenMicrophones: FakeTrack[]
  cameras: FakeTrack[]
  problems: unknown[]
}

// build wires a session to fakes. Every AnswerChatCallMedia waits on a
// deferred of its own, and JoinChatCallMedia on one for the join.
function build(options: Partial<ChatCallSessionOptions> = {}, cameraCount = 1): Harness {
  const connection = new FakePeerConnection()
  const joinAnswer = deferred<{ JoinChatCallMedia: { answerSdp: string } }>()
  const answers: ReturnType<typeof deferred<boolean>>[] = []
  const graphql = vi.fn(async (query: string) => {
    if (query.includes('JoinChatCallMedia')) return joinAnswer.promise
    if (query.includes('AnswerChatCallMedia')) {
      const answer = deferred<boolean>()
      answers.push(answer)
      return answer.promise
    }
    return {}
  })
  let onSignal: (data: { ChatCallSignals: { offerSdp: string } }) => void = () => {}
  const subscribe = vi.fn((_query: string, _variables: unknown, onData: typeof onSignal) => {
    onSignal = onData
    return () => {}
  })
  const microphone = fakeTrack('audio', 'microphone-built-in')
  const chosenMicrophones: FakeTrack[] = []
  const cameras: FakeTrack[] = []
  const problems: unknown[] = []
  const session = new ChatCallSession({
    callId: 'call-one',
    isVideoOn: false,
    graphql: graphql as unknown as ChatCallSessionOptions['graphql'],
    subscribe: subscribe as unknown as ChatCallSessionOptions['subscribe'],
    createPeerConnection: () => connection as unknown as RTCPeerConnection,
    mediaDevices: {
      getUserMedia: vi.fn(async (constraints: MediaStreamConstraints) => {
        if (constraints.video) {
          const camera = fakeTrack('video')
          cameras.push(camera)
          return { getVideoTracks: () => [camera], getAudioTracks: () => [], getTracks: () => [camera] }
        }
        const audio = constraints.audio
        if (typeof audio === 'object' && audio.deviceId) {
          const chosen = fakeTrack('audio', (audio.deviceId as { exact: string }).exact)
          chosenMicrophones.push(chosen)
          return { getAudioTracks: () => [chosen], getVideoTracks: () => [], getTracks: () => [chosen] }
        }
        return { getAudioTracks: () => [microphone], getVideoTracks: () => [], getTracks: () => [microphone] }
      }) as unknown as MediaDevices['getUserMedia'],
      enumerateDevices: vi.fn(async () => [
        ...Array.from({ length: cameraCount }, (_, index) => ({ kind: 'videoinput', deviceId: `camera-${index}` })),
        { kind: 'audioinput', deviceId: 'microphone-built-in' },
        { kind: 'audioinput', deviceId: 'microphone-headset' },
        { kind: 'audiooutput', deviceId: 'speaker-headset' },
      ]) as unknown as MediaDevices['enumerateDevices'],
    },
    createMediaStream: (tracks) => ({ tracks }) as unknown as MediaStream,
    onProblem: (_problem, caught) => problems.push(caught),
    ...options,
  })
  return {
    session,
    connection,
    graphql,
    subscribe,
    joinAnswer,
    answers,
    signal: (offerSdp) => onSignal({ ChatCallSignals: { offerSdp } }),
    microphone,
    chosenMicrophones,
    cameras,
    problems,
  }
}

afterEach(() => {
  vi.useRealTimers()
})

describe('a device joining a call', () => {
  it('opens the signals before it sends the offer', async () => {
    const harness = build()
    const started = harness.session.start()
    await vi.waitFor(() => expect(harness.graphql).toHaveBeenCalled())
    expect(harness.subscribe.mock.invocationCallOrder[0]).toBeLessThan(harness.graphql.mock.invocationCallOrder[0])
    expect(harness.graphql.mock.calls[0][1]).toEqual({ callId: 'call-one', offerSdp: 'device-offer' })
    harness.joinAnswer.resolve({ JoinChatCallMedia: { answerSdp: 'server-answer' } })
    await started
    expect(harness.session.getSnapshot().phase).toBe('connected')
  })

  it('adds the microphone and an empty video seat that sends and receives', async () => {
    const harness = build()
    const started = harness.session.start()
    harness.joinAnswer.resolve({ JoinChatCallMedia: { answerSdp: 'server-answer' } })
    await started
    expect(harness.connection.log.slice(0, 2)).toEqual(['addTrack', 'addTransceiver video sendrecv'])
    expect(harness.connection.sender.replaceTrack).not.toHaveBeenCalled()
    expect(harness.session.getSnapshot().microphoneDeviceId).toBe('microphone-built-in')
  })

  it('holds an offer that arrives before the answer to the join is set', async () => {
    const harness = build()
    const started = harness.session.start()
    await vi.waitFor(() => expect(harness.graphql).toHaveBeenCalled())
    harness.signal('server-offer-1')
    // Nothing is answered while the join is open.
    await new Promise((resolve) => setTimeout(resolve, 20))
    expect(harness.connection.log.some((entry) => entry.startsWith('remote'))).toBe(false)

    harness.joinAnswer.resolve({ JoinChatCallMedia: { answerSdp: 'server-answer' } })
    await started
    await vi.waitFor(() => expect(harness.answers).toHaveLength(1))
    // The join's answer first, then the offer that waited.
    const remote = harness.connection.log.filter((entry) => entry.startsWith('remote'))
    expect(remote).toEqual(['remote answer server-answer', 'remote offer server-offer-1'])
    expect(harness.graphql).toHaveBeenLastCalledWith(expect.stringContaining('AnswerChatCallMedia'), {
      callId: 'call-one',
      answerSdp: 'device-answer-1',
    })
  })

  it('answers offers one at a time, in the order they came', async () => {
    const harness = build()
    const started = harness.session.start()
    harness.joinAnswer.resolve({ JoinChatCallMedia: { answerSdp: 'server-answer' } })
    await started

    harness.signal('server-offer-1')
    harness.signal('server-offer-2')
    harness.signal('server-offer-3')
    await vi.waitFor(() => expect(harness.answers).toHaveLength(1))
    // The first answer is still on its way: the others are not touched.
    await new Promise((resolve) => setTimeout(resolve, 20))
    expect(harness.connection.log.filter((entry) => entry.startsWith('remote offer'))).toEqual([
      'remote offer server-offer-1',
    ])

    harness.answers[0].resolve(true)
    await vi.waitFor(() => expect(harness.answers).toHaveLength(2))
    expect(harness.connection.log.filter((entry) => entry.startsWith('remote offer'))).toEqual([
      'remote offer server-offer-1',
      'remote offer server-offer-2',
    ])

    harness.answers[1].resolve(true)
    await vi.waitFor(() => expect(harness.answers).toHaveLength(3))
    harness.answers[2].resolve(true)

    const sent = harness.graphql.mock.calls
      .filter(([query]) => String(query).includes('AnswerChatCallMedia'))
      .map(([, variables]) => (variables as { answerSdp: string }).answerSdp)
    expect(sent).toEqual(['device-answer-1', 'device-answer-2', 'device-answer-3'])
  })

  it('goes on to the next offer when one cannot be answered', async () => {
    const harness = build()
    const started = harness.session.start()
    harness.joinAnswer.resolve({ JoinChatCallMedia: { answerSdp: 'server-answer' } })
    await started

    harness.signal('server-offer-1')
    harness.signal('server-offer-2')
    await vi.waitFor(() => expect(harness.answers).toHaveLength(1))
    harness.answers[0].reject(new Error('refused'))
    await vi.waitFor(() => expect(harness.answers).toHaveLength(2))
    expect(harness.problems).toHaveLength(1)
  })

  it('closes and rethrows when the join is refused', async () => {
    const harness = build()
    const started = harness.session.start()
    await vi.waitFor(() => expect(harness.graphql).toHaveBeenCalled())
    harness.joinAnswer.reject(new Error('calls are off'))
    await expect(started).rejects.toThrow('calls are off')
    expect(harness.session.getSnapshot().phase).toBe('closed')
    expect(harness.connection.close).toHaveBeenCalled()
    expect(harness.microphone.stop).toHaveBeenCalled()
  })
})

describe('what arrives from the others', () => {
  const arrive = (harness: Harness, userId: string, track: FakeTrack) => {
    const stream = { id: userId, addEventListener: vi.fn() }
    harness.connection.fire('track', { track, streams: [stream] })
    return stream
  }

  it('files a track under the person named by its stream', async () => {
    const harness = build()
    const started = harness.session.start()
    harness.joinAnswer.resolve({ JoinChatCallMedia: { answerSdp: 'server-answer' } })
    await started

    const voice = arrive(harness, 'user-mira', fakeTrack('audio'))
    const picture = fakeTrack('video')
    arrive(harness, 'user-mira', picture)
    arrive(harness, 'user-tobias', fakeTrack('audio'))

    const { participants } = harness.session.getSnapshot()
    expect(Object.keys(participants).sort()).toEqual(['user-mira', 'user-tobias'])
    expect(participants['user-mira'].audioStream).toBe(voice)
    expect(participants['user-mira'].videoStream).toEqual({ tracks: [picture] })
    expect(participants['user-tobias'].videoStream).toBeNull()
  })

  it('drops a track that ends', async () => {
    const harness = build()
    const started = harness.session.start()
    harness.joinAnswer.resolve({ JoinChatCallMedia: { answerSdp: 'server-answer' } })
    await started

    const picture = fakeTrack('video')
    arrive(harness, 'user-mira', fakeTrack('audio'))
    arrive(harness, 'user-mira', picture)
    picture.fire('ended')

    const { participants } = harness.session.getSnapshot()
    expect(participants['user-mira'].videoStream).toBeNull()
    expect(participants['user-mira'].audioStream).not.toBeNull()
  })
})

describe('the controls of a call', () => {
  const connected = async (harness: Harness) => {
    const started = harness.session.start()
    harness.joinAnswer.resolve({ JoinChatCallMedia: { answerSdp: 'server-answer' } })
    await started
  }

  it('mutes by disabling the microphone track', async () => {
    const harness = build()
    await connected(harness)
    harness.session.setMuted(true)
    expect(harness.microphone.enabled).toBe(false)
    expect(harness.session.getSnapshot().isMuted).toBe(true)
    harness.session.setMuted(false)
    expect(harness.microphone.enabled).toBe(true)
  })

  it('moves to another microphone by swapping the track, with no new offer', async () => {
    const harness = build()
    await connected(harness)
    const offersBefore = harness.connection.log.filter((entry) => entry.startsWith('local')).length

    await harness.session.setMicrophone('microphone-headset')

    const [headset] = harness.chosenMicrophones
    expect(harness.connection.audioSender.replaceTrack).toHaveBeenCalledWith(headset)
    expect(harness.microphone.stop).toHaveBeenCalled()
    expect(headset.stop).not.toHaveBeenCalled()
    expect(harness.session.getSnapshot().microphoneDeviceId).toBe('microphone-headset')
    expect(harness.session.getSnapshot().localAudioStream?.getAudioTracks()[0]).toBe(headset)
    expect(harness.connection.log.filter((entry) => entry.startsWith('local'))).toHaveLength(offersBefore)
  })

  it('keeps the person muted on the new microphone', async () => {
    const harness = build()
    await connected(harness)
    harness.session.setMuted(true)

    await harness.session.setMicrophone('microphone-headset')
    expect(harness.chosenMicrophones[0].enabled).toBe(false)
    expect(harness.session.getSnapshot().isMuted).toBe(true)

    // And unmuting reaches the microphone now in use, not the one let go.
    harness.session.setMuted(false)
    expect(harness.chosenMicrophones[0].enabled).toBe(true)
  })

  it('keeps the old microphone when the new one cannot be opened or swapped in', async () => {
    const harness = build()
    await connected(harness)

    harness.connection.audioSender.replaceTrack.mockRejectedValueOnce(new Error('refused'))
    await expect(harness.session.setMicrophone('microphone-headset')).rejects.toThrow('refused')
    expect(harness.chosenMicrophones[0].stop).toHaveBeenCalled()
    expect(harness.microphone.stop).not.toHaveBeenCalled()
    expect(harness.session.getSnapshot().microphoneDeviceId).toBe('microphone-built-in')

    // A choice that failed does not block the next.
    await harness.session.setMicrophone('microphone-headset')
    expect(harness.microphone.stop).toHaveBeenCalled()
  })

  it('lists the microphones and the loudspeakers apart', async () => {
    const harness = build()
    const { microphones, speakers } = await harness.session.listAudioDevices()
    expect(microphones.map((device) => device.deviceId)).toEqual(['microphone-built-in', 'microphone-headset'])
    expect(speakers.map((device) => device.deviceId)).toEqual(['speaker-headset'])
  })

  it('keeps where the sound plays, to be told to every audio element', async () => {
    const harness = build()
    const heard = vi.fn()
    harness.session.subscribe(heard)
    harness.session.setSoundOutput('speaker-headset')
    expect(harness.session.getSnapshot().soundOutputDeviceId).toBe('speaker-headset')
    expect(heard).toHaveBeenCalled()
  })

  it('turns the camera on by swapping the track in and telling the others', async () => {
    const harness = build()
    await connected(harness)
    await harness.session.setVideo(true)
    expect(harness.connection.sender.replaceTrack).toHaveBeenCalledWith(harness.cameras[0])
    expect(harness.graphql).toHaveBeenLastCalledWith(expect.stringContaining('SetChatCallVideo'), {
      callId: 'call-one',
      isVideoOn: true,
    })
    expect(harness.session.getSnapshot().isVideoOn).toBe(true)

    await harness.session.setVideo(false)
    expect(harness.cameras[0].stop).toHaveBeenCalled()
    expect(harness.connection.sender.replaceTrack).toHaveBeenLastCalledWith(null)
    expect(harness.session.getSnapshot().isVideoOn).toBe(false)
  })

  it('offers to switch cameras only when there are several', async () => {
    const one = build({}, 1)
    await connected(one)
    await one.session.setVideo(true)
    await vi.waitFor(() => expect(one.session.getSnapshot().isVideoOn).toBe(true))
    expect(one.session.getSnapshot().canSwitchCamera).toBe(false)

    const two = build({}, 2)
    await connected(two)
    await two.session.setVideo(true)
    await vi.waitFor(() => expect(two.session.getSnapshot().canSwitchCamera).toBe(true))
    await two.session.switchCamera()
    expect(two.cameras[0].stop).toHaveBeenCalled()
    expect(two.connection.sender.replaceTrack).toHaveBeenLastCalledWith(two.cameras[1])
  })

  it('closes everything and leaves the call', async () => {
    const harness = build()
    await connected(harness)
    await harness.session.leave()
    expect(harness.connection.close).toHaveBeenCalled()
    expect(harness.microphone.stop).toHaveBeenCalled()
    expect(harness.graphql).toHaveBeenLastCalledWith(expect.stringContaining('LeaveChatCall'), { callId: 'call-one' })
    expect(harness.session.getSnapshot().phase).toBe('closed')

    // Said once, however many times it is asked.
    await harness.session.leave()
    const leaves = harness.graphql.mock.calls.filter(([query]) => String(query).includes('LeaveChatCall'))
    expect(leaves).toHaveLength(1)
  })
})

describe('waiting for the local addresses', () => {
  it('stops waiting after the time given', async () => {
    vi.useFakeTimers()
    const connection = new FakePeerConnection()
    connection.iceGatheringState = 'gathering'
    const waited = vi.fn()
    void waitForGathering(connection as unknown as RTCPeerConnection, 2000).then(waited)
    await vi.advanceTimersByTimeAsync(1999)
    expect(waited).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(2)
    expect(waited).toHaveBeenCalled()
  })

  it('does not wait when the addresses are gathered', async () => {
    const connection = new FakePeerConnection()
    await expect(waitForGathering(connection as unknown as RTCPeerConnection, 2000)).resolves.toBeUndefined()
  })
})
