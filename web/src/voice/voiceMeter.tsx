import { useEffect, useRef } from 'react'

// VoiceMeter is voice mode's picture: five bars that follow how loud the
// microphone is, frame by frame. Heard (the provider says somebody is
// talking) they stand up in the accent; while the agent works they breathe
// slowly on their own; otherwise they rest low. Drawn by setting each bar's
// height directly, not through React, since it changes sixty times a
// second.
export function VoiceMeter({
  level,
  isHearing,
  isAnswering,
}: {
  level: () => number
  isHearing: boolean
  isAnswering: boolean
}) {
  const bars = useRef<(HTMLSpanElement | null)[]>([])
  const smoothed = useRef(0)
  const state = useRef({ isHearing, isAnswering })
  state.current = { isHearing, isAnswering }

  useEffect(() => {
    const isReduced = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false
    // Each bar answers the level by its own share, so the row has a shape.
    const shares = [0.55, 0.85, 1, 0.85, 0.55]
    let frame = 0
    const draw = (time: number) => {
      const now = level()
      // Up quickly, down gently: speech reads as a pulse, not a flicker.
      smoothed.current = now > smoothed.current ? now : smoothed.current * 0.88 + now * 0.12
      const { isAnswering } = state.current
      bars.current.forEach((bar, index) => {
        if (!bar) return
        let height: number
        if (isReduced) {
          height = 0.3 + smoothed.current * 0.5
        } else if (isAnswering && smoothed.current < 0.08) {
          height = 0.25 + 0.2 * (1 + Math.sin(time / 420 + index * 0.9)) * 0.5
        } else {
          const wobble = 0.08 * Math.sin(time / 90 + index * 1.7)
          height = 0.15 + Math.max(0, smoothed.current * shares[index] + wobble * smoothed.current)
        }
        bar.style.transform = `scaleY(${Math.min(1, height).toFixed(3)})`
      })
      frame = requestAnimationFrame(draw)
    }
    frame = requestAnimationFrame(draw)
    return () => cancelAnimationFrame(frame)
  }, [level])

  return (
    <span
      className={['voice-meter', isHearing ? 'is-hearing' : '', isAnswering ? 'is-answering' : '']
        .filter(Boolean)
        .join(' ')}
      aria-hidden="true"
    >
      {[0, 1, 2, 3, 4].map((index) => (
        <span
          key={index}
          className="voice-meter-bar"
          ref={(element) => {
            bars.current[index] = element
          }}
        />
      ))}
    </span>
  )
}
