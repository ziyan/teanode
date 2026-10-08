// Backchannel: the small sounds a listener makes while somebody talks at
// length, "mm-hmm", "uh-huh", that say they are still there. The model
// cannot make them: it reads what was said only once the person has
// finished. So the drawer makes them, from two clips spoken once at the
// start of a call in the agent's own voice, played quietly at a short
// pause in a long stretch of speech.

// How long the person has to have been talking before the first sound.
const TALKING_BEFORE_MS = 4000
// The least time between two sounds.
const BETWEEN_SOUNDS_MS = 6000
// How long a pause in their speech must last for a sound to fit in it.
const PAUSE_MS = 250
// How quiet, against how loud they have been talking, a pause is.
const PAUSE_SHARE = 0.25
// How loud the sound plays, against an answer.
const SOUND_GAIN = 0.4

// The words of the sounds, and what a transcription makes of them when the
// microphone hears one back.
export const BACKCHANNEL_WORDS = ['Mm-hmm.', 'Uh-huh.']
const BACKCHANNEL_HEARD = /(^|[\s,.!?])(mm+[- ]?hmm+|m+hm+|uh[- ]?huh)[.,!?]?(?=\s|$)/gi

// withoutBackchannel takes out of what the person said the sounds the
// drawer made while they said it, which the microphone may have heard.
export function withoutBackchannel(transcriptText: string): string {
  return transcriptText.replace(BACKCHANNEL_HEARD, '$1').replace(/\s+/g, ' ').trim()
}

// BackchannelTiming decides when a sound fits: well into a long stretch of
// speech, at a pause, not too often. Kept apart from the audio so that it
// can be told the time.
export class BackchannelTiming {
  private talkingSince?: number
  private quietSince?: number
  private lastSoundAt = -Infinity
  private loudestRms = 0
  private soundedInUtterance = false

  speechStarted(now: number) {
    if (this.talkingSince === undefined) this.talkingSince = now
    this.quietSince = undefined
  }

  speechStopped() {
    this.talkingSince = undefined
    this.quietSince = undefined
    this.loudestRms = 0
  }

  // hasSounded says whether a sound was made during the utterance just
  // finished, and forgets it.
  takeSounded(): boolean {
    const sounded = this.soundedInUtterance
    this.soundedInUtterance = false
    return sounded
  }

  // isTime takes how loud the microphone is now and says whether to make
  // a sound now.
  isTime(now: number, microphoneRms: number): boolean {
    if (this.talkingSince === undefined) return false
    // How loud they talk, slowly forgotten.
    this.loudestRms = Math.max(microphoneRms, this.loudestRms * 0.995)
    const isQuiet = this.loudestRms > 0 && microphoneRms < this.loudestRms * PAUSE_SHARE
    if (!isQuiet) {
      this.quietSince = undefined
      return false
    }
    if (this.quietSince === undefined) this.quietSince = now
    if (now - this.talkingSince < TALKING_BEFORE_MS) return false
    if (now - this.lastSoundAt < BETWEEN_SOUNDS_MS) return false
    if (now - this.quietSince < PAUSE_MS) return false
    this.lastSoundAt = now
    this.quietSince = undefined
    this.soundedInUtterance = true
    return true
  }
}

// Backchannel plays the sounds, through the audio context the microphone
// is read in, so that the browser's echo cancellation knows them.
export class Backchannel {
  private clips = new Map<string, Float32Array[]>()
  private ready: AudioBuffer[] = []
  private output: GainNode
  private next = 0

  constructor(
    private context: AudioContext,
    request: (answerSegmentId: string, answerText: string) => void,
  ) {
    this.output = context.createGain()
    this.output.gain.value = SOUND_GAIN
    this.output.connect(context.destination)
    BACKCHANNEL_WORDS.forEach((words, index) => {
      const answerSegmentId = `backchannel-${index}`
      this.clips.set(answerSegmentId, [])
      request(answerSegmentId, words)
    })
  }

  // owns says whether a piece of audio is one of the sounds.
  owns(answerSegmentId: string): boolean {
    return this.clips.has(answerSegmentId)
  }

  accept(answerSegmentId: string, pcm: Int16Array) {
    const chunks = this.clips.get(answerSegmentId)
    if (!chunks) return
    const chunk = new Float32Array(pcm.length)
    for (let index = 0; index < pcm.length; index++) chunk[index] = pcm[index] / 32768
    chunks.push(chunk)
  }

  complete(answerSegmentId: string) {
    const chunks = this.clips.get(answerSegmentId)
    if (!chunks) return
    this.clips.delete(answerSegmentId)
    const sampleCount = chunks.reduce((sum, chunk) => sum + chunk.length, 0)
    if (sampleCount === 0) return
    const buffer = this.context.createBuffer(1, sampleCount, 24000)
    const channel = buffer.getChannelData(0)
    let offset = 0
    for (const chunk of chunks) {
      channel.set(chunk, offset)
      offset += chunk.length
    }
    this.ready.push(buffer)
  }

  // play makes one of the sounds, taking turns between them.
  play() {
    if (this.ready.length === 0) return
    const source = this.context.createBufferSource()
    source.buffer = this.ready[this.next % this.ready.length]
    this.next += 1
    source.connect(this.output)
    source.start()
  }
}
