// titleNamesPlaces says whether a suggested album's title is made of its
// places, so the line that lists them under the dates would only say them
// again. A place is a "City, Country" name, and a title may say the country
// once for several cities, so a place is in the title when its city and its
// country each appear in it. A title of dates, or one that stops at "and
// more", leaves some place unsaid and the line stays.
export function titleNamesPlaces(albumTitle: string, placeNames: string[]): boolean {
  if (placeNames.length === 0) return false
  return placeNames.every((placeName) =>
    placeName
      .split(', ')
      .map((part) => part.trim())
      .filter(Boolean)
      .every((part) => albumTitle.includes(part)),
  )
}
