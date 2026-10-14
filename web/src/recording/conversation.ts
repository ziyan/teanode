import { type LiveUtterance, type TranscriptSegment } from './processing'

export interface ConversationGroup {
  groupKey: string
  isAccountOwner: boolean
  // The confirmed name; null for an unconfirmed speaker, who is shown by letter.
  speakerName: string | null
  speakerLetter: string
  // The label of a speaker of a capture in progress ("Speaker 1") that nobody
  // has named yet; empty for everyone else.
  temporarySpeakerLabel: string
  // A bubble of words heard live, which no speaker-separated transcript
  // covers yet: it has no speaker, and no segments.
  isLive: boolean
  liveUtterances: LiveUtterance[]
  groupStartMS: number
  segments: TranscriptSegment[]
}

const letterAt = (index: number) =>
  String.fromCharCode(65 + (index % 26)) + (index >= 26 ? String(Math.floor(index / 26)) : '')

// How many words and how long a passage can be and still be taken as a word
// thrown in (diarization splits a few words spoken together into passages
// under a second), which stays in the bubble it came in the middle of. The
// same limits as the iPhone's.
export const SHORT_PASSAGE_WORDS_MAXIMUM = 3
export const SHORT_PASSAGE_DURATION_MS_MAXIMUM = 800

const isConfirmedSegment = (segment: TranscriptSegment) =>
  Boolean(
    (segment.hasUserConfirmedSpeaker || segment.isRecognizedSpeaker) &&
    segment.effectiveSpeakerIdentityId &&
    segment.effectiveSpeakerName,
  )

// A passage with no words (a breath or a click a provider gave its own time)
// is not drawn: it would be an empty bubble. Nor is one of only filler
// ("Mm-hmm. Okay."), which the server marks: it says nothing a reader needs.
const hasWords = (segment: TranscriptSegment) =>
  !segment.isFiller && (segment.effectiveTranscriptText || segment.transcriptText).trim() !== ''

// Whether this is a passage that is only a few words and a moment long.
const isShortPassage = (segment: TranscriptSegment) => {
  const wordCount = (segment.effectiveTranscriptText || segment.transcriptText).split(/\s+/).filter(Boolean).length
  return (
    wordCount <= SHORT_PASSAGE_WORDS_MAXIMUM &&
    segment.segmentEndMS - segment.segmentStartMS <= SHORT_PASSAGE_DURATION_MS_MAXIMUM
  )
}

// Which words of a capture in progress are drawn with the page of passages
// that is shown: the first page starts at the beginning, a later page at its
// first passage, and a page that is not the last stops at its last passage.
export interface LiveRange {
  fromMS: number
  toMS: number
}

export function liveRangeOfPage(segments: TranscriptSegment[], isFirstPage: boolean, isLastPage: boolean): LiveRange {
  return {
    fromMS: isFirstPage || segments.length === 0 ? Number.NEGATIVE_INFINITY : segments[0].segmentStartMS,
    toMS:
      isLastPage || segments.length === 0
        ? Number.POSITIVE_INFINITY
        : Math.max(...segments.map((segment) => segment.segmentEndMS)),
  }
}

