import { beforeEach, describe, expect, it, vi } from 'vitest'
import { graphql } from '../api'
import { importRecording, type RecordingMedia, type RecordingView } from './api'
import { recordingDigest } from './sha256'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))
const original = new Uint8Array([31, 42, 53, 64])
const digest = recordingDigest(original)
const media = {
  id: 'fixture-native-media',
  clientMediaId: 'fixture-native-client-media',
  recordingId: 'fixture-native-recording',
  originalFilename: 'fixture.wav',
  originalContentType: 'audio/wav',
  originalSizeBytes: 4,
  originalSha256: digest,
  partCount: 1,
  recordingUploadState: 'pending',
} as RecordingMedia
const file = {
  name: 'fixture.wav',
  type: 'audio/wav',
  size: original.length,
  slice: (start: number, end: number) => ({
    size: original.slice(start, end).length,
    arrayBuffer: async () => original.slice(start, end).buffer,
  }),
} as File
let ownerId = 'fixture-owner'
beforeEach(() => {
  vi.clearAllMocks()
  ownerId = 'fixture-owner'
  vi.mocked(graphql).mockImplementation(async (query) => {
    if (query.includes('ReadRecordingLimits'))
      return { ReadRecordingLimits: { maximumMediaBytes: 1000, uploadPartBytes: 8 * 1024 * 1024 } }
    if (query.includes('ReadRecording('))
      return {
        ReadRecording: {
          recording: { id: media.recordingId, userId: ownerId },
          mediaFiles: [media],
          uploadParts: [
            {
              recordingMediaId: media.id,
              partIndex: 0,
              partSizeBytes: 4,
              partSha256: digest,
              storedAt: '2030-01-02T03:04:05Z',
            },
          ],
        } as RecordingView,
      }
    throw new Error('unexpected create during existing-source resume')
  })
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ ...media, recordingUploadState: 'complete', verifiedAt: '2030-01-02T03:04:05Z' }),
    }),
  )
})
describe('browser resume of native originals', () => {
  it('uses the exact recording/media and reuses verified parts without creating another import', async () => {
    await importRecording(file, 'fixture', ownerId, new AbortController().signal, () => {}, media)
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(vi.mocked(fetch).mock.calls[0][0]).toBe('/api/v1/recordings/media/fixture-native-media/complete')
    expect(vi.mocked(graphql).mock.calls.every(([query]) => !query.includes('CreateRecording'))).toBe(true)
  })
  it('refuses a different original or changed account before uploading', async () => {
    await expect(
      importRecording(file, 'fixture', ownerId, new AbortController().signal, () => {}, {
        ...media,
        originalSha256: 'other-digest',
      }),
    ).rejects.toThrow()
    ownerId = 'other-owner'
    await expect(
      importRecording(file, 'fixture', 'fixture-owner', new AbortController().signal, () => {}, media),
    ).rejects.toThrow()
    expect(fetch).not.toHaveBeenCalled()
  })
})

describe('mismatched server import manifests', () => {
  for (const mismatch of ['source', 'recording', 'client-media', 'size', 'sha', 'parts', 'filename', 'content-type']) {
    it(`refuses ${mismatch} before any original upload or completion`, async () => {
      vi.mocked(graphql).mockImplementation(async (query) => {
        if (query.includes('ReadRecordingLimits'))
          return { ReadRecordingLimits: { maximumMediaBytes: 1000, uploadPartBytes: 8 * 1024 * 1024 } }
        if (query.includes('CreateRecordingMedia')) {
          const originalManifest = { ...media, clientMediaId: `original-${digest}` }
          const overrides: Record<string, Partial<RecordingMedia>> = {
            recording: { recordingId: 'other-recording' },
            'client-media': { clientMediaId: 'other-client-media' },
            size: { originalSizeBytes: 8 * 1024 * 1024 + 1 },
            sha: { originalSha256: 'other-digest' },
            parts: { partCount: 0 },
            filename: { originalFilename: 'other.wav' },
            'content-type': { originalContentType: 'video/mp4' },
          }
          return { CreateRecordingMedia: { ...originalManifest, ...overrides[mismatch] } }
        }
        if (query.includes('CreateRecording'))
          return {
            CreateRecording: {
              id: media.recordingId,
              userId: ownerId,
              clientRecordingId: mismatch === 'source' ? 'other-source' : `file-${digest}`,
              recordingOrigin: 'audioImport',
            },
          }
        throw new Error('mismatched manifest reached a recording read')
      })
      await expect(importRecording(file, 'fixture', ownerId, new AbortController().signal, () => {})).rejects.toThrow()
      expect(fetch).not.toHaveBeenCalled()
    })
  }
})
