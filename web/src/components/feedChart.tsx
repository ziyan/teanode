import { useState } from 'react'

import { useTranslation } from '../i18n/i18n'

// The charts of a feed card, drawn as inline SVG: a series over time, with a
// reference line, a projection and labeled markers, or horizontal bars. The
// provenance, one line saying where and when the numbers were read, is under
// every chart. A reading of a field this does not know is drawn as measured.

export type FeedSeriesRole = 'measured' | 'reference' | 'projection'

export interface FeedChartSeries {
  seriesName: string
  seriesRole: FeedSeriesRole
  values: (number | null)[]
}

export interface FeedChartMarker {
  markerAt: string
  markerLabel: string
}

export interface FeedChartBar {
  barLabel: string
  barValue: number
}

export interface FeedChartBlockData {
  chartKind: 'series' | 'bars'
  chartTitle: string
  unit: string
  provenance: string
  xLabels: string[]
  series: FeedChartSeries[]
  markers: FeedChartMarker[]
  bars: FeedChartBar[]
}

type Json = Record<string, unknown>
const isObject = (value: unknown): value is Json => typeof value === 'object' && value !== null && !Array.isArray(value)
const textOf = (value: unknown): string => (typeof value === 'string' ? value : '')
const listOf = (value: unknown): unknown[] => (Array.isArray(value) ? value : [])

// chartBlocksOf reads what the server sent, dropping what cannot be drawn: a
// series whose values do not match its labels, a chart with nothing in it.
export function chartBlocksOf(sent: unknown): FeedChartBlockData[] {
  return listOf(sent).flatMap((entry): FeedChartBlockData[] => {
    if (!isObject(entry)) return []
    const unit = textOf(entry.unit)
    const common = {
      chartTitle: textOf(entry.chartTitle),
      unit,
      provenance: textOf(entry.provenance),
    }
    if (entry.chartKind === 'bars') {
      const bars = listOf(entry.bars).flatMap((bar) =>
        isObject(bar) && typeof bar.barValue === 'number' && Number.isFinite(bar.barValue) && bar.barValue >= 0
          ? [{ barLabel: textOf(bar.barLabel), barValue: bar.barValue }]
          : [],
      )
      if (bars.length === 0) return []
      return [{ ...common, chartKind: 'bars' as const, xLabels: [], series: [], markers: [], bars }]
    }
    const xLabels = listOf(entry.xLabels).map(textOf)
    if (xLabels.length < 2) return []
    const series = listOf(entry.series).flatMap((line) => {
      if (!isObject(line)) return []
      const values = listOf(line.values).map((value) =>
        typeof value === 'number' && Number.isFinite(value) ? value : null,
      )
      if (values.length !== xLabels.length || values.every((value) => value === null)) return []
      const role = line.seriesRole === 'reference' || line.seriesRole === 'projection' ? line.seriesRole : 'measured'
      return [{ seriesName: textOf(line.seriesName), seriesRole: role as FeedSeriesRole, values }]
    })
    if (series.length === 0) return []
    const markers = listOf(entry.markers).flatMap((marker) =>
      isObject(marker) && xLabels.includes(textOf(marker.markerAt))
        ? [{ markerAt: textOf(marker.markerAt), markerLabel: textOf(marker.markerLabel) }]
        : [],
    )
    return [{ ...common, chartKind: 'series' as const, xLabels, series, markers, bars: [] }]
  })
}

// valueText is a number with its unit, as the tooltip and the bars say it.
export function valueText(value: number, unit: string): string {
  const number = value.toLocaleString(undefined, { maximumFractionDigits: 2 })
  return unit ? `${number} ${unit}` : number
}

const WIDTH = 360
const HEIGHT = 168
const LEFT = 44
const RIGHT = 10
const BOTTOM = 22
const MARKER_ROW_PX = 11

// segmentsOf splits a series into runs of consecutive values, so a missing
// value leaves a gap instead of a line to nowhere.
function segmentsOf(values: (number | null)[], xOf: (index: number) => number, yOf: (value: number) => number) {
  const segments: string[] = []
  let current = ''
  values.forEach((value, index) => {
    if (value === null) {
      if (current) segments.push(current)
      current = ''
      return
    }
    current += `${current ? 'L' : 'M'}${xOf(index).toFixed(1)} ${yOf(value).toFixed(1)}`
  })
  if (current) segments.push(current)
  return segments
}

