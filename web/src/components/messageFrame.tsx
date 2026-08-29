import { useCallback, useEffect, useRef, useState } from 'react'

// A frame that grows to fit the message, so the page scrolls rather than the
// message scrolling inside a box on a page that also scrolls. Two scrollbars
// for one document is a thing to read past, not a feature.
//
// Growing means measuring, and measuring means reaching into the frame's
// document — which the sandbox has to allow. It allows same-origin and
// nothing else: with no allow-scripts, and a policy of default-src 'none'
// inside, the message cannot execute anything, so there is nothing to use the
// shared origin for. The pair that defeats a sandbox is same-origin *and*
// scripts together; only one of them is here.
//
// SANDBOX, one flag at a time, because each is a decision:
//
//   allow-same-origin   so the height can be measured, as above.
//
//   allow-popups        so a link opens at all. The sanitiser puts
//                       target="_blank" on every link it keeps, and without
//                       this the browser silently drops the click — which is
//                       what it used to do.
//
//   allow-popups-to-escape-sandbox
//                       so the page that opens is a normal tab rather than
//                       one inheriting this sandbox. Without it a link
//                       "works" and lands on a crippled page with no scripts
//                       and no origin, which is worse than not opening.
//
// Deliberately absent:
//
//   allow-scripts       a message does not get to run code. This is the flag
//                       that would make same-origin dangerous.
//   allow-top-navigation
//                       a message does not get to navigate the dashboard away
//                       from under the reader, which is how a phishing page
//                       replaces the thing you thought you were looking at.
//                       Belt and braces with the forced target="_blank".
//   allow-forms         nothing in a message is submitting anywhere. CSP does
//                       not help here: form-action does not fall back to
//                       default-src, so the sandbox is what stops it.
//
// Escaping the sandbox needs a real click on a real anchor, because there is
// no script in here to synthesise one.

// A frame shorter than this looks like a mistake rather than a short message,
// and it is what is shown while the first measurement is still pending.
const MINIMUM_HEIGHT = 240

export function MessageFrame({ document: source, title }: { document: string; title: string }) {
  const frame = useRef<HTMLIFrameElement>(null)
  const lastWidth = useRef(0)
  const [height, setHeight] = useState(MINIMUM_HEIGHT)

  const measure = useCallback(() => {
    const element = frame.current
    const inner = element?.contentDocument
    // A wrapper the message cannot reach, put there by buildDocument. Not the
    // body and not the root: both are at least as tall as the frame's
    // viewport, which is the height being set from the measurement, so
    // measuring either feeds the frame's size back into itself. Mail sets
    // "body { height: 100% }" often enough that this is not a corner case —
    // it is most of the mail that arrives from a marketing tool.
    const content = inner?.getElementById('teanode-content')
    if (!element || !content) {
      return
    }

    const measured = Math.max(content.scrollHeight, MINIMUM_HEIGHT)
    const width = element.clientWidth

    setHeight((previous) => {
      // Sub-pixel churn from a reflow is not worth a re-render, and
      // re-rendering on it is the other way this loops.
      if (Math.abs(previous - measured) <= 1) {
        return previous
      }
      // Only ever taller, unless the frame itself changed width. Content
      // grows as images arrive; it does not legitimately shrink while the
      // frame stays the same size, so a shrink is the signature of a
      // measurement chasing its own tail.
      if (measured < previous && width === lastWidth.current) {
        return previous
      }
      lastWidth.current = width
      return measured
    })
  }, [])

  // A new message starts over: the height of the last one is not a floor for
  // this one.
  useEffect(() => {
    lastWidth.current = 0
    setHeight(MINIMUM_HEIGHT)
  }, [source])

  useEffect(() => {
    const element = frame.current
    if (!element) {
      return
    }

    // The content settles after load rather than at it: images arrive and
    // tables reflow. Watching the body is more reliable than measuring once
    // and hoping.
    let observer: ResizeObserver | undefined
    const onLoad = () => {
      measure()
      const inner = element.contentDocument
      if (!inner?.documentElement) {
        return
      }
      const content = inner.getElementById('teanode-content')
      if (!content) {
        return
      }
      observer = new ResizeObserver(measure)
      observer.observe(content)
    }

    element.addEventListener('load', onLoad)
    // Already loaded, if React reused the element for a new message.
    if (element.contentDocument?.readyState === 'complete') {
      onLoad()
    }

    return () => {
      element.removeEventListener('load', onLoad)
      observer?.disconnect()
    }
  }, [measure, source])

  return (
    <iframe
      ref={frame}
      className="message-frame"
      sandbox="allow-same-origin allow-popups allow-popups-to-escape-sandbox"
      srcDoc={source}
      title={title}
      style={{ height }}
      scrolling="no"
    />
  )
}
