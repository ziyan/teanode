import SwiftUI
import TeaNodeKit
import UIKit

/// PhotoSection is what the segmented control under the title chooses: three
/// lists of photos, the albums and the people.
enum PhotoSection: String, CaseIterable, Identifiable {
    case library, favorites, albums, people, trash

    var id: String { rawValue }

    /// The name in the segmented control.
    var title: String {
        switch self {
        case .library: String(localized: "Library")
        case .favorites: String(localized: "Favorites")
        case .albums: String(localized: "Albums")
        case .people: String(localized: "photos.people", defaultValue: "People")
        case .trash: String(localized: "Trash")
        }
    }

    /// The list of photos the section shows; nil for Albums and People, which
    /// show albums and people and open each on a list of its own.
    var collection: PhotoCollection? {
        switch self {
        case .library: .library
        case .favorites: .favorites
        case .trash: .trash
        case .albums, .people: nil
        }
    }
}

/// PhotoListKey names one list of photos the model holds: a list of the
/// library, the photos of one album, the photos of one person (a group of
/// faces), or the photos that match the words typed in the library's search.
enum PhotoListKey: Hashable {
    case collection(PhotoCollection)
    /// A list of the library narrowed by kind or by days.
    case filtered(PhotoCollection, PhotoFilter)
    case album(String)
    case faceGroup(String)
    case search(String)

    static let library = PhotoListKey.collection(.library)
    static let favorites = PhotoListKey.collection(.favorites)
    static let trash = PhotoListKey.collection(.trash)

    /// The list of the library this is, narrowed or not; nil for the photos of
    /// an album or a person, and for a search.
    var photoCollection: PhotoCollection? {
        switch self {
        case .collection(let collection), .filtered(let collection, _): collection
        case .album, .faceGroup, .search: nil
        }
    }

    /// The album whose photos the list holds, if it is one.
    var albumId: String? {
        if case .album(let albumId) = self { return albumId }
        return nil
    }

    /// The group of faces whose photos the list holds, if it is one.
    var photoFaceGroupId: String? {
        if case .faceGroup(let photoFaceGroupId) = self { return photoFaceGroupId }
        return nil
    }

    /// Whether the list is the answer to a search, ranked by match.
    var isSearch: Bool {
        if case .search = self { return true }
        return false
    }
}

/// PhotoListing is one list of the library as far as it has been read, with
/// its photos grouped in days.
struct PhotoListing {
    private(set) var photos: [Photo]
    private(set) var days: [PhotoDay]
    /// Whether another page may follow what has been read.
    private(set) var hasMore: Bool
    /// Whether the photos are ranked by how well they match a search, and so
    /// are laid out in that order in one group, not in days.
    let isRanked: Bool
    var isLoadingPage = false

    init(photos: [Photo], hasMore: Bool, isRanked: Bool = false) {
        self.photos = photos
        self.isRanked = isRanked
        self.days = isRanked ? PhotoTimeline.rankedDay(photos) : PhotoTimeline.days(photos)
        self.hasMore = hasMore
    }

    private mutating func layOut() {
        days = isRanked ? PhotoTimeline.rankedDay(photos) : PhotoTimeline.days(photos)
    }

    mutating func append(_ page: [Photo]) {
        photos = PhotoPaging.appending(page, to: photos)
        layOut()
        hasMore = PhotoPaging.hasMore(afterPageOf: page.count)
    }

    /// Puts a photo as the server now has it in place of the one held.
    mutating func replace(_ photo: Photo) {
        guard let index = photos.firstIndex(where: { $0.id == photo.id }) else { return }
        photos[index] = photo
        layOut()
    }

    /// Puts the photos of a page just read in place of the ones held, for
    /// those that are held, and leaves the rest.
    mutating func replaceHeld(by page: [Photo]) {
        let newest = Dictionary(page.map { ($0.id, $0) }, uniquingKeysWith: { _, second in second })
        photos = photos.map { newest[$0.id] ?? $0 }
        layOut()
    }

    mutating func remove(_ photoIds: Set<String>) {
        photos.removeAll { photoIds.contains($0.id) }
        layOut()
    }
}

/// PhotosModel is the person's photo library as the phone shows it: the
/// lists of photos, read a page at a time, the albums, and what can be done to
/// a photo.
@MainActor
@Observable
final class PhotosModel {
    let connection: Connection
    var section = PhotoSection.library
    private(set) var listings: [PhotoListKey: PhotoListing] = [:]
    private(set) var failureTexts: [PhotoListKey: String] = [:]
    private(set) var library: PhotoLibrary?
    private(set) var savingPhotoIds: Set<String> = []
    /// The albums, the one changed most recently first; nil until read.
    private(set) var albums: [PhotoAlbum]?
    private(set) var albumsFailureText: String?
    /// The trips and events found in photos that are in no album.
    private(set) var suggestions: [PhotoAlbumSuggestion] = []
    /// The sample photos of the suggestions, by id, for their pictures.
    private(set) var suggestionSamples: [String: Photo] = [:]
    private(set) var hasReadSuggestions = false
    /// The groups of faces, the ones the person hid left out unless
    /// isHiddenPeopleShown; nil until read. See PhotoPeople.swift.
    var faceGroups: [PhotoFaceGroup]?
    var faceGroupsFailureText: String?
    var isHiddenPeopleShown = false
    /// The kind and days the library, Favorites and Trash are narrowed to.
    var photoFilter = PhotoFilter()
    /// The circles the person is in, for making an album for one; empty until read.
    private(set) var circles: [PeopleCircle] = []
    var doneText: String?
    var problemText: String?
    /// The title being asked for, by an alert on whichever screen raised it.
    var albumTitleRequest: AlbumTitleRequest?

    init(connection: Connection) {
        self.connection = connection
    }

    /// What to show for an error, as every other screen of the app says it:
    /// the Kit names what went wrong and the app writes the words.
    func describe(_ error: Error) -> String {
        AppModel.shared.describe(error)
    }

    /// Makes a list of just these photos, for a viewer opened on photos the
    /// lists were not read for, such as one the agent showed in a conversation.
    func hold(_ photos: [Photo], as key: PhotoListKey) {
        listings[key] = PhotoListing(photos: photos, hasMore: false)
    }

    func listing(_ key: PhotoListKey) -> PhotoListing? { listings[key] }

    /// Forgets a list that has been read, so that it is read again when it is
    /// next shown.
    func forgetListing(_ key: PhotoListKey) { listings[key] = nil }

    /// Forgets every list that has been read and matches.
    func forgetListings(where matches: (PhotoListKey) -> Bool) {
        for key in Array(listings.keys) where matches(key) { listings[key] = nil }
    }
    func photos(in key: PhotoListKey) -> [Photo] { listings[key]?.photos ?? [] }
    func failureText(_ key: PhotoListKey) -> String? { failureTexts[key] }
    func hasWaitingPhotos(in key: PhotoListKey) -> Bool { photos(in: key).contains { $0.isWaiting } }

