import { useSyncExternalStore } from 'react'

// What the recording bar shows of a recording this tab is making. The
// recorder itself (capture.ts) is fetched only when somebody starts one; this
// is all the shell of the dashboard holds, so a page that never records does
// not download the recorder.
export type CapturePhase = 'idle' | 'starting' | 'recording' | 'paused' | 'stopping'

export interface CaptureSnapshot {
  phase: CapturePhase
  recordingId: string | null
  // elapsedMS is how far the recording has run on its timeline, pauses included.
  elapsedMS: number
  // newestWords are the latest words the live transcription heard.
  newestWords: string
  isLiveConnected: boolean
  // pendingPieceCount are the pieces this browser holds that the server has not verified yet.
  pendingPieceCount: number
  // isStorageDurable is false when pieces can only be kept in memory.
  isStorageDurable: boolean
  errorMessage: string
}

export const IDLE_SNAPSHOT: CaptureSnapshot = {
  phase: 'idle',
  recordingId: null,
  elapsedMS: 0,
  newestWords: '',
  isLiveConnected: false,
  pendingPieceCount: 0,
  isStorageDurable: true,
  errorMessage: '',
}

let current: CaptureSnapshot = IDLE_SNAPSHOT
const listeners = new Set<() => void>()

export function getCaptureSnapshot(): CaptureSnapshot {
  return current
}

export function updateCaptureSnapshot(changes: Partial<CaptureSnapshot>) {
  const next = { ...current, ...changes }
  if ((Object.keys(next) as (keyof CaptureSnapshot)[]).every((key) => next[key] === current[key])) return
  current = next
  for (const listener of listeners) listener()
}

export function subscribeToCaptureSnapshot(listener: () => void) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

export function useCaptureSnapshot(): CaptureSnapshot {
  return useSyncExternalStore(subscribeToCaptureSnapshot, getCaptureSnapshot, getCaptureSnapshot)
}

// isCaptureBusy says whether something would be lost by leaving the page: a
// recording in progress, or pieces not yet on the server.
export const isCaptureBusy = (snapshot: CaptureSnapshot) => snapshot.phase !== 'idle' || snapshot.pendingPieceCount > 0
