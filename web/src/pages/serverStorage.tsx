import { useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'

import { graphql } from '../api'
import { MeterBar } from '../components/budgetBar'
import { ErrorMessage, Loading, Tag } from '../components/common'
import { Select } from '../components/select'
import { ChartSeries, SeriesChart, SeriesTone, dayLabel, daysBetween } from '../components/seriesChart'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../components/settingsList'
import { useQuery } from '../components/useQuery'
import { Key, useTranslation } from '../i18n/i18n'

// What the database and the files hold, for the operator: the sizes now, by
// area, kind of file or person; the days sampled, drawn; the disks the
// server measured with the day each fills at the recent growth; and a banner
// when one fills within thirty days. The range and the breakdown are in the
// address, so a view can be sent to somebody and survives a reload.

type KeySize = { sizeKey: string; sizeBytes: number }
type UsageRow = {
  rowKey: string
  rowLabel: string
  rowSizeBytes: number
  rowTableCount: number
  rowFileSizes: KeySize[]
}
type HistoryDay = {
  sampledOn: string
  databaseSizeBytes: number
  fileSizeBytes: number
  totalSizeBytes: number
  breakdownSizes: KeySize[]
}
type Disk = {
  diskLabel: string
  isDiskMeasured: boolean
  diskTotalBytes?: number | null
  diskFreeBytes?: number | null
  diskUsedBytes?: number | null
  projectedFullOn?: string | null
  daysUntilFull?: number | null
  isProjectedFull: boolean
}
type Growth = {
  growthSampleCount: number
  isGrowthProjectable: boolean
  growthBytesPerDay: number
  currentSizeBytes: number
  projectedSize30DaysBytes: number
  projectedSize90DaysBytes: number
  projectedSize365DaysBytes: number
  areFilesCounted: boolean
}
type Usage = {
  breakdownBy: string
  databaseSizeBytes: number
  fileSizeBytes: number
  totalSizeBytes: number
  usageRows: UsageRow[]
  historyDays: HistoryDay[]
  disks: Disk[]
  growth: Growth
}

const STORAGE_USAGE = `query ($by: String, $since: DateTime, $until: DateTime) {
  StorageUsage(by: $by, since: $since, until: $until) {
    breakdownBy databaseSizeBytes fileSizeBytes totalSizeBytes
    usageRows { rowKey rowLabel rowSizeBytes rowTableCount rowFileSizes { sizeKey sizeBytes } }
    historyDays { sampledOn databaseSizeBytes fileSizeBytes totalSizeBytes breakdownSizes { sizeKey sizeBytes } }
    disks { diskLabel isDiskMeasured diskTotalBytes diskFreeBytes diskUsedBytes projectedFullOn daysUntilFull isProjectedFull }
    growth { growthSampleCount isGrowthProjectable growthBytesPerDay currentSizeBytes projectedSize30DaysBytes projectedSize90DaysBytes projectedSize365DaysBytes areFilesCounted }
  }
}`

const BREAKDOWNS = ['area', 'kind', 'person'] as const
type Breakdown = (typeof BREAKDOWNS)[number]

const FILE_KINDS = [
  'photoOriginal',
  'photoPreview',
  'recordingOriginal',
  'recordingDerived',
  'mail',
  'agentAttachment',
] as const

// The most series the chart draws: the largest few, and the rest as one.
const CHART_SERIES = 5
const TONES: SeriesTone[] = ['output', 'input', 'cached', 'deep', 'core', 'rem']

// dayOf is a date as a date input wants it, in the person's own zone.
function dayOf(date: Date): string {
  const pad = (value: number) => String(value).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}

// formatStorageBytes is a size in 1024-based units to the terabyte and
// beyond, with a decimal from a megabyte up.
export function formatStorageBytes(value: number, precision = 0): string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let size = Math.abs(value)
  let index = 0
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024
    index++
  }
  const places = index < 2 ? precision : Math.max(1, precision)
  return `${value < 0 ? '-' : ''}${size.toFixed(places)} ${units[index]}`
}