    func loadLibrary() async {
        library = try? await connection.photoLibrary()
        // A server that cannot find faces still has the people found before,
        // and People is offered if there are any.
        if library?.isFaceRecognitionAvailable == false, faceGroups == nil { await reloadFaceGroups(isQuiet: true) }
    }

    /// The choices of the segmented control: People only when faces can be
    /// found, or people were found before, or it is the one being looked at.
    var sections: [PhotoSection] {
        PhotoSection.allCases.filter { $0 != .people || isPeopleShown || section == .people }
    }

    var isPeopleShown: Bool {
        library?.isFaceRecognitionAvailable == true || !(faceGroups ?? []).isEmpty
    }

    private func fetchPage(_ key: PhotoListKey, beforePhotoId: String? = nil, pageSize: Int = PhotoPaging.pageSize) async throws -> [Photo] {
        switch key {
        case .collection(let collection):
            return try await connection.photos(in: collection, beforePhotoId: beforePhotoId, pageSize: pageSize)
        case .filtered(let collection, let filter):
            return try await connection.photos(in: collection, filter: filter, beforePhotoId: beforePhotoId, pageSize: pageSize)
        case .album(let albumId):
            return try await connection.photos(inAlbum: albumId, beforePhotoId: beforePhotoId, pageSize: pageSize)
        case .faceGroup(let photoFaceGroupId):
            return try await connection.photos(inFaceGroup: photoFaceGroupId, beforePhotoId: beforePhotoId, pageSize: pageSize)
        case .search(let words):
            // A search is answered whole, best match first, and not in pages.
            return try await connection.photos(matching: words)
        }
    }

    /// Reads the first page of a list, unless it has been read.
    func load(_ key: PhotoListKey) async {
        guard listings[key] == nil else { return }
        await reload(key)
    }

    /// Reads the first page of a list again, which drops any further pages read.
    func reload(_ key: PhotoListKey) async {
        do {
            let page = try await fetchPage(key)
            listings[key] = PhotoListing(photos: page, hasMore: key.isSearch ? false : PhotoPaging.hasMore(afterPageOf: page.count), isRanked: key.isSearch)
            failureTexts[key] = nil
        } catch {
            guard !Task.isCancelled else { return }
            if listings[key] == nil { failureTexts[key] = describe(error) }
            problemText = describe(error)
        }
    }

    /// Reads the next page when a cell near the end of what is loaded comes
    /// on screen.
    func loadMoreIfNeeded(showing photoId: String, in key: PhotoListKey) async {
        guard let listing = listings[key], listing.hasMore, !listing.isLoadingPage,
            PhotoPaging.shouldLoadMore(whenShowing: photoId, in: listing.photos), let last = listing.photos.last
        else { return }
        listings[key]?.isLoadingPage = true
        defer { listings[key]?.isLoadingPage = false }
        do {
            let page = try await fetchPage(key, beforePhotoId: last.id)
            // The list may have been read again meanwhile; this page then
            // follows something that is no longer its end.
            guard listings[key]?.photos.last?.id == last.id else { return }
            listings[key]?.append(page)
        } catch {
            if !Task.isCancelled { problemText = describe(error) }
        }
    }

    /// While a photo of the list is still being made ready, asks for the list
    /// again now and then, a little less often each time, so its picture
    /// appears without the person pulling to refresh.
    func followWaitingPhotos(in key: PhotoListKey) async {
        var attempt = 1
        while !Task.isCancelled, hasWaitingPhotos(in: key) {
            try? await Task.sleep(for: .seconds(min(30, 5 * attempt)))
            attempt += 1
            guard !Task.isCancelled, let held = listings[key] else { return }
            if let page = try? await fetchPage(key, pageSize: min(max(held.photos.count, 1), 500)) {
                listings[key]?.replaceHeld(by: page)
            }
        }
    }

    // MARK: Changing photos

    /// Marks photos favorites, or not. The Favorites list being looked at
    /// keeps a photo just un-favorited until it is left (see
    /// dropPhotosNoLongerFavorite); any other copy of that list is read anew.
    func setFavorite(_ photos: [Photo], isFavorite: Bool, in key: PhotoListKey) async {
        do {
            for photo in photos where photo.isFavorite != isFavorite {
                let updated = try await connection.setPhotoFavorite(photo.id, isFavorite: isFavorite)
                for listKey in Array(listings.keys) { listings[listKey]?.replace(updated) }
            }
            forgetListings { $0.photoCollection == .favorites && $0 != key }
        } catch {
            problemText = describe(error)
        }
    }

    func toggleFavorite(_ photo: Photo, in key: PhotoListKey) async {
        await setFavorite([photo], isFavorite: !photo.isFavorite, in: key)
    }

    /// Takes the photos that are no longer favorites out of the Favorites
    /// list, once the person has left them.
    func dropPhotosNoLongerFavorite() {
        for key in Array(listings.keys) where key.photoCollection == .favorites {
            guard let favorites = listings[key] else { continue }
            let dropped = Set(favorites.photos.filter { !$0.isFavorite }.map(\.id))
            if !dropped.isEmpty { listings[key]?.remove(dropped) }
        }
    }

    /// Moves photos to the Trash; true when they went, so that a viewer leaves
    /// a photo only when it is gone.
    @discardableResult
    func moveToTrash(_ photos: [Photo]) async -> Bool {
        let photoIds = photos.map(\.id)
        do {
            try await connection.trashPhotos(photoIds)
            // A photo in the Trash is in no list but the Trash, and no album
            // counts it.
            for key in Array(listings.keys) where key.photoCollection != .trash { listings[key]?.remove(Set(photoIds)) }
            forgetListings { $0.photoCollection == .trash }
            library = nil
            doneText = String(localized: "Moved to Trash")
            await loadLibrary()
            if albums != nil { await albumsChanged() }
            return true
        } catch {
            problemText = describe(error)
            return false
        }
    }

    @discardableResult
    func moveToTrash(_ photo: Photo) async -> Bool {
        await moveToTrash([photo])
    }

    /// Takes photos out of the Trash; true when they came back.
    @discardableResult
    func restore(_ photos: [Photo]) async -> Bool {
        let photoIds = photos.map(\.id)
        do {
            try await connection.restorePhotos(photoIds)
            for key in Array(listings.keys) where key.photoCollection == .trash { listings[key]?.remove(Set(photoIds)) }
            forgetListings { $0.photoCollection != .trash }
            doneText = String(localized: "Restored to your library")
            await loadLibrary()
            if albums != nil { await albumsChanged() }
            return true
        } catch {
            problemText = describe(error)
            return false
        }
    }

