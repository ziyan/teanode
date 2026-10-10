import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'

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

// ChartScale is the range a chart is drawn over and its gridlines.
export type ChartScale = { floor: number; ceiling: number; grid: number[] }

// CHART_NEGLIGIBLE_DIP is how far below zero a chart can go, as a share of
// its highest value, before the scale reaches down for it. A month whose
// first day was a refund dips a few dollars under a line that climbs to
// thousands; a whole gridline below zero for that spent a quarter of the
// chart on an empty band.
export const CHART_NEGLIGIBLE_DIP = 0.05

// chartScale is a range in round steps, about four of them, from a step at
// or below the lowest value to one at or above the highest. Zero is always
// inside it and always on a gridline, so a line that goes below zero is
// drawn whole and money in reads against money out. A dip below zero that
// is negligible beside the highest value does not add a step; it is drawn
// at zero instead.
//
// isFitted draws the range around the values instead, zero or not: a net
// worth of a million that moved by ten thousand is a flat line on a scale
// from zero. Only a line may be fitted; a column's height is its value.
//
// isEveryDipShown reaches below zero for any value under it, however
// small: a month that lost a little money is not drawn as one that broke
// even.
export function chartScale(
  values: number[],
  { isFitted = false, isEveryDipShown = false }: { isFitted?: boolean; isEveryDipShown?: boolean } = {},
): ChartScale {
  if (isFitted) return fittedScale(values)
  const highest = Math.max(0, ...values)
  const deepest = Math.min(0, ...values)
  const lowest = !isEveryDipShown && -deepest < highest * CHART_NEGLIGIBLE_DIP ? 0 : deepest
  if (highest === lowest) return { floor: 0, ceiling: 1, grid: [0, 1] }
  const step = niceCeiling((highest - lowest) / 4)
  const below = Math.ceil(-lowest / step - 1e-9)
  const above = Math.ceil(highest / step - 1e-9)
  const grid: number[] = []
  for (let index = -below; index <= above; index++) grid.push(index * step)
  return { floor: -below * step, ceiling: above * step, grid }
}

// fittedScale is a range in round steps, about four of them, from a step
// at or below the lowest value to one at or above the highest. A line that
// does not move gets a band around it, so it is drawn across the middle.
function fittedScale(values: number[]): ChartScale {
  if (values.length === 0) return { floor: 0, ceiling: 1, grid: [0, 1] }
  let lowest = Math.min(...values)
  let highest = Math.max(...values)
  if (highest === lowest) {
    const margin = Math.max(1, Math.abs(highest) * 0.01)
    lowest -= margin
    highest += margin
  }
  const step = niceCeiling((highest - lowest) / 4)
  const bottom = Math.floor(lowest / step + 1e-9)
  const top = Math.ceil(highest / step - 1e-9)
  const grid: number[] = []
  for (let index = bottom; index <= top; index++) grid.push(index * step)
  return { floor: bottom * step, ceiling: top * step, grid }
}

// CHART_AXIS_LETTER is about how wide one figure of an axis label is drawn,
// at the axis's type size, in the drawing's units.
const CHART_AXIS_LETTER = 6.6

