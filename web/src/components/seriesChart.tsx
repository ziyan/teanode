import { useLayoutEffect, useMemo, useRef, useState } from 'react'

// The drawing the usage chart and the finance charts share: a slot per key
// across the width, gridlines on a round scale, columns and lines in the
// page's own colours, and a tooltip for the slot under the pointer. The
// usage chart stacks its tokens with the helpers here; the finance charts
// draw whole series with SeriesChart.

export const CHART_HEIGHT = 220
export const CHART_AXIS_WIDTH = 52
export const CHART_TOP = 12
export const CHART_BOTTOM = 24

// niceCeiling is the top of the scale: the largest value rounded up to 1, 2
// or 5 of its power of ten, so the gridlines fall on round numbers.
export function niceCeiling(value: number): number {
  if (value <= 0) return 1
  const power = 10 ** Math.floor(Math.log10(value))
  for (const step of [1, 2, 5, 10]) {
    if (value <= step * power) return step * power
  }
  return 10 * power
}

// daysBetween is every day from the first to the last, as the keys the
// usage is grouped by, so a day nothing was spent on is a gap and not
// missing from the axis.
export function daysBetween(first: string, last: string): string[] {
  const days: string[] = []
  const start = new Date(first + 'T00:00:00')
  const end = new Date(last + 'T00:00:00')
  if (Number.isNaN(start.getTime()) || Number.isNaN(end.getTime()) || end < start) return days
  for (let day = start; day <= end && days.length < 400; day = new Date(day.getTime() + 86_400_000)) {
    const month = String(day.getMonth() + 1).padStart(2, '0')
    const date = String(day.getDate()).padStart(2, '0')
    days.push(`${day.getFullYear()}-${month}-${date}`)
  }
  return days
}

