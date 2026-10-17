import { useTranslation } from '../../i18n/i18n'
import type { Photo } from '../../photos/api'
import { personTimeZone, photoAltText } from '../../photos/days'

// What a picture is called to somebody who cannot see it, in the reader's
// language. The grid knows only where and when a photo was taken; the viewer
// has its description as well and passes the first sentence of it.
export function usePhotoAltText(): (photo: Photo, description?: string) => string {
  const { t, language } = useTranslation()
  return (photo, description = '') =>
    photoAltText(
      photo,
      description,
      language,
      {
        placeAndDate: (place, date) => t('photos.altPlaceAndDate', { place, date }),
        dateOnly: (date) => t('photos.altDateOnly', { date }),
      },
      personTimeZone(),
    )
}
