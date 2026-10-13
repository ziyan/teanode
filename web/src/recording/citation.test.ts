import { expect, it } from 'vitest'
import { overlapsRecordingCitation, readRecordingCitation } from './citation'
it('preserves the exact historical version and legacy run with absolute citation bounds', () => {
  const citation = readRecordingCitation(new URLSearchParams('run=old-run&startMS=602000&endMS=605000'))
  expect(citation).toEqual({
    requestedRunId: 'old-run',
    passageStartMS: 602000,
    passageEndMS: 605000,
    hasInvalidBounds: false,
  })
  expect(overlapsRecordingCitation(600000, 603000, citation)).toBe(true)
  expect(overlapsRecordingCitation(605000, 606000, citation)).toBe(false)
  expect(readRecordingCitation(new URLSearchParams('version=exact-run&run=other-run')).requestedRunId).toBe('exact-run')
})
it('refuses incomplete, reversed, fractional, negative or unsafe citation bounds', () => {
  for (const query of [
    'startMS=1&endMS=2',
    'version=old-run&startMS=1',
    'version=old-run&startMS=3&endMS=2',
    'version=old-run&startMS=-1&endMS=2',
    'version=old-run&startMS=1.5&endMS=2',
    'version=old-run&startMS=1e3&endMS=2000',
    'version=old-run&startMS=1&endMS=9007199254740992',
  ]) {
    expect(readRecordingCitation(new URLSearchParams(query)).hasInvalidBounds).toBe(true)
  }
})
