import { createContext, useCallback, useContext, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'

import { openAgentConversation } from '../../api'
import { Lightbox } from '../../components/lightbox'
import { useTranslation } from '../../i18n/i18n'
import { photoLargePreviewPath } from '../../photos/api'
import { JournalPhoto, clockText } from './journalApi'

// How the day's records open without taking the person off the page. Photos
// open in a lightbox over the day, and a conversation opens in the agent's
// drawer. Every other record is a link to its own page, which Back returns
// from to the same place on the day.

export type JournalOpeners = {
  // Opens the lightbox on a photo of the day. False when the photo is not
  // among the ones the day holds, and the link goes to its page instead.
  openPhoto?: (photoId: string, opener: HTMLElement | null) => boolean
}

const JournalOpenersContext = createContext<JournalOpeners>({})

// conversationOfPath is the conversation a dashboard path opens in the
// agent's chats, with "" for the person's main one, or null for any other
// path. Both /chat and /agent forward to the chats.
export function conversationOfPath(path: string): string | null {
  try {
    const parsed = new URL(path, 'http://dashboard.invalid')
    if (parsed.pathname !== '/chat' && parsed.pathname !== '/agent') return null
    return parsed.searchParams.get('conversation') ?? ''
  } catch {
    return null
  }
}

// photoOfPath is the photo a dashboard path opens, or null.
export function photoOfPath(path: string): string | null {
  const found = /^\/photos\/([^/?#]+)$/.exec(path)
  if (!found) return null
  try {
    return decodeURIComponent(found[1])
  } catch {
    return null
  }
}

// RecordLink is a link to a record that opens it in place when it can: the
// photo in the lightbox, the conversation in the drawer. A click with a
// modifier key, or a drawer that is not there (the agent is off), follows the
// address like any link.
export function RecordLink({
  to,
  className,
  label,
  children,
}: {
  to: string
  className?: string
  // The link's name when its words are not enough, such as a picture.
  label?: string
  children: React.ReactNode
}) {
  const openers = useContext(JournalOpenersContext)
  function onClick(event: React.MouseEvent<HTMLAnchorElement>) {
    if (event.defaultPrevented || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
    const conversationId = conversationOfPath(to)
    if (conversationId !== null) {
      if (openAgentConversation(conversationId)) event.preventDefault()
      return
    }
    const photoId = photoOfPath(to)
    if (photoId !== null && openers.openPhoto?.(photoId, event.currentTarget)) event.preventDefault()
  }
  return (
    <Link to={to} className={className} aria-label={label} onClick={onClick}>
      {children}
    </Link>
  )
}

// PhotoLightbox holds the lightbox of a day's photos and tells the records
// below it how to open one. The lightbox moves through the day's photos in
// the order the day lists them, and closing it puts the focus back on the
// photo that was opened.
export function PhotoLightbox({
  photos,
  timeZone,
  children,
}: {
  photos: JournalPhoto[]
  timeZone: string
  children: React.ReactNode
}) {
  const { t, language } = useTranslation()
  const [index, setIndex] = useState<number | null>(null)
  const opener = useRef<HTMLElement | null>(null)
  const photoIds = useMemo(() => photos.map((photo) => photo.photoId), [photos])
  const openPhoto = useCallback(
    (photoId: string, element: HTMLElement | null) => {
      const found = photoIds.indexOf(photoId)
      if (found < 0) return false
      opener.current = element
      setIndex(found)
      return true
    },
    [photoIds],
  )
  const openers = useMemo(() => ({ openPhoto }), [openPhoto])
  const photo = index === null ? null : photos[index]
  const close = useCallback(() => {
    setIndex(null)
    opener.current?.focus()
  }, [])
  const step = (direction: number) =>
    setIndex((previous) => (previous === null ? null : (previous + direction + photos.length) % photos.length))
  return (
    <JournalOpenersContext.Provider value={openers}>
      {children}
      {photo && index !== null && (
        <Lightbox
          source={photoLargePreviewPath(photo.photoId)}
          name={t(photo.isVideo ? 'journal.videoAt' : 'journal.photoAt', {
            time: clockText(photo.takenAt, timeZone, language),
          })}
          where={photo.placeName}
          openHref={`/photos/${encodeURIComponent(photo.photoId)}`}
          openLabel={t('journal.openInPhotos')}
          position={
            photos.length > 1 ? t('lightbox.position', { current: index + 1, total: photos.length }) : undefined
          }
          onPrevious={photos.length > 1 ? () => step(-1) : undefined}
          onNext={photos.length > 1 ? () => step(1) : undefined}
          onClose={close}
        />
      )}
    </JournalOpenersContext.Provider>
  )
}