    @discardableResult
    func restore(_ photo: Photo) async -> Bool {
        await restore([photo])
    }

    /// Deletes photos of the Trash for good; true when they went.
    @discardableResult
    func deleteForever(_ photos: [Photo]) async -> Bool {
        let photoIds = photos.map(\.id)
        do {
            try await connection.deletePhotos(photoIds)
            for key in Array(listings.keys) where key.photoCollection == .trash { listings[key]?.remove(Set(photoIds)) }
            doneText = String(localized: "Deleted forever")
            await loadLibrary()
            return true
        } catch {
            problemText = describe(error)
            return false
        }
    }

    @discardableResult
    func deleteForever(_ photo: Photo) async -> Bool {
        await deleteForever([photo])
    }

    func emptyTrash() async {
        do {
            try await connection.emptyPhotoTrash()
            forgetListings { $0.photoCollection == .trash && $0 != .trash }
            listings[.trash] = PhotoListing(photos: [], hasMore: false)
            doneText = String(localized: "The Trash is empty")
            await loadLibrary()
        } catch {
            problemText = describe(error)
        }
    }

    /// Puts photos that failed back in the queue to have their previews made
    /// again, and reads them anew so the grid shows them waiting. True when
    /// the server took the request.
    @discardableResult
    func retryProcessing(_ photos: [Photo]) async -> Bool {
        do {
            try await connection.retryPhotoProcessing(photos.map(\.id))
            for photo in photos {
                if let fresh = try? await connection.photoDetail(photo.id).photo {
                    for key in Array(listings.keys) { listings[key]?.replace(fresh) }
                }
            }
            doneText = String(localized: "Trying again")
            return true
        } catch {
            problemText = describe(error)
            return false
        }
    }

    /// What of a photo is added to the phone's own Photos app: the original, the
    /// version edited on a phone, or the original with its video as a Live Photo.
    enum PhoneSave {
        case original, edited, livePhoto
    }

    /// Adds a photo to the phone's own Photos app: the original file, or the
    /// large preview when the server has no original for the person. A failed
    /// read is told, never answered with a smaller copy.
    func saveToPhotos(_ photo: Photo, saving phoneSave: PhoneSave = .original) async {
        guard !savingPhotoIds.contains(photo.id) else { return }
        savingPhotoIds.insert(photo.id)
        defer { savingPhotoIds.remove(photo.id) }
        do {
            switch phoneSave {
            case .original:
                let download = try await connection.downloadPhotoFile(photo)
                defer { try? FileManager.default.removeItem(at: download.fileURL.deletingLastPathComponent()) }
                try await PhotoLibrarySaver.save(fileURL: download.fileURL, isVideo: photo.isVideo)
                doneText = download.isOriginal ? String(localized: "Saved to the Photos app") : String(localized: "Saved a smaller copy to the Photos app")
            case .edited:
                let download = try await connection.downloadPhotoFile(photo, role: "edited")
                defer { try? FileManager.default.removeItem(at: download.fileURL.deletingLastPathComponent()) }
                try await PhotoLibrarySaver.save(fileURL: download.fileURL, isVideo: photo.isVideo)
                doneText = String(localized: "Saved the edited version to the Photos app")
            case .livePhoto:
                let picture = try await connection.downloadPhotoFile(photo, role: "original")
                defer { try? FileManager.default.removeItem(at: picture.fileURL.deletingLastPathComponent()) }
                let video = try await connection.downloadPhotoFile(photo, role: "liveVideo")
                defer { try? FileManager.default.removeItem(at: video.fileURL.deletingLastPathComponent()) }
                try await PhotoLibrarySaver.saveLivePhoto(pictureURL: picture.fileURL, videoURL: video.fileURL)
                doneText = String(localized: "Saved the Live Photo to the Photos app")
            }
        } catch {
            problemText = describe(error)
        }
    }

    // MARK: Photos that are somebody else's, and dates

    /// Keeps photos of a shared album as photos of the person's own. The
    /// library is read again where it is held, since it has more photos now.
    func saveToLibrary(_ photos: [Photo]) async {
        let others = photos.filter { !$0.isOwnedByCaller }
        guard !others.isEmpty else { return }
        savingPhotoIds.formUnion(others.map(\.id))
        defer { savingPhotoIds.subtract(others.map(\.id)) }
        do {
            for photo in others { _ = try await connection.savePhotoToLibrary(photo.id) }
            doneText = others.count == 1 ? String(localized: "Saved to your library") : String(localized: "Saved \(others.count) photos to your library")
            await loadLibrary()
            for key in Array(listings.keys) where key.photoCollection == .library || key.photoCollection == .favorites { await reload(key) }
        } catch {
            problemText = describe(error)
        }
    }

    /// Places a photo at another date. The lists are read again, since the
    /// photo has moved in them; the one the viewer is paging through is read
    /// first so the viewer stays on the photo.
    func changeDate(of photo: Photo, to date: Date, in key: PhotoListKey) async -> Bool {
        do {
            let text = PhotoDateChange.takenAtText(for: date, photo: photo)
            let changed = try await connection.changePhotoDate(photo.id, takenAtText: text)
            for listKey in Array(listings.keys) { listings[listKey]?.replace(changed) }
            await reload(key)
            for listKey in Array(listings.keys) where listKey != key { forgetListing(listKey) }
            if albums != nil { await reloadAlbums() }
            doneText = String(localized: "Changed the date")
            return true
        } catch {
            problemText = describe(error)
            return false
        }
    }

    /// Reads the circles the person is in, once, for the albums that can be
    /// made for one. A failure leaves the choice out.
    func loadCircles() async {
        guard circles.isEmpty, let found = try? await connection.circles() else { return }
        circles = found
    }

    // MARK: Albums

    /// Reads the albums, unless they have been read.
    func loadAlbums() async {
        guard albums == nil else { return }
        await reloadAlbums()
    }

    /// Reads the albums again.
    func reloadAlbums() async {
        do {
            albums = try await connection.photoAlbums()
            albumsFailureText = nil
        } catch {
            guard !Task.isCancelled else { return }
            if albums == nil { albumsFailureText = describe(error) }
            problemText = describe(error)
        }
    }

    /// Reads the suggestions again. They are a help, not the page: a failure
    /// to find them leaves the albums as they are.
    func reloadSuggestions() async {
        guard let found = try? await connection.photoAlbumSuggestions() else { return }
        suggestions = found
        hasReadSuggestions = true
        await loadSuggestionSamples()
    }

    /// What the Albums section needs on arriving: the albums and the
    /// suggestions, each read once.
    func loadAlbumsSection() async {
        await loadAlbums()
        if !hasReadSuggestions { await reloadSuggestions() }
    }

    /// Reads the albums and the suggestions anew, on a pull to refresh.
    func refreshAlbumsSection() async {
        await reloadAlbums()
        await reloadSuggestions()
    }