function areaKey(area: string): Key {
  return `storage.area.${area}` as Key
}
function kindKey(kind: string): Key {
  return `storage.kind.${kind}` as Key
}

export function ServerStoragePage() {
  const { t } = useTranslation()
  const [parameters, setParameters] = useSearchParams()
  const requestedBy = parameters.get('by')
  const by: Breakdown = (BREAKDOWNS as readonly string[]).includes(requestedBy ?? '')
    ? (requestedBy as Breakdown)
    : 'area'
  const since = parameters.get('since') ?? dayOf(new Date(Date.now() - 30 * 24 * 60 * 60 * 1000))
  const until = parameters.get('until') ?? dayOf(new Date())
  const change = (name: string, value: string) => {
    const next = new URLSearchParams(parameters)
    next.set(name, value)
    setParameters(next, { replace: true })
  }

  const { data, error, loading } = useQuery(
    () =>
      graphql<{ StorageUsage: Usage }>(STORAGE_USAGE, {
        by,
        since: since ? new Date(since + 'T00:00:00').toISOString() : undefined,
        until: until ? new Date(until + 'T00:00:00').toISOString() : undefined,
      }),
    [by, since, until],
  )
  const usage = data?.StorageUsage

  const chart = useMemo(() => (usage ? chartOf(usage, since, until, t) : null), [usage, since, until, t])

  if (loading && !data) return <Loading />
  if (error) return <ErrorMessage error={error} />
  if (!usage) return null

  const diskName = (label: string) => t(`storage.disk.${label}` as Key)
  const filling = usage.disks.filter((disk) => disk.isProjectedFull && disk.projectedFullOn)

  return (
    <>
      {filling.map((disk) => (
        <div className="banner" role="alert" key={disk.diskLabel}>
          {t('storage.banner', {
            disk: diskName(disk.diskLabel),
            date: disk.projectedFullOn ?? '',
            days: String(Math.max(0, Math.round(disk.daysUntilFull ?? 0))),
          })}
        </div>
      ))}

      <SettingsSection card title={t('storage.disks')}>
        {usage.disks.length === 0 ? <SettingsEmpty>{t('storage.noDisks')}</SettingsEmpty> : null}
        {usage.disks.map((disk) => (
          <DiskRow key={disk.diskLabel} disk={disk} name={diskName(disk.diskLabel)} growth={usage.growth} />
        ))}
      </SettingsSection>

      <SettingsSection
        card
        title={t('storage.title')}
        action={
          <div className="usage-range">
            <label className="shrink">
              <span>{t('agentAdmin.since')}</span>
              <input
                type="date"
                value={since}
                max={until || undefined}
                onChange={(event) => change('since', event.target.value)}
              />
            </label>
            <label className="shrink">
              <span>{t('agentAdmin.until')}</span>
              <input
                type="date"
                value={until}
                min={since || undefined}
                onChange={(event) => change('until', event.target.value)}
              />
            </label>
            <label className="shrink">
              <span>{t('agentAdmin.by')}</span>
              <Select
                value={by}
                label={t('agentAdmin.by')}
                options={[
                  { value: 'area', label: t('storage.byArea') },
                  { value: 'kind', label: t('storage.byKind') },
                  { value: 'person', label: t('storage.byPerson') },
                ]}
                onChange={(value) => change('by', value)}
              />
            </label>
          </div>
        }
      >
        <div className="storage-totals">
          <Figure label={t('storage.database')} bytes={usage.databaseSizeBytes} />
          <Figure label={t('storage.files')} bytes={usage.fileSizeBytes} />
          <Figure label={t('storage.total')} bytes={usage.totalSizeBytes} />
        </div>
        {chart && chart.keys.length > 0 ? (
          <SeriesChart
            label={t('storage.chartLabel')}
            keys={chart.keys}
            keyLabel={dayLabel}
            series={chart.series}
            isStacked
            format={formatStorageBytes}
            axisFormat={(value, precision) => formatStorageBytes(value, precision)}
            headline={formatStorageBytes(usage.totalSizeBytes)}
            caption={growthCaption(usage.growth, t)}
          />
        ) : (
          <SettingsEmpty>{t('storage.noHistory')}</SettingsEmpty>
        )}
        <Projection growth={usage.growth} />
        <Breakdown usage={usage} by={by} />
      </SettingsSection>
    </>
  )
}

