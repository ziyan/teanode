import { Link } from 'react-router-dom'

import type { AgentReference } from '../api'
import { useTranslation } from '../i18n/i18n'
import { mailPath, memoryPath } from './dashboardPath'
import { FeedRowEmptyThumbnail, FeedRowThumbnail } from './feedMedia'
import { webAddressOf, type FeedMedia } from './feedMediaLayout'
import { WeatherIcon } from './icons'

export { webAddressOf }

// The structured layouts a feed card can be drawn in besides text: a list of
// rows, the weather, a day's agenda. The server sends the content as a JSON
// string; anything that does not have the shape of its layout is drawn as a
// plain text card, whose body is always filled in, so a card is never blank.

export interface ListRow {
  rowTitle: string
  rowDetail: string
  rowMeta: string
  rowUrl: string
  rowRecordReference: AgentReference | null
}

export interface HourlyForecast {
  forecastHour: string
  forecastTemperature: number
  forecastSymbol: string
  precipitationChancePercent: number
}

export interface WeatherContent {
  placeName: string
  currentTemperature: number
  temperatureUnit: string
  conditionText: string
  conditionSymbol: string
  highTemperature: number | null
  lowTemperature: number | null
  hourlyForecast: HourlyForecast[]
}

export type AgendaEntryKind = 'meeting' | 'focus' | 'task' | 'break'

export interface AgendaEntry {
  startsAt: string
  endsAt: string
  agendaTitle: string
  agendaEntryKind: AgendaEntryKind
  agendaDetail: string
  agendaRecordReference: AgentReference | null
}

export type FeedLayout =
  | { kind: 'list'; listRows: ListRow[] }
  | { kind: 'weather'; weather: WeatherContent }
  | { kind: 'agenda'; agendaEntries: AgendaEntry[] }

type Json = Record<string, unknown>

const isObject = (value: unknown): value is Json => typeof value === 'object' && value !== null && !Array.isArray(value)
const textOf = (value: unknown): string => (typeof value === 'string' ? value : '')
const numberOf = (value: unknown): number | null => (typeof value === 'number' && Number.isFinite(value) ? value : null)
const referenceOf = (value: unknown): AgentReference | null => (isObject(value) ? (value as AgentReference) : null)

// parseFeedLayout reads a card's layout and its content, and says null for a
// card that is to be drawn as text: the text layout, content that is not JSON,
// content of the wrong shape, or a layout with nothing in it to draw.
export function parseFeedLayout(feedCardLayout: string, layoutContent: string): FeedLayout | null {
  if (feedCardLayout !== 'list' && feedCardLayout !== 'weather' && feedCardLayout !== 'agenda') return null
  let content: unknown
  try {
    content = JSON.parse(layoutContent)
  } catch {
    return null
  }
  if (!isObject(content)) return null
  if (feedCardLayout === 'list') {
    if (!Array.isArray(content.listRows)) return null
    const rows = content.listRows.filter(isObject).map((row) => ({
      rowTitle: textOf(row.rowTitle),
      rowDetail: textOf(row.rowDetail),
      rowMeta: textOf(row.rowMeta),
      rowUrl: textOf(row.rowUrl),
      rowRecordReference: referenceOf(row.rowRecordReference),
    }))
    if (rows.length === 0 || rows.some((row) => row.rowTitle === '')) return null
    return { kind: 'list', listRows: rows }
  }
  if (feedCardLayout === 'weather') {
    const currentTemperature = numberOf(content.currentTemperature)
    if (currentTemperature === null) return null
    const hourly = Array.isArray(content.hourlyForecast) ? content.hourlyForecast.filter(isObject) : []
    return {
      kind: 'weather',
      weather: {
        placeName: textOf(content.placeName),
        currentTemperature,
        temperatureUnit: textOf(content.temperatureUnit),
        conditionText: textOf(content.conditionText),
        conditionSymbol: textOf(content.conditionSymbol),
        highTemperature: numberOf(content.highTemperature),
        lowTemperature: numberOf(content.lowTemperature),
        hourlyForecast: hourly.flatMap((hour) => {
          const forecastTemperature = numberOf(hour.forecastTemperature)
          const forecastHour = textOf(hour.forecastHour)
          if (forecastTemperature === null || Number.isNaN(new Date(forecastHour).getTime())) return []
          return [
            {
              forecastHour,
              forecastTemperature,
              forecastSymbol: textOf(hour.forecastSymbol),
              precipitationChancePercent: numberOf(hour.precipitationChancePercent) ?? 0,
            },
          ]
        }),
      },
    }
  }
  if (!Array.isArray(content.agendaEntries)) return null
  const entries = content.agendaEntries.filter(isObject).map((entry) => ({
    startsAt: textOf(entry.startsAt),
    endsAt: textOf(entry.endsAt),
    agendaTitle: textOf(entry.agendaTitle),
    agendaEntryKind: (['meeting', 'focus', 'task', 'break'].includes(textOf(entry.agendaEntryKind))
      ? entry.agendaEntryKind
      : 'task') as AgendaEntryKind,
    agendaDetail: textOf(entry.agendaDetail),
    agendaRecordReference: referenceOf(entry.agendaRecordReference),
  }))
  if (
    entries.length === 0 ||
    entries.some((entry) => entry.agendaTitle === '' || Number.isNaN(new Date(entry.startsAt).getTime()))
  )
    return null
  return { kind: 'agenda', agendaEntries: entries }
}

// recordPathOf is the dashboard page a record reference opens: a mail by its
// item or thread, a page of memory by its path, a calendar record by the
// calendar the caller names. Null for a reference this cannot place.
function recordPathOf(reference: AgentReference | null, calendarPath?: string): string | null {
  if (!reference) return null
  if (reference.itemId) return calendarPath ?? mailPath(reference.itemId)
  if (reference.threadId && !calendarPath) return mailPath(reference.threadId)
  if (reference.path) return memoryPath(reference.path)
  return null
}

