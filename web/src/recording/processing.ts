import { graphql } from '../api'
import { storedSettings } from '../components/sourceTypeSettings'
import { RECORDING_PROCESSING_DOCUMENTS } from './processingDocuments'

export interface TranscriptionAvailability {
  isTranscriptionAvailable: boolean
  transcriptionUnavailableMessage: string
  transcriptionProviderModelName: string
}
export interface TranscriptRun {
  id: string
  recordingId: string
  recordingMediaId: string
  versionNumber: number
  recordingProcessingState: string
  providerModelName: string
  processingErrorMessage: string
  derivedDurationMS: number
  createdAt: string
}
export interface TranscriptSegment {
  id: string
  recordingTranscriptRunId: string
  recordingTranscriptionAttemptId: string
  localSpeakerId: string
  segmentStartMS: number
  segmentEndMS: number
  transcriptText: string
  effectiveTranscriptText: string
  appliedTextCorrectionId: string
  hasUnalignedTextCorrection: boolean
  effectiveSpeakerIdentityId: string
  effectiveSpeakerName: string
  hasUserConfirmedSpeaker: boolean
  // isRecognizedSpeaker is a speaker the transcription named after a voice sample.
  isRecognizedSpeaker?: boolean
  hasUnalignedSpeakerCorrection: boolean
  appliedSpeakerCorrectionId: string
  isAccountOwner?: boolean
  // temporarySpeakerLabel ("Speaker 1") is a speaker of a capture in progress that nobody has named yet.
  temporarySpeakerLabel?: string
  // mergedTemporarySpeakerLabel is this passage's own label when it was found to be the voice of another speaker and reads as that one; the person separates from it.
  mergedTemporarySpeakerLabel?: string
  // A passage of only filler ("Mm-hmm. Okay."), which the conversation leaves out.
  isFiller?: boolean
}
export interface LiveUtterance {
  utteranceId: string
  utteranceStartMS: number
  utteranceEndMS: number
  transcriptText: string
}
export interface TranscriptionAttempt {
  id: string
  recordingAttemptState: string
  attemptErrorMessage: string
  providerModelName: string
  hasProviderUsage: boolean
  inputAudioTokens: number
  inputTextTokens: number
  promptTokens: number
  completionTokens: number
  providerAudioDurationMS: number
  estimatedCostAmount: number | null
  actualCostAmount: number | null
  currencyCode: string
}
export interface TranscriptView {
  transcriptRun: TranscriptRun
  segments: TranscriptSegment[]
  attempts: TranscriptionAttempt[]
  segmentCount: number
  hasMoreSegments: boolean
}
export interface TranscriptTimeline {
  sourceGeneration: number
  segments: TranscriptSegment[]
  transcriptRuns: TranscriptRun[]
  temporarySpeakers?: { speakerLabel: string; speakerIdentityId: string; mergedIntoSpeakerLabel?: string }[]
  segmentCount: number
  hasMoreSegments: boolean
}
export interface SpeakerIdentity {
  id: string
  confirmedName: string
}
export interface SpeakerCorrection {
  id: string
  recordingMediaId: string
  recordingTranscriptionAttemptId: string
  speakerIdentityId: string
  localSpeakerId: string
  passageStartMS: number
  passageEndMS: number
  sourceGeneration: number
  isRemoved: boolean
}
export interface VoiceReference {
  id: string
  recordingMediaId: string
  speakerIdentityId: string
  passageStartMS: number
  passageEndMS: number
  sourceGeneration: number
  isRevoked: boolean
}
export interface SpeakersView {
  sourceGeneration: number
  speakerIdentities: SpeakerIdentity[]
  speakerCorrections: SpeakerCorrection[]
  voiceReferences: VoiceReference[]
}
export interface TextCorrection {
  id: string
  recordingMediaId: string
  recordingTranscriptSegmentId: string
  passageStartMS: number
  passageEndMS: number
  originalTranscriptText: string
  correctedTranscriptText: string
  sourceGeneration: number
  isRemoved: boolean
}
export interface ReferenceInvalidation {
  id: string
  recordingTranscriptRunId: string
  recordingTranscriptionAttemptId: string
  invalidatedConfirmedName: string
  invalidationMessage: string
  isReconciled: boolean
}
export interface RecordingLearning {
  sourceGeneration: number
  hasAgent: boolean
  hasStoppedReading: boolean
  pendingDocumentCount: number
  digestedDocumentCount: number
  lastPublishedSourceGeneration: number
  isCurationPending: boolean
  knowledgeSource: {
    id: string
    kind: string
    name: string
    specification: { settings: unknown }
    enabled: boolean
    cron: string
    lastError: string
    lastRunAt: string | null
    documentCount: number
    chunkCount: number
  } | null
}
export async function recordingOperation<T>(
  operationName: keyof typeof RECORDING_PROCESSING_DOCUMENTS,
  variables: Record<string, unknown>,
  signal?: AbortSignal,
): Promise<T> {
  const response = await graphql<Record<string, T>>(RECORDING_PROCESSING_DOCUMENTS[operationName], variables, signal)
  return response[operationName]
}
// Only the server's canonical projection can confirm a segment speaker.
// Equal-time overlapping speakers and ambiguous reruns remain unconfirmed.
export function confirmedSegmentName(segment: TranscriptSegment): string | null {
  return (segment.hasUserConfirmedSpeaker || segment.isRecognizedSpeaker) &&
    segment.effectiveSpeakerIdentityId &&
    segment.effectiveSpeakerName
    ? segment.effectiveSpeakerName
    : null
}
export const passagePlaybackPath = (mediaId: string, startMS: number, endMS: number, timelineStartMS: number) =>
  `/api/v1/recordings/media/${encodeURIComponent(mediaId)}#t=${Math.max(0, startMS - timelineStartMS) / 1000},${Math.max(0, endMS - timelineStartMS) / 1000}`

export function recordingSourceTitle(settings: unknown): string | null {
  const recordingTitle = storedSettings(settings).recordingTitle
  return typeof recordingTitle === 'string' && recordingTitle.trim() ? recordingTitle : null
}
