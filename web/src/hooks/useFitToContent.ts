import { RefObject, useLayoutEffect } from 'react'

// minimumHeightPixels is one line of the reply box.
const minimumHeightPixels = 36

// useFitToContent keeps a text box as tall as what is in it: one line until
// there is more, then as tall as the words, up to the cap the stylesheet
// sets. Whenever the value changes, not only while typing, so that text
// put there by the page -- a request to send, a draft kept from before, the
// box emptied after sending -- is shown whole or shrinks back. Before the
// paint, so the box never shows at its old height for a frame. isShown is
// for a box that is drawn only some of the time: drawn again, it starts
// from nothing and is fitted to the value it comes back with.
export function useFitToContent(element: RefObject<HTMLTextAreaElement | null>, value: string, isShown = true) {
  useLayoutEffect(() => {
    const box = element.current
    if (!box) return
    box.style.height = 'auto'
    box.style.height = `${Math.max(minimumHeightPixels, box.scrollHeight)}px`
  }, [element, value, isShown])
}
