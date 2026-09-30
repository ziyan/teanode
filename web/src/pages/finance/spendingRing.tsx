import { formatMoney } from '../../components/common'

// A ring of where a month's money went, a slice a spending category, drawn
// in SVG with the page's own colours. The table under it has every
// category; the ring keeps the largest few and folds the rest into one
// slice, since a ring of twenty slivers says nothing a table does not say
// better.

// RING_SLICE_COUNT is how many categories keep a slice of their own, and
// how many colours there are for them.
export const RING_SLICE_COUNT = 8

export type RingSlice = {
  key: string
  label: string
  amount: number
  // The slice the smaller categories were folded into, and how many.
  isOther: boolean
  foldedCount: number
}

// foldIntoOther keeps the largest keepCount slices, largest first, and adds
// up the rest into one. Nothing is folded when only one would be: a slice
// called "other" holding a single category hides its name for nothing.
// Slices of nothing are left out.
export function foldIntoOther(
  slices: { key: string; label: string; amount: number }[],
  keepCount: number,
): RingSlice[] {
  const sorted = slices
    .filter((slice) => slice.amount > 0)
    .sort((left, right) => right.amount - left.amount)
    .map((slice) => ({ ...slice, isOther: false, foldedCount: 0 }))
  if (sorted.length <= keepCount + 1) return sorted
  const folded = sorted.slice(keepCount)
  return [
    ...sorted.slice(0, keepCount),
    {
      key: 'other',
      label: '',
      amount: folded.reduce((total, slice) => total + slice.amount, 0),
      isOther: true,
      foldedCount: folded.length,
    },
  ]
}

function ringPoint(center: number, radius: number, fraction: number): string {
  // Fractions of a turn, clockwise from the top.
  const angle = fraction * 2 * Math.PI - Math.PI / 2
  return `${(center + radius * Math.cos(angle)).toFixed(2)},${(center + radius * Math.sin(angle)).toFixed(2)}`
}

// ringSlicePath is the outline of one slice of a ring centred in a square
// drawing: from startFraction to endFraction of a turn, clockwise from the
// top, between the two radii. A whole turn is drawn as two halves, because
// an SVG arc whose ends meet draws nothing.
export function ringSlicePath(
  center: number,
  outerRadius: number,
  innerRadius: number,
  startFraction: number,
  endFraction: number,
): string {
  const span = endFraction - startFraction
  if (span <= 0) return ''
  if (span >= 1 - 1e-9) {
    const middle = startFraction + 0.5
    return `${ringSlicePath(center, outerRadius, innerRadius, startFraction, middle)} ${ringSlicePath(
      center,
      outerRadius,
      innerRadius,
      middle,
      startFraction + 1,
    )}`
  }
  const isLarge = span > 0.5 ? 1 : 0
  return [
    `M${ringPoint(center, outerRadius, startFraction)}`,
    `A${outerRadius},${outerRadius} 0 ${isLarge} 1 ${ringPoint(center, outerRadius, endFraction)}`,
    `L${ringPoint(center, innerRadius, endFraction)}`,
    `A${innerRadius},${innerRadius} 0 ${isLarge} 0 ${ringPoint(center, innerRadius, startFraction)}`,
    'Z',
  ].join(' ')
}

const RING_SIZE = 200
const RING_OUTER = 96
const RING_INNER = 62

// ringSliceClass is the colour a slice is drawn in, as a class, for the
// slice and for the swatch that names it elsewhere: the table under the
// ring is its legend.
export function ringSliceClass(slice: RingSlice, index: number): string {
  return slice.isOther ? 'other' : `tone-${index % RING_SLICE_COUNT}`
}

// SpendingRing draws the slices, the total in the middle. It has no legend
// of its own: the table under it names each slice with its swatch, and a
// legend beside it said everything the table did a second time. The slice
// of the row under the pointer or the keyboard can be picked out.
export function SpendingRing({
  slices,
  currency,
  label,
  totalLabel,
  highlightedKey,
  totalAmount,
}: {
  slices: RingSlice[]
  currency: string
  // What the ring is, for a screen reader, ahead of its slices.
  label: string
  // The word under the total in the middle.
  totalLabel: string
  highlightedKey?: string | null
  // The figure in the middle, where it is not the slices added up: the
  // month's spending, which counts a category whose refunds outweighed its
  // purchases, where no slice can.
  totalAmount?: number
}) {
  const total = slices.reduce((sum, slice) => sum + slice.amount, 0)
  if (total <= 0) return null
  const percent = new Intl.NumberFormat(undefined, { style: 'percent', maximumFractionDigits: 0 })
  let reached = 0
  const drawn = slices.map((slice, index) => {
    const start = reached / total
    reached += slice.amount
    return { slice, index, path: ringSlicePath(RING_SIZE / 2, RING_OUTER, RING_INNER, start, reached / total) }
  })
  const said = slices
    .map((slice) => `${slice.label}: ${formatMoney(slice.amount, currency)} (${percent.format(slice.amount / total)})`)
    .join('; ')
  return (
    <div className="spending-ring">
      <svg
        className="spending-ring-drawing"
        viewBox={`0 0 ${RING_SIZE} ${RING_SIZE}`}
        role="img"
        aria-label={`${label}. ${said}`}
      >
        {drawn.map(({ slice, index, path }) => (
          <path
            key={slice.key}
            className={[
              'spending-ring-slice',
              ringSliceClass(slice, index),
              highlightedKey && highlightedKey !== slice.key ? 'dimmed' : '',
            ]
              .filter(Boolean)
              .join(' ')}
            d={path}
          />
        ))}
        <text className="spending-ring-total" x={RING_SIZE / 2} y={RING_SIZE / 2 - 2} textAnchor="middle">
          {formatMoney(totalAmount ?? total, currency)}
        </text>
        <text className="spending-ring-caption" x={RING_SIZE / 2} y={RING_SIZE / 2 + 18} textAnchor="middle">
          {totalLabel}
        </text>
      </svg>
    </div>
  )
}