// axisWidthFor is the width the axis column needs for its widest label, so
// a label as long as "$1.2M" is not cut off at the left and a short one
// does not leave the plot narrower than it has to be.
export function axisWidthFor(labels: string[]): number {
  const longest = Math.max(1, ...labels.map((label) => label.length))
  return Math.min(120, Math.max(28, Math.ceil(longest * CHART_AXIS_LETTER) + 12))
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
// month of reading run past a thousand million. precision adds that many
// figures after the point, for an axis whose gridlines are closer together
// than the shortest form can tell apart.
export function compact(value: number, precision = 0): string {
  const size = Math.abs(value)
  if (size >= 1e9) return `${(value / 1e9).toFixed((size >= 1e10 ? 0 : 1) + precision)}B`
  if (size >= 1e6) return `${(value / 1e6).toFixed((size >= 1e7 ? 0 : 1) + precision)}M`
  if (size >= 1e3) return `${(value / 1e3).toFixed((size >= 1e4 ? 0 : 1) + precision)}k`
  return precision > 0 ? value.toFixed(precision) : String(Math.round(value))
}

// axisLabelsFor is the gridlines' labels in the shortest form that still
// tells each from the next: a net worth axis from $1.20M to $1.23M read
// "$1.2M" on every line, which said nothing about the change it was drawn
// to show. Where three more figures do not tell them apart either, a large
// total that barely moved, each is said whole with fullLabel, and the axis
// widens to fit.
export function axisLabelsFor(
  grid: number[],
  label: (value: number, precision: number) => string,
  fullLabel?: (value: number) => string,
): string[] {
  for (let precision = 0; precision <= 3; precision++) {
    const labels = grid.map((value) => label(value, precision))
    if (new Set(labels).size === labels.length) return labels
  }
  return grid.map((value) => (fullLabel ? fullLabel(value) : label(value, 3)))
}

// useChartHover is the key a chart's tooltip is shown for. A touch screen
// has no hover: a tap shows a key's tooltip, and it stays until a tap
// somewhere else, rather than going the moment the finger lifts.
export function useChartHover(
  holder: React.RefObject<HTMLDivElement | null>,
): [number | null, (index: number | null) => void] {
  const [hovered, setHovered] = useState<number | null>(null)
  useEffect(() => {
    if (hovered === null) return
    const onPointerDown = (event: PointerEvent) => {
      if (!holder.current?.contains(event.target as Node)) setHovered(null)
    }
    document.addEventListener('pointerdown', onPointerDown)
    return () => document.removeEventListener('pointerdown', onPointerDown)
  }, [hovered, holder])
  return [hovered, setHovered]
}

// chartPointerHandlers are a chart drawing's pointer events: a mouse shows
// the key under it and clears on leaving; a touch shows the key it lands
// on or is dragged sideways to, and a touch that turns into a scroll of the
// page (pointercancel) clears it, rather than leaving a tooltip open over
// a chart the reader has scrolled past.
export function chartPointerHandlers(
  pointAt: (clientX: number, element: SVGSVGElement) => void,
  onHover: (index: number | null) => void,
) {
  return {
    onPointerMove: (event: React.PointerEvent<SVGSVGElement>) => pointAt(event.clientX, event.currentTarget),
    onPointerDown: (event: React.PointerEvent<SVGSVGElement>) => pointAt(event.clientX, event.currentTarget),
    onPointerLeave: (event: React.PointerEvent<SVGSVGElement>) => {
      if (event.pointerType === 'mouse') onHover(null)
    },
    onPointerCancel: () => onHover(null),
  }
}

// CHART_TOOLTIP_GAP is how far a tooltip stands off the key it is about.
const CHART_TOOLTIP_GAP = 10

// ChartTooltip is the box of a key's values beside it: to its right, or to
// its left past halfway, and in either case held inside the plot. On a
// phone the plot is narrower than twice the box, and a box placed only by
// which half the key is in ran off the edge, losing the amounts.
export function ChartTooltip({
  center,
  width,
  children,
}: {
  center: number
  width: number
  children: React.ReactNode
}) {
  const box = useRef<HTMLDivElement>(null)
  const [boxWidth, setBoxWidth] = useState(0)
  useLayoutEffect(() => {
    const measured = box.current?.offsetWidth ?? 0
    if (measured !== boxWidth) setBoxWidth(measured)
  })
  const isLeft = center > width / 2
  const wanted = isLeft ? center - CHART_TOOLTIP_GAP - boxWidth : center + CHART_TOOLTIP_GAP
  const left = Math.max(0, Math.min(wanted, width - boxWidth))
  return (
    <div ref={box} className="usage-chart-tooltip" style={{ left: `${left}px` }}>
      {children}
    </div>
  )
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
// draws under its line. The axis is labelled with axisFormat, a shorter
// form of format where one reads better at the side of a chart.
//
// Given onSelectKey, each key's slot is also a button: clicked, or reached
// with Tab and moved along with the arrow keys, it chooses that key, and
// selectedKey is drawn as the chosen one. The slot is one tab stop, not one
// per key, so a year of months does not put twelve stops in the way.
export function SeriesChart({
  keys,
  keyLabel,
  keyTitle,
  series,
  format,
  axisFormat,
  headline,
  caption,
  label,
  selectedKey,
  onSelectKey,
  headAction,
  tooltipNote,
  isFitted = false,
  isEveryDipShown = false,
}: {
  keys: string[]
  keyLabel: (key: string) => string
  // The key at the top of its tooltip, where the axis's label is too short
  // to stand alone ("12" for a day of a month) or too long to fit under a
  // column; keyLabel when not given.
  keyTitle?: (key: string) => string
  series: ChartSeries[]
  format: (value: number) => string
  // The axis's labels; precision asks for that many more figures, when the
  // shortest form would label two gridlines alike.
  axisFormat?: (value: number, precision: number) => string
  headline?: React.ReactNode
  caption?: React.ReactNode
  // Beside the headline, at its right: a control choosing what the chart
  // shows, on the line it is about rather than in the panel's heading.
  headAction?: React.ReactNode
  // What the chart is, for a screen reader, ahead of its values.
  label: string
  selectedKey?: string | null
  onSelectKey?: (key: string) => void
  // A line under the series in the tooltip, for what the reader would
  // otherwise work out from them: how far one series is from another on
  // that key. Null leaves it out for that key.
  tooltipNote?: (index: number) => { label: string; text: string } | null
  // The scale drawn around the values rather than from zero, for a line
  // whose changes are small beside its size (see chartScale).
  isFitted?: boolean
  // Any value below zero is drawn below it (see chartScale), for a flow
  // whose small losses matter.
  isEveryDipShown?: boolean
}) {
  const [holder, width] = useWidth()
  const [hovered, setHovered] = useChartHover(holder)

  const scale = useMemo(
    () =>
      chartScale(
        series.flatMap((one) => one.values.filter((value): value is number => value !== null)),
        { isFitted, isEveryDipShown },
      ),
    [series, isFitted, isEveryDipShown],
  )
  const axisLabels = axisLabelsFor(scale.grid, axisFormat ?? ((value) => format(value)), format)
  const axisWidth = axisWidthFor(axisLabels)

  return (
    <div className="usage-chart">
      {headline || caption || headAction ? (
        <div className="usage-chart-head">
          <div>
            {headline ? <div className="usage-chart-total">{headline}</div> : null}
            {caption ? <div className="muted usage-chart-caption">{caption}</div> : null}
          </div>
          {headAction}
        </div>
      ) : null}
      <div className="usage-chart-plot" ref={holder}>
        {width > 0 && keys.length > 0 ? (
          <SeriesDrawing
            keys={keys}
            keyLabel={keyLabel}
            keyTitle={keyTitle ?? keyLabel}
            series={series}
            scale={scale}
            axisWidth={axisWidth}
            width={width}
            format={format}
            axisLabels={axisLabels}
            hovered={hovered}
            onHover={setHovered}
            label={label}
            selectedKey={selectedKey ?? null}
            onSelectKey={onSelectKey}
          />
        ) : null}
        {hovered !== null && keys[hovered] !== undefined && width > 0 ? (
          <SeriesTooltip
            title={(keyTitle ?? keyLabel)(keys[hovered])}
            index={hovered}
            count={keys.length}
            width={width}
            axisWidth={axisWidth}
            lines={series.map((one) => ({
              id: one.id,
              label: one.label,
              tone: one.tone,
              value: one.values[hovered] ?? null,
            }))}
            format={format}
            note={tooltipNote?.(hovered) ?? null}
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
  keyTitle,
  series,
  scale,
  axisWidth,
  width,
  format,
  axisLabels,
  hovered,
  onHover,
  label,
  selectedKey,
  onSelectKey,
}: {
  keys: string[]
  keyLabel: (key: string) => string
  keyTitle: (key: string) => string
  series: ChartSeries[]
  scale: ChartScale
  axisWidth: number
  width: number
  format: (value: number) => string
  axisLabels: string[]
  hovered: number | null
  onHover: (index: number | null) => void
  label: string
  selectedKey: string | null
  onSelectKey?: (key: string) => void
}) {
  const slotButtons = useRef<(SVGRectElement | null)[]>([])
  const { ceiling, floor } = scale
  const plotWidth = Math.max(40, width - axisWidth)
  const plotHeight = CHART_HEIGHT - CHART_TOP - CHART_BOTTOM
  const slot = plotWidth / Math.max(1, keys.length)
  const columns = series.filter((one) => one.shape === 'column')
  const lines = series.filter((one) => one.shape === 'line')
  const groupWidth = Math.max(2, Math.min(28 * Math.max(1, columns.length), slot * 0.62))
  const columnWidth = groupWidth / Math.max(1, columns.length)
  const span = Math.max(1e-9, ceiling - floor)
  // Held inside the scale, so a dip the scale chose not to reach for is
  // drawn on the zero line rather than over the axis labels.
  const yOf = (value: number) => CHART_TOP + ((ceiling - Math.max(floor, Math.min(ceiling, value))) / span) * plotHeight
  const zero = yOf(0)
  const labelEvery = Math.max(1, Math.ceil(keys.length / Math.max(2, Math.floor(plotWidth / 64))))
  const grid = scale.grid
  const selectedIndex = selectedKey === null ? -1 : keys.indexOf(selectedKey)
  const labeled = labeledIndexes(keys.length, labelEvery, selectedIndex)

  // The drawing is scaled to the width it is shown at, so a pointer is
  // turned back into the drawing's own units first.
  const pointAt = (clientX: number, element: SVGSVGElement) => {
    const bounds = element.getBoundingClientRect()
    const x = ((clientX - bounds.left) * width) / Math.max(1, bounds.width)
    const index = Math.floor((x - axisWidth) / slot)
    onHover(index >= 0 && index < keys.length ? index : null)
  }

  const slotSaid = (key: string, index: number) =>
    `${keyTitle(key)}: ${series
      .map((one) => `${one.label} ${one.values[index] === null ? '—' : format(one.values[index] ?? 0)}`)
      .join(', ')}`
  const said = keys.map(slotSaid).join('; ')
  const isSelectable = onSelectKey !== undefined
  // The one slot Tab lands on: the chosen one, or the latest when none is.
  const focusIndex = selectedIndex >= 0 ? selectedIndex : keys.length - 1

  const chooseAt = (index: number) => {
    if (!onSelectKey || index < 0 || index >= keys.length) return
    onSelectKey(keys[index])
    slotButtons.current[index]?.focus()
  }
  const onSlotKey = (event: React.KeyboardEvent<SVGRectElement>, index: number) => {
    const moves: Record<string, number> = {
      ArrowLeft: index - 1,
      ArrowDown: index - 1,
      ArrowRight: index + 1,
      ArrowUp: index + 1,
      Home: 0,
      End: keys.length - 1,
      Enter: index,
      ' ': index,
    }
    if (!(event.key in moves)) return
    event.preventDefault()
    chooseAt(Math.max(0, Math.min(keys.length - 1, moves[event.key])))
  }

  return (
    <svg
      viewBox={`0 0 ${width} ${CHART_HEIGHT}`}
      role={isSelectable ? 'group' : 'img'}
      aria-label={isSelectable ? label : `${label}. ${said}`}
      {...chartPointerHandlers(pointAt, onHover)}
    >
      {selectedIndex >= 0 ? (
        <rect
          className="series-chart-selected-band"
          x={axisWidth + selectedIndex * slot}
          y={CHART_TOP}
          width={slot}
          height={plotHeight}
          rx={4}
        />
      ) : null}
      {grid.map((value, index) => {
        const y = yOf(value)
        return (
          <g key={value}>
            <line className="usage-chart-grid" x1={axisWidth} x2={width} y1={y} y2={y} />
            <text className="usage-chart-axis" x={axisWidth - 8} y={y} textAnchor="end" dominantBaseline="middle">
              {axisLabels[index]}
            </text>
          </g>
        )
      })}
      {floor < 0 && ceiling > 0 ? (
        <line className="series-chart-zero" x1={axisWidth} x2={width} y1={zero} y2={zero} />
      ) : null}
      {keys.map((key, index) => {
        const groupX = axisWidth + index * slot + (slot - groupWidth) / 2
        // With a key chosen, the others step back (less far than for the
        // pointer: they are still being read), and the chosen one stays
        // forward either way.
        const isUnselected = selectedIndex >= 0 && index !== selectedIndex && index !== hovered
        const isDimmed = selectedIndex < 0 && hovered !== null && hovered !== index
        return (
          <g key={key} className={isUnselected ? 'series-chart-unselected' : isDimmed ? 'usage-chart-dim' : ''}>
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
            {labeled.has(index) ? (
              <text
                className={index === selectedIndex ? 'usage-chart-axis series-chart-axis-selected' : 'usage-chart-axis'}
                x={groupX + groupWidth / 2}
                y={CHART_HEIGHT - 6}
                textAnchor={
                  index === keys.length - 1 || (index % labelEvery === 0 && index + labelEvery >= keys.length)
                    ? 'end'
                    : 'middle'
                }
              >
                {keyLabel(key)}
              </text>
            ) : null}
          </g>
        )
      })}
      {lines.map((one) => (
        <path key={one.id} className={`series-chart-line ${one.tone}`} d={linePath(one.values, slot, yOf, axisWidth)} />
      ))}
      {/* A value with nothing either side of it is a line of one point,
          which draws nothing: net worth on the first day of a finance
          source was an empty chart. It gets a dot instead. */}
      {lines.flatMap((one) =>
        isolatedIndexes(one.values).map((index) => (
          <circle
            key={`${one.id}-${index}`}
            className={`series-chart-point ${one.tone}`}
            cx={axisWidth + index * slot + slot / 2}
            cy={yOf(one.values[index] as number)}
            r={3.5}
          />
        )),
      )}
      {hovered !== null
        ? lines.map((one) => {
            const value = one.values[hovered]
            if (value === null || value === undefined) return null
            return (
              <circle
                key={one.id}
                className={`series-chart-point ${one.tone}`}
                cx={axisWidth + hovered * slot + slot / 2}
                cy={yOf(value)}
                r={3.5}
              />
            )
          })
        : null}
      {/* Last, so they are on top: a slot as tall as the chart, clear, to
          press or to reach from the keyboard. */}
      {isSelectable
        ? keys.map((key, index) => (
            <rect
              key={key}
              ref={(element) => {
                slotButtons.current[index] = element
              }}
              className="series-chart-slot"
              x={axisWidth + index * slot}
              y={0}
              width={slot}
              height={CHART_HEIGHT}
              role="button"
              tabIndex={index === focusIndex ? 0 : -1}
              aria-pressed={index === selectedIndex}
              aria-label={slotSaid(key, index)}
              onClick={() => chooseAt(index)}
              onKeyDown={(event) => onSlotKey(event, index)}
            />
          ))
        : null}
    </svg>
  )
}

// labeledIndexes are the keys whose label is drawn under the axis: every
// labelEvery-th, as many as fit, and the chosen key always, since a chosen
// month with no name under it reads as a bar picked at random. The regular
// labels near enough to the chosen one to run into it step aside.
export function labeledIndexes(count: number, labelEvery: number, selectedIndex: number): Set<number> {
  const labeled = new Set<number>()
  for (let index = 0; index < count; index += Math.max(1, labelEvery)) {
    if (selectedIndex < 0 || Math.abs(index - selectedIndex) >= labelEvery) labeled.add(index)
  }
  if (selectedIndex >= 0 && selectedIndex < count) labeled.add(selectedIndex)
  return labeled
}

// isolatedIndexes are the values with no value beside them, which a line
// cannot show.
export function isolatedIndexes(values: (number | null | undefined)[]): number[] {
  const isPresent = (index: number) => values[index] !== null && values[index] !== undefined
  return values.flatMap((_, index) =>
    isPresent(index) && !isPresent(index - 1) && !isPresent(index + 1) ? [index] : [],
  )
}

// linePath runs through the middle of each slot, lifting the pen over a
// key the series has nothing for.
function linePath(values: (number | null)[], slot: number, yOf: (value: number) => number, axisWidth: number): string {
  const parts: string[] = []
  let isDrawing = false
  values.forEach((value, index) => {
    if (value === null || value === undefined) {
      isDrawing = false
      return
    }
    const x = axisWidth + index * slot + slot / 2
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
  axisWidth,
  lines,
  format,
  note,
}: {
  title: string
  index: number
  count: number
  width: number
  axisWidth: number
  lines: { id: string; label: string; tone: SeriesTone; value: number | null }[]
  format: (value: number) => string
  note: { label: string; text: string } | null
}) {
  const slot = (width - axisWidth) / Math.max(1, count)
  const center = axisWidth + index * slot + slot / 2
  return (
    <ChartTooltip center={center} width={width}>
      <div className="usage-chart-tooltip-day">{title}</div>
      {lines.map((line) => (
        <div key={line.id} className="usage-chart-tooltip-line">
          <i className={`usage-chart-swatch ${line.tone}`} />
          <span>{line.label}</span>
          <strong>{line.value === null ? '—' : format(line.value)}</strong>
        </div>
      ))}
      {note ? (
        <div className="usage-chart-tooltip-line usage-chart-tooltip-note">
          <i className="usage-chart-swatch none" />
          <span>{note.label}</span>
          <strong>{note.text}</strong>
        </div>
      ) : null}
    </ChartTooltip>
  )
}
