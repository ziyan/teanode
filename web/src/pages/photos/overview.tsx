import { useEffect, useRef, useState, type RefObject } from 'react'
import { ErrorMessage, Loading } from '../../components/common'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import type { PhotoFilter } from '../../photos/api'
import {
  MONTH_INDEXES_NEWEST_FIRST,
  formatYearRange,
  mapWithLimit,
  readOverviewPeriod,
  readOverviewYears,
  yearsBetween,
  type OverviewPeriod,
  type OverviewScale,
} from '../../photos/overview'
import { useShowDateRange } from './dateRange'
import { PhotoImage } from './grid'

// The library as an overview: a picture for each year, or for each month under
// its year, newest first. Choosing one goes to the grid at that place (the
// page does that: it knows the grid's pages).
//
// A year of months is asked of the server only when it is near the screen, so
// a library of many years opens at once and reads the older ones as they are
// scrolled to.

// How near the screen a year of months is when it is asked for, in pixels.
const NEAR_SCREEN_PIXELS = 600
// How many questions are with the server at once from one list of periods.
const QUESTIONS_AT_ONCE = 6

export function PhotoOverview({
  scale,
  filter,
  isChoosing,
  onChoose,
}: {
  scale: OverviewScale
  // What the grid would show: the list, the kind and the dates. The overview
  // is of exactly these photos.
  filter: PhotoFilter
  // The page is finding the place in the grid of the period chosen.
  isChoosing: boolean
  onChoose: (period: OverviewPeriod) => void
}) {
  const { language } = useTranslation()
  const span = useQuery(() => readOverviewYears(filter), [filter], { refresh: false })
  // Under the page's heading, the years the overview spans.
  useShowDateRange(span.data ? formatYearRange(span.data.oldestYear, span.data.newestYear, language) : '')
  if (span.error) return <ErrorMessage error={span.error} />
  if (!span.data) return span.loading ? <Loading /> : null
  const years = yearsBetween(span.data.newestYear, span.data.oldestYear)
  return (
    <div className={isChoosing ? 'photo-overview is-choosing' : 'photo-overview'} aria-busy={isChoosing}>
      {scale === 'years' ? (
        <YearsOverview filter={filter} years={years} onChoose={onChoose} />
      ) : (
        years.map((year) => <MonthsOfYear key={year} filter={filter} year={year} onChoose={onChoose} />)
      )}
    </div>
  )
}

function YearsOverview({
  filter,
  years,
  onChoose,
}: {
  filter: PhotoFilter
  years: number[]
  onChoose: (period: OverviewPeriod) => void
}) {
  const { language } = useTranslation()
  const periods = useQuery(
    () => mapWithLimit(years, QUESTIONS_AT_ONCE, (year) => readOverviewPeriod(filter, year, null)),
    [filter, years.join()],
    { refresh: false },
  )
  if (periods.error) return <ErrorMessage error={periods.error} />
  if (!periods.data) return <Loading />
  return (
    <div className="photo-period-grid is-years">
      {periods.data.map(
        (period) =>
          period && (
            <PeriodTile
              key={period.key}
              period={period}
              label={yearText(period.year, language)}
              spokenLabel={yearText(period.year, language)}
              onChoose={onChoose}
            />
          ),
      )}
    </div>
  )
}

function MonthsOfYear({
  filter,
  year,
  onChoose,
}: {
  filter: PhotoFilter
  year: number
  onChoose: (period: OverviewPeriod) => void
}) {
  const { language } = useTranslation()
  const section = useRef<HTMLElement>(null)
  const isNear = useIsNearScreen(section)
  const months = useQuery(
    async () =>
      isNear
        ? await mapWithLimit(MONTH_INDEXES_NEWEST_FIRST, QUESTIONS_AT_ONCE, (monthIndex) =>
            readOverviewPeriod(filter, year, monthIndex),
          )
        : null,
    [filter, year, isNear],
    { refresh: false },
  )
  const periods = (months.data ?? []).filter((period): period is OverviewPeriod => period !== null)
  // A year in the middle of the library with nothing in it (or nothing that
  // the filter keeps) has no heading either.
  if (months.data !== null && periods.length === 0) return null
  return (
    <section className="photo-overview-year" ref={section} aria-label={yearText(year, language)}>
      <h2 className="photo-overview-heading">{yearText(year, language)}</h2>
      {months.error ? (
        <ErrorMessage error={months.error} />
      ) : months.data === null ? (
        <div className="photo-overview-waiting" aria-hidden="true" />
      ) : (
        <div className="photo-period-grid">
          {periods.map((period) => (
            <PeriodTile
              key={period.key}
              period={period}
              label={monthText(period.monthIndex ?? 0, language)}
              spokenLabel={`${monthText(period.monthIndex ?? 0, language)} ${yearText(year, language)}`}
              onChoose={onChoose}
            />
          ))}
        </div>
      )}
    </section>
  )
}

function PeriodTile({
  period,
  label,
  spokenLabel,
  onChoose,
}: {
  period: OverviewPeriod
  label: string
  spokenLabel: string
  onChoose: (period: OverviewPeriod) => void
}) {
  const { t } = useTranslation()
  const source = period.photo.largePreviewUrl || period.photo.smallPreviewUrl
  return (
    <button
      type="button"
      className="photo-period"
      aria-label={t('photos.showPeriod', { period: spokenLabel })}
      onClick={() => onChoose(period)}
    >
      {source ? <PhotoImage src={source} alt="" /> : null}
      <span className="photo-period-label" aria-hidden="true">
        {label}
      </span>
    </button>
  )
}

// Whether an element is on the screen or near it, kept true once it has been.
// A browser that cannot tell says yes at once.
function useIsNearScreen(element: RefObject<HTMLElement | null>): boolean {
  const [isNear, setIsNear] = useState(typeof IntersectionObserver === 'undefined')
  useEffect(() => {
    const target = element.current
    if (isNear || !target) return
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) setIsNear(true)
      },
      { rootMargin: `${NEAR_SCREEN_PIXELS}px` },
    )
    observer.observe(target)
    return () => observer.disconnect()
  }, [element, isNear])
  return isNear
}

function yearText(year: number, language: string): string {
  // A year, written the way the language writes one: 2030, or 2030年.
  const moment = new Date(2000, 5, 15)
  moment.setFullYear(year)
  return new Intl.DateTimeFormat(language, { year: 'numeric' }).format(moment)
}

function monthText(monthIndex: number, language: string): string {
  return new Intl.DateTimeFormat(language, { month: 'long' }).format(new Date(2000, monthIndex, 15))
}
