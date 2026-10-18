import { createContext, useContext, useEffect, useState, type ReactNode, type RefObject } from 'react'
import { useTranslation } from '../../i18n/i18n'
import type { Photo } from '../../photos/api'
import { personTimeZone, photoDayRange } from '../../photos/days'

// The line under the page's heading that says which days the photos on the
// screen were taken on, "Oct 3 to 6, 2026", changing as one scrolls.
//
// Every page of Photos has the line, empty where it has nothing to say (Albums,
// People, the Trash), so that the tabs under it are in the same place on all of
// them. It belongs to the layout the pages sit in, and the page that has
// something to say hands the words up.

const SetDateRangeContext = createContext<(text: string) => void>(() => {})

export function PhotoDateRangeLine({ children }: { children: ReactNode }) {
  const [text, setText] = useState('')
  return (
    <SetDateRangeContext.Provider value={setText}>
      {/* A non-breaking space for none, so that the line keeps its height. */}
      <p className="photo-date-range" data-testid="photo-date-range">
        {text || ' '}
      </p>
      {children}
    </SetDateRangeContext.Provider>
  )
}

// Says words on the line for as long as the page is shown, and nothing after.
export function useShowDateRange(text: string) {
  const setText = useContext(SetDateRangeContext)
  useEffect(() => {
    setText(text)
    return () => setText('')
  }, [setText, text])
}

// The days the cells of a grid that are on the screen were taken on. Read from
// where the cells are, so that it follows scrolling in any container, and
// kept as the words and not as the photos: the page is drawn again only when
// the words change, not every time a cell crosses an edge.
//
// A browser that cannot say what is on screen reads the whole list. When
// nothing is on screen for a moment (the list is being replaced) the last
// words stay, so the line does not flicker.
export function useVisibleDayRange(
  container: RefObject<HTMLElement | null>,
  photos: Photo[],
  isEnabled: boolean,
): string {
  const { language } = useTranslation()
  const [text, setText] = useState('')
  useEffect(() => {
    const element = container.current
    if (!isEnabled || !element || photos.length === 0) {
      setText('')
      return
    }
    const timeZone = personTimeZone()
    const wholeList = () => setText(photoDayRange(photos, language, timeZone))
    if (typeof IntersectionObserver === 'undefined') {
      wholeList()
      return
    }
    const byId = new Map(photos.map((photo) => [photo.id, photo]))
    const visibleIds = new Set<string>()
    const observer = new IntersectionObserver((entries) => {
      for (const entry of entries) {
        const photoId = (entry.target as HTMLElement).dataset.photoId
        if (!photoId) continue
        if (entry.isIntersecting) visibleIds.add(photoId)
        else visibleIds.delete(photoId)
      }
      const visible = Array.from(visibleIds, (photoId) => byId.get(photoId)).filter(
        (photo): photo is Photo => photo !== undefined,
      )
      if (visible.length > 0) setText(photoDayRange(visible, language, timeZone))
    })
    element.querySelectorAll('[data-photo-id]').forEach((cell) => observer.observe(cell))
    return () => observer.disconnect()
  }, [container, photos, isEnabled, language])
  return text
}
