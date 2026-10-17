import { useTranslation } from '../../i18n/i18n'

// The order a browser writes a date in: year, month and day. A date field
// draws itself in the language of the browser, not of the page (the lang
// attribute does not change it), so a page in Japanese in an English browser
// has fields that read month/day/year.
export function dateFieldOrder(browserLanguage: string): ('year' | 'month' | 'day')[] {
  try {
    return new Intl.DateTimeFormat(browserLanguage, { year: 'numeric', month: '2-digit', day: '2-digit' })
      .formatToParts(new Date(2000, 0, 2))
      .map((part) => part.type)
      .filter((type): type is 'year' | 'month' | 'day' => type === 'year' || type === 'month' || type === 'day')
  } catch {
    return []
  }
}

// The line under a date field that says how it is written, when that is not
// the way the page's own language writes it: nothing for a browser and a page
// in one language, which is most.
export function DateFormatHint() {
  const { t, language } = useTranslation()
  const browserLanguage = typeof navigator === 'undefined' ? '' : navigator.language
  if (!browserLanguage || browserLanguage.split('-')[0] === language.split('-')[0]) return null
  const order = dateFieldOrder(browserLanguage)
  if (order.length !== 3) return null
  const words = { year: t('photos.datePartYear'), month: t('photos.datePartMonth'), day: t('photos.datePartDay') }
  return (
    <p className="muted field-hint">
      {t('photos.dateFormatHint', { format: order.map((part) => words[part]).join('/') })}
    </p>
  )
}