    /// After photos move in or out of albums: the albums, and the suggestions
    /// if they have been read, since photos in an album are no longer
    /// suggested.
    private func albumsChanged() async {
        await reloadAlbums()
        if hasReadSuggestions { await reloadSuggestions() }
    }

    /// The small pictures a suggestion is shown by, read once each.
    private func loadSuggestionSamples() async {
        let photoIds = suggestions.flatMap { Array($0.samplePhotoIds.prefix(PhotoAlbumSuggestion.shownSampleCount)) }.filter { suggestionSamples[$0] == nil }
        let connection = connection
        await withTaskGroup(of: Photo?.self) { group in
            for photoId in photoIds {
                group.addTask { try? await connection.photoDetail(photoId).photo }
            }
            for await photo in group {
                if let photo { suggestionSamples[photo.id] = photo }
            }
        }
    }

    /// Makes an album holding the photos, which may be none, from a title the
    /// person gave.
    func createAlbum(title: String, photos: [Photo], circle: PeopleCircle? = nil) async {
        do {
            let created = try await connection.createPhotoAlbum(title: title, photoIds: photos.map(\.id), circleId: circle?.id)
            doneText = String(localized: "Created “\(created.albumTitle)”")
            await albumsChanged()
        } catch {
            problemText = describe(error)
        }
    }

    /// Makes an album of the photos a suggestion covers.
    func createAlbum(title: String, from suggestion: PhotoAlbumSuggestion) async {
        do {
            let photoIds = try await connection.photoIds(of: suggestion)
            let created = try await connection.createPhotoAlbum(title: title, photoIds: photoIds)
            doneText = String(localized: "Created “\(created.albumTitle)”")
            await albumsChanged()
        } catch {
            problemText = describe(error)
        }
    }

    func add(_ photos: [Photo], to album: PhotoAlbum) async {
        do {
            let addedCount = try await connection.addPhotos(photos.map(\.id), toAlbum: album.id)
            listings[.album(album.id)] = nil
            doneText = addedCount == 0 ? String(localized: "Already in “\(album.albumTitle)”") : String(localized: "Added to “\(album.albumTitle)”")
            await albumsChanged()
        } catch {
            problemText = describe(error)
        }
    }

    /// Takes photos out of an album, which leaves them in the library; true
    /// when they came out, so that a viewer leaves a photo only when it is gone.
    @discardableResult
    func remove(_ photos: [Photo], fromAlbum albumId: String) async -> Bool {
        do {
            try await connection.removePhotos(photos.map(\.id), fromAlbum: albumId)
            listings[.album(albumId)]?.remove(Set(photos.map(\.id)))
            doneText = String(localized: "Removed from the album")
            await albumsChanged()
            return true
        } catch {
            problemText = describe(error)
            return false
        }
    }

    func rename(_ album: PhotoAlbum, to title: String) async {
        do {
            let renamed = try await connection.renamePhotoAlbum(album.id, title: title)
            albums = albums?.map { $0.id == renamed.id ? renamed : $0 }
            await reloadAlbums()
        } catch {
            problemText = describe(error)
        }
    }

    /// Deletes an album; true when it is gone. Its photos stay in the library.
    func delete(_ album: PhotoAlbum) async -> Bool {
        do {
            try await connection.deletePhotoAlbum(album.id)
            albums?.removeAll { $0.id == album.id }
            listings[.album(album.id)] = nil
            doneText = String(localized: "Deleted the album")
            return true
        } catch {
            problemText = describe(error)
            return false
        }
    }

    func setCover(of album: PhotoAlbum, to photo: Photo) async {
        do {
            let changed = try await connection.setPhotoAlbumCover(album.id, photoId: photo.id)
            albums = albums?.map { $0.id == changed.id ? changed : $0 }
            doneText = String(localized: "Album cover changed")
        } catch {
            problemText = describe(error)
        }
    }

    /// Asks for an album's title on the screen that raised the request.
    func requestAlbumTitle(_ purpose: AlbumTitleRequest.Purpose, scope: AlbumTitleRequest.Scope) {
        albumTitleRequest = AlbumTitleRequest(purpose: purpose, scope: scope)
    }

    /// What the title the person gave is used for.
    func submit(_ request: AlbumTitleRequest, title: String) async {
        switch request.purpose {
        case .new: await createAlbum(title: title, photos: [])
        case .newForCircle(let circle): await createAlbum(title: title, photos: [], circle: circle)
        case .newWith(let photos): await createAlbum(title: title, photos: photos)
        case .suggestion(let suggestion): await createAlbum(title: title, from: suggestion)
        case .rename(let album): await rename(album, to: title)
        }
    }
}

/// OpenedPhoto is the photo the viewer was opened on.
struct OpenedPhoto: Identifiable {
    let id: String
}

/// PhotosScreen is the person's photo library: a grid by day with Library,
/// Favorites, Albums and Trash under the title, a Select mode, and a viewer
/// for one photo.
struct PhotosScreen: View {
    @State private var model: PhotosModel
    @State private var opened: OpenedPhoto?
    @State private var isConfirmingEmptyTrash = false
    @State private var isSelecting = false
    @State private var selectedPhotoIds: Set<String> = []
    @State private var isConfirmingTrashOfSelection = false
    @State private var isConfirmingDeleteOfSelection = false
    /// What is typed in the library's search field.
    @State private var searchText = ""
    @State private var isChoosingDates = false
    #if DEBUG
        @State private var isDemoViewerOpened = false
        @State private var isDemoAlbumOpened = false
        @State private var isDemoPersonOpened = false
    #endif

    init(connection: Connection) {
        let model = PhotosModel(connection: connection)
        #if DEBUG
            // `-TeaNodeScreen photos-trash` opens on the Trash, and
            // `photos-albums` on the Albums.
            if DemoServer.isRequested, DemoServer.screen == "photos-trash" || DemoServer.screen == "photos-trash-select" { model.section = .trash }
            if DemoServer.isRequested, DemoServer.screen == "photos-albums" || DemoServer.screen == "photos-album" { model.section = .albums }
            if DemoServer.isRequested, DemoServer.screen == "photos-people" || DemoServer.screen == "photos-person" { model.section = .people }
            if DemoServer.isRequested, ["photos-shared-albums", "photos-share", "photos-shared-album", "photos-shared-viewer"].contains(DemoServer.screen ?? "") {
                model.section = .albums
            }
            // `photos-filter` is the library narrowed to videos, and `photos-filter-dates` has the days to choose over it.
            if DemoServer.isRequested, DemoServer.screen == "photos-filter" || DemoServer.screen == "photos-filter-dates" {
                model.photoFilter = PhotoFilter(photoKindFilter: .video)
            }
        #endif
        _model = State(initialValue: model)
        #if DEBUG
            // `-TeaNodeScreen photos-select` starts in Select mode with two photos chosen.
            if DemoServer.isRequested, DemoServer.screen == "photos-select" {
                _isSelecting = State(initialValue: true)
                _selectedPhotoIds = State(initialValue: ["demo-02", "demo-05"])
            }
            // `photos-trash-select` is the same in the Trash, with its own actions,
            // and `photos-search` has the words typed in the library's search.
            if DemoServer.isRequested, DemoServer.screen == "photos-trash-select" {
                _isSelecting = State(initialValue: true)
                _selectedPhotoIds = State(initialValue: ["demo-t1"])
            }
            if DemoServer.isRequested, DemoServer.screen == "photos-search" { _searchText = State(initialValue: "harbor") }
            if DemoServer.isRequested, DemoServer.screen == "photos-filter-dates" { _isChoosingDates = State(initialValue: true) }
        #endif
    }