function Figure({ label, bytes }: { label: string; bytes: number }) {
  return (
    <div className="storage-figure">
      <div className="muted">{label}</div>
      <strong>{formatStorageBytes(bytes)}</strong>
    </div>
  )
}

function growthCaption(growth: Growth, t: ReturnType<typeof useTranslation>['t']): string {
  if (!growth.isGrowthProjectable) return t('storage.notEnoughHistory')
  const sign = growth.growthBytesPerDay < 0 ? '-' : '+'
  return t('storage.growthPerDay', { size: `${sign}${formatStorageBytes(Math.abs(growth.growthBytesPerDay), 1)}` })
}

function DiskRow({ disk, name, growth }: { disk: Disk; name: string; growth: Growth }) {
  const { t } = useTranslation()
  if (!disk.isDiskMeasured || disk.diskTotalBytes == null || disk.diskFreeBytes == null || disk.diskUsedBytes == null) {
    return <SettingsRow title={name} subtitle={t('storage.notMeasured')} />
  }
  const fraction = disk.diskTotalBytes > 0 ? disk.diskUsedBytes / disk.diskTotalBytes : 0
  const tone = disk.isProjectedFull || fraction >= 0.9 ? 'bad' : fraction >= 0.75 ? 'warn' : 'good'
  let fullOn = t('storage.notEnoughHistory')
  if (disk.projectedFullOn) fullOn = t('storage.fullOn', { date: disk.projectedFullOn })
  else if (growth.isGrowthProjectable) fullOn = t('storage.notGrowing')
  return (
    <SettingsRow
      title={name}
      badge={<Tag value={fullOn} tone={disk.isProjectedFull ? 'warn' : undefined} />}
      subtitle={
        <>
          <span>
            {t('storage.diskFree', {
              free: formatStorageBytes(disk.diskFreeBytes, 1),
              total: formatStorageBytes(disk.diskTotalBytes, 1),
            })}
          </span>
          <MeterBar fraction={fraction} tone={tone} label={name} />
        </>
      }
    />
  )
}

function Projection({ growth }: { growth: Growth }) {
  const { t } = useTranslation()
  if (!growth.isGrowthProjectable) return null
  return (
    <>
      <div className="table-wrap">
        <table className="numbers-table">
          <thead>
            <tr>
              <th>{t('storage.projection')}</th>
              <th className="numeric">{t('storage.size')}</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td>{t('storage.now')}</td>
              <td className="numeric">{formatStorageBytes(growth.currentSizeBytes, 1)}</td>
            </tr>
            <tr>
              <td>{t('storage.inDays', { days: '30' })}</td>
              <td className="numeric">{formatStorageBytes(growth.projectedSize30DaysBytes, 1)}</td>
            </tr>
            <tr>
              <td>{t('storage.inDays', { days: '90' })}</td>
              <td className="numeric">{formatStorageBytes(growth.projectedSize90DaysBytes, 1)}</td>
            </tr>
            <tr>
              <td>{t('storage.inDays', { days: '365' })}</td>
              <td className="numeric">{formatStorageBytes(growth.projectedSize365DaysBytes, 1)}</td>
            </tr>
          </tbody>
        </table>
      </div>
      {!growth.areFilesCounted ? <p className="muted">{t('storage.filesNotCounted')}</p> : null}
    </>
  )
}

