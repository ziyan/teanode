import { APIError, authorization, graphql } from '../api'
import { translateNow } from '../i18n/i18n'
import { RecordingSha256, recordingDigest } from './sha256'

export interface Recording {
  clientRecordingId: string
  id: string
  userId: string
  recordingTitle: string
  recordingOrigin: string
  recordingCaptureState: string
  sourceGeneration: number
  timelineDurationMS: number
  captureGaps: { gapId: string; gapStartMS: number; gapEndMS: number; recordingGapReason: string }[]
}
export interface RecordingMedia {
  id: string
  recordingId: string
  clientMediaId: string
  originalFilename: string
  originalContentType: string
  originalSizeBytes: number
  originalSha256: string
  timelineStartMS: number
  mediaDurationMS: number
  partCount: number
  recordingUploadState: string
  verifiedAt: string | null
}
export interface RecordingPart {
  recordingMediaId: string
  partIndex: number
  partSizeBytes: number
  partSha256: string
  storedAt: string
}
export interface RecordingView {
  recording: Recording
  mediaFiles: RecordingMedia[]
  uploadParts: RecordingPart[]
}
export interface RecordingLimits {
  maximumMediaBytes: number
  maximumAccountBytes: number
  uploadPartBytes: number
}
const recordingFields =
  'id userId clientRecordingId recordingTitle recordingOrigin recordingCaptureState sourceGeneration timelineDurationMS captureGaps { gapId gapStartMS gapEndMS recordingGapReason }'
const mediaFields =
  'id recordingId clientMediaId originalFilename originalContentType originalSizeBytes originalSha256 timelineStartMS mediaDurationMS partCount recordingUploadState verifiedAt'
