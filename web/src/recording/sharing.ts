// Sharing a recording: with one person in a circle the owner shares, or with a
// whole circle, at one of two levels. docs/subsystems/recordings.md
// ("Sharing") says what each shows. The audio, the voice samples and the
// contacts a speaker is tied to are never shared at either level.

export const RECORDING_SHARE_LEVELS = ['summary', 'transcript'] as const
export type RecordingShareLevel = (typeof RECORDING_SHARE_LEVELS)[number]

export interface RecordingShare {
  id: string
  recordingId: string
  granteeUserId: string | null
  granteeUsername: string
  granteeName: string
  granteeCircleId: string | null
  granteeCircleName: string
  recordingShareLevel: RecordingShareLevel
  createdAt: string
}

export interface SharedRecordingPassage {
  segmentStartMS: number
  segmentEndMS: number
  speakerName: string
  transcriptText: string
  isFiller: boolean
}

// A recording as a reader sees it, at the level the share gives them. The
// list leaves out the speaker names and the passages; a read of one has them.
export interface SharedRecording {
  recordingId: string
  ownerUserId: string
  ownerUsername: string
  ownerName: string
  recordingTitle: string
  createdAt: string
  recordedAt: string | null
  timelineDurationMS: number
  recordingSummary: string
  recordingShareLevel: RecordingShareLevel
  speakerNames: string[]
  passages: SharedRecordingPassage[]
  passageCount: number
  hasMorePassages: boolean
}

const shareFields =
  'id recordingId granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName recordingShareLevel createdAt'
const sharedListingFields =
  'recordingId ownerUserId ownerUsername ownerName recordingTitle createdAt recordedAt timelineDurationMS recordingSummary recordingShareLevel'

export const RECORDING_SHARE_DOCUMENTS = {
  ListRecordingShares: `query ($recordingId: String!) { ListRecordingShares(recordingId: $recordingId) { ${shareFields} } }`,
  ShareRecording: `mutation ($recordingId: String!, $granteeUserId: String, $granteeCircleId: String, $recordingShareLevel: String!) { ShareRecording(recordingId: $recordingId, granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, recordingShareLevel: $recordingShareLevel) { id } }`,
  UnshareRecording: 'mutation ($recordingShareId: String!) { UnshareRecording(recordingShareId: $recordingShareId) }',
  ListSharedRecordings: `query ($beforeRecordingId: String, $pageSize: Int!) { ListSharedRecordings(beforeRecordingId: $beforeRecordingId, pageSize: $pageSize) { ${sharedListingFields} } }`,
  ReadSharedRecording: `query ($recordingId: String!, $passageOffset: Int!, $pageSize: Int!) { ReadSharedRecording(recordingId: $recordingId, passageOffset: $passageOffset, pageSize: $pageSize) { ${sharedListingFields} speakerNames passages { segmentStartMS segmentEndMS speakerName transcriptText isFiller } passageCount hasMorePassages } }`,
}

export interface SpeakerTurn {
  turnKey: string
  speakerName: string
  startMS: number
  passages: SharedRecordingPassage[]
}

// The passages a reader sees as the owner's screens show them: filler left
// out, and the passages of one speaker in a row together as one turn.
export function groupSharedPassages(passages: SharedRecordingPassage[]): SpeakerTurn[] {
  const turns: SpeakerTurn[] = []
  for (const passage of passages) {
    if (passage.isFiller || passage.transcriptText.trim() === '') continue
    const last = turns[turns.length - 1]
    if (last && last.speakerName === passage.speakerName) {
      last.passages.push(passage)
      continue
    }
    turns.push({
      turnKey: `${passage.segmentStartMS}-${passage.segmentEndMS}`,
      speakerName: passage.speakerName,
      startMS: passage.segmentStartMS,
      passages: [passage],
    })
  }
  return turns
}