    /// The words searched for: only the library is searched.
    private var searchWords: String? { model.section == .library ? PhotoSearch.words(from: searchText) : nil }

    /// The list of photos on show; nil under Albums and People.
    private var key: PhotoListKey? {
        if let searchWords { return .search(searchWords) }
        guard let collection = model.section.collection else { return nil }
        return model.photoFilter.isActive ? .filtered(collection, model.photoFilter) : .collection(collection)
    }
    private var photos: [Photo] { key.map { model.photos(in: $0) } ?? [] }
    private var selectedPhotos: [Photo] { photos.filter { selectedPhotoIds.contains($0.id) } }
    private var canSelect: Bool { (model.section == .library || model.section == .favorites || model.section == .trash) && !photos.isEmpty }

    var body: some View {
        NavigationStack {
            content
                .modifier(LibrarySearch(isActive: model.section == .library, text: $searchText))
                .navigationTitle(isSelecting ? selectionTitle : String(localized: "Photos"))
                .safeAreaInset(edge: .top, spacing: 0) {
                    Picker("Show", selection: $model.section) {
                        ForEach(model.sections) { section in
                            Text(section.title).tag(section)
                        }
                    }
                    .pickerStyle(.segmented)
                    .labelsHidden()
                    .padding(.horizontal)
                    .padding(.bottom, 8)
                }
                .toolbar { toolbarContent }
                .withoutSwipeBack()
                .refreshable {
                    if let key {
                        await model.reload(key)
                    } else if model.section == .people {
                        await model.reloadFaceGroups()
                    } else {
                        await model.refreshAlbumsSection()
                    }
                }
                // The words are asked of the server a moment after the typing
                // stops; an answer to words no longer typed is forgotten.
                .task(id: searchWords) {
                    guard let searchWords else {
                        model.forgetListings { $0.isSearch }
                        return
                    }
                    try? await Task.sleep(for: .milliseconds(350))
                    guard !Task.isCancelled else { return }
                    model.forgetListings { $0.isSearch && $0 != .search(searchWords) }
                    await model.load(.search(searchWords))
                }
                .task(id: model.section) {
                    if let key {
                        await model.load(key)
                    } else if model.section == .people {
                        await model.loadFaceGroups()
                    } else {
                        await model.loadAlbumsSection()
                    }
                }
                .task { await model.loadLibrary() }
                // The albums the Add to Album menus list, and the circles an album can be for.
                .task { await model.loadAlbums() }
                .task { await model.loadCircles() }
                .task(id: key.map { model.hasWaitingPhotos(in: $0) }) {
                    if let key, model.hasWaitingPhotos(in: key) { await model.followWaitingPhotos(in: key) }
                }
                .onChange(of: model.section) { _, _ in endSelection() }
                // Typing in the search field changes the list under the chosen photos.
                .onChange(of: searchWords) { _, _ in endSelection() }
                .onChange(of: model.photoFilter) { _, _ in endSelection() }
                .sheet(isPresented: $isChoosingDates) { PhotoDateFilterSheet(model: model) }
                .albumTitleAlert(model: model, scope: .library, isActive: opened == nil)
                #if DEBUG
                    .navigationDestination(isPresented: $isDemoAlbumOpened) {
                        if let demoAlbum { PhotoAlbumScreen(model: model, albumId: demoAlbum.id) }
                    }
                    .navigationDestination(isPresented: $isDemoPersonOpened) {
                        if let first = model.faceGroups?.first { PhotoPersonScreen(model: model, photoFaceGroupId: first.id) }
                    }
                #endif
        }
        .fullScreenCover(item: $opened) { opened in
            PhotoViewer(model: model, listKey: key ?? .library, openedPhotoId: opened.id)
        }
        .toast(done: $model.doneText, problem: $model.problemText)
        #if DEBUG
            // `-TeaNodeScreen photos-album` opens the first album, `photos-share` the same with its
            // share sheet over it, and `photos-shared-album` and `photos-shared-viewer` the first
            // album another person shared (the viewer over its first photo).
            .task(id: model.albums?.map(\.id)) {
                if DemoServer.isRequested, ["photos-album", "photos-share", "photos-shared-album", "photos-shared-viewer"].contains(DemoServer.screen ?? ""), demoAlbum != nil {
                    isDemoAlbumOpened = true
                }
            }
            // `-TeaNodeScreen photos-person` opens the first person.
            .task(id: model.faceGroups?.first?.id) {
                if DemoServer.isRequested, DemoServer.screen == "photos-person", model.faceGroups?.first != nil { isDemoPersonOpened = true }
            }
            // `-TeaNodeScreen photos-viewer` opens the viewer on the first photo,
            // `photos-viewer-described` opens it with Info over it, and
            // `photos-viewer-failed` opens Info over a photo that could not be made ready.
            .task(id: model.photos(in: .library).first?.id) {
                let screen = DemoServer.screen
                guard DemoServer.isRequested, ["photos-viewer", "photos-viewer-described", "photos-viewer-failed", "photos-change-date"].contains(screen ?? ""), !isDemoViewerOpened else { return }
                let photoId = screen == "photos-viewer-failed" ? "demo-08" : model.photos(in: .library).first?.id
                if let photoId {
                    isDemoViewerOpened = true
                    opened = OpenedPhoto(id: photoId)
                }
            }
        #endif
    }

    #if DEBUG
        /// The album the demo screens open on: another person's for the shared ones.
        private var demoAlbum: PhotoAlbum? {
            if ["photos-shared-album", "photos-shared-viewer"].contains(DemoServer.screen ?? "") { return model.albums?.first { !$0.isOwnedByCaller } }
            return model.albums?.first
        }
    #endif

    private var selectionTitle: String {
        selectedPhotoIds.isEmpty ? String(localized: "Select photos") : String(localized: "\(selectedPhotoIds.count) Selected")
    }

