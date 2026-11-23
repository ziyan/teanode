import { useState } from 'react'

import { useTranslation } from '../i18n/i18n'
import { mediaArrangementOf, mediaAspectOf, webAddressOf, type FeedMedia } from './feedMediaLayout'
import { GlobeIcon, PlayIcon } from './icons'

// Pictures and videos of a feed card. The server sends paths that work for the
// signed-in person's session, so they are used as the address of an image or
// video as they are, the way a link preview's picture is.

function MediaTile({ media, isSingle }: { media: FeedMedia; isSingle: boolean }) {
  const { t } = useTranslation()
  const [isPlaying, setIsPlaying] = useState(false)
  // A picture that cannot be had is left out, and the card keeps its words.
  const [isBroken, setIsBroken] = useState(false)
  if (isBroken) return null
  const aspect = isSingle ? mediaAspectOf(media) : 'wide'
  const description = media.mediaCaption || media.siteName
  const pageAddress = media.feedMediaKind === 'picture' ? webAddressOf(media.linkUrl) : ''
  const hasSize = media.widthPixels !== null && media.heightPixels !== null
  const image = (
    <img
      className="feed-media-image"
      src={media.imageUrl}
      alt={description}
      loading="lazy"
      width={hasSize ? (media.widthPixels ?? undefined) : undefined}
      height={hasSize ? (media.heightPixels ?? undefined) : undefined}
      onError={() => setIsBroken(true)}
    />
  )
  let frame
  if (media.feedMediaKind === 'video' && media.videoUrl) {
    frame = isPlaying ? (
      <video
        className="feed-media-video"
        src={media.videoUrl}
        poster={media.imageUrl}
        controls
        autoPlay
        playsInline
        preload="metadata"
        onError={() => setIsPlaying(false)}
      />
    ) : (
      <button
        type="button"
        className="feed-media-play-button"
        aria-label={t('feed.playVideo')}
        onClick={() => setIsPlaying(true)}
      >
        {image}
        <span className="feed-media-play" aria-hidden="true">
          <PlayIcon size={22} />
        </span>
      </button>
    )
  } else if (pageAddress) {
    frame = (
      <a
        className="feed-media-link"
        href={pageAddress}
        target="_blank"
        rel="noopener noreferrer"
        aria-label={t('feed.openPage', { title: description || pageAddress })}
      >
        {image}
      </a>
    )
  } else {
    frame = image
  }
  return (
    <figure className={`feed-media-tile is-${aspect}`}>
      <div className="feed-media-frame">{frame}</div>
      {media.mediaCaption && <figcaption className="muted feed-media-caption">{media.mediaCaption}</figcaption>}
    </figure>
  )
}

// FeedMediaGallery draws the media of a whole card: one item full width, two to
// four in a grid, more in a row that scrolls sideways.
export function FeedMediaGallery({ cardMedia }: { cardMedia: FeedMedia[] }) {
  const { t } = useTranslation()
  const arrangement = mediaArrangementOf(cardMedia.length)
  if (arrangement === 'none') return null
  return (
    <div className={`feed-media is-${arrangement}`} role="group" aria-label={t('feed.mediaGroup')}>
      {cardMedia.map((media) => (
        <MediaTile key={media.mediaKey} media={media} isSingle={arrangement === 'single'} />
      ))}
    </div>
  )
}

// FeedRowThumbnail is the small square picture at the leading edge of a list row.
// A picture that fails to load leaves the empty tile, so the row's words stay
// in line with the other rows'.
export function FeedRowThumbnail({ media }: { media: FeedMedia }) {
  const [isBroken, setIsBroken] = useState(false)
  if (isBroken) return <FeedRowEmptyThumbnail />
  return (
    <img
      className="feed-row-thumbnail"
      src={media.imageUrl}
      alt=""
      loading="lazy"
      width={56}
      height={56}
      onError={() => setIsBroken(true)}
    />
  )
}

// FeedRowEmptyThumbnail stands in a list row without a picture when other rows
// of the list have one, so every row's words start at the same edge.
export function FeedRowEmptyThumbnail() {
  return (
    <span className="feed-row-thumbnail feed-row-thumbnail-empty" aria-hidden="true">
      <GlobeIcon size={22} />
    </span>
  )
}
