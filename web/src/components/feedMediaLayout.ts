// The pure decisions behind the pictures, videos and moods of a feed card:
// which media belongs to the card and which to a list row, how a card's media
// is laid out, what shape one picture takes, and which gradient a weather card
// is drawn on. They live apart from the components so they can be tested
// without drawing anything.

export type FeedMediaKind = 'photo' | 'video' | 'picture' | 'camera_still'

export interface FeedMedia {
  mediaKey: string
  feedMediaKind: FeedMediaKind
  imageUrl: string
  videoUrl: string
  linkUrl: string
  siteName: string
  mediaCaption: string
  widthPixels: number | null
  heightPixels: number | null
  // Null: the card's own media. A number: the thumbnail of that row of a list card.
  listRowIndex: number | null
  // Of a camera still: which camera, when it was taken on that camera's own
  // clock (written with its offset), why this camera was chosen, and where
  // the recording and the live view open. Empty for other media.
  cameraName?: string
  capturedAt?: string
  stillReason?: string
  playbackUrl?: string
  liveUrl?: string
}

export interface SplitFeedMedia {
  cardMedia: FeedMedia[]
  rowMediaByIndex: Map<number, FeedMedia>
}

// splitFeedMedia separates the pictures of the whole card from the thumbnails
// of its list rows. A row keeps only its first thumbnail.
export function splitFeedMedia(feedMedia: FeedMedia[] | null | undefined): SplitFeedMedia {
  const cardMedia: FeedMedia[] = []
  const rowMediaByIndex = new Map<number, FeedMedia>()
  for (const media of feedMedia ?? []) {
    if (media.listRowIndex === null || media.listRowIndex === undefined) {
      cardMedia.push(media)
    } else if (!rowMediaByIndex.has(media.listRowIndex)) {
      rowMediaByIndex.set(media.listRowIndex, media)
    }
  }
  return { cardMedia, rowMediaByIndex }
}

export type FeedMediaArrangement = 'none' | 'single' | 'grid' | 'scroll'

// mediaArrangementOf is how a card's media is laid out: one item full width,
// two to four in a grid, more in a row that scrolls sideways.
export function mediaArrangementOf(mediaCount: number): FeedMediaArrangement {
  if (mediaCount <= 0) return 'none'
  if (mediaCount === 1) return 'single'
  if (mediaCount <= 4) return 'grid'
  return 'scroll'
}

export type FeedMediaAspect = 'wide' | 'portrait'

// mediaAspectOf is the shape a single item is cropped to: 16:9, or 4:5 for a
// photo that is taller than it is wide. Without a size, 16:9.
export function mediaAspectOf(media: FeedMedia): FeedMediaAspect {
  const { widthPixels, heightPixels } = media
  if (media.feedMediaKind !== 'photo' || !widthPixels || !heightPixels) return 'wide'
  return heightPixels > widthPixels ? 'portrait' : 'wide'
}

// feedHeaderLabelOf is the small line above the headline: the site of the
// first picture that names one, otherwise the name of the card's source.
export function feedHeaderLabelOf(cardMedia: FeedMedia[], feedSourceTitle: string): string {
  const siteName = cardMedia.find((media) => media.feedMediaKind === 'picture' && media.siteName !== '')?.siteName
  return siteName ?? feedSourceTitle
}

// webAddressOf is a link as it may be followed: http or https only.
export function webAddressOf(written: string): string {
  try {
    const parsed = new URL(written)
    return parsed.protocol === 'http:' || parsed.protocol === 'https:' ? parsed.href : ''
  } catch {
    return ''
  }
}

// openableAddressOf is a link a camera still may open: http or https, or the
// app's own scheme.
export function openableAddressOf(written: string): string {
  const web = webAddressOf(written)
  if (web) return web
  try {
    const parsed = new URL(written)
    return parsed.protocol === 'teanode:' ? parsed.href : ''
  } catch {
    return ''
  }
}

export type WeatherGradient = 'clearDay' | 'clearNight' | 'cloudy' | 'rain' | 'snow' | 'storm'

// weatherGradientOf picks the mood of a weather card from the symbol name the
// agent chose and, when that says nothing, the words of the condition. The
// stormiest reading wins: a thunderstorm with rain is a storm.
export function weatherGradientOf(conditionSymbol: string, conditionText: string): WeatherGradient {
  const symbol = conditionSymbol.toLowerCase()
  const text = conditionText.toLowerCase()
  const says = (symbolPart: string, textPattern: RegExp) => symbol.includes(symbolPart) || textPattern.test(text)
  if (says('bolt', /thunder|storm|lightning|雷/)) return 'storm'
  if (says('snow', /snow|sleet|flurr|blizzard|雪/)) return 'snow'
  if (says('rain', /rain|drizzle|shower|雨/) || symbol.includes('drizzle')) return 'rain'
  if (symbol.includes('cloud') || symbol.includes('fog') || /cloud|overcast|fog|mist|haze|云|阴|曇|くもり/.test(text)) {
    return 'cloudy'
  }
  if (says('moon', /night|moon|夜/)) return 'clearNight'
  return 'clearDay'
}

// isMotionReduced says whether the person asked for less motion. Where it cannot
// be asked (no browser), motion is left out.
export function isMotionReduced(): boolean {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return true
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches
}
