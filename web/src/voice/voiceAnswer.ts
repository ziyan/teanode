// Spoken answers: in voice mode the agent's answer is read aloud as it is
// written. The text of each turn is cut into sentences as it streams in,
// each sentence is spoken by the provider through the voice socket, and
// the audio is played in order. When the person starts talking over it the
// answer quietens at once and pauses if they keep on; what they said then
// decides whether it was them cutting in, which ends the answer and tells
// the agent how much of it was heard, or the answer heard back through the
// microphone, which is dropped and the answer goes on.

// AnswerRunEvent is the part of a conversation event an answer is made of.
export type AnswerRunEvent = {
  kind: string
  runId: string
  sequence: number
  text?: string
  note?: string
}

// InterruptedAnswer is how much of a spoken answer the person heard before
// they talked over it, for the agent's next turn.
export type InterruptedAnswer = { heardText: string; unheardText: string }

// speakableText is a piece of a Markdown answer as it is said: the marks
// that only mean something on a screen go, links say their words, and
// addresses nobody wants read out are left out.
export function speakableText(markdown: string): string {
  return markdown
    .replace(/!\[[^\]]*\]\([^)]*\)/g, '')
    .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
    .replace(/<?https?:\/\/[^\s>)]*[^\s>).,!?;:]>?/g, '')
    .replace(/`([^`]*)`/g, '$1')
    .replace(/^\s{0,3}#{1,6}\s+/gm, '')
    .replace(/^\s*>\s?/gm, '')
    .replace(/^\s*(?:[-*+]|\d+[.)])\s+/gm, '')
    .replace(/^\s*\|?\s*:?-{3,}.*$/gm, '')
    .replace(/\s*\|\s*/g, ', ')
    .replace(/(\*\*|__|~~)(.+?)\1/g, '$2')
    .replace(/(^|[\s(])[*_]([^*_\n]+)[*_](?=[\s).,!?;:]|$)/g, '$1$2')
    .replace(/\s+/g, ' ')
    .replace(/ ([.,!?;:])/g, '$1')
    .replace(/^[,\s]+|[,\s]+$/g, '')
    .trim()
}

// The shortest a piece after the first is, so that the provider reads a
// few sentences together, with their flow, rather than one at a time; the
// first is a single sentence, so the answer starts as soon as it can.
const FOLLOWING_SEGMENT_LENGTH = 80
// The longest a piece waits for the end of its sentence.
const LONGEST_SEGMENT_LENGTH = 320

const SENTENCE_END = /[.!?;:。！？；](?=["')\]]*(\s|$))|\n/g
const FENCE = '```'

// AnswerSegmenter cuts the text of an answer, as it streams in, into the
// pieces it is spoken in: whole sentences, code blocks left out.
export class AnswerSegmenter {
  private pending = ''
  private hasSegment = false

  // push takes more of the answer and says the pieces now complete.
  push(text: string): string[] {
    this.pending += text
    return this.take(false)
  }

  // flush says what is left as the last piece: the answer has ended, or a
  // tool is about to be called.
  flush(): string[] {
    return this.take(true)
  }

  // reset forgets what was not yet said, and says what it was.
  reset(): string {
    const forgotten = this.pending
    this.pending = ''
    this.hasSegment = false
    return forgotten
  }