    private func endSelection() {
        isSelecting = false
        selectedPhotoIds = []
    }

    @ToolbarContentBuilder
    private var toolbarContent: some ToolbarContent {
        MenuButton()
        if model.section == .albums {
            ToolbarItem(placement: .primaryAction) {
                NewAlbumMenu(model: model)
            }
        } else if model.section == .people {
            ToolbarItem(placement: .primaryAction) {
                Menu {
                    Toggle("Show hidden", systemImage: "eye.slash", isOn: Bindable(model).isHiddenPeopleShown)
                } label: {
                    Label("More", systemImage: "ellipsis")
                }
            }
        } else if isSelecting {
            ToolbarItem(placement: .primaryAction) {
                Button("Done") { endSelection() }
            }
            if model.section == .trash {
                PhotoTrashSelectionToolbar(model: model, selectedPhotos: selectedPhotos, isConfirmingDelete: $isConfirmingDeleteOfSelection, onEnd: endSelection)
            } else if let key {
                PhotoSelectionToolbar(
                    model: model, selectedPhotos: selectedPhotos, listKey: key, scope: .library, isConfirmingTrash: $isConfirmingTrashOfSelection,
                    onEnd: endSelection)
            }
        } else {
            if searchWords == nil, model.section == .library || model.section == .favorites || model.section == .trash {
                ToolbarItem(placement: .primaryAction) {
                    PhotoFilterMenu(model: model, isChoosingDates: $isChoosingDates)
                }
            }
            if model.section != .trash {
                ToolbarItem(placement: .primaryAction) {
                    NavigationLink {
                        PhotoBackupView(connection: model.connection)
                    } label: {
                        Label("Photo backup", systemImage: "icloud.and.arrow.up")
                    }
                }
            }
            if canSelect {
                ToolbarItem(placement: .primaryAction) {
                    Button("Select") { isSelecting = true }
                }
            }
            if model.section == .trash, !photos.isEmpty {
                ToolbarItem(placement: .primaryAction) {
                    Menu {
                        Button("Empty trash", systemImage: "trash", role: .destructive) { isConfirmingEmptyTrash = true }
                    } label: {
                        Label("More", systemImage: "ellipsis")
                    }
                    .confirmationDialog("Empty trash?", isPresented: $isConfirmingEmptyTrash, titleVisibility: .visible) {
                        Button("Delete forever", role: .destructive) { Task { await model.emptyTrash() } }
                    } message: {
                        Text("These photos will be deleted forever. This cannot be undone.")
                    }
                }
            }
        }
    }

    @ViewBuilder
    private var content: some View {
        if let key {
            if let listing = model.listing(key) {
                if listing.photos.isEmpty {
                    ScrollView {
                        if let searchWords {
                            ContentUnavailableView.search(text: searchWords)
                                .containerRelativeFrame(.vertical)
                        } else if model.photoFilter.isActive {
                            ContentUnavailableView {
                                Label("No photos match your filters", systemImage: "line.3.horizontal.decrease.circle")
                            } actions: {
                                Button("Clear filters") { model.photoFilter = PhotoFilter() }
                                    .buttonStyle(.borderedProminent)
                            }
                            .containerRelativeFrame(.vertical)
                        } else {
                            PhotosEmptyView(
                                collection: model.section.collection ?? .library, trashRetentionDays: model.library?.trashRetentionDays,
                                backupConnection: model.connection
                            )
                            .containerRelativeFrame(.vertical)
                        }
                    }
                } else {
                    ScrollView {
                        if model.section == .trash, let retentionDays = model.library?.trashRetentionDays, retentionDays > 0 {
                            Text("Photos in the Trash are deleted forever after \(retentionDays) days.")
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .padding(.horizontal)
                                .padding(.bottom, 4)
                        }
                        PhotoGrid(
                            listing: listing, listKey: key, model: model, scope: .library, isSelecting: isSelecting, selectedPhotoIds: $selectedPhotoIds
                        ) { photo in
                            opened = OpenedPhoto(id: photo.id)
                        }
                        // What the library holds, under the last photo, as the dashboard counts it.
                        if model.section == .library, searchWords == nil, !model.photoFilter.isActive, let library = model.library {
                            PhotoLibrarySummary(library: library)
                        }
                    }
                }
            } else if let failureText = model.failureText(key) {
                ScrollView {
                    PhotosFailedView(failureText: failureText) {
                        Task { await model.reload(key) }
                    }
                    .containerRelativeFrame(.vertical)
                }
            } else {
                ProgressView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        } else if model.section == .people {
            PhotoPeopleView(model: model)
        } else {
            PhotoAlbumsView(model: model)
        }
    }
}

/// LibrarySearch puts the search field on the library: the same words as the
/// dashboard's field, over the same search. It is applied only where it
/// searches (not under Favorites, Albums, People or Trash, and not while
/// choosing photos).
private struct LibrarySearch: ViewModifier {
    let isActive: Bool
    @Binding var text: String

    func body(content: Content) -> some View {
        if isActive {
            content.searchable(text: $text, placement: .navigationBarDrawer(displayMode: .always), prompt: Text("Search places, files, cameras, descriptions"))
        } else {
            content
        }
    }
}

/// PhotoTrashSelectionToolbar is the bar under the Trash in Select mode: what
/// can be done to the photos chosen, which is to bring them back or to delete
/// them forever (after asking). The chosen stay chosen when the server refuses.
struct PhotoTrashSelectionToolbar: ToolbarContent {
    let model: PhotosModel
    let selectedPhotos: [Photo]
    @Binding var isConfirmingDelete: Bool
    let onEnd: () -> Void

    private var isEmpty: Bool { selectedPhotos.isEmpty }

    var body: some ToolbarContent {
        ToolbarItem(placement: .bottomBar) {
            Button("Restore", systemImage: "arrow.uturn.backward") {
                let chosen = selectedPhotos
                Task { if await model.restore(chosen) { onEnd() } }
            }
            .disabled(isEmpty)
        }
        ToolbarSpacer(.flexible, placement: .bottomBar)
        ToolbarItem(placement: .bottomBar) {
            Button("Delete forever", systemImage: "trash", role: .destructive) { isConfirmingDelete = true }
                .disabled(isEmpty)
                .confirmationDialog("Delete these photos forever?", isPresented: $isConfirmingDelete, titleVisibility: .visible) {
                    Button("Delete forever", role: .destructive) {
                        let chosen = selectedPhotos
                        Task { if await model.deleteForever(chosen) { onEnd() } }
                    }
                } message: {
                    Text("This cannot be undone.")
                }
        }
    }
}

/// PhotoGrid is the photos of a list in days, as squares. It is the library's
/// grid and an album's: both open a photo with a tap, and in Select mode a tap
/// chooses it instead.
struct PhotoGrid: View {
    let listing: PhotoListing
    let listKey: PhotoListKey
    let model: PhotosModel
    let scope: AlbumTitleRequest.Scope
    let isSelecting: Bool
    @Binding var selectedPhotoIds: Set<String>
    /// Whether a touch and hold on a photo opens its menu. A picker that only
    /// chooses leaves it out.
    var isMenuAvailable = true
    let onOpen: (Photo) -> Void

