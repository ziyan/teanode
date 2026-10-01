import { Fragment } from 'react'

// Text with every place a search matched it marked, ignoring case. The
// search is matched as typed, the same way a list filters by it, so what is
// marked is exactly why the row is still there.
export function HighlightText({ text, search }: { text: string; search: string }) {
  const words = search.trim()
  if (!words) return <>{text}</>
  const escaped = words.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const parts = text.split(new RegExp(`(${escaped})`, 'gi'))
  return (
    <>
      {parts.map((part, index) =>
        // split with one capturing group puts the matches at the odd places.
        index % 2 === 1 ? <mark key={index}>{part}</mark> : <Fragment key={index}>{part}</Fragment>,
      )}
    </>
  )
}