  private take(isFinal: boolean): string[] {
    const segments: string[] = []
    for (;;) {
      // A code block is never read out: dropped whole once it is closed,
      // and what follows an open one waits for its end.
      const opening = this.pending.indexOf(FENCE)
      let readable = this.pending
      if (opening >= 0) {
        const closing = this.pending.indexOf(FENCE, opening + FENCE.length)
        if (closing >= 0) {
          this.pending = this.pending.slice(0, opening) + '\n' + this.pending.slice(closing + FENCE.length)
          continue
        }
        readable = this.pending.slice(0, opening)
      }
      const shortest = this.hasSegment ? FOLLOWING_SEGMENT_LENGTH : 1
      let cut = -1
      SENTENCE_END.lastIndex = 0
      for (let match = SENTENCE_END.exec(readable); match; match = SENTENCE_END.exec(readable)) {
        const end = match.index + match[0].length
        // A full stop at the very end of what has come may be the middle of
        // a number still coming.
        if (!isFinal && end >= readable.length && match[0] !== '\n') break
        if (readable.slice(0, end).trim().length >= shortest) {
          cut = end
          break
        }
      }
      if (cut < 0 && readable.length > LONGEST_SEGMENT_LENGTH) {
        // No end of a sentence in sight: at the last pause or space.
        const window = readable.slice(0, LONGEST_SEGMENT_LENGTH)
        const pause = Math.max(window.lastIndexOf(', '), window.lastIndexOf(' '))
        cut = pause > 0 ? pause + 1 : LONGEST_SEGMENT_LENGTH
      }
      if (cut < 0) {
        if (isFinal) {
          const rest = speakableText(readable)
          this.pending = ''
          if (rest) {
            segments.push(rest)
            this.hasSegment = true
          }
        }
        return segments
      }
      const segment = speakableText(readable.slice(0, cut))
      this.pending = this.pending.slice(cut)
      if (segment) {
        segments.push(segment)
        this.hasSegment = true
      }
    }
  }
}

