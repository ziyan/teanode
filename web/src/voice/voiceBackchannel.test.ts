import { describe, expect, it } from 'vitest'
import { BackchannelTiming, withoutBackchannel } from './voiceBackchannel'

// Feeds the timing the microphone every 50 ms from a start to an end, and
// says when it made a sound.
function listen(timing: BackchannelTiming, from: number, until: number, level: (now: number) => number): number[] {
  const sounded: number[] = []
  for (let now = from; now < until; now += 50) {
    if (timing.isTime(now, level(now))) sounded.push(now)
  }
  return sounded
}

describe('BackchannelTiming', () => {
  it('makes a sound at a pause well into a long stretch of speech, not too often', () => {
    const timing = new BackchannelTiming()
    timing.speechStarted(0)
    // Talking, with a short pause every two seconds.
    const level = (now: number) => (now % 2000 >= 1700 ? 0.01 : 0.2)
    const sounded = listen(timing, 0, 16000, level)
    // Not in the first four seconds, at a pause, and six seconds apart.
    expect(sounded.length).toBe(2)
    expect(sounded[0]).toBeGreaterThanOrEqual(4000)
    expect(sounded[0] % 2000).toBeGreaterThanOrEqual(1700)
    expect(sounded[1] - sounded[0]).toBeGreaterThanOrEqual(6000)
    expect(timing.takeSounded()).toBe(true)
    expect(timing.takeSounded()).toBe(false)
  })

  it('says nothing in a short utterance, or without a pause', () => {
    const short = new BackchannelTiming()
    short.speechStarted(0)
    expect(listen(short, 0, 3500, (now) => (now % 1000 >= 700 ? 0.01 : 0.2))).toEqual([])
    const unbroken = new BackchannelTiming()
    unbroken.speechStarted(0)
    expect(listen(unbroken, 0, 12000, () => 0.2)).toEqual([])
    // Nor once they have stopped.
    unbroken.speechStopped()
    expect(listen(unbroken, 12000, 14000, () => 0.01)).toEqual([])
  })
})

describe('withoutBackchannel', () => {
  it('takes the sounds heard back out of what was said', () => {
    expect(withoutBackchannel('So the plan is mm-hmm to fly on Friday.')).toBe('So the plan is to fly on Friday.')
    expect(withoutBackchannel('Uh-huh. Then back on Sunday.')).toBe('Then back on Sunday.')
    expect(withoutBackchannel('Mhm')).toBe('')
    // A word that only starts the same way stays.
    expect(withoutBackchannel('The hummingbird came back.')).toBe('The hummingbird came back.')
  })
})
