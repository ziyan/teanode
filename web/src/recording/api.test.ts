import { describe, expect, it } from 'vitest'
import { isMatchingCompletion, recordingContentType, type RecordingMedia } from './api'
describe('recording original receipt', () => {
  it('requires account source identity, size, digest and verified publication', () => {
    const media = {
      id: 'fixture-media',
      recordingId: 'fixture-recording',
      originalSizeBytes: 32,
      originalSha256: 'fixture-digest',
      recordingUploadState: 'complete',
      verifiedAt: '2030-01-02T03:04:05Z',
    } as RecordingMedia
    expect(isMatchingCompletion(media, media)).toBe(true)
    for (const override of [
      { id: 'other-media' },
      { recordingId: 'other-recording' },
      { originalSizeBytes: 33 },
      { originalSha256: 'other-digest' },
      { recordingUploadState: 'pending' },
      { verifiedAt: null },
      { verifiedAt: 'invalid' },
    ])
      expect(isMatchingCompletion(media, { ...media, ...override })).toBe(false)
  })
  it('allows imported audio/video and refuses arbitrary file types', () => {
    expect(recordingContentType({ name: 'fixture.caf', type: '' })).toBe('audio/x-caf')
    expect(recordingContentType({ name: 'fixture.bin', type: 'video/mp4' })).toBe('video/mp4')
    expect(recordingContentType({ name: 'fixture.txt', type: 'text/plain' })).toBe('')
  })
})