function wordsOf(text: string): string[] {
  return text
    .toLowerCase()
    .normalize('NFKC')
    .split(/[^\p{L}\p{N}']+/u)
    .filter(Boolean)
}

// isLikelyEcho says whether what the microphone heard is the answer itself,
// played through a speaker and picked up again: its words follow the
// answer's, in the answer's order.
export function isLikelyEcho(heardText: string, spokenText: string): boolean {
  const heard = wordsOf(heardText)
  const spoken = wordsOf(spokenText)
  if (heard.length === 0 || spoken.length === 0) return false
  // Run together, which also covers languages written without spaces.
  if (spoken.join('').includes(heard.join(''))) return true
  if (heard.length < 3) return false
  const spokenPairs = new Set(spoken.slice(1).map((word, index) => `${spoken[index]} ${word}`))
  const heardPairs = heard.slice(1).map((word, index) => `${heard[index]} ${word}`)
  const matchedCount = heardPairs.filter((pair) => spokenPairs.has(pair)).length
  return matchedCount / heardPairs.length >= 0.6
}

// heardSplit divides an answer at the point the person stopped hearing it:
// each piece with how much of it was played, from 0 to 1. A piece cut off
// in the middle counts as heard only up to the last whole word before the
// cut, so the agent never believes more was heard than was.
export function heardSplit(pieces: { text: string; playedFraction: number }[]): InterruptedAnswer {
  const heard: string[] = []
  const unheard: string[] = []
  for (const piece of pieces) {
    if (piece.playedFraction >= 0.999) {
      heard.push(piece.text)
      continue
    }
    if (piece.playedFraction <= 0.02) {
      unheard.push(piece.text)
      continue
    }
    const roughCut = Math.floor(piece.text.length * piece.playedFraction)
    const wordCut = piece.text.lastIndexOf(' ', roughCut)
    const cut = wordCut > 0 ? wordCut : 0
    if (cut > 0) heard.push(piece.text.slice(0, cut).trim())
    unheard.push(piece.text.slice(cut).trim())
  }
  return { heardText: heard.join(' ').trim(), unheardText: unheard.filter(Boolean).join(' ').trim() }
}

// The provider's speech: mono 16-bit PCM at this rate.
const ANSWER_SAMPLE_RATE = 24000
// How many pieces have their audio asked for ahead of the one playing.
const PIECES_AHEAD = 3
// How quiet an answer goes the moment somebody starts talking.
const DUCKED_GAIN = 0.15
// How long they talk before the answer pauses rather than only quietens.
const PAUSE_AFTER_MS = 600
// How long a paused answer waits for what was said before it goes on.
const LONGEST_PAUSE_MS = 5000
// How long after an answer what is heard may still be its echo.
const ECHO_AFTER_MS = 2500
// How far back the answer is remembered to recognize its echo.
const ECHO_MEMORY_MS = 20000
// A conservative speaking rate, to guess how long a piece will be before
// all of its audio has come.
const CHARACTERS_PER_SECOND = 14

// AnswerTransport is how the player asks for a piece's audio: the voice
// socket.
export type AnswerTransport = {
  request: (answerSegmentId: string, answerText: string) => void
  cancel: (answerSegmentIds: string[]) => void
}

type Piece = {
  answerSegmentId: string
  text: string
  chunks: Float32Array[]
  sampleCount: number
  isRequested: boolean
  isComplete: boolean
  // The provider could not speak it: it is skipped, and was never heard.
  isFailed: boolean
  // The next sample to play: everything before it has been scheduled.
  cursorSamples: number
}

type Scheduled = {
  source: AudioBufferSourceNode
  piece: Piece
  startSample: number
  sampleCount: number
  startTime: number
}

// AnswerPlayer plays the pieces of an answer in order, through its own
// gain, as their audio arrives; it can quieten, pause where it is and go
// on from there, or stop and say how much was heard.
export class AnswerPlayer {
  private pieces: Piece[] = []
  private scheduled: Scheduled[] = []
  private played: { text: string; endedAt: number }[] = []
  private heardTexts: string[] = []
  private nextStartTime = 0
  private isPaused = false
  private counter = 0
  private lastActiveAt = 0
  private output: GainNode
  private analyser: AnalyserNode
  private samples: Float32Array<ArrayBuffer>
  private ticker?: number

  constructor(
    private context: AudioContext,
    private transport: AnswerTransport,
    private onActive: (isActive: boolean) => void,
  ) {
    this.output = context.createGain()
    this.analyser = context.createAnalyser()
    this.analyser.fftSize = 512
    this.samples = new Float32Array(this.analyser.fftSize)
    this.output.connect(this.analyser)
    this.analyser.connect(context.destination)
  }

  // isActive says whether an answer is being spoken or waits to be.
  isActive(): boolean {
    return this.pieces.length > 0
  }

  // wasRecentlyActive says whether what is heard now may be the answer's
  // echo: it is playing, or stopped a moment ago.
  wasRecentlyActive(): boolean {
    return this.isActive() || performance.now() - this.lastActiveAt < ECHO_AFTER_MS
  }

  // recentText is what was played lately, and what is playing.
  recentText(): string {
    const now = performance.now()
    this.played = this.played.filter((entry) => now - entry.endedAt < ECHO_MEMORY_MS)
    return [...this.played.map((entry) => entry.text), ...this.pieces.slice(0, 2).map((piece) => piece.text)].join(' ')
  }

  enqueue(texts: string[]) {
    for (const text of texts) {
      if (!text.trim()) continue
      this.counter += 1
      this.pieces.push({
        answerSegmentId: `answer-${Date.now().toString(36)}-${this.counter}`,
        text,
        chunks: [],
        sampleCount: 0,
        isRequested: false,
        isComplete: false,
        isFailed: false,
        cursorSamples: 0,
      })
    }
    this.pump()
  }

  // accept takes some of a piece's audio; a piece no longer wanted is
  // ignored, however late it comes.
  accept(answerSegmentId: string, pcm: Int16Array) {
    const piece = this.pieces.find((candidate) => candidate.answerSegmentId === answerSegmentId)
    if (!piece || piece.isComplete) return
    const chunk = new Float32Array(pcm.length)
    for (let index = 0; index < pcm.length; index++) chunk[index] = pcm[index] / 32768
    piece.chunks.push(chunk)
    piece.sampleCount += chunk.length
    this.pump()
  }

  // complete says a piece's audio has all come; a piece that failed is
  // complete with what it has.
  complete(answerSegmentId: string, isFailed = false) {
    const piece = this.pieces.find((candidate) => candidate.answerSegmentId === answerSegmentId)
    if (!piece) return
    piece.isComplete = true
    piece.isFailed = isFailed && piece.sampleCount === 0
    this.pump()
  }

  duck(isDucked: boolean) {
    this.output.gain.setTargetAtTime(isDucked ? DUCKED_GAIN : 1, this.context.currentTime, 0.03)
  }

  // pause stops the sound where it is; resume goes on from there.
  pause() {
    if (this.isPaused) return
    this.isPaused = true
    this.unschedule()
  }

  resume() {
    if (!this.isPaused) return
    this.isPaused = false
    this.pump()
  }

  // interrupt ends the answer for good: nothing of it plays again, its
  // audio still being made is cancelled, and what comes later is ignored.
  // It says how much was heard; notSegmentedText is what was written but
  // not yet cut into pieces, which nobody heard either.
  interrupt(notSegmentedText = ''): InterruptedAnswer {
    this.unschedule()
    const split = heardSplit([
      ...this.heardTexts.map((text) => ({ text, playedFraction: 1 })),
      ...this.pieces.map((piece) => ({ text: piece.text, playedFraction: this.playedFraction(piece) })),
    ])
    const waiting = this.pieces.filter((piece) => piece.isRequested && !piece.isComplete)
    if (waiting.length > 0) this.transport.cancel(waiting.map((piece) => piece.answerSegmentId))
    for (const piece of this.pieces) {
      if (piece.cursorSamples > 0) this.played.push({ text: piece.text, endedAt: performance.now() })
    }
    this.pieces = []
    this.heardTexts = []
    this.isPaused = false
    this.duck(false)
    this.setActive()
    const rest = speakableText(notSegmentedText)
    return {
      heardText: split.heardText,
      unheardText: [split.unheardText, rest].filter(Boolean).join(' '),
    }
  }

  // level is how loud the answer is now, from 0 to 1, for drawing.
  level(): number {
    if (!this.isActive()) return 0
    this.analyser.getFloatTimeDomainData(this.samples)
    let sum = 0
    for (const sample of this.samples) sum += sample * sample
    return Math.min(1, Math.sqrt(sum / this.samples.length) * 6)
  }

  close() {
    this.interrupt()
    if (this.ticker !== undefined) window.clearInterval(this.ticker)
    this.ticker = undefined
  }

  // How much of a piece has been played, from 0 to 1, as far as anyone
  // can tell before its audio has all come.
  private playedFraction(piece: Piece): number {
    if (piece.cursorSamples <= 0) return 0
    const expected = piece.isComplete
      ? piece.sampleCount
      : Math.max(piece.sampleCount, (piece.text.length / CHARACTERS_PER_SECOND) * ANSWER_SAMPLE_RATE)
    return expected > 0 ? Math.min(1, piece.cursorSamples / expected) : 0
  }

  // unschedule stops everything scheduled and moves each piece's cursor
  // back to what had actually played.
  private unschedule() {
    const now = this.context.currentTime
    for (const entry of this.scheduled) {
      const playedSamples = Math.max(
        0,
        Math.min(entry.sampleCount, Math.floor((now - entry.startTime) * ANSWER_SAMPLE_RATE)),
      )
      if (playedSamples < entry.sampleCount) {
        entry.piece.cursorSamples = Math.min(entry.piece.cursorSamples, entry.startSample + playedSamples)
      }
      entry.source.onended = null
      try {
        entry.source.stop()
      } catch {
        // Not started yet, or ended already.
      }
    }
    this.scheduled = []
    this.nextStartTime = 0
  }

  // pump asks for audio ahead, schedules what has arrived, and lets go of
  // the pieces that have finished playing.
  private pump() {
    for (const piece of this.pieces.slice(0, PIECES_AHEAD)) {
      if (!piece.isRequested) {
        piece.isRequested = true
        this.transport.request(piece.answerSegmentId, piece.text)
      }
    }
    const now = this.context.currentTime
    this.scheduled = this.scheduled.filter((entry) => entry.startTime + entry.sampleCount / ANSWER_SAMPLE_RATE > now)
    // Done: complete, all scheduled, and nothing of it still sounding.
    while (this.pieces.length > 0) {
      const first = this.pieces[0]
      const isSounding = this.scheduled.some((entry) => entry.piece === first)
      if (!(first.isComplete && first.cursorSamples >= first.sampleCount && !isSounding)) break
      this.pieces.shift()
      if (first.isFailed) continue
      this.heardTexts.push(first.text)
      this.played.push({ text: first.text, endedAt: performance.now() })
    }
    if (!this.isPaused) {
      for (const piece of this.pieces) {
        if (piece.cursorSamples < piece.sampleCount) this.schedule(piece)
        if (!piece.isComplete || piece.cursorSamples < piece.sampleCount) break
      }
    }
    if (this.pieces.length === 0) this.heardTexts = []
    this.setActive()
  }

  private schedule(piece: Piece) {
    const sampleCount = piece.sampleCount - piece.cursorSamples
    const buffer = this.context.createBuffer(1, sampleCount, ANSWER_SAMPLE_RATE)
    const channel = buffer.getChannelData(0)
    let offset = 0
    let written = 0
    for (const chunk of piece.chunks) {
      const chunkEnd = offset + chunk.length
      if (chunkEnd > piece.cursorSamples) {
        const from = Math.max(0, piece.cursorSamples - offset)
        channel.set(chunk.subarray(from), written)
        written += chunk.length - from
      }
      offset = chunkEnd
    }
    const source = this.context.createBufferSource()
    source.buffer = buffer
    source.connect(this.output)
    // Back to back with what is scheduled; after a gap, a little ahead of
    // now so that the start is not clipped.
    const startTime = Math.max(this.nextStartTime, this.context.currentTime + 0.03)
    source.start(startTime)
    source.onended = () => this.pump()
    this.scheduled.push({ source, piece, startSample: piece.cursorSamples, sampleCount, startTime })
    this.nextStartTime = startTime + sampleCount / ANSWER_SAMPLE_RATE
    piece.cursorSamples = piece.sampleCount
  }

  private wasActive = false

  private setActive() {
    const isActive = this.isActive()
    if (isActive) this.lastActiveAt = performance.now()
    // Ended sources call pump; a piece whose audio is all scheduled but
    // not yet complete needs a look now and then as well.
    if (isActive && this.ticker === undefined) this.ticker = window.setInterval(() => this.pump(), 250)
    if (!isActive && this.ticker !== undefined) {
      window.clearInterval(this.ticker)
      this.ticker = undefined
    }
    if (isActive !== this.wasActive) {
      this.wasActive = isActive
      this.onActive(isActive)
    }
  }
}

type FollowedRun = {
  segmenter: AnswerSegmenter
  lastSequence: number
  // The text of the assistant message being written, from its deltas.
  messageText: string
  // After the person cut in, the rest of the message being written then
  // is not spoken: it answers what they said before.
  isSkippingMessage: boolean
}

// TranscriptVerdict is what to do with what the person was heard saying.
export type TranscriptVerdict = { isEcho: boolean; interruptedAnswer?: InterruptedAnswer }

// AnswerVoice follows the conversation's turns, speaks their answers, and
// decides what speech heard while one plays means.
export class AnswerVoice {
  private runs = new Map<string, FollowedRun>()
  private isMuted = false
  private isCandidate = false
  private pauseTimer?: number
  private resumeTimer?: number

  constructor(
    private player: AnswerPlayer,
    private messages: { confirmationNeeded: string },
  ) {}

  // follow reads one event of the conversation. A turn is spoken from its
  // start when voice mode saw it start, or from where it is when the
  // person spoke into it (adopt).
  follow(event: AnswerRunEvent) {
    if (event.kind === 'asked' && !this.runs.has(event.runId)) {
      this.runs.set(event.runId, this.newRun(event.sequence))
      return
    }
    const run = this.runs.get(event.runId)
    if (!run) return
    // A replay after a reconnection says nothing twice.
    if (event.sequence <= run.lastSequence) return
    run.lastSequence = event.sequence
    if (this.isMuted && event.kind !== 'done') return
    switch (event.kind) {
      case 'text':
        if (run.isSkippingMessage) return
        run.messageText += event.text ?? ''
        this.player.enqueue(run.segmenter.push(event.text ?? ''))
        return
      case 'tool_call':
        if (!run.isSkippingMessage) this.player.enqueue(run.segmenter.flush())
        run.isSkippingMessage = false
        run.messageText = ''
        return
      case 'message': {
        // The whole message, which the deltas have said already: only what
        // they missed is spoken.
        const messageText = event.text ?? ''
        if (!run.isSkippingMessage) {
          if (run.messageText && messageText.startsWith(run.messageText)) {
            this.player.enqueue(run.segmenter.push(messageText.slice(run.messageText.length)))
          } else if (!run.messageText) {
            this.player.enqueue(run.segmenter.push(messageText))
          }
          this.player.enqueue(run.segmenter.flush())
        } else {
          run.segmenter.reset()
        }
        run.isSkippingMessage = false
        run.messageText = ''
        return
      }
      case 'question':
        this.player.enqueue(run.segmenter.flush())
        this.player.enqueue([speakableText(event.note ?? '')])
        return
      case 'confirmation':
        this.player.enqueue(run.segmenter.flush())
        this.player.enqueue([`${this.messages.confirmationNeeded} ${speakableText(event.note ?? '')}`])
        return
      case 'done':
        if (!this.isMuted && !run.isSkippingMessage) this.player.enqueue(run.segmenter.flush())
        this.runs.delete(event.runId)
        return
    }
  }

  // adopt speaks the turns running when the person spoke into them, from
  // the next thing they say.
  adopt(runIds: string[]) {
    for (const runId of runIds) {
      if (!this.runs.has(runId)) this.runs.set(runId, this.newRun(-1))
    }
  }

  setMuted(isMuted: boolean) {
    this.isMuted = isMuted
    if (isMuted) this.cutIn()
  }

  accept(answerSegmentId: string, pcm: Int16Array) {
    this.player.accept(answerSegmentId, pcm)
  }

  complete(answerSegmentId: string, isFailed: boolean) {
    this.player.complete(answerSegmentId, isFailed)
  }

  isSpeaking(): boolean {
    return this.player.isActive()
  }

  level(): number {
    return this.player.level()
  }

  // speechStarted: somebody may be talking over the answer. It quietens
  // at once, and pauses if they keep on.
  speechStarted() {
    if (!this.player.isActive()) return
    this.isCandidate = true
    window.clearTimeout(this.resumeTimer)
    this.player.duck(true)
    window.clearTimeout(this.pauseTimer)
    this.pauseTimer = window.setTimeout(() => this.player.pause(), PAUSE_AFTER_MS)
  }

  // speechStopped: what they said is being transcribed; should it never
  // come, the answer goes on by itself.
  speechStopped() {
    window.clearTimeout(this.pauseTimer)
    if (!this.isCandidate) return
    window.clearTimeout(this.resumeTimer)
    this.resumeTimer = window.setTimeout(() => this.goOn(), LONGEST_PAUSE_MS)
  }

  // transcript decides what words heard mean: the answer's own echo, which
  // is dropped while the answer goes on; or the person, who ends the
  // answer, which the agent is told how much of was heard.
  transcript(transcriptText: string): TranscriptVerdict {
    if (this.player.wasRecentlyActive() && isLikelyEcho(transcriptText, this.player.recentText())) {
      this.goOn()
      return { isEcho: true }
    }
    if (!this.player.isActive()) {
      this.goOn()
      return { isEcho: false }
    }
    const cut = this.cutIn()
    return { isEcho: false, interruptedAnswer: cut.unheardText ? cut : undefined }
  }

  // notHeard: nothing came of the speech (a cough, a noise); the answer
  // goes on.
  notHeard() {
    if (this.isCandidate) this.goOn()
  }

  close() {
    window.clearTimeout(this.pauseTimer)
    window.clearTimeout(this.resumeTimer)
    this.player.close()
    this.runs.clear()
  }

  private goOn() {
    this.isCandidate = false
    window.clearTimeout(this.pauseTimer)
    window.clearTimeout(this.resumeTimer)
    this.player.resume()
    this.player.duck(false)
  }

  private cutIn(): InterruptedAnswer {
    this.isCandidate = false
    window.clearTimeout(this.pauseTimer)
    window.clearTimeout(this.resumeTimer)
    let notSegmentedText = ''
    for (const run of this.runs.values()) {
      notSegmentedText += run.segmenter.reset()
      if (run.messageText) run.isSkippingMessage = true
      run.messageText = ''
    }
    return this.player.interrupt(notSegmentedText)
  }

  private newRun(sequence: number): FollowedRun {
    return { segmenter: new AnswerSegmenter(), lastSequence: sequence, messageText: '', isSkippingMessage: false }
  }
}