export const RECORDING_DOCUMENTS = {
  ListRecordings: `query ($beforeRecordingId: String!, $pageSize: Int!) { ListRecordings(beforeRecordingId: $beforeRecordingId, pageSize: $pageSize) { ${recordingFields} } }`,
  ReadRecording: `query ($recordingId: String!) { ReadRecording(recordingId: $recordingId) { recording { ${recordingFields} } mediaFiles { ${mediaFields} } uploadParts { recordingMediaId partIndex partSizeBytes partSha256 storedAt } } }`,
  ReadRecordingLimits: 'query { ReadRecordingLimits { maximumMediaBytes maximumAccountBytes uploadPartBytes } }',
  CreateRecording: `mutation ($clientRecordingId: String!, $recordingTitle: String!, $recordingOrigin: String!) { CreateRecording(clientRecordingId: $clientRecordingId, recordingTitle: $recordingTitle, recordingOrigin: $recordingOrigin) { ${recordingFields} } }`,
  CreateRecordingMedia: `mutation ($recordingId: String!, $clientMediaId: String!, $originalFilename: String!, $originalContentType: String!, $originalSizeBytes: Int64!, $originalSha256: String!, $timelineStartMS: Int64!, $mediaDurationMS: Int64!) { CreateRecordingMedia(recordingId: $recordingId, clientMediaId: $clientMediaId, originalFilename: $originalFilename, originalContentType: $originalContentType, originalSizeBytes: $originalSizeBytes, originalSha256: $originalSha256, timelineStartMS: $timelineStartMS, mediaDurationMS: $mediaDurationMS) { ${mediaFields} } }`,
  DeleteRecording: 'mutation ($recordingId: String!) { DeleteRecording(recordingId: $recordingId) }',
}
export const RECORDING_PART_BYTES = 8 * 1024 * 1024
export const recordingMediaPath = (mediaId: string) => `/api/v1/recordings/media/${encodeURIComponent(mediaId)}`
export async function readRecording(recordingId: string, signal?: AbortSignal): Promise<RecordingView> {
  const response = await graphql<{ ReadRecording: RecordingView }>(
    RECORDING_DOCUMENTS.ReadRecording,
    { recordingId },
    signal,
  )
  return response.ReadRecording
}
async function recordingTransport<T>(
  resourcePath: string,
  content: Blob | string,
  headers: Record<string, string>,
  signal: AbortSignal,
): Promise<T> {
  const response = await fetch(resourcePath, {
    method: resourcePath.endsWith('/complete') ? 'POST' : 'PUT',
    headers: { ...headers, ...authorization() },
    body: content,
    signal,
  })
  if (!response.ok)
    throw new APIError(
      translateNow('api.serverReturned', { statusCode: response.status }),
      response.status === 401,
      response.status,
    )
  return (await response.json()) as T
}
export function isMatchingCompletion(expected: RecordingMedia, receipt: RecordingMedia): boolean {
  return (
    receipt.id === expected.id &&
    receipt.recordingId === expected.recordingId &&
    receipt.originalSizeBytes === expected.originalSizeBytes &&
    receipt.originalSha256 === expected.originalSha256 &&
    receipt.recordingUploadState === 'complete' &&
    Boolean(receipt.verifiedAt && Number.isFinite(Date.parse(receipt.verifiedAt)))
  )
}
// Only account-scoped recovery metadata is kept. After restart the person must
// reselect the original; it is rehashed before existing server receipts are used.
export async function importRecording(
  file: File,
  recordingTitle: string,
  userId: string,
  signal: AbortSignal,
  onProgress: (completedBytes: number, totalBytes: number, isHashing: boolean) => void,
  resumeMedia?: RecordingMedia | null,
): Promise<RecordingView> {
  if (!userId) throw new Error(translateNow('recordings.accountRequired'))
  const { ReadRecordingLimits: limits } = await graphql<{ ReadRecordingLimits: RecordingLimits }>(
    RECORDING_DOCUMENTS.ReadRecordingLimits,
    {},
    signal,
  )
  if (file.size <= 0 || file.size > limits.maximumMediaBytes || limits.uploadPartBytes !== RECORDING_PART_BYTES)
    throw new Error(translateNow('recordings.limitError'))
  const contentType = recordingContentType(file)
  if (!contentType) throw new Error(translateNow('recordings.typeError'))
  const digest = new RecordingSha256()
  for (let byteOffset = 0; byteOffset < file.size; byteOffset += RECORDING_PART_BYTES) {
    signal.throwIfAborted()
    digest.update(new Uint8Array(await file.slice(byteOffset, byteOffset + RECORDING_PART_BYTES).arrayBuffer()))
    onProgress(Math.min(byteOffset + RECORDING_PART_BYTES, file.size), file.size, true)
  }
  const originalSha256 = digest.hex(),
    clientRecordingId = `file-${originalSha256}`
  const recoveryKey = `recording-import:${window.location.origin}:${userId}:${originalSha256}`
  if (resumeMedia && (resumeMedia.originalSha256 !== originalSha256 || resumeMedia.originalSizeBytes !== file.size))
    throw new Error(translateNow('recordings.resumeMismatch'))
  const resumedView = resumeMedia ? await readRecording(resumeMedia.recordingId, signal) : null
  const recording = resumedView
    ? resumedView.recording
    : (
        await graphql<{ CreateRecording: Recording }>(
          RECORDING_DOCUMENTS.CreateRecording,
          {
            clientRecordingId,
            recordingTitle,
            recordingOrigin: contentType.startsWith('video/') ? 'videoImport' : 'audioImport',
          },
          signal,
        )
      ).CreateRecording
  if (recording.userId !== userId) throw new Error(translateNow('recordings.accountChanged'))
  if (
    typeof recording.id !== 'string' ||
    !recording.id ||
    (!resumeMedia &&
      (recording.clientRecordingId !== clientRecordingId ||
        recording.recordingOrigin !== (contentType.startsWith('video/') ? 'videoImport' : 'audioImport'))) ||
    (resumeMedia && recording.id !== resumeMedia.recordingId)
  )
    throw new Error(translateNow('recordings.manifestError'))
  const media = resumeMedia
    ? resumedView?.mediaFiles.find((candidate) => candidate.id === resumeMedia.id)
    : (
        await graphql<{ CreateRecordingMedia: RecordingMedia }>(
          RECORDING_DOCUMENTS.CreateRecordingMedia,
          {
            recordingId: recording.id,
            clientMediaId: `original-${originalSha256}`,
            originalFilename: file.name,
            originalContentType: contentType,
            originalSizeBytes: file.size,
            originalSha256,
            timelineStartMS: 0,
            mediaDurationMS: 0,
          },
          signal,
        )
      ).CreateRecordingMedia
  if (
    !media ||
    !isMatchingOriginalManifest(media, {
      recordingId: recording.id,
      clientMediaId: resumeMedia?.clientMediaId ?? `original-${originalSha256}`,
      originalFilename: resumeMedia?.originalFilename ?? file.name,
      originalContentType: resumeMedia?.originalContentType ?? contentType,
      originalSizeBytes: file.size,
      originalSha256,
    })
  )
    throw new Error(translateNow('recordings.manifestError'))
  try {
    localStorage.setItem(
      recoveryKey,
      JSON.stringify({ recordingId: recording.id, mediaId: media.id, originalSha256, originalSizeBytes: file.size }),
    )
  } catch {
    /* Server manifests remain recoverable when browser storage is unavailable. */
  }
  const view = await readRecording(recording.id, signal)
  for (let partIndex = 0; partIndex < media.partCount; partIndex++) {
    signal.throwIfAborted()
    const byteOffset = partIndex * RECORDING_PART_BYTES,
      partBlob = file.slice(byteOffset, byteOffset + RECORDING_PART_BYTES)
    const partSha256 = recordingDigest(new Uint8Array(await partBlob.arrayBuffer()))
    const receipt = view.uploadParts.find((part) => part.recordingMediaId === media.id && part.partIndex === partIndex)
    if (!receipt || receipt.partSizeBytes !== partBlob.size || receipt.partSha256 !== partSha256 || !receipt.storedAt) {
      const uploaded = await recordingTransport<RecordingPart>(
        `${recordingMediaPath(media.id)}/parts/${partIndex}`,
        partBlob,
        { 'Content-Type': 'application/octet-stream', 'X-Recording-Part-SHA256': partSha256 },
        signal,
      )
      if (
        uploaded.recordingMediaId !== media.id ||
        uploaded.partIndex !== partIndex ||
        uploaded.partSizeBytes !== partBlob.size ||
        uploaded.partSha256 !== partSha256 ||
        !uploaded.storedAt
      )
        throw new Error(translateNow('recordings.partReceiptError'))
    }
    onProgress(Math.min(byteOffset + RECORDING_PART_BYTES, file.size), file.size, false)
  }
  const receipt = await recordingTransport<RecordingMedia>(
    `${recordingMediaPath(media.id)}/complete`,
    '{}',
    { 'Content-Type': 'application/json' },
    signal,
  )
  if (!isMatchingCompletion(media, receipt)) throw new Error(translateNow('recordings.completionError'))
  try {
    localStorage.removeItem(recoveryKey)
  } catch {
    /* No original bytes are stored or removed by the dashboard. */
  }
  return await readRecording(recording.id, signal)
}
export function recordingContentType(file: Pick<File, 'name' | 'type'>): string {
  if (/^(audio|video)\//.test(file.type)) return file.type.split(';')[0]
  const extension = file.name.split('.').pop()?.toLowerCase()
  const types: Record<string, string> = {
    wav: 'audio/wav',
    mp3: 'audio/mpeg',
    m4a: 'audio/mp4',
    aac: 'audio/aac',
    flac: 'audio/flac',
    ogg: 'audio/ogg',
    caf: 'audio/x-caf',
    aiff: 'audio/aiff',
    mp4: 'video/mp4',
    mov: 'video/quicktime',
    webm: 'video/webm',
    mkv: 'video/x-matroska',
  }
  return extension ? (types[extension] ?? '') : ''
}

export function isMatchingOriginalManifest(
  media: RecordingMedia,
  expected: Pick<
    RecordingMedia,
    | 'recordingId'
    | 'clientMediaId'
    | 'originalFilename'
    | 'originalContentType'
    | 'originalSizeBytes'
    | 'originalSha256'
  >,
): boolean {
  return (
    Number.isSafeInteger(expected.originalSizeBytes) &&
    expected.originalSizeBytes > 0 &&
    typeof media.id === 'string' &&
    media.id.length > 0 &&
    typeof media.clientMediaId === 'string' &&
    media.clientMediaId.length > 0 &&
    Object.entries(expected).every(
      ([fieldName, expectedValue]) => media[fieldName as keyof RecordingMedia] === expectedValue,
    ) &&
    Number.isSafeInteger(media.partCount) &&
    media.partCount === Math.ceil(expected.originalSizeBytes / RECORDING_PART_BYTES)
  )
}
