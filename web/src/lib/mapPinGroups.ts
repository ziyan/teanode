// Pictures on a map that would cover each other at its zoom are drawn as one
// group, as Find My draws them, so nobody is hidden under somebody else.

// How close two pictures' middles may come, in pixels, before one covers the
// other: a 40px picture and a little more.
export const PICTURE_OVERLAP_PIXELS = 42

export type ProjectedPin<Pin> = { pin: Pin; x: number; y: number; isChosen: boolean }

// groupPins puts each pin with the first group whose first picture it would
// cover. The chosen pin is placed first, so it leads its group and the group
// stays where it is; the chosen one's group comes last, to be drawn on top.
export function groupPins<Pin>(pins: ProjectedPin<Pin>[], overlapPixels = PICTURE_OVERLAP_PIXELS): Pin[][] {
  const ordered = [...pins.filter((each) => each.isChosen), ...pins.filter((each) => !each.isChosen)]
  const groups: { x: number; y: number; pins: Pin[]; isChosen: boolean }[] = []
  for (const each of ordered) {
    const covered = groups.find((group) => Math.hypot(group.x - each.x, group.y - each.y) < overlapPixels)
    if (covered) {
      covered.pins.push(each.pin)
    } else {
      groups.push({ x: each.x, y: each.y, pins: [each.pin], isChosen: each.isChosen })
    }
  }
  return [...groups.filter((group) => !group.isChosen), ...groups.filter((group) => group.isChosen)].map(
    (group) => group.pins,
  )
}