export function dayLabel(key: string): string {
  const day = new Date(key + 'T00:00:00')
  if (Number.isNaN(day.getTime())) return key
  return day.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

// useWidth is the width the chart has, followed as the page is resized. Read
// at once as well as observed: an observer reports only when the page is
// painted, and a page opened in a tab nobody is looking at would otherwise
// draw nothing until it was.
export function useWidth(): [React.RefObject<HTMLDivElement | null>, number] {
  const element = useRef<HTMLDivElement | null>(null)
  const [width, setWidth] = useState(0)
  useLayoutEffect(() => {
    if (!element.current) return
    setWidth(Math.floor(element.current.getBoundingClientRect().width))
    const observer = new ResizeObserver((entries) => setWidth(Math.floor(entries[0].contentRect.width)))
    observer.observe(element.current)
    return () => observer.disconnect()
  }, [])
  return [element, width]
}

// compact is a count in a few characters, to the billions: the tokens of a
// month of reading run past a thousand million.
export function compact(value: number): string {
  const size = Math.abs(value)
  if (size >= 1e9) return `${(value / 1e9).toFixed(size >= 1e10 ? 0 : 1)}B`
  if (size >= 1e6) return `${(value / 1e6).toFixed(size >= 1e7 ? 0 : 1)}M`
  if (size >= 1e3) return `${(value / 1e3).toFixed(size >= 1e4 ? 0 : 1)}k`
  return String(Math.round(value))
}

// roundedTop is a column with its top corners rounded and its foot square,
// so a stack reads as one column rather than a pile of pills.
export function roundedTop(x: number, y: number, width: number, height: number, radius: number): string {
  return [
    `M${x},${y + height}`,
    `V${y + radius}`,
    `Q${x},${y} ${x + radius},${y}`,
    `H${x + width - radius}`,
    `Q${x + width},${y} ${x + width},${y + radius}`,
    `V${y + height}`,
    'Z',
  ].join(' ')
}

// The colours a series can take, named for the usage chart's parts whose
// colours they are: the full leaf, a paler leaf, and a neutral grey.
export type SeriesTone = 'output' | 'input' | 'cached'

export type ChartSeries = {
  id: string
  label: string
  tone: SeriesTone
  // A column per key, side by side with the other column series, or a
  // line through the keys. A null value is a key the series has nothing
  // for, which a line leaves a gap at.
  shape: 'column' | 'line'
  values: (number | null)[]
}

// SeriesChart draws one or more series over the same keys: days of a
// month, months of a year, or every day of a range. The scale reaches
// below zero only when a value does, so money that went out of a total
// draws under its line.
export function SeriesChart({
  keys,
  keyLabel,
  series,
  format,
  headline,
  caption,
  label,
}: {
  keys: string[]
  keyLabel: (key: string) => string
  series: ChartSeries[]
  format: (value: number) => string
  headline?: React.ReactNode
  caption?: React.ReactNode
  // What the chart is, for a screen reader, ahead of its values.
  label: string
}) {
  const [hovered, setHovered] = useState<number | null>(null)
  const [holder, width] = useWidth()

  const { ceiling, floor } = useMemo(() => {
    const values = series.flatMap((one) => one.values.filter((value): value is number => value !== null))
    const highest = Math.max(0, ...values)
    const lowest = Math.min(0, ...values)
    return {
      ceiling: highest > 0 ? niceCeiling(highest) : lowest < 0 ? 0 : 1,
      floor: lowest < 0 ? -niceCeiling(-lowest) : 0,
    }
  }, [series])

  return (
    <div className="usage-chart">
      {headline || caption ? (
        <div className="usage-chart-head">
          <div>
            {headline ? <div className="usage-chart-total">{headline}</div> : null}
            {caption ? <div className="muted usage-chart-caption">{caption}</div> : null}
          </div>
        </div>
      ) : null}
      <div className="usage-chart-plot" ref={holder}>
        {width > 0 && keys.length > 0 ? (
          <SeriesDrawing
            keys={keys}
            keyLabel={keyLabel}
            series={series}
            ceiling={ceiling}
            floor={floor}
            width={width}
            format={format}
            hovered={hovered}
            onHover={setHovered}
            label={label}
          />
        ) : null}
        {hovered !== null && keys[hovered] !== undefined && width > 0 ? (
          <SeriesTooltip
            title={keyLabel(keys[hovered])}
            index={hovered}
            count={keys.length}
            width={width}
            lines={series.map((one) => ({
              id: one.id,
              label: one.label,
              tone: one.tone,
              value: one.values[hovered] ?? null,
            }))}
            format={format}
          />
        ) : null}
      </div>
      {series.length > 1 ? (
        <div className="usage-chart-legend">
          {series.map((one) => (
            <span key={one.id}>
              <i className={`usage-chart-swatch ${one.tone}`} />
              {one.label}
            </span>
          ))}
        </div>
      ) : null}
    </div>
  )
}

function SeriesDrawing({
  keys,
  keyLabel,
  series,
  ceiling,
  floor,
  width,
  format,
  hovered,
  onHover,
  label,
}: {
  keys: string[]
  keyLabel: (key: string) => string
  series: ChartSeries[]
  ceiling: number
  floor: number
  width: number
  format: (value: number) => string
  hovered: number | null
  onHover: (index: number | null) => void
  label: string
}) {
  const plotWidth = Math.max(40, width - CHART_AXIS_WIDTH)
  const plotHeight = CHART_HEIGHT - CHART_TOP - CHART_BOTTOM
  const slot = plotWidth / Math.max(1, keys.length)
  const columns = series.filter((one) => one.shape === 'column')
  const lines = series.filter((one) => one.shape === 'line')
  const groupWidth = Math.max(2, Math.min(28 * Math.max(1, columns.length), slot * 0.62))
  const columnWidth = groupWidth / Math.max(1, columns.length)
  const span = Math.max(1e-9, ceiling - floor)
  const yOf = (value: number) => CHART_TOP + ((ceiling - value) / span) * plotHeight
  const zero = yOf(0)
  const labelEvery = Math.max(1, Math.ceil(keys.length / Math.max(2, Math.floor(plotWidth / 64))))
  const grid = [0, 0.25, 0.5, 0.75, 1].map((fraction) => floor + fraction * span)

  // The drawing is scaled to the width it is shown at, so a pointer is
  // turned back into the drawing's own units first.
  const pointAt = (clientX: number, element: SVGSVGElement) => {
    const bounds = element.getBoundingClientRect()
    const x = ((clientX - bounds.left) * width) / Math.max(1, bounds.width)
    const index = Math.floor((x - CHART_AXIS_WIDTH) / slot)
    onHover(index >= 0 && index < keys.length ? index : null)
  }

  const said = keys
    .map(
      (key, index) =>
        `${keyLabel(key)}: ${series
          .map((one) => `${one.label} ${one.values[index] === null ? '—' : format(one.values[index] ?? 0)}`)
          .join(', ')}`,
    )
    .join('; ')

  return (
    <svg
      viewBox={`0 0 ${width} ${CHART_HEIGHT}`}
      role="img"
      aria-label={`${label}. ${said}`}
      onPointerMove={(event) => pointAt(event.clientX, event.currentTarget)}
      onPointerLeave={() => onHover(null)}
    >
      {grid.map((value) => {
        const y = yOf(value)
        return (
          <g key={value}>
            <line className="usage-chart-grid" x1={CHART_AXIS_WIDTH} x2={width} y1={y} y2={y} />
            <text
              className="usage-chart-axis"
              x={CHART_AXIS_WIDTH - 8}
              y={y}
              textAnchor="end"
              dominantBaseline="middle"
            >
              {format(value)}
            </text>
          </g>
        )
      })}
      {floor < 0 ? <line className="series-chart-zero" x1={CHART_AXIS_WIDTH} x2={width} y1={zero} y2={zero} /> : null}
      {keys.map((key, index) => {
        const groupX = CHART_AXIS_WIDTH + index * slot + (slot - groupWidth) / 2
        const isDimmed = hovered !== null && hovered !== index
        return (
          <g key={key} className={isDimmed ? 'usage-chart-dim' : ''}>
            {columns.map((one, position) => {
              const value = one.values[index]
              if (value === null || value === undefined || value === 0) return null
              const x = groupX + position * columnWidth
              const height = Math.max(1, Math.abs(yOf(value) - zero))
              const radius = Math.min(4, columnWidth / 2, height)
              return (
                <path
                  key={one.id}
                  className={`usage-chart-column ${one.tone}`}
                  d={
                    value > 0
                      ? roundedTop(x, zero - height, columnWidth, height, radius)
                      : `M${x},${zero}h${columnWidth}v${height}h${-columnWidth}z`
                  }
                />
              )
            })}
            {index % labelEvery === 0 ? (
              <text
                className="usage-chart-axis"
                x={groupX + groupWidth / 2}
                y={CHART_HEIGHT - 6}
                textAnchor={index + labelEvery >= keys.length ? 'end' : 'middle'}
              >
                {keyLabel(key)}
              </text>
            ) : null}
          </g>
        )
      })}
      {lines.map((one) => (
        <path key={one.id} className={`series-chart-line ${one.tone}`} d={linePath(one.values, slot, yOf)} />
      ))}
      {hovered !== null
        ? lines.map((one) => {
            const value = one.values[hovered]
            if (value === null || value === undefined) return null
            return (
              <circle
                key={one.id}
                className={`series-chart-point ${one.tone}`}
                cx={CHART_AXIS_WIDTH + hovered * slot + slot / 2}
                cy={yOf(value)}
                r={3.5}
              />
            )
          })
        : null}
    </svg>
  )
}

// linePath runs through the middle of each slot, lifting the pen over a
// key the series has nothing for.
function linePath(values: (number | null)[], slot: number, yOf: (value: number) => number): string {
  const parts: string[] = []
  let isDrawing = false
  values.forEach((value, index) => {
    if (value === null || value === undefined) {
      isDrawing = false
      return
    }
    const x = CHART_AXIS_WIDTH + index * slot + slot / 2
    parts.push(`${isDrawing ? 'L' : 'M'}${x.toFixed(1)},${yOf(value).toFixed(1)}`)
    isDrawing = true
  })
  return parts.join(' ')
}

function SeriesTooltip({
  title,
  index,
  count,
  width,
  lines,
  format,
}: {
  title: string
  index: number
  count: number
  width: number
  lines: { id: string; label: string; tone: SeriesTone; value: number | null }[]
  format: (value: number) => string
}) {
  const slot = (width - CHART_AXIS_WIDTH) / Math.max(1, count)
  const center = CHART_AXIS_WIDTH + index * slot + slot / 2
  // Kept inside the chart: flipped to the left of the slot past halfway.
  const isLeft = center > width / 2
  return (
    <div
      className={`usage-chart-tooltip ${isLeft ? 'left' : 'right'}`}
      style={isLeft ? { right: `${((width - center) / width) * 100}%` } : { left: `${(center / width) * 100}%` }}
      role="status"
    >
      <div className="usage-chart-tooltip-day">{title}</div>
      {lines.map((line) => (
        <div key={line.id} className="usage-chart-tooltip-line">
          <i className={`usage-chart-swatch ${line.tone}`} />
          <span>{line.label}</span>
          <strong>{line.value === null ? '—' : format(line.value)}</strong>
        </div>
      ))}
    </div>
  )
}
