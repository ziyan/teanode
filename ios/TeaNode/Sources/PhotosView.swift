import SwiftUI
import TeaNodeKit
import UIKit

extension PhotoCollection {
    /// The name of the list in the segmented control.
    var title: String {
        switch self {
        case .library: String(localized: "Library")
        case .favorites: String(localized: "Favorites")
        case .trash: String(localized: "Trash")
        }
    }
}

/// PhotoListing is one list of the library as far as it has been read, with
/// its photos grouped in days.
struct PhotoListing {
    private(set) var photos: [Photo]
    private(set) var days: [PhotoDay]
    /// Whether another page may follow what has been read.
    private(set) var hasMore: Bool
    var isLoadingPage = false

    init(photos: [Photo], hasMore: Bool) {
        self.photos = photos
        self.days = PhotoTimeline.days(photos)
        self.hasMore = hasMore
    }

    mutating func append(_ page: [Photo]) {
        photos = PhotoPaging.appending(page, to: photos)
        days = PhotoTimeline.days(photos)
        hasMore = PhotoPaging.hasMore(afterPageOf: page.count)
    }

    /// Puts a photo as the server now has it in place of the one held.
    mutating func replace(_ photo: Photo) {
        guard let index = photos.firstIndex(where: { $0.id == photo.id }) else { return }
        photos[index] = photo
        days = PhotoTimeline.days(photos)
    }

    /// Puts the photos of a page just read in place of the ones held, for
    /// those that are held, and leaves the rest.
    mutating func replaceHeld(by page: [Photo]) {
        let newest = Dictionary(page.map { ($0.id, $0) }, uniquingKeysWith: { _, second in second })
        photos = photos.map { newest[$0.id] ?? $0 }
        days = PhotoTimeline.days(photos)
    }

    mutating func remove(_ photoIds: Set<String>) {
        photos.removeAll { photoIds.contains($0.id) }
        days = PhotoTimeline.days(photos)
    }
}

/// PhotosModel is the person's photo library as the phone shows it: the
/// three lists, read a page at a time, and what can be done to a photo.
@MainActor
@Observable
final class PhotosModel {
    let connection: Connection
    var collection = PhotoCollection.library
    private(set) var listings: [PhotoCollection: PhotoListing] = [:]
    private(set) var failureTexts: [PhotoCollection: String] = [:]
    private(set) var library: PhotoLibrary?
    private(set) var savingPhotoIds: Set<String> = []
    var doneText: String?
    var problemText: String?

    init(connection: Connection) {
        self.connection = connection
    }

    var listing: PhotoListing? { listings[collection] }
    var photos: [Photo] { listings[collection]?.photos ?? [] }
    var failureText: String? { failureTexts[collection] }
    var hasWaitingPhotos: Bool { photos.contains { $0.isWaiting } }

    func loadLibrary() async {
        library = try? await connection.photoLibrary()
    }

    /// Reads the first page of a list, unless it has been read.
    func load(_ collection: PhotoCollection) async {
        guard listings[collection] == nil else { return }
        await reload(collection)
    }

    /// Reads the first page of a list again, which drops any further pages read.
    func reload(_ collection: PhotoCollection) async {
        do {
            let page = try await connection.photos(in: collection)
            listings[collection] = PhotoListing(photos: page, hasMore: PhotoPaging.hasMore(afterPageOf: page.count))
            failureTexts[collection] = nil
        } catch {
            guard !Task.isCancelled else { return }
            if listings[collection] == nil { failureTexts[collection] = error.localizedDescription }
            problemText = error.localizedDescription
        }
    }

