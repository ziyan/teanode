# The phones' photo library is read a window at a time

This ExecPlan is a living document. Keep Progress, Surprises & Discoveries and
the Decision Log current as work proceeds.

## Purpose / Big Picture

On a library of many years, the iPhone and Android apps grow slower and more
laggy the further back the person scrolls, and the Years and Months views are
laggy from the moment they open. Both apps read the library a page at a time
from the newest photo backwards (`beforePhotoId`), keep every page read, and
group every photo held into days or years again on each change. The Years
view shows only the years of the photos held, so its last tile is on screen at
once and it reads page after page until the whole library is held.

After this change, an app holds only the pages near what is on screen. It asks
the server how many photos the list holds, lays out a square for each, and
reads a page by its number (`offset`) when a square of it comes near the
screen; pages far from the screen are let go. Years and Months ask the server
for each period, as the dashboard does (`web/src/photos/overview.ts`): the
span of years first, then for each year or month near the screen its newest
photo and how many it holds. Choosing a year or month counts the photos newer
than it and scrolls the grid to that number, which reads only the page there.

## Progress

- [ ] iPhone: TeaNodeKit reads a page by number and counts a list; a window of pages; the grid, Years, Months and the viewer on it.
- [ ] Android: the same in kit and app.
- [ ] Review, screenshots, tester builds.

## Surprises & Discoveries

(none yet)

## Decision Log

- Decision: the windowed list is the library (all, favorites, the filtered library), albums and people. The Trash and a search keep how they are read now.
  Rationale: the Trash is short and is laid out in days, each with when it goes; a search is answered whole, ranked. Both are small, and their layouts need every photo.
- Decision: no new server operation. `ListPhotos` already takes `offset` (up to 100000) with every filter, and `CountPhotos` takes the same arguments.
  Rationale: the dashboard already reads the library this way.

## Context and Orientation

Server: `ListPhotos(offset, pageSize <= 500, isFavorite, isTrashed, albumId, faceGroupId, accountUserId, photoKind, takenAfter, takenBefore, searchText, near...)` and `CountPhotos(same)` in `internal/api/v1api/apigraph/photo.go`. Order is newest first by when a photo was taken (or uploaded when it has no date).

Dashboard reference: `web/src/photos/overview.ts` (`readOverviewYears`, `readOverviewPeriod`, `periodBounds`, `countPhotosNewerThan`) and `web/src/pages/photos/overview.tsx`.

iPhone: `ios/TeaNode/Sources/PhotosView.swift` (`PhotoListing`, `PhotosModel`, `PhotoGrid`, the scale switch), `PhotoLibraryBrowsing.swift` (`PhotoOverviewList`, `PhotoOverviewTile`), `PhotoViewer.swift`, `ios/TeaNodeKit/Sources/TeaNodeKit/Photos.swift` (the documents and `connection.photos(...)`), `PhotoOverview.swift`, `PhotoTimeline` and `PhotoPaging`.

Android: `android/app/src/main/java/com/teanode/app/photos/PhotoList.kt` (the list and its paging), `PhotoGrid.kt` (grid and `PhotoOverviewGrid`), `PhotosSection.kt`, `PhotoViewer.kt`, and `android/kit/src/main/kotlin/com/teanode/kit/photos/`.

## Plan of Work

1. Kit (each platform): `photos(..., offset:, pageSize:)` beside the cursor form, and `countPhotos(...)` for the same lists. A `PhotoWindow` value type: `totalCount`, `pageSize` (200), `pages: [pageIndex: [Photo]]`, `photo(at index) -> Photo?`, `pageIndex(of index)`, `missingPages(around index, reach: 2)`, `pagesToRelease(around index, keep: 4)`, `replace(photo)`, `remove(ids)` (lowers the count and marks the pages from the first removed one stale), `index(of photoId)` among held pages. Tests for each.
2. Overview (kit): `overviewYears(list)` = count, the newest photo (offset 0) and the oldest (offset count-1), giving newest and oldest year; `overviewPeriod(list, year, month?)` = the newest photo in the period (takenAfter/takenBefore as the person's local calendar bounds, like `periodBounds`) and `CountPhotos` of it; `countNewer(list, than: period)` = `CountPhotos` with takenAfter = the period's end. Tests with invented data.
3. Model and grid (app): the list's model holds a `PhotoWindow` and the count; the grid is a lazy grid over `0..<totalCount`, each square the photo at that index or a plain placeholder of the same size; when a square appears the model reads the missing pages around it (no two reads of one page at once) and lets go of far pages. Ids for scrolling are the index. The date range under the title comes from the photos on screen, as now. Selecting, favoriting, deleting, moving to an album and changing a date keep working: a change replaces the photo where it is held; a removal lowers the count and reads the held pages again; a change of date reads the held pages again.
4. Years and Months (app): the span from `overviewYears`, a tile per year (or per month, under its year) that reads its period when it comes near the screen, at most six reads at once, with its cover, name and count. Choosing one counts the photos newer than it, switches to All and scrolls to that index.
5. Viewer (app): opened on an index of the window; swiping reads the page next to it when needed; closing returns to the same index.
6. Remove what this replaces: the cursor paging of the windowed lists, the regrouping of every held photo into days on each change (`PhotoListing.days` where nothing reads it), the years and months built from held photos.

## Validation and Acceptance

- Kit tests for the window and the overview on both platforms.
- iPhone: the app builds and TeaNodeKit's tests pass on the Mac (`teanode-build.sh <commit> build|kit`); the demo screenshots of the library, Years and Months still look right.
- Android: `:kit:test :app:testDebugUnitTest :app:compileDebugKotlin`.
- On a phone with a library of several years: Years opens with tiles at once; choosing an early year shows its photos within a second; scrolling the grid quickly to the bottom stays smooth and shows placeholders that fill in.

## Idempotence and Recovery

No server or data change; the apps read differently. A tester build from before still works against the server.

## Interfaces and Dependencies

GraphQL `ListPhotos` with `offset` and `CountPhotos`, unchanged.