function Breakdown({ usage, by }: { usage: Usage; by: Breakdown }) {
  const { t } = useTranslation()
  if (usage.usageRows.length === 0) return <SettingsEmpty>{t('storage.nothingStored')}</SettingsEmpty>
  const labelOf = (row: UsageRow): string => {
    if (by === 'area') return t(areaKey(row.rowKey))
    if (by === 'kind') return t(kindKey(row.rowKey))
    return row.rowKey === '' ? t('storage.unowned') : row.rowLabel
  }
  const header = by === 'area' ? t('storage.areaColumn') : by === 'kind' ? t('storage.kindColumn') : t('storage.person')
  const kindBytes = (row: UsageRow, kind: string) =>
    row.rowFileSizes.find((entry) => entry.sizeKey === kind)?.sizeBytes ?? 0
  return (
    <div className="table-wrap">
      <table className="numbers-table">
        <thead>
          <tr>
            <th>{header}</th>
            <th className="numeric">{t('storage.size')}</th>
            {by === 'area' ? <th className="numeric">{t('storage.tables')}</th> : null}
            {by === 'person'
              ? FILE_KINDS.map((kind) => (
                  <th className="numeric" key={kind}>
                    {t(kindKey(kind))}
                  </th>
                ))
              : null}
          </tr>
        </thead>
        <tbody>
          {usage.usageRows.map((row) => (
            <tr key={row.rowKey || 'unowned'}>
              <td>{labelOf(row)}</td>
              <td className="numeric">{formatStorageBytes(row.rowSizeBytes, 1)}</td>
              {by === 'area' ? <td className="numeric">{row.rowTableCount}</td> : null}
              {by === 'person'
                ? FILE_KINDS.map((kind) => (
                    <td className="numeric" key={kind}>
                      {kindBytes(row, kind) > 0 ? formatStorageBytes(kindBytes(row, kind), 1) : ''}
                    </td>
                  ))
                : null}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

// chartOf is the days of the range as stacked columns: the database and the
// files for a person (which is not sampled), the areas that make up the
// database, or the kinds that make up the files. The largest few of the
// newest day are drawn apart and the rest are one series, so a table of
// eleven areas does not need eleven colors.
function chartOf(
  usage: Usage,
  since: string,
  until: string,
  t: ReturnType<typeof useTranslation>['t'],
): { keys: string[]; series: ChartSeries[] } {
  const days = usage.historyDays
  if (days.length === 0) return { keys: [], series: [] }
  const keys = daysBetween(since || days[0].sampledOn, until || days[days.length - 1].sampledOn)
  const byDay = new Map(days.map((day) => [day.sampledOn, day]))
  const valuesOf = (read: (day: HistoryDay) => number): (number | null)[] =>
    keys.map((key) => {
      const day = byDay.get(key)
      return day ? read(day) : null
    })

  if (usage.breakdownBy === 'person') {
    return {
      keys,
      series: [
        {
          id: 'database',
          label: t('storage.database'),
          tone: 'output',
          shape: 'column',
          values: valuesOf((day) => day.databaseSizeBytes),
        },
        {
          id: 'files',
          label: t('storage.files'),
          tone: 'cached',
          shape: 'column',
          values: valuesOf((day) => day.fileSizeBytes),
        },
      ],
    }
  }
  const newest = days[days.length - 1]
  const ranked = [...newest.breakdownSizes]
    .sort((left, right) => right.sizeBytes - left.sizeBytes)
    .map((entry) => entry.sizeKey)
  const drawn = ranked.slice(0, CHART_SERIES)
  const labelOf = (key: string) => t(usage.breakdownBy === 'area' ? areaKey(key) : kindKey(key))
  const sizeOf = (day: HistoryDay, key: string) =>
    day.breakdownSizes.find((entry) => entry.sizeKey === key)?.sizeBytes ?? 0
  const series: ChartSeries[] = drawn.map((key, index) => ({
    id: key,
    label: labelOf(key),
    tone: TONES[index],
    shape: 'column',
    values: valuesOf((day) => sizeOf(day, key)),
  }))
  if (ranked.length > drawn.length) {
    series.push({
      id: 'rest',
      label: t('storage.rest'),
      tone: TONES[CHART_SERIES],
      shape: 'column',
      values: valuesOf((day) =>
        day.breakdownSizes
          .filter((entry) => !drawn.includes(entry.sizeKey))
          .reduce((sum, entry) => sum + entry.sizeBytes, 0),
      ),
    })
  }
  return { keys, series }
}
