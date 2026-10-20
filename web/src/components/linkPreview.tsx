import { useEffect, useMemo, useRef, useState } from 'react'

import { useTranslation } from '../i18n/i18n'
import { requestLinkPreview, type LinkPreview } from '../lib/linkPreviews'
import { firstLinkOf } from '../lib/linkText'

// The card under a message that holds a link: the page's picture, its site, its
// title in bold and two lines about it. One card for the first link, and the
// whole card opens it. Nothing is drawn while the preview is on its way or when
// there is none, so a message never makes room for a card that does not come.
//
// The picture is the server's copy of the page's, never the page's own: the
// page's site is not told who is reading.
export function LinkPreviewCard({
  text,
  isMarkdown,
  isOwn = false,
}: {
  text: string
  isMarkdown: boolean
  /** The card sits under a message of the signed-in person's, on the right. */
  isOwn?: boolean
}) {
  const link = useMemo(() => firstLinkOf(text, isMarkdown), [text, isMarkdown])
  const [preview, setPreview] = useState<LinkPreview | null>(null)
  // Whether the message has been near the screen. A long conversation holds
  // many links and only the ones that are read are worth the server's time.
  const [isNear, setIsNear] = useState(typeof IntersectionObserver === 'undefined')
  const marker = useRef<HTMLSpanElement>(null)

  useEffect(() => {
    const element = marker.current
    if (isNear || !link || !element) return
    const watcher = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) setIsNear(true)
      },
      { rootMargin: '300px' },
    )
    watcher.observe(element)
    return () => watcher.disconnect()
  }, [isNear, link])

  useEffect(() => {
    if (!link || !isNear) return
    let isCancelled = false
    void requestLinkPreview(link).then((found) => {
      if (!isCancelled) setPreview(found)
    })
    return () => {
      isCancelled = true
    }
  }, [link, isNear])

  if (!link) return null
  return (
    <>
      <span ref={marker} className="link-card-marker" aria-hidden="true" />
      {preview?.isAvailable && /^https?:\/\//i.test(preview.cleanedUrl) ? (
        <Card preview={preview} isOwn={isOwn} />
      ) : null}
    </>
  )
}

function Card({ preview, isOwn }: { preview: LinkPreview; isOwn: boolean }) {
  const { t } = useTranslation()
  // A picture that cannot be had is left out, and the card keeps its words.
  const [isImageBroken, setIsImageBroken] = useState(false)
  const hasSize = preview.imageWidthPixels !== null && preview.imageHeightPixels !== null
  return (
    <a
      className={isOwn ? 'link-card is-own' : 'link-card'}
      href={preview.cleanedUrl}
      target="_blank"
      rel="noopener noreferrer"
      aria-label={t('linkPreview.open', { title: preview.title || preview.siteName })}
    >
      {preview.imageUrl && !isImageBroken ? (
        <img
          className="link-card-image"
          src={preview.imageUrl}
          alt=""
          loading="lazy"
          onError={() => setIsImageBroken(true)}
          width={hasSize ? (preview.imageWidthPixels ?? undefined) : undefined}
          height={hasSize ? (preview.imageHeightPixels ?? undefined) : undefined}
        />
      ) : null}
      <span className="link-card-text">
        {preview.siteName ? <span className="link-card-site">{preview.siteName}</span> : null}
        {preview.title ? <strong className="link-card-title">{preview.title}</strong> : null}
        {preview.description ? <span className="link-card-description">{preview.description}</span> : null}
      </span>
    </a>
  )
}
