export interface RecordingCitation {
  requestedRunId: string | null
  passageStartMS: number | null
  passageEndMS: number | null
  hasInvalidBounds: boolean
}

export function readRecordingCitation(parameters: URLSearchParams): RecordingCitation {
  const requestedRunId = parameters.get('version') || parameters.get('run') || null
  const startText = parameters.get('startMS')
  const endText = parameters.get('endMS')
  if (startText === null && endText === null)
    return { requestedRunId, passageStartMS: null, passageEndMS: null, hasInvalidBounds: false }
  const passageStartMS = startText !== null && /^\d+$/.test(startText) ? Number(startText) : NaN
  const passageEndMS = endText !== null && /^\d+$/.test(endText) ? Number(endText) : NaN
  const hasInvalidBounds =
    !requestedRunId ||
    !Number.isSafeInteger(passageStartMS) ||
    !Number.isSafeInteger(passageEndMS) ||
    passageEndMS <= passageStartMS
  return {
    requestedRunId,
    passageStartMS: hasInvalidBounds ? null : passageStartMS,
    passageEndMS: hasInvalidBounds ? null : passageEndMS,
    hasInvalidBounds,
  }
}

export function overlapsRecordingCitation(segmentStartMS: number, segmentEndMS: number, citation: RecordingCitation) {
  return (
    citation.passageStartMS !== null &&
    citation.passageEndMS !== null &&
    segmentEndMS > citation.passageStartMS &&
    segmentStartMS < citation.passageEndMS
  )
}