    var body: some View {
        LazyVGrid(columns: [GridItem(.adaptive(minimum: 110), spacing: 2)], spacing: 2) {
            ForEach(listing.days) { day in
                Section {
                    ForEach(day.photos) { photo in
                        PhotoCell(
                            photo: photo, model: model, listKey: listKey, scope: scope, isSelecting: isSelecting, isSelected: selectedPhotoIds.contains(photo.id),
                            isMenuAvailable: isMenuAvailable
                        ) {
                            if isSelecting {
                                if selectedPhotoIds.contains(photo.id) { selectedPhotoIds.remove(photo.id) } else { selectedPhotoIds.insert(photo.id) }
                            } else {
                                onOpen(photo)
                            }
                        }
                    }
                } header: {
                    // A search lists the best match first, in one group.
                    if !listing.isRanked { PhotoDayHeader(day: day) }
                }
            }
        }
        if listing.isLoadingPage {
            ProgressView()
                .padding()
        }
    }
}

/// PhotoDayHeader is the date above the photos of one day.
struct PhotoDayHeader: View {
    let day: PhotoDay

    var body: some View {
        Text(title)
            .font(.headline)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, 12)
            .padding(.top, 14)
            .padding(.bottom, 6)
            .accessibilityAddTraits(.isHeader)
    }

    private var title: String {
        guard let noon = day.noonUTC else { return String(localized: "Unknown date") }
        // The day is written in UTC, the zone it was made in, so that it names
        // the day the photos were taken on wherever the phone is.
        var style = Date.FormatStyle(timeZone: TimeZone(secondsFromGMT: 0) ?? .gmt).weekday(.abbreviated).month(.abbreviated).day()
        if day.year != Calendar.current.component(.year, from: Date()) { style = style.year() }
        return noon.formatted(style)
    }
}

/// PhotoCell is one photo in the grid: its small preview in a square, with its
/// length if it is a video, a heart if it is a favorite, and a mark while it is
/// being made ready or if that failed. In Select mode it has a circle that a
/// tap fills.
struct PhotoCell: View {
    let photo: Photo
    let model: PhotosModel
    let listKey: PhotoListKey
    let scope: AlbumTitleRequest.Scope
    let isSelecting: Bool
    let isSelected: Bool
    let isMenuAvailable: Bool
    let onTap: () -> Void
    @State private var picture: UIImage?
    @State private var isConfirmingTrash = false
    @State private var isConfirmingDelete = false

    init(
        photo: Photo, model: PhotosModel, listKey: PhotoListKey, scope: AlbumTitleRequest.Scope, isSelecting: Bool, isSelected: Bool, isMenuAvailable: Bool = true,
        onTap: @escaping () -> Void
    ) {
        self.photo = photo
        self.model = model
        self.listKey = listKey
        self.scope = scope
        self.isSelecting = isSelecting
        self.isSelected = isSelected
        self.isMenuAvailable = isMenuAvailable
        self.onTap = onTap
        _picture = State(initialValue: photo.gridPreviewPath.flatMap { PhotoImages.cachedPicture(path: $0, accountKey: model.connection.accountKey) })
    }

    var body: some View {
        Button(action: onTap) {
            // A square that the picture fills and is cut to; the color holds
            // the place until the picture is read.
            Color(.secondarySystemBackground)
                .aspectRatio(1, contentMode: .fit)
                .overlay { cellContent }
                .overlay(alignment: .bottomTrailing) {
                    if photo.isVideo { durationBadge }
                }
                .overlay(alignment: .bottomLeading) {
                    if photo.isFavorite { favoriteBadge }
                }
                .overlay {
                    if isSelected { Color.accentColor.opacity(0.25) }
                }
                .overlay(alignment: .topTrailing) {
                    if isSelecting { selectionMark }
                }
                .clipped()
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(accessibilityText)
        .accessibilityAddTraits(isSelected ? .isSelected : [])
        .contextMenu { if isMenuAvailable { menu } }
        .confirmationDialog("Move this photo to the Trash?", isPresented: $isConfirmingTrash, titleVisibility: .visible) {
            Button("Move to Trash", role: .destructive) { Task { await model.moveToTrash(photo) } }
        }
        .confirmationDialog("Delete this photo forever?", isPresented: $isConfirmingDelete, titleVisibility: .visible) {
            Button("Delete forever", role: .destructive) { Task { await model.deleteForever(photo) } }
        } message: {
            Text("This cannot be undone.")
        }
        .task(id: photo.gridPreviewPath) {
            guard let path = photo.gridPreviewPath else {
                picture = nil
                return
            }
            if let loaded = await PhotoImages.picture(path: path, connection: model.connection) { picture = loaded }
        }
        .onAppear {
            Task { await model.loadMoreIfNeeded(showing: photo.id, in: listKey) }
        }
    }

    @ViewBuilder
    private var cellContent: some View {
        if let picture {
            Image(uiImage: picture)
                .resizable()
                .scaledToFill()
        } else if photo.isFailed {
            Image(systemName: "exclamationmark.circle")
                .font(.title2)
                .foregroundStyle(.secondary)
                .accessibilityLabel("This photo could not be read")
        } else {
            ProgressView()
                .accessibilityLabel("Getting this photo ready")
        }
    }

    // The circle is white over a dark shadow, whatever the picture under it
    // is, and filled with the accent once chosen.
    private var selectionMark: some View {
        Image(systemName: isSelected ? "checkmark.circle.fill" : "circle")
            .symbolRenderingMode(isSelected ? .palette : .monochrome)
            .foregroundStyle(.white, Color.accentColor)
            .font(.title3)
            .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
            .shadow(color: .black.opacity(0.4), radius: 2)
            .padding(6)
            .accessibilityHidden(true)
    }

    // Light text on a dark pill, whatever the picture under it is.
    private var durationBadge: some View {
        Label {
            Text(PhotoTimeline.durationText(milliseconds: photo.durationMS))
        } icon: {
            Image(systemName: "play.fill")
        }
        .font(.caption2.weight(.semibold))
        .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
        .foregroundStyle(.white)
        .padding(.horizontal, 6)
        .padding(.vertical, 3)
        .background(.black.opacity(0.5), in: .capsule)
        .padding(4)
    }

    private var favoriteBadge: some View {
        Image(systemName: "heart.fill")
            .font(.caption2)
            .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
            .foregroundStyle(.white)
            .padding(5)
            .background(.black.opacity(0.5), in: .circle)
            .padding(4)
    }

    private var accessibilityText: String {
        var parts = [PhotoWords.kind(of: photo)]
        if photo.isVideo { parts.append(PhotoTimeline.durationText(milliseconds: photo.durationMS)) }
        if let taken = PhotoTimeline.takenText(of: photo) { parts.append(taken) }
        if photo.isFavorite { parts.append(String(localized: "Favorite")) }
        if !photo.isOwnedByCaller, !photo.ownerName.isEmpty { parts.append(String(localized: "From \(photo.ownerName)")) }
        if photo.isWaiting { parts.append(String(localized: "Getting this photo ready")) }
        if photo.isFailed { parts.append(String(localized: "This photo could not be read")) }
        return parts.joined(separator: ", ")
    }

    @ViewBuilder
    private var menu: some View {
        if photo.isTrashed {
            Button("Restore", systemImage: "arrow.uturn.backward") { Task { await model.restore(photo) } }
            Button("Delete forever", systemImage: "trash", role: .destructive) { isConfirmingDelete = true }
        } else if !photo.isOwnedByCaller {
            // Somebody else's photo, seen through an album: keep it, save it to
            // the phone, or share it; the rest is its owner's to do.
            Button("Save to my library", systemImage: "photo.badge.plus") { Task { await model.saveToLibrary([photo]) } }
            Button("Save to the Photos app", systemImage: "square.and.arrow.down") { Task { await model.saveToPhotos(photo) } }
            PhotoShareLink(photo: photo, connection: model.connection)
        } else {
            if photo.isFailed {
                Button("Try again", systemImage: "arrow.clockwise") { Task { await model.retryProcessing([photo]) } }
            }
            Button(
                photo.isFavorite ? LocalizedStringKey("Remove from Favorites") : LocalizedStringKey("Add to Favorites"),
                systemImage: photo.isFavorite ? "heart.slash" : "heart"
            ) {
                Task { await model.toggleFavorite(photo, in: listKey) }
            }
            AddToAlbumMenu(model: model, photos: [photo], scope: scope, hiddenAlbumId: listKey.albumId)
            if let albumId = listKey.albumId {
                Button("Remove from album", systemImage: "rectangle.stack.badge.minus") {
                    Task { await model.remove([photo], fromAlbum: albumId) }
                }
            }
            Button("Save to the Photos app", systemImage: "square.and.arrow.down") { Task { await model.saveToPhotos(photo) } }
            PhotoShareLink(photo: photo, connection: model.connection)
            Button("Move to Trash", systemImage: "trash", role: .destructive) { isConfirmingTrash = true }
        }
    }
}

/// PhotosEmptyView is what a list says while it holds no photos.
struct PhotosEmptyView: View {
    let collection: PhotoCollection
    var trashRetentionDays: Int?
    /// Where the empty library offers to back this iPhone up; nil leaves the offer out.
    var backupConnection: Connection?