// Consecutive passages by one speaker become one bubble. A confirmed speaker,
// or one recognized by a voice sample, is told apart by identity; a speaker of
// a capture in progress that nobody has named by the temporary label the
// server gave it; any other unconfirmed one only by the generated label, which
// is shown as a letter in order of first appearance.
//
// A word or two the diarization split off would otherwise become a bubble of
// its own, and two speakers answering in turn at the same second make a bubble
// for each word: such a passage stays in the bubble that is being built, and
// plays and copies as part of it.
//
// Words heard live are placed among the passages by the time they began, and
// consecutive ones are one bubble with no speaker. The server leaves out any
// that a finished passage covers, so a word is never drawn twice.
export function groupConversation(
  segments: TranscriptSegment[],
  liveUtterances: LiveUtterance[] = [],
  liveRange: LiveRange = { fromMS: Number.NEGATIVE_INFINITY, toMS: Number.POSITIVE_INFINITY },
): ConversationGroup[] {
  const groups: ConversationGroup[] = []
  const letterIndexByLocalSpeaker = new Map<string, number>()
  type Item = { startMS: number; segment?: TranscriptSegment; utterance?: LiveUtterance }
  const items: Item[] = [
    ...segments.filter(hasWords).map((segment) => ({ startMS: segment.segmentStartMS, segment })),
    ...liveUtterances
      .filter(
        (utterance) => utterance.utteranceStartMS >= liveRange.fromMS && utterance.utteranceStartMS < liveRange.toMS,
      )
      .map((utterance) => ({ startMS: utterance.utteranceStartMS, utterance })),
  ]
  // Stable: a passage and words that began together keep the passage first.
  items.sort((first, second) => first.startMS - second.startMS)
  for (const [itemIndex, item] of items.entries()) {
    if (item.utterance) {
      const previous = groups[groups.length - 1]
      if (previous?.isLive) {
        previous.liveUtterances.push(item.utterance)
        continue
      }
      groups.push({
        groupKey: `live@${item.utterance.utteranceId}`,
        isAccountOwner: false,
        speakerName: null,
        speakerLetter: '',
        temporarySpeakerLabel: '',
        isLive: true,
        liveUtterances: [item.utterance],
        groupStartMS: item.utterance.utteranceStartMS,
        segments: [],
      })
      continue
    }
    const segment = item.segment as TranscriptSegment
    const isConfirmed = isConfirmedSegment(segment)
    const temporarySpeakerLabel = !isConfirmed ? (segment.temporarySpeakerLabel ?? '') : ''
    const speakerKey = speakerKeyOf(segment)
    const previous = groups[groups.length - 1]
    // A named person's few words stand as their answer, unless they came in
    // the middle of someone's sentence (it has not ended) and that person goes
    // straight on: diarization splits words said together.
    const following = items[itemIndex + 1]?.segment
    const interrupted = previous?.segments[previous.segments.length - 1]
    const isBetweenOnePersonsWords =
      !isConfirmed ||
      (following !== undefined &&
        interrupted !== undefined &&
        !/[.?!。？！]\s*$/.test(interrupted.effectiveTranscriptText || interrupted.transcriptText) &&
        previous?.groupKey.startsWith(`${speakerKeyOf(following)}@`))
    if (
      previous &&
      !previous.isLive &&
      !previous.groupKey.startsWith(`${speakerKey}@`) &&
      isShortPassage(segment) &&
      isBetweenOnePersonsWords
    ) {
      previous.segments.push(segment)
      continue
    }
    if (!isConfirmed && !temporarySpeakerLabel && !letterIndexByLocalSpeaker.has(segment.localSpeakerId))
      letterIndexByLocalSpeaker.set(segment.localSpeakerId, letterIndexByLocalSpeaker.size)
    if (previous && !previous.isLive && previous.groupKey.startsWith(`${speakerKey}@`)) {
      previous.segments.push(segment)
      continue
    }
    groups.push({
      groupKey: `${speakerKey}@${segment.id}`,
      isAccountOwner: isConfirmed && segment.isAccountOwner === true,
      speakerName: isConfirmed ? segment.effectiveSpeakerName : null,
      speakerLetter:
        isConfirmed || temporarySpeakerLabel
          ? ''
          : letterAt(letterIndexByLocalSpeaker.get(segment.localSpeakerId) ?? 0),
      temporarySpeakerLabel,
      isLive: false,
      liveUtterances: [],
      groupStartMS: segment.segmentStartMS,
      segments: [segment],
    })
  }
  return groups
}

// speakerKeyOf is who a passage is by, as far as the data goes: a confirmed or
// recognized person by identity, a speaker of a capture by its temporary label,
// anyone else by the label the transcription gave.
function speakerKeyOf(segment: TranscriptSegment): string {
  if (isConfirmedSegment(segment)) return `identity:${segment.effectiveSpeakerIdentityId}`
  if (segment.temporarySpeakerLabel) return `temporary:${segment.temporarySpeakerLabel}`
  return `local:${segment.localSpeakerId}`
}

export const distinctSpeakerCount = (segments: TranscriptSegment[]) =>
  new Set(
    segments
      .filter(hasWords)
      .map((segment) =>
        (segment.hasUserConfirmedSpeaker || segment.isRecognizedSpeaker) && segment.effectiveSpeakerIdentityId
          ? `identity:${segment.effectiveSpeakerIdentityId}`
          : segment.temporarySpeakerLabel
            ? `temporary:${segment.temporarySpeakerLabel}`
            : `local:${segment.localSpeakerId}`,
      ),
  ).size

// temporarySpeakerNumber is the 1 of "Speaker 1", so the label can be said in
// the reader's language; null for a label that does not end in a number.
export function temporarySpeakerNumber(speakerLabel: string): string | null {
  return /(\d+)\s*$/.exec(speakerLabel)?.[1] ?? null
}
