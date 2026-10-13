import { useCallback, useEffect, useRef, useState } from 'react'
import { recordingOperation, type TranscriptSegment, type TranscriptTimeline, type TranscriptView } from './processing'

// How many passages are read at a time as the reader scrolls.
export const TRANSCRIPT_PAGE_SIZE = 100

// readTranscriptPage reads the passages from an offset: the latest transcript of
// the recording, or one earlier version of it that a citation points at.
export async function readTranscriptPage(
  recordingId: string,
  olderRunId: string,
  segmentOffset: number,
): Promise<TranscriptTimeline> {
  if (olderRunId) {
    const view = await recordingOperation<TranscriptView>('ReadRecordingTranscript', {
      recordingTranscriptRunId: olderRunId,
      segmentOffset,
      pageSize: TRANSCRIPT_PAGE_SIZE,
    })
    return { ...view, sourceGeneration: 0, transcriptRuns: [view.transcriptRun] }
  }
  return recordingOperation<TranscriptTimeline>('ReadRecordingTranscriptTimeline', {
    recordingId,
    segmentOffset,
    pageSize: TRANSCRIPT_PAGE_SIZE,
  })
}

// appendSegments adds a page after the passages already read. A passage that is
// already there (the transcript grew or changed between two reads) is not added
// twice.
export function appendSegments(loaded: TranscriptSegment[], page: TranscriptSegment[]): TranscriptSegment[] {
  const known = new Set(loaded.map((segment) => segment.id))
  return [...loaded, ...page.filter((segment) => !known.has(segment.id))]
}

// useTranscriptPages reads the transcript a page at a time and keeps what it has
// read as one list, so the conversation is drawn from the whole of it and a
// bubble is grouped across the edge of two pages. Reading again (the reload
// token changing: a capture in progress, a transcript being made, an edit) reads
// the same number of passages afresh from the start.
export function useTranscriptPages({
  recordingId,
  olderRunId,
  userId,
  reloadToken,
}: {
  recordingId: string
  olderRunId: string
  userId: string
  reloadToken: number
}) {
  const [timeline, setTimeline] = useState<TranscriptTimeline | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [isLoading, setIsLoading] = useState(true)
  const timelineReference = useRef<TranscriptTimeline | null>(null)
  const isLoadingMore = useRef(false)
  // Which read is the current one; a read that has been replaced drops its answer.
  const generation = useRef(0)
  const lastKey = useRef('')

  const publish = (next: TranscriptTimeline | null) => {
    timelineReference.current = next
    setTimeline(next)
  }

  useEffect(() => {
    const key = `${recordingId}/${olderRunId}/${userId}`
    const isSameTranscript = lastKey.current === key
    lastKey.current = key
    const mine = ++generation.current
    isLoadingMore.current = false
    const wantedCount = isSameTranscript ? (timelineReference.current?.segments.length ?? 0) : 0
    if (!isSameTranscript) {
      publish(null)
      setIsLoading(true)
    }
    void (async () => {
      try {
        let result: TranscriptTimeline | null = null
        let segments: TranscriptSegment[] = []
        do {
          const page = await readTranscriptPage(recordingId, olderRunId, segments.length)
          if (mine !== generation.current) return
          const readCount = segments.length
          segments = appendSegments(segments, page.segments)
          // A page that adds nothing ends the reading, however many more the server counts.
          result = { ...page, segments, hasMoreSegments: segments.length > readCount && page.hasMoreSegments }
        } while (result.hasMoreSegments && segments.length < wantedCount)
        publish(result)
        setError(null)
      } catch (caught) {
        if (mine !== generation.current) return
        setError(caught)
      } finally {
        if (mine === generation.current) setIsLoading(false)
      }
    })()
    return () => {
      generation.current++
    }
    // The token and the transcript's identity are what make a new read.
  }, [recordingId, olderRunId, userId, reloadToken])

  // loadMore reads the page after the last passage read. It does nothing while
  // another page is being read or when the transcript has no more.
  const loadMore = useCallback(async () => {
    const current = timelineReference.current
    if (!current || !current.hasMoreSegments || isLoadingMore.current) return
    const mine = generation.current
    isLoadingMore.current = true
    setIsLoading(true)
    try {
      const page = await readTranscriptPage(recordingId, olderRunId, current.segments.length)
      if (mine !== generation.current) return
      const segments = appendSegments(current.segments, page.segments)
      publish({
        ...page,
        segments,
        hasMoreSegments: segments.length > current.segments.length && page.hasMoreSegments,
      })
      setError(null)
    } catch (caught) {
      if (mine === generation.current) setError(caught)
    } finally {
      if (mine === generation.current) {
        isLoadingMore.current = false
        setIsLoading(false)
      }
    }
  }, [recordingId, olderRunId])

  // loadAll reads every page that is left, for the reader who asks for the end.
  const loadAll = useCallback(async () => {
    for (;;) {
      const before = timelineReference.current?.segments.length ?? 0
      await loadMore()
      const after = timelineReference.current
      if (!after || !after.hasMoreSegments || after.segments.length === before) return
    }
  }, [loadMore])

  return { timeline, error, isLoading, loadMore, loadAll }
}