    var body: some View {
        switch collection {
        case .library:
            ContentUnavailableView {
                Label("No photos yet", systemImage: "photo.on.rectangle")
            } description: {
                Text("Turn on photo backup to copy this iPhone's photos here, or add photos on the dashboard.")
            } actions: {
                if let backupConnection {
                    NavigationLink("Turn on photo backup") {
                        PhotoBackupView(connection: backupConnection)
                    }
                    .buttonStyle(.borderedProminent)
                }
            }
        case .favorites:
            ContentUnavailableView {
                Label("No favorites", systemImage: "heart")
            } description: {
                Text("Photos you mark with a heart are kept here.")
            }
        case .trash:
            ContentUnavailableView {
                Label("The Trash is empty", systemImage: "trash")
            } description: {
                if let trashRetentionDays, trashRetentionDays > 0 {
                    Text("Photos you move to the Trash stay here for \(trashRetentionDays) days.")
                } else {
                    Text("Photos you move to the Trash stay here for a while before they are deleted forever.")
                }
            }
        }
    }
}

/// PhotosFailedView is what the library says when its first page could not be
/// read.
struct PhotosFailedView: View {
    let failureText: String
    let onRetry: () -> Void

    var body: some View {
        ContentUnavailableView {
            Label("Cannot load photos", systemImage: "wifi.exclamationmark")
        } description: {
            Text(failureText)
        } actions: {
            Button("Try again", action: onRetry)
                .buttonStyle(.borderedProminent)
        }
    }
}

/// PhotoImages reads the pictures of photos: through the disk cache, which
/// fetches with the app's sign-in, and then decoded once and kept in memory.
@MainActor
enum PhotoImages {
    /// The previews on disk, for every account of the app.
    static let cache = PhotoPreviewCache()

    private static let decoded: NSCache<NSString, UIImage> = {
        let cache = NSCache<NSString, UIImage>()
        cache.totalCostLimit = 150 * 1024 * 1024
        return cache
    }()

    private static func key(path: String, accountKey: String) -> NSString {
        "\(accountKey)\n\(path)" as NSString
    }

    /// The picture if it has been read already in this launch.
    static func cachedPicture(path: String, accountKey: String) -> UIImage? {
        decoded.object(forKey: key(path: path, accountKey: accountKey))
    }

    /// Forgets every preview of an account that was signed out of or removed:
    /// the files on disk and the pictures held in memory. The pictures in
    /// memory are not told apart by account, so all of them are let go; the
    /// other accounts' pictures are read again from their files.
    static func forget(accountKey: String) async {
        decoded.removeAllObjects()
        await cache.removeAccount(accountKey: accountKey)
    }

    /// The picture at an address on the server; nil if it cannot be read.
    static func picture(path: String, connection: Connection) async -> UIImage? {
        let key = key(path: path, accountKey: connection.accountKey)
        if let known = decoded.object(forKey: key) { return known }
        guard let data = try? await cache.data(for: path, using: connection), let picture = UIImage(data: data) else { return nil }
        let cost = Int(picture.size.width * picture.scale * picture.size.height * picture.scale * 4)
        decoded.setObject(picture, forKey: key, cost: cost)
        return picture
    }
}

#if DEBUG
    #Preview("Library empty, light") {
        PhotosEmptyView(collection: .library).preferredColorScheme(.light)
    }
    #Preview("Library empty, dark") {
        PhotosEmptyView(collection: .library).preferredColorScheme(.dark)
    }
    #Preview("Favorites empty, light") {
        PhotosEmptyView(collection: .favorites).preferredColorScheme(.light)
    }
    #Preview("Trash empty, dark") {
        PhotosEmptyView(collection: .trash, trashRetentionDays: 30).preferredColorScheme(.dark)
    }
    #Preview("Failed, light") {
        PhotosFailedView(failureText: "The server could not be reached.") {}.preferredColorScheme(.light)
    }
#endif
