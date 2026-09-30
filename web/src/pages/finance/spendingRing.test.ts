import { expect, it } from 'vitest'

import { foldIntoOther, ringSlicePath } from './spendingRing'

const slice = (key: string, amount: number) => ({ key, label: key, amount })

it('keeps the largest slices, largest first, and folds the rest into one', () => {
  const folded = foldIntoOther(
    [slice('a', 10), slice('b', 50), slice('c', 5), slice('d', 30), slice('e', 1), slice('f', 2)],
    3,
  )
  expect(folded.map((one) => one.key)).toEqual(['b', 'd', 'a', 'other'])
  expect(folded[3]).toMatchObject({ isOther: true, amount: 8, foldedCount: 3 })
  expect(folded.slice(0, 3).every((one) => !one.isOther)).toBe(true)
})

// Folding a single category into "other" would hide its name and gain
// nothing, so one more than the limit keeps them all.
it('does not fold a single slice, and leaves out slices of nothing', () => {
  expect(foldIntoOther([slice('a', 3), slice('b', 2), slice('c', 1)], 2).map((one) => one.key)).toEqual([
    'a',
    'b',
    'c',
  ])
  expect(foldIntoOther([slice('a', 3), slice('b', 0), slice('c', -4)], 8).map((one) => one.key)).toEqual(['a'])
  expect(foldIntoOther([], 8)).toEqual([])
})

// A quarter turn from the top ends at three o'clock: outer arc from the
// top to the right, a line in, the inner arc back.
it('draws a slice clockwise from the top between the two radii', () => {
  const path = ringSlicePath(100, 90, 60, 0, 0.25)
  expect(path).toBe('M100.00,10.00 A90,90 0 0 1 190.00,100.00 L160.00,100.00 A60,60 0 0 0 100.00,40.00 Z')
})

it('marks a slice past half a turn as the large arc', () => {
  const path = ringSlicePath(100, 90, 60, 0, 0.75)
  expect(path).toContain('A90,90 0 1 1 10.00,100.00')
  expect(path).toContain('A60,60 0 1 0 100.00,40.00')
})

// An arc whose two ends meet draws nothing, so one category that is the
// whole month is drawn as two halves.
it('draws a whole turn as two halves and nothing for an empty slice', () => {
  const path = ringSlicePath(100, 90, 60, 0, 1)
  expect(path.match(/M/g)?.length).toBe(2)
  expect(path).toContain('190.00')
  expect(path).toContain('100.00,190.00')
  expect(ringSlicePath(100, 90, 60, 0.4, 0.4)).toBe('')
})