function SeriesChart({ block }: { block: FeedChartBlockData }) {
  const { t } = useTranslation()
  const [activeIndex, setActiveIndex] = useState<number | null>(null)
  const top = 8 + block.markers.length * MARKER_ROW_PX
  const everyValue = block.series.flatMap((line) => line.values).filter((value): value is number => value !== null)
  let low = Math.min(...everyValue)
  let high = Math.max(...everyValue)
  if (high === low) {
    low -= 1
    high += 1
  }
  const padding = (high - low) * 0.08
  low -= padding
  high += padding
  const count = block.xLabels.length
  const plotWidth = WIDTH - LEFT - RIGHT
  const plotHeight = HEIGHT - top - BOTTOM
  const xOf = (index: number) => LEFT + (plotWidth * index) / (count - 1)
  const yOf = (value: number) => top + plotHeight * (1 - (value - low) / (high - low))
  const columnWidth = plotWidth / (count - 1)
  const shownLow = Math.min(...everyValue)
  const shownHigh = Math.max(...everyValue)
  // Reference and projection are drawn first so the measured line is on top.
  const drawOrder = ['reference', 'projection', 'measured']
  const ordered = [...block.series].sort((a, b) => drawOrder.indexOf(a.seriesRole) - drawOrder.indexOf(b.seriesRole))
  const activeLines = activeIndex === null ? [] : block.series.filter((line) => line.values[activeIndex] !== null)
  return (
    <div className="feed-chart-plot" onPointerLeave={() => setActiveIndex(null)}>
      <svg
        className="feed-chart-svg"
        viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
        role="group"
        aria-label={`${block.chartTitle}. ${block.provenance}`}
      >
        <text className="feed-chart-axis" x={LEFT - 6} y={yOf(shownHigh) + 3} textAnchor="end">
          {shownHigh.toLocaleString(undefined, { maximumFractionDigits: 1 })}
        </text>
        {shownLow !== shownHigh && (
          <text className="feed-chart-axis" x={LEFT - 6} y={yOf(shownLow) + 3} textAnchor="end">
            {shownLow.toLocaleString(undefined, { maximumFractionDigits: 1 })}
          </text>
        )}
        <line
          className="feed-chart-baseline"
          x1={LEFT}
          x2={WIDTH - RIGHT}
          y1={top + plotHeight}
          y2={top + plotHeight}
        />
        <text className="feed-chart-axis" x={LEFT} y={HEIGHT - 6} textAnchor="start">
          {block.xLabels[0]}
        </text>
        <text className="feed-chart-axis" x={WIDTH - RIGHT} y={HEIGHT - 6} textAnchor="end">
          {block.xLabels[count - 1]}
        </text>
        {block.markers.map((marker, markerIndex) => {
          const index = block.xLabels.indexOf(marker.markerAt)
          const x = xOf(index)
          const isNearEnd = index > count / 2
          return (
            <g key={`${marker.markerAt}-${markerIndex}`} className="feed-chart-marker">
              <line x1={x} x2={x} y1={8 + markerIndex * MARKER_ROW_PX} y2={top + plotHeight} />
              <text
                x={isNearEnd ? x - 4 : x + 4}
                y={8 + markerIndex * MARKER_ROW_PX + 8}
                textAnchor={isNearEnd ? 'end' : 'start'}
              >
                {marker.markerLabel || marker.markerAt}
              </text>
            </g>
          )
        })}
        {ordered.map((line, lineIndex) => (
          <g key={`${line.seriesName}-${lineIndex}`} className={`feed-chart-line is-${line.seriesRole}`}>
            {segmentsOf(line.values, xOf, yOf).map((path, segmentIndex) => (
              <path key={segmentIndex} d={path} />
            ))}
            {line.values.map((value, index) =>
              value !== null && line.values.every((other, otherIndex) => otherIndex === index || other === null) ? (
                <circle key={index} cx={xOf(index)} cy={yOf(value)} r={2.5} />
              ) : null,
            )}
          </g>
        ))}
        {activeIndex !== null && (
          <g className="feed-chart-hover">
            <line x1={xOf(activeIndex)} x2={xOf(activeIndex)} y1={top} y2={top + plotHeight} />
            {activeLines.map((line, lineIndex) => (
              <circle
                key={`${line.seriesName}-${lineIndex}`}
                className={`is-${line.seriesRole}`}
                cx={xOf(activeIndex)}
                cy={yOf(line.values[activeIndex] as number)}
                r={3.5}
              />
            ))}
          </g>
        )}
        {block.xLabels.map((label, index) => (
          <rect
            key={`${label}-${index}`}
            className="feed-chart-hit"
            x={xOf(index) - columnWidth / 2}
            y={0}
            width={columnWidth}
            height={HEIGHT}
            tabIndex={0}
            role="img"
            aria-label={`${label}: ${block.series
              .map(
                (line) =>
                  `${line.seriesName} ${line.values[index] === null ? t('feed.chartNoValue') : valueText(line.values[index] as number, block.unit)}`,
              )
              .join(', ')}`}
            onPointerEnter={() => setActiveIndex(index)}
            onPointerDown={() => setActiveIndex(index)}
            onFocus={() => setActiveIndex(index)}
            onBlur={() => setActiveIndex(null)}
          />
        ))}
      </svg>
      {activeIndex !== null && (
        <div
          className={`feed-chart-tooltip${activeIndex > count / 2 ? ' is-left' : ''}`}
          role="status"
          style={{ left: `${(xOf(activeIndex) / WIDTH) * 100}%` }}
        >
          <strong>{block.xLabels[activeIndex]}</strong>
          {block.series.map((line, lineIndex) => (
            <span key={`${line.seriesName}-${lineIndex}`} className="feed-chart-tooltip-row">
              <span className={`feed-chart-swatch is-${line.seriesRole}`} aria-hidden="true" />
              {line.seriesName}:{' '}
              {line.values[activeIndex] === null
                ? t('feed.chartNoValue')
                : valueText(line.values[activeIndex] as number, block.unit)}
            </span>
          ))}
        </div>
      )}
      {block.series.length > 1 && (
        <ul className="feed-chart-legend">
          {block.series.map((line, lineIndex) => (
            <li key={`${line.seriesName}-${lineIndex}`}>
              <span className={`feed-chart-swatch is-${line.seriesRole}`} aria-hidden="true" />
              {line.seriesName}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function BarsChart({ block }: { block: FeedChartBlockData }) {
  const largest = Math.max(...block.bars.map((bar) => bar.barValue), 0)
  return (
    <ul className="feed-chart-bars">
      {block.bars.map((bar, index) => (
        <li key={`${bar.barLabel}-${index}`} title={`${bar.barLabel}: ${valueText(bar.barValue, block.unit)}`}>
          <span className="feed-chart-bar-label">{bar.barLabel}</span>
          <svg className="feed-chart-bar-track" viewBox="0 0 100 10" preserveAspectRatio="none" aria-hidden="true">
            <rect x={0} y={0} width={100} height={10} className="feed-chart-bar-rail" />
            <rect
              x={0}
              y={0}
              width={largest > 0 ? (bar.barValue / largest) * 100 : 0}
              height={10}
              className="feed-chart-bar-fill"
            />
          </svg>
          <span className="feed-chart-bar-value">{valueText(bar.barValue, block.unit)}</span>
        </li>
      ))}
    </ul>
  )
}

export function FeedChartBlocks({ blocks }: { blocks: FeedChartBlockData[] }) {
  if (blocks.length === 0) return null
  return (
    <div className="feed-charts">
      {blocks.map((block, index) => (
        <figure key={`${block.chartTitle}-${index}`} className="feed-chart">
          {block.chartTitle && (
            <figcaption className="feed-chart-title">
              {block.chartTitle}
              {block.unit && <span className="muted"> ({block.unit})</span>}
            </figcaption>
          )}
          {block.chartKind === 'bars' ? <BarsChart block={block} /> : <SeriesChart block={block} />}
          <p className="muted feed-chart-provenance">{block.provenance}</p>
        </figure>
      ))}
    </div>
  )
}
