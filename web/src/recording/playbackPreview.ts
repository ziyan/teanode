import { graphql } from '../api'
import { translateNow } from '../i18n/i18n'
import { recordingMediaPath, type RecordingMedia } from './api'

export interface RecordingPlayback {
  recordingMediaId: string
  playbackPath: string
  playbackContentType: string
  playbackSizeBytes: number
  playbackSha256: string
  playbackDurationMS: number
  timelineStartMS: number
}
const playbackSelection =
  '{ recordingMediaId playbackPath playbackContentType playbackSizeBytes playbackSha256 playbackDurationMS timelineStartMS }'
export const PLAYBACK_DOCUMENTS = {
  ReadRecordingPlayback: `query ($recordingMediaId: String!) { ReadRecordingPlayback(recordingMediaId: $recordingMediaId) ${playbackSelection} }`,
  PrepareRecordingPlayback: `mutation ($recordingMediaId: String!) { PrepareRecordingPlayback(recordingMediaId: $recordingMediaId) ${playbackSelection} }`,
}

export function isMatchingPlayback(
  media: Pick<RecordingMedia, 'id' | 'timelineStartMS'>,
  playback: RecordingPlayback,
): boolean {
  return (
    playback.recordingMediaId === media.id &&
    playback.playbackPath === `${recordingMediaPath(media.id)}/playback` &&
    playback.playbackContentType === 'audio/wav' &&
    Number.isSafeInteger(playback.playbackSizeBytes) &&
    playback.playbackSizeBytes > 0 &&
    /^[a-f0-9]{64}$/.test(playback.playbackSha256) &&
    Number.isSafeInteger(playback.playbackDurationMS) &&
    playback.playbackDurationMS > 0 &&
    Number.isSafeInteger(playback.timelineStartMS) &&
    playback.timelineStartMS >= 0 &&
    playback.timelineStartMS === media.timelineStartMS
  )
}

export async function prepareRecordingPlayback(
  media: Pick<RecordingMedia, 'id' | 'timelineStartMS'>,
  signal: AbortSignal,
): Promise<RecordingPlayback> {
  const response = await graphql<{ PrepareRecordingPlayback: RecordingPlayback }>(
    PLAYBACK_DOCUMENTS.PrepareRecordingPlayback,
    { recordingMediaId: media.id },
    signal,
  )
  const playback = response.PrepareRecordingPlayback
  if (!playback || !isMatchingPlayback(media, playback)) throw new Error(translateNow('recordings.previewReceiptError'))
  return playback
}
