// The microphone as the voice socket takes it: mono, 16-bit PCM, at the
// rate the server asks for (24 kHz), in frames of a tenth of a second.
//
// An AudioWorklet runs on the audio thread, so this file is served as it is
// (webpack copies it to assets/voice-capture-worklet.js): the dashboard's
// content policy takes scripts only from the server, never from a blob.
//
// The input is at whatever rate the device runs, usually 48 kHz. Each
// output sample is read between the two input samples either side of it;
// for speech that is enough, and it keeps the audio thread's work small.
class VoiceCaptureProcessor extends AudioWorkletProcessor {
  constructor(options) {
    super()
    const targetRate = (options && options.processorOptions && options.processorOptions.targetRate) || 24000
    // How many input samples one output sample advances by.
    this.step = sampleRate / targetRate
    // Where the next output sample falls, in input samples from the start
    // of the next block; -1 is the last sample of the block before.
    this.position = 0
    this.previousSample = 0
    this.frameLength = Math.round(targetRate / 10)
    this.frame = new Int16Array(this.frameLength)
    this.filledCount = 0
  }

  process(inputs) {
    const channel = inputs[0] && inputs[0][0]
    if (!channel || channel.length === 0) return true
    const sampleAt = (index) => (index < 0 ? this.previousSample : channel[index])
    let position = this.position
    while (Math.floor(position) + 1 <= channel.length - 1) {
      const lower = Math.floor(position)
      const before = sampleAt(lower)
      const value = before + (sampleAt(lower + 1) - before) * (position - lower)
      const clamped = Math.max(-1, Math.min(1, value))
      this.frame[this.filledCount] = clamped < 0 ? clamped * 0x8000 : clamped * 0x7fff
      this.filledCount += 1
      if (this.filledCount === this.frameLength) {
        this.port.postMessage(this.frame.buffer, [this.frame.buffer])
        this.frame = new Int16Array(this.frameLength)
        this.filledCount = 0
      }
      position += this.step
    }
    this.position = position - channel.length
    this.previousSample = channel[channel.length - 1]
    return true
  }
}

registerProcessor('voice-capture', VoiceCaptureProcessor)
