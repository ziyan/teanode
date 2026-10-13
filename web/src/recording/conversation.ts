import { type TranscriptSegment } from './processing'

export interface ConversationGroup {
  groupKey: string
  isAccountOwner: boolean
  // The confirmed name; null for an unconfirmed speaker, who is shown by letter.
  speakerName: string | null
  speakerLetter: string
  groupStartMS: number
  segments: TranscriptSegment[]
}

const letterAt = (index: number) =>
  String.fromCharCode(65 + (index % 26)) + (index >= 26 ? String(Math.floor(index / 26)) : '')

// Consecutive passages by one speaker become one bubble. A confirmed speaker,
// or one recognized by a voice sample, is told apart by identity; an unconfirmed one only by the generated label, which
// is shown as a letter in order of first appearance.
export function groupConversation(segments: TranscriptSegment[]): ConversationGroup[] {
  const groups: ConversationGroup[] = []
  const letterIndexByLocalSpeaker = new Map<string, number>()
  for (const segment of segments) {
    const isConfirmed = Boolean(
      (segment.hasUserConfirmedSpeaker || segment.isRecognizedSpeaker) &&
      segment.effectiveSpeakerIdentityId &&
      segment.effectiveSpeakerName,
    )
    const speakerKey = isConfirmed
      ? `identity:${segment.effectiveSpeakerIdentityId}`
      : `local:${segment.localSpeakerId}`
    if (!isConfirmed && !letterIndexByLocalSpeaker.has(segment.localSpeakerId))
      letterIndexByLocalSpeaker.set(segment.localSpeakerId, letterIndexByLocalSpeaker.size)
    const previous = groups[groups.length - 1]
    if (previous && previous.groupKey.startsWith(`${speakerKey}@`)) {
      previous.segments.push(segment)
      continue
    }
    groups.push({
      groupKey: `${speakerKey}@${segment.id}`,
      isAccountOwner: isConfirmed && segment.isAccountOwner === true,
      speakerName: isConfirmed ? segment.effectiveSpeakerName : null,
      speakerLetter: isConfirmed ? '' : letterAt(letterIndexByLocalSpeaker.get(segment.localSpeakerId) ?? 0),
      groupStartMS: segment.segmentStartMS,
      segments: [segment],
    })
  }
  return groups
}

export const distinctSpeakerCount = (segments: TranscriptSegment[]) =>
  new Set(
    segments.map((segment) =>
      (segment.hasUserConfirmedSpeaker || segment.isRecognizedSpeaker) && segment.effectiveSpeakerIdentityId
        ? `identity:${segment.effectiveSpeakerIdentityId}`
        : `local:${segment.localSpeakerId}`,
    ),
  ).size
