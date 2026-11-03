// Whether somebody on a call is speaking, from the level of their stream, so
// that their tile can light up. The level is read a few times a second from an
// analyser rather than every frame: the ring only has to follow a sentence.

// SPEAKING_LEVEL is the loudness, from 0 to 1, above which a stream counts as
// somebody speaking. Room noise stays under it.
const SPEAKING_LEVEL = 0.03

// READ_EVERY_MS is how often the level is read.
const READ_EVERY_MS = 120

// SPEAKING_HOLD_MS is how long the ring stays lit after the last loud read, so
// that the pauses between words do not make it flicker.
const SPEAKING_HOLD_MS = 450

let sharedContext: AudioContext | null = null

// audioContext is the one audio context all the tiles share: a page may only
// hold a handful of them.
function audioContext(): AudioContext | null {
  if (sharedContext) return sharedContext
  const constructor =
    window.AudioContext ?? (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext
  if (!constructor) return null
  sharedContext = new constructor()
  return sharedContext
}

// watchVoice calls back with true while the stream carries a voice and false
// when it stops. The returned function stops watching. A stream with no
// microphone in it, or a browser with no audio analysis, is never speaking.
export function watchVoice(stream: MediaStream, onChange: (isSpeaking: boolean) => void): () => void {
  const context = audioContext()
  if (!context || stream.getAudioTracks().length === 0) return () => {}
  void context.resume().catch(() => {})
  const source = context.createMediaStreamSource(stream)
  const analyser = context.createAnalyser()
  analyser.fftSize = 512
  source.connect(analyser)
  const samples = new Uint8Array(analyser.fftSize)
  let isSpeaking = false
  let lastLoudAt = 0
  const timer = window.setInterval(() => {
    analyser.getByteTimeDomainData(samples)
    let loudest = 0
    for (const sample of samples) loudest = Math.max(loudest, Math.abs(sample - 128) / 128)
    const now = Date.now()
    if (loudest > SPEAKING_LEVEL) lastLoudAt = now
    const next = now - lastLoudAt < SPEAKING_HOLD_MS
    if (next !== isSpeaking) {
      isSpeaking = next
      onChange(next)
    }
  }, READ_EVERY_MS)
  return () => {
    window.clearInterval(timer)
    source.disconnect()
    if (isSpeaking) onChange(false)
  }
}
