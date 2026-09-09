import { useMemo } from 'react'

// The mail still travelling, drawn where there is nothing to read yet.
//
// The same figure the front page uses: a dashed line wandering across, with an
// envelope following it. It is here because an empty reading pane is the one
// place in the program with room for it and nothing else to say — a sentence
// alone in the middle of a large empty panel reads as something missing rather
// than as something waiting.
//
// The path is drawn once and the envelope is moved along it by the browser
// with animateMotion, so nothing here runs per frame in script.

// The box the line is drawn in. Its proportions are the element's, so nothing
// is cropped and the envelope keeps its own shape.
const WIDTH = 1200
const HEIGHT = 190

// The line is a different line every time. A fixed wave is a picture, and a
// picture in the same place every day stops being looked at; this way the
// pane is recognisably itself without being identical.
const MIDDLE = HEIGHT / 2
const LEAST_RISE = 30
const MOST_RISE = 62
// Long steps, so the line crosses in two or three easy turns rather than
// rippling. A wave with many turns in a wide panel reads as a graph.
const LEAST_STEP = 560
const MOST_STEP = 900

// How far past each edge the line runs. Enough that it arrives from somewhere
// and leaves for somewhere, and no more: everything out here is line the
// envelope travels along where nobody can see it.
const OVERHANG = 110

// The stretch of the journey that happens where it can be seen, as a share of
// the whole. The line is WIDTH wide with OVERHANG at each end, so the visible
// part begins a little way in and ends a little way before the finish; a
// little further in again, so the envelope is not half off the edge.
const VISIBLE_FROM = (OVERHANG + 60) / (WIDTH + OVERHANG * 2)
const VISIBLE_TO = (OVERHANG + WIDTH - 60) / (WIDTH + OVERHANG * 2)

// between is a number in a range, which is all the randomness there is here.
function between(least: number, most: number): number {
  return least + Math.random() * (most - least)
}

// serpentine draws a wave from off one edge to off the other, alternating
// above and below the middle. Off both edges on purpose: a line that starts
// and stops inside the panel reads as a diagram of itself rather than as
// something passing through.
function serpentine(): string {
  const start = -OVERHANG
  const end = WIDTH + OVERHANG
  let x = start
  let above = Math.random() < 0.5
  let path = `M ${x.toFixed(0)} ${MIDDLE.toFixed(0)}`

  let first = true
  while (x < end) {
    const step = Math.min(between(LEAST_STEP, MOST_STEP), end - x)
    const next = x + step
    const rise = between(LEAST_RISE, MOST_RISE)
    const y = above ? MIDDLE - rise : MIDDLE + rise

    // The first crest is a curve of its own; every one after it is a smooth
    // curve, which reflects the previous control point through the crest it
    // is leaving. That reflection is what makes the line continuous: giving
    // each segment its own control points instead left a kink at every crest,
    // because the line arrived at one angle and departed at another.
    if (first) {
      path += ` C ${(x + step * 0.4).toFixed(0)} ${MIDDLE.toFixed(0)}, ${(x + step * 0.6).toFixed(0)} ${y.toFixed(0)}, ${next.toFixed(0)} ${y.toFixed(0)}`
      first = false
    } else {
      path += ` S ${(x + step * 0.6).toFixed(0)} ${y.toFixed(0)}, ${next.toFixed(0)} ${y.toFixed(0)}`
    }
    x = next
    above = !above
  }
  return path
}

export function EnvelopeTrail() {
  // Chosen once, when the pane appears. Not on every render: the line must not
  // change under somebody who has done nothing but click a folder.
  const { path, duration, begin } = useMemo(() => {
    const seconds = between(14, 22)
    return {
      path: serpentine(),
      duration: seconds,
      // Part way along already. A negative begin winds the animation back, so
      // the envelope is wherever it would have got to by now rather than
      // setting off from the edge every time somebody opens a folder.
      //
      // Not anywhere along it, though: the line runs past both edges, and a
      // start chosen from the whole of it put the envelope out of sight about
      // a third of the time — which looked like a drawing with no envelope in
      // it. The range is the part of the journey that is on the screen.
      begin: -between(seconds * VISIBLE_FROM, seconds * VISIBLE_TO),
    }
  }, [])

  // Somebody who has asked for less movement gets the line and the envelope,
  // standing still where the line begins. The drawing still says what it says;
  // it simply does not travel.
  const still =
    typeof window !== 'undefined' && window.matchMedia('(prefers-reduced-motion: reduce)').matches

  return (
    <svg
      className="envelope-trail"
      viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
      preserveAspectRatio="xMidYMid slice"
      aria-hidden="true"
      focusable="false"
    >
      <path d={path} fill="none" stroke="currentColor" strokeWidth="2" strokeDasharray="9 12" opacity="0.45" />
      <g opacity="0.85">
        <g transform={still ? `translate(-240 ${MIDDLE})` : undefined}>
          {/* Drawn about its own middle, so rotate="auto" turns it on the
              line rather than swinging it around a corner. */}
          <g transform="translate(-21 -15)">
            <rect width="42" height="30" rx="4" fill="var(--surface)" stroke="currentColor" strokeWidth="2" />
            <path d="M 3 5 L 21 18 L 39 5" fill="none" stroke="currentColor" strokeWidth="2" />
          </g>
          {!still && (
            <animateMotion
              dur={`${duration.toFixed(1)}s`}
              begin={`${begin.toFixed(1)}s`}
              repeatCount="indefinite"
              path={path}
              rotate="auto"
            />
          )}
        </g>
      </g>
    </svg>
  )
}
