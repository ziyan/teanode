import { useEffect, useRef } from 'react'

// Something outside the page that shows the photos changed them: files that
// were uploaded, photos restored from a toast on another page. The page reads
// its list again. The upload queue and the toasts about it outlive any one page
// of Photos, so they cannot call the page that is open; they say so here.

const PHOTOS_CHANGED = 'teanode:photos-changed'

export function announcePhotosChanged() {
  window.dispatchEvent(new Event(PHOTOS_CHANGED))
}

// Calls the latest `onChanged` each time photos are announced as changed, for as
// long as the component is mounted.
export function usePhotosChanged(onChanged: () => void) {
  const latest = useRef(onChanged)
  latest.current = onChanged
  useEffect(() => {
    const changed = () => latest.current()
    window.addEventListener(PHOTOS_CHANGED, changed)
    return () => window.removeEventListener(PHOTOS_CHANGED, changed)
  }, [])
}
