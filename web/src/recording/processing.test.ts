import { describe, expect, it } from 'vitest'
import { confirmedSegmentIdentity, passagePlaybackPath, type SpeakersView, type TranscriptSegment } from './processing'

describe('recording passage confirmation', () => {
  const segment: TranscriptSegment = {
    id: 'fixture-segment',
    recordingTranscriptRunId: 'fixture-run',
    recordingTranscriptionAttemptId: 'new-attempt',
    localSpeakerId: 'local-one',
    segmentStartMS: 2000,
    segmentEndMS: 5000,
    transcriptText: 'Evidence',
    effectiveTranscriptText: 'Evidence',
    appliedTextCorrectionId: '',
  }
  const view: SpeakersView = {
    sourceGeneration: 5,
    speakerIdentities: [{ id: 'fixture-identity', confirmedName: 'Speaker Fixture' }],
    voiceReferences: [],
    speakerCorrections: [
      {
        id: 'fixture-correction',
        recordingMediaId: 'fixture-media',
        recordingTranscriptionAttemptId: 'old-attempt',
        speakerIdentityId: 'fixture-identity',
        localSpeakerId: 'local-one',
        passageStartMS: 2000,
        passageEndMS: 5000,
        sourceGeneration: 3,
        isRemoved: false,
      },
    ],
  }
  it('preserves full interval confirmation across new attempts without guessing local group identity', () => {
    expect(confirmedSegmentIdentity(segment, 'fixture-media', view)?.confirmedName).toBe('Speaker Fixture')
    expect(confirmedSegmentIdentity(segment, 'another-media', view)).toBeNull()
    expect(confirmedSegmentIdentity({ ...segment, segmentStartMS: 1000 }, 'fixture-media', view)).toBeNull()
  })
  it('does not restore an old confirmation after removal or attribute partial corrections to whole text', () => {
    expect(
      confirmedSegmentIdentity(segment, 'fixture-media', {
        ...view,
        speakerCorrections: [
          ...view.speakerCorrections,
          { ...view.speakerCorrections[0], sourceGeneration: 5, isRemoved: true },
        ],
      }),
    ).toBeNull()
    expect(
      confirmedSegmentIdentity(segment, 'fixture-media', {
        ...view,
        speakerCorrections: [{ ...view.speakerCorrections[0], passageEndMS: 3000 }],
      }),
    ).toBeNull()
  })
  it('plays the cited interval through the owned original route', () => {
    expect(passagePlaybackPath('fixture/media', 2000, 5000)).toBe('/api/v1/recordings/media/fixture%2Fmedia#t=2,5')
  })
})