    /// Reads the next page when a cell near the end of what is loaded comes
    /// on screen.
    func loadMoreIfNeeded(showing photoId: String) async {
        let collection = collection
        guard let listing = listings[collection], listing.hasMore, !listing.isLoadingPage,
            PhotoPaging.shouldLoadMore(whenShowing: photoId, in: listing.photos), let last = listing.photos.last
        else { return }
        listings[collection]?.isLoadingPage = true
        defer { listings[collection]?.isLoadingPage = false }
        do {
            let page = try await connection.photos(in: collection, beforePhotoId: last.id)
            // The list may have been read again meanwhile; this page then
            // follows something that is no longer its end.
            guard listings[collection]?.photos.last?.id == last.id else { return }
            listings[collection]?.append(page)
        } catch {
            if !Task.isCancelled { problemText = error.localizedDescription }
        }
    }

    /// While a photo of the list is still being made ready, asks for the list
    /// again now and then, a little less often each time, so its picture
    /// appears without the person pulling to refresh.
    func followWaitingPhotos() async {
        var attempt = 1
        while !Task.isCancelled, hasWaitingPhotos {
            try? await Task.sleep(for: .seconds(min(30, 5 * attempt)))
            attempt += 1
            let collection = collection
            guard !Task.isCancelled, let held = listings[collection] else { return }
            if let page = try? await connection.photos(in: collection, pageSize: min(max(held.photos.count, 1), 500)) {
                listings[collection]?.replaceHeld(by: page)
            }
        }
    }

    // MARK: Changing photos

    func toggleFavorite(_ photo: Photo) async {
        do {
            let updated = try await connection.setPhotoFavorite(photo.id, isFavorite: !photo.isFavorite)
            for key in Array(listings.keys) { listings[key]?.replace(updated) }
            // Which photos are favorites changed. The Favorites list being
            // looked at keeps a photo just un-favorited until it is left (see
            // dropPhotosNoLongerFavorite); any other copy of it is read anew.
            if collection != .favorites { listings[.favorites] = nil }
        } catch {
            problemText = error.localizedDescription
        }
    }

    /// Takes the photos that are no longer favorites out of the Favorites
    /// list, once the person has left them.
    func dropPhotosNoLongerFavorite() {
        guard let favorites = listings[.favorites] else { return }
        let dropped = Set(favorites.photos.filter { !$0.isFavorite }.map(\.id))
        if !dropped.isEmpty { listings[.favorites]?.remove(dropped) }
    }

    func moveToTrash(_ photo: Photo) async {
        do {
            try await connection.trashPhotos([photo.id])
            listings[.library]?.remove([photo.id])
            listings[.favorites]?.remove([photo.id])
            listings[.trash] = nil
            library = nil
            doneText = String(localized: "Moved to Trash")
            await loadLibrary()
        } catch {
            problemText = error.localizedDescription
        }
    }

    func restore(_ photo: Photo) async {
        do {
            try await connection.restorePhotos([photo.id])
            listings[.trash]?.remove([photo.id])
            listings[.library] = nil
            listings[.favorites] = nil
            doneText = String(localized: "Restored to your library")
            await loadLibrary()
        } catch {
            problemText = error.localizedDescription
        }
    }

    func deleteForever(_ photo: Photo) async {
        do {
            try await connection.deletePhotos([photo.id])
            listings[.trash]?.remove([photo.id])
            doneText = String(localized: "Deleted for good")
            await loadLibrary()
        } catch {
            problemText = error.localizedDescription
        }
    }

    func emptyTrash() async {
        do {
            try await connection.emptyPhotoTrash()
            listings[.trash] = PhotoListing(photos: [], hasMore: false)
            doneText = String(localized: "The Trash is empty")
            await loadLibrary()
        } catch {
            problemText = error.localizedDescription
        }
    }