// weatherKindOf maps a symbol name the agent chose to the dashboard's own
// weather drawing.
function weatherKindOf(symbol: string): string {
  if (symbol.includes('bolt')) return 'thunderstorm'
  if (symbol.includes('snow')) return 'snow'
  if (symbol.includes('rain') || symbol.includes('drizzle')) return 'rain'
  if (symbol.includes('fog')) return 'fog'
  if (symbol.includes('cloud') && symbol.includes('sun')) return 'partlyCloudy'
  if (symbol.includes('cloud')) return 'cloudy'
  if (symbol.includes('sun') || symbol.includes('moon')) return 'clear'
  return 'cloudy'
}

function degrees(value: number, unit?: string): string {
  const suffix = unit ? unit.replace(/^°/, '') : ''
  return `${Math.round(value)}°${suffix}`
}

export function FeedLayoutContent({
  layout,
  rowMediaByIndex,
}: {
  layout: FeedLayout
  rowMediaByIndex?: Map<number, FeedMedia>
}) {
  if (layout.kind === 'list') return <FeedListRows rows={layout.listRows} rowMediaByIndex={rowMediaByIndex} />
  if (layout.kind === 'weather') return <FeedWeather weather={layout.weather} />
  return <FeedAgenda entries={layout.agendaEntries} />
}

function FeedListRows({ rows, rowMediaByIndex }: { rows: ListRow[]; rowMediaByIndex?: Map<number, FeedMedia> }) {
  const hasPictureColumn = (rowMediaByIndex?.size ?? 0) > 0
  return (
    <ul className="feed-rows">
      {rows.map((row, index) => {
        const address = webAddressOf(row.rowUrl)
        const recordPath = recordPathOf(row.rowRecordReference)
        const thumbnail = rowMediaByIndex?.get(index)
        const inside = (
          <>
            {thumbnail ? <FeedRowThumbnail media={thumbnail} /> : hasPictureColumn && <FeedRowEmptyThumbnail />}
            <span className="feed-row-text">
              <span className="feed-row-title">{row.rowTitle}</span>
              {row.rowDetail && <span className="feed-row-detail">{row.rowDetail}</span>}
              {row.rowMeta && <span className="feed-row-meta muted">{row.rowMeta}</span>}
            </span>
          </>
        )
        return (
          <li key={`${row.rowTitle}-${index}`}>
            {address ? (
              <a className="feed-row" href={address} target="_blank" rel="noopener noreferrer">
                {inside}
              </a>
            ) : recordPath ? (
              <Link className="feed-row" to={recordPath}>
                {inside}
              </Link>
            ) : (
              <div className="feed-row">{inside}</div>
            )}
          </li>
        )
      })}
    </ul>
  )
}

function FeedWeather({ weather }: { weather: WeatherContent }) {
  const { t } = useTranslation()
  return (
    <div className="feed-weather">
      {weather.placeName && <p className="muted feed-weather-place">{weather.placeName}</p>}
      <div className="feed-weather-now">
        <WeatherIcon kind={weatherKindOf(weather.conditionSymbol)} size={40} />
        <span className="feed-weather-temperature">{degrees(weather.currentTemperature, weather.temperatureUnit)}</span>
        <span className="feed-weather-condition">
          {weather.conditionText && <span>{weather.conditionText}</span>}
          {weather.highTemperature !== null && weather.lowTemperature !== null && (
            <span className="muted">
              {t('feed.weatherHighLow', {
                high: degrees(weather.highTemperature),
                low: degrees(weather.lowTemperature),
              })}
            </span>
          )}
        </span>
      </div>
      {weather.hourlyForecast.length > 0 && (
        <ul className="feed-weather-hours">
          {weather.hourlyForecast.map((hour) => (
            <li key={hour.forecastHour}>
              <span className="muted">
                {new Date(hour.forecastHour).toLocaleTimeString(undefined, { hour: 'numeric' })}
              </span>
              <WeatherIcon kind={weatherKindOf(hour.forecastSymbol)} size={20} />
              <span>{degrees(hour.forecastTemperature)}</span>
              {hour.precipitationChancePercent > 0 && (
                <span className="feed-weather-rain">{Math.round(hour.precipitationChancePercent)}%</span>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function clockOf(written: string): string {
  const date = new Date(written)
  return Number.isNaN(date.getTime()) ? '' : date.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })
}

function FeedAgenda({ entries }: { entries: AgendaEntry[] }) {
  return (
    <ol className="feed-agenda">
      {entries.map((entry, index) => {
        const recordPath = recordPathOf(entry.agendaRecordReference, '/calendar')
        const inside = (
          <>
            <span className="feed-agenda-title">{entry.agendaTitle}</span>
            {entry.agendaDetail && <span className="feed-row-detail">{entry.agendaDetail}</span>}
          </>
        )
        const end = clockOf(entry.endsAt)
        return (
          <li key={`${entry.startsAt}-${index}`} className={`feed-agenda-entry is-${entry.agendaEntryKind}`}>
            <span className="feed-agenda-time muted">
              {clockOf(entry.startsAt)}
              {end && <span className="feed-agenda-end">{end}</span>}
            </span>
            <span className="feed-agenda-marker" aria-hidden="true" />
            {recordPath ? (
              <Link className="feed-agenda-body" to={recordPath}>
                {inside}
              </Link>
            ) : (
              <span className="feed-agenda-body">{inside}</span>
            )}
          </li>
        )
      })}
    </ol>
  )
}