    /// Adds a photo to the phone's own Photos: the original file, or the large
    /// preview when the original cannot be read.
    func saveToPhotos(_ photo: Photo) async {
        guard !savingPhotoIds.contains(photo.id) else { return }
        savingPhotoIds.insert(photo.id)
        defer { savingPhotoIds.remove(photo.id) }
        do {
            let download = try await connection.downloadPhotoFile(photo)
            let fileURL = try PhotoTemporaryFile.write(download)
            defer { try? FileManager.default.removeItem(at: fileURL.deletingLastPathComponent()) }
            try await PhotoLibrarySaver.save(fileURL: fileURL, isVideo: photo.isVideo)
            doneText = download.isOriginal ? String(localized: "Saved to Photos") : String(localized: "Saved a smaller copy to Photos")
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// OpenedPhoto is the photo the viewer was opened on.
struct OpenedPhoto: Identifiable {
    let id: String
}

/// PhotosScreen is the person's photo library: a grid by day with Library,
/// Favorites and Trash under the title, and a viewer for one photo.
struct PhotosScreen: View {
    @State private var model: PhotosModel
    @State private var opened: OpenedPhoto?
    @State private var isConfirmingEmptyTrash = false
    #if DEBUG
        @State private var isDemoViewerOpened = false
    #endif

    init(connection: Connection) {
        let model = PhotosModel(connection: connection)
        #if DEBUG
            // `-TeaNodeScreen photos-trash` opens on the Trash.
            if DemoServer.isRequested, DemoServer.screen == "photos-trash" { model.collection = .trash }
        #endif
        _model = State(initialValue: model)
    }

    var body: some View {
        NavigationStack {
            content
                .navigationTitle("Photos")
                .safeAreaInset(edge: .top, spacing: 0) {
                    Picker("Show", selection: $model.collection) {
                        ForEach(PhotoCollection.allCases) { collection in
                            Text(collection.title).tag(collection)
                        }
                    }
                    .pickerStyle(.segmented)
                    .labelsHidden()
                    .padding(.horizontal)
                    .padding(.bottom, 8)
                }
                .toolbar {
                    MenuButton()
                    if model.collection == .trash, !model.photos.isEmpty {
                        ToolbarItem(placement: .primaryAction) {
                            Menu {
                                Button("Empty Trash", systemImage: "trash", role: .destructive) { isConfirmingEmptyTrash = true }
                            } label: {
                                Label("More", systemImage: "ellipsis")
                            }
                            .confirmationDialog("Empty Trash?", isPresented: $isConfirmingEmptyTrash, titleVisibility: .visible) {
                                Button("Delete All Forever", role: .destructive) { Task { await model.emptyTrash() } }
                            } message: {
                                Text("These photos will be deleted for good. This cannot be undone.")
                            }
                        }
                    }
                }
                .withoutSwipeBack()
                .refreshable { await model.reload(model.collection) }
                .task(id: model.collection) { await model.load(model.collection) }
                .task { await model.loadLibrary() }
                .task(id: model.hasWaitingPhotos) {
                    if model.hasWaitingPhotos { await model.followWaitingPhotos() }
                }
        }
        .fullScreenCover(item: $opened) { opened in
            PhotoViewer(model: model, openedPhotoId: opened.id)
        }
        .toast(done: $model.doneText, problem: $model.problemText)
        #if DEBUG
            // `-TeaNodeScreen photos-viewer` opens the viewer on the first photo.
            .task(id: model.photos.first?.id) {
                if DemoServer.isRequested, DemoServer.screen == "photos-viewer", !isDemoViewerOpened, let first = model.photos.first {
                    isDemoViewerOpened = true
                    opened = OpenedPhoto(id: first.id)
                }
            }
        #endif
    }

    @ViewBuilder
    private var content: some View {
        if let listing = model.listing {
            if listing.photos.isEmpty {
                ScrollView {
                    PhotosEmptyView(collection: model.collection, trashRetentionDays: model.library?.trashRetentionDays)
                        .containerRelativeFrame(.vertical)
                }
            } else {
                grid(listing)
            }
        } else if let failureText = model.failureText {
            ScrollView {
                PhotosFailedView(failureText: failureText) {
                    Task { await model.reload(model.collection) }
                }
                .containerRelativeFrame(.vertical)
            }
        } else {
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    private func grid(_ listing: PhotoListing) -> some View {
        ScrollView {
            if model.collection == .trash, let retentionDays = model.library?.trashRetentionDays, retentionDays > 0 {
                Text("Photos in the Trash are deleted for good after \(retentionDays) days.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal)
                    .padding(.bottom, 4)
            }
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 110), spacing: 2)], spacing: 2) {
                ForEach(listing.days) { day in
                    Section {
                        ForEach(day.photos) { photo in
                            PhotoCell(photo: photo, model: model) { opened = OpenedPhoto(id: photo.id) }
                        }
                    } header: {
                        PhotoDayHeader(day: day)
                    }
                }
            }
            if listing.isLoadingPage {
                ProgressView()
                    .padding()
            }
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
/// being made ready or if that failed.
struct PhotoCell: View {
    let photo: Photo
    let model: PhotosModel
    let onOpen: () -> Void
    @State private var picture: UIImage?
    @State private var isConfirmingTrash = false
    @State private var isConfirmingDelete = false

    init(photo: Photo, model: PhotosModel, onOpen: @escaping () -> Void) {
        self.photo = photo
        self.model = model
        self.onOpen = onOpen
        _picture = State(initialValue: photo.gridPreviewPath.flatMap { PhotoImages.cachedPicture(path: $0, accountKey: model.connection.accountKey) })
    }

    var body: some View {
        Button(action: onOpen) {
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
                .clipped()
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(accessibilityText)
        .contextMenu { menu }
        .confirmationDialog("Move this photo to the Trash?", isPresented: $isConfirmingTrash, titleVisibility: .visible) {
            Button("Move to Trash", role: .destructive) { Task { await model.moveToTrash(photo) } }
        }
        .confirmationDialog("Delete this photo for good?", isPresented: $isConfirmingDelete, titleVisibility: .visible) {
            Button("Delete Forever", role: .destructive) { Task { await model.deleteForever(photo) } }
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
            Task { await model.loadMoreIfNeeded(showing: photo.id) }
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
                .accessibilityLabel("Could not be made ready")
        } else {
            ProgressView()
        }
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
        var parts = [photo.isVideo ? String(localized: "Video") : String(localized: "Photo")]
        if photo.isVideo { parts.append(PhotoTimeline.durationText(milliseconds: photo.durationMS)) }
        if let taken = PhotoTimeline.takenText(of: photo) { parts.append(taken) }
        if photo.isFavorite { parts.append(String(localized: "Favorite")) }
        return parts.joined(separator: ", ")
    }

    @ViewBuilder
    private var menu: some View {
        if photo.isTrashed {
            Button("Restore", systemImage: "arrow.uturn.backward") { Task { await model.restore(photo) } }
            Button("Delete Forever", systemImage: "trash", role: .destructive) { isConfirmingDelete = true }
        } else {
            Button(photo.isFavorite ? LocalizedStringKey("Remove from Favorites") : LocalizedStringKey("Favorite"), systemImage: photo.isFavorite ? "heart.slash" : "heart") {
                Task { await model.toggleFavorite(photo) }
            }
            Button("Save to Photos", systemImage: "square.and.arrow.down") { Task { await model.saveToPhotos(photo) } }
            PhotoShareLink(photo: photo, connection: model.connection)
            Button("Move to Trash", systemImage: "trash", role: .destructive) { isConfirmingTrash = true }
        }
    }
}

/// PhotosEmptyView is what a list says while it holds no photos.
struct PhotosEmptyView: View {
    let collection: PhotoCollection
    var trashRetentionDays: Int?

    var body: some View {
        switch collection {
        case .library:
            ContentUnavailableView {
                Label("No photos yet", systemImage: "photo.on.rectangle")
            } description: {
                Text("Back up this iPhone's photos in Settings, or add photos on the dashboard.")
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
                    Text("Photos you move to the Trash stay here for a while before they are deleted for good.")
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
