import Foundation

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// Photo is one item in the person's library: a picture, a video or a Live
/// Photo. The keys are the server's. The three preview addresses are paths on
/// the server, empty until that preview is made; they carry the time the photo
/// last changed, so a new preview is a new address.
public struct Photo: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let createdAt: String
    /// "picture", "video" or "livePhoto".
    public let photoKind: String
    /// When it was taken, if the file said.
    public let takenAt: String?
    /// How far the place it was taken was from UTC, in minutes, if known.
    public let takenAtOffsetMinutes: Int?
    public let widthPixels: Int
    public let heightPixels: Int
    public let durationMS: Int
    public let latitude: Double?
    public let longitude: Double?
    public let placeName: String
    public let cameraMake: String
    public let cameraModel: String
    public let originalFilename: String
    public let isFavorite: Bool
    public let isScreenshot: Bool
    /// "waiting", "ready" or "failed".
    public let photoProcessingState: String
    public let trashedAt: String?
    public let smallPreviewUrl: String
    public let largePreviewUrl: String
    public let videoPreviewUrl: String

    public var isVideo: Bool { photoKind == "video" }
    public var isLivePhoto: Bool { photoKind == "livePhoto" }
    public var isWaiting: Bool { photoProcessingState == "waiting" }
    public var isFailed: Bool { photoProcessingState == "failed" }
    public var isTrashed: Bool { trashedAt != nil }

    /// The picture a grid cell shows: the small preview, or the large one
    /// while only that exists. Nil until a preview is made.
    public var gridPreviewPath: String? {
        if !smallPreviewUrl.isEmpty { return smallPreviewUrl }
        return largePreviewUrl.isEmpty ? nil : largePreviewUrl
    }

    /// The picture the viewer shows: the large preview, or the small one
    /// until it is made.
    public var viewerPreviewPath: String? {
        if !largePreviewUrl.isEmpty { return largePreviewUrl }
        return smallPreviewUrl.isEmpty ? nil : smallPreviewUrl
    }

    /// The video the viewer plays, once it is made.
    public var videoPath: String? { videoPreviewUrl.isEmpty ? nil : videoPreviewUrl }

    /// The name to give the file when it is shared or saved.
    public var fileName: String {
        originalFilename.isEmpty ? (isVideo ? "video.mp4" : "photo.jpg") : originalFilename
    }
}

/// PhotoFile is one file a photo is made of: its original, a version edited on
/// a phone, or a Live Photo's video.
public struct PhotoFile: Codable, Hashable, Sendable {
    /// "original", "edited" or "liveVideo".
    public let photoFileRole: String
    public let contentType: String
    public let fileSizeBytes: Int
    public let originalFilename: String
    /// Where the file is read from on the server.
    public let fileUrl: String
}

/// PhotoPreview is one smaller copy the server made of a photo.
public struct PhotoPreview: Codable, Hashable, Sendable {
    /// "small", "large" or "video".
    public let photoPreviewSize: String
    public let contentType: String
    public let previewSizeBytes: Int
    public let previewUrl: String
}

/// PhotoAlbumReference is an album a photo is in, as the photo says it: enough
/// to name it and to open it.
public struct PhotoAlbumReference: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let albumTitle: String

    public init(id: String, albumTitle: String) {
        self.id = id
        self.albumTitle = albumTitle
    }
}

/// PhotoDetail is one photo with its files, previews and the albums it is in.
public struct PhotoDetail: Codable, Hashable, Sendable {
    public let photo: Photo
    public let photoFiles: [PhotoFile]
    public let photoPreviews: [PhotoPreview]
    public let albums: [PhotoAlbumReference]

    public init(photo: Photo, photoFiles: [PhotoFile], photoPreviews: [PhotoPreview], albums: [PhotoAlbumReference] = []) {
        self.photo = photo
        self.photoFiles = photoFiles
        self.photoPreviews = photoPreviews
        self.albums = albums
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        photo = try container.decode(Photo.self, forKey: .photo)
        photoFiles = try container.decode([PhotoFile].self, forKey: .photoFiles)
        photoPreviews = try container.decode([PhotoPreview].self, forKey: .photoPreviews)
        // A server that does not know albums yet says nothing of them.
        albums = try container.decodeIfPresent([PhotoAlbumReference].self, forKey: .albums) ?? []
    }

    /// The file Save to Photos and Share hand on: the original.
    public var originalFile: PhotoFile? { photoFiles.first { $0.photoFileRole == "original" } }
}

/// PhotoLibrary is what the library page shows beside the photos.
public struct PhotoLibrary: Codable, Hashable, Sendable {
    public let photoCount: Int
    public let videoCount: Int
    public let favoriteCount: Int
    public let trashedCount: Int
    public let usedBytes: Int
    public let trashRetentionDays: Int
}

/// PhotoAlbum is a named set of the person's photos. It holds references: a
/// photo can be in several albums, and deleting the album leaves the photos.
public struct PhotoAlbum: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let albumTitle: String
    public let createdAt: String
    public let updatedAt: String
    /// The photo the person chose to stand for the album; nil until they do,
    /// and the album then shows its newest photo.
    public let coverPhotoId: String?
    public let photoCount: Int
    /// The dates the album's photos are placed at; nil for an empty album.
    public let firstTakenAt: String?
    public let lastTakenAt: String?
    /// Where the cover picture is read from; empty until a photo of the album
    /// has a small preview.
    public let coverPreviewUrl: String

    public init(
        id: String, albumTitle: String, createdAt: String = "", updatedAt: String = "", coverPhotoId: String? = nil, photoCount: Int = 0,
        firstTakenAt: String? = nil, lastTakenAt: String? = nil, coverPreviewUrl: String = ""
    ) {
        self.id = id
        self.albumTitle = albumTitle
        self.createdAt = createdAt
        self.updatedAt = updatedAt
        self.coverPhotoId = coverPhotoId
        self.photoCount = photoCount
        self.firstTakenAt = firstTakenAt
        self.lastTakenAt = lastTakenAt
        self.coverPreviewUrl = coverPreviewUrl
    }

    /// The dates the album's photos span, as one date or a range, in the
    /// phone's time zone. Nil for an album with no photos.
    public func dateRangeText(timeZone: TimeZone = .current, locale: Locale = .autoupdatingCurrent) -> String? {
        PhotoAlbumText.dateRange(from: firstTakenAt, to: lastTakenAt, timeZone: timeZone, locale: locale)
    }
}

/// PhotoAlbumSuggestion is a run of photos that are in no album and belong
/// together: a trip or an event. The server makes nothing from it until the
/// person asks, and the person names the album.
public struct PhotoAlbumSuggestion: Codable, Identifiable, Hashable, Sendable {
    /// The run's first and last moments, as the server wrote them. They are
    /// handed back unchanged to list the run's photos.
    public let takenAfter: String
    public let takenBefore: String
    public let placeNames: [String]
    /// A proposal for the album's title, which the person may change.
    public let albumTitle: String
    public let photoCount: Int
    /// A few photos spread across the run, to show it by.
    public let samplePhotoIds: [String]

    public var id: String { "\(takenAfter)/\(takenBefore)" }

    /// How many of the sample photos a suggestion is shown by on a phone.
    public static let shownSampleCount = 4

    public init(takenAfter: String, takenBefore: String, placeNames: [String], albumTitle: String, photoCount: Int, samplePhotoIds: [String]) {
        self.takenAfter = takenAfter
        self.takenBefore = takenBefore
        self.placeNames = placeNames
        self.albumTitle = albumTitle
        self.photoCount = photoCount
        self.samplePhotoIds = samplePhotoIds
    }

    /// The dates of the run, as one date or a range.
    public func dateRangeText(timeZone: TimeZone = .current, locale: Locale = .autoupdatingCurrent) -> String? {
        PhotoAlbumText.dateRange(from: takenAfter, to: takenBefore, timeZone: timeZone, locale: locale)
    }
}

/// PhotoAlbumText writes what an album screen says in muted text.
public enum PhotoAlbumText {
    /// A date, or two dates joined as the reader's language joins them
    /// ("Jul 12 to 14, 2025"), for the moments the server wrote. Nil unless
    /// both are moments.
    public static func dateRange(from first: String?, to last: String?, timeZone: TimeZone = .current, locale: Locale = .autoupdatingCurrent) -> String? {
        guard let firstDate = ServerTime.date(first), let lastDate = ServerTime.date(last) else { return nil }
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = timeZone
        if calendar.isDate(firstDate, inSameDayAs: lastDate) {
            return firstDate.formatted(Date.FormatStyle(date: .abbreviated, time: .omitted, locale: locale, timeZone: timeZone))
        }
        let range = firstDate..<max(lastDate, firstDate)
        return range.formatted(Date.IntervalFormatStyle(date: .abbreviated, time: .omitted, locale: locale, timeZone: timeZone))
    }

    /// The album's title as it is saved: without the spaces round it.
    public static func trimmedTitle(_ title: String) -> String {
        title.trimmingCharacters(in: .whitespacesAndNewlines)
    }
}

/// PhotoBatches splits photo ids into the batches the server takes in one call.
public enum PhotoBatches {
    /// The most photo ids one call may name.
    public static let mostPhotoIdsPerCall = 500

    public static func batches(of photoIds: [String], size: Int = PhotoBatches.mostPhotoIdsPerCall) -> [[String]] {
        guard size > 0 else { return [] }
        return stride(from: 0, to: photoIds.count, by: size).map { Array(photoIds[$0..<min($0 + size, photoIds.count)]) }
    }
}

/// PhotoCollection is which of the library's lists is shown.
public enum PhotoCollection: String, CaseIterable, Identifiable, Hashable, Sendable {
    case library, favorites, trash

    public var id: String { rawValue }

    /// The filters ListPhotos takes for this list. Nil leaves a filter out.
    var isFavoriteFilter: Bool? { self == .favorites ? true : nil }
    var isTrashedFilter: Bool? { self == .trash ? true : nil }
}

extension Documents {
    static let photoFields = """
        id createdAt photoKind takenAt takenAtOffsetMinutes widthPixels heightPixels durationMS latitude longitude placeName cameraMake cameraModel
        originalFilename isFavorite isScreenshot photoProcessingState trashedAt smallPreviewUrl largePreviewUrl videoPreviewUrl
        """
    public static let listPhotos = """
        query ($beforePhotoId: String, $pageSize: Int, $isFavorite: Boolean, $isTrashed: Boolean, $albumId: String, $takenAfter: DateTime, $takenBefore: DateTime) {
          ListPhotos(beforePhotoId: $beforePhotoId, pageSize: $pageSize, isFavorite: $isFavorite, isTrashed: $isTrashed, albumId: $albumId, takenAfter: $takenAfter, takenBefore: $takenBefore) { \(photoFields) }
        }
        """
    public static let readPhoto = """
        query ($photoId: String!) {
          ReadPhoto(photoId: $photoId) {
            photo { \(photoFields) }
            photoFiles { photoFileRole contentType fileSizeBytes originalFilename fileUrl }
            photoPreviews { photoPreviewSize contentType previewSizeBytes previewUrl }
            albums { id albumTitle }
          }
        }
        """
    public static let readPhotoLibrary = """
        query { ReadPhotoLibrary { photoCount videoCount favoriteCount trashedCount usedBytes trashRetentionDays } }
        """
    public static let updatePhoto = """
        mutation ($photoId: String!, $isFavorite: Boolean) { UpdatePhoto(photoId: $photoId, isFavorite: $isFavorite) { \(photoFields) } }
        """
    public static let trashPhotos = """
        mutation ($photoIds: [String!]!) { TrashPhotos(photoIds: $photoIds) }
        """
    public static let restorePhotos = """
        mutation ($photoIds: [String!]!) { RestorePhotos(photoIds: $photoIds) }
        """
    public static let deletePhotos = """
        mutation ($photoIds: [String!]!) { DeletePhotos(photoIds: $photoIds) }
        """
    public static let emptyPhotoTrash = """
        mutation { EmptyPhotoTrash }
        """
    static let photoAlbumFields = "id albumTitle createdAt updatedAt coverPhotoId photoCount firstTakenAt lastTakenAt coverPreviewUrl"
    public static let listPhotoAlbums = """
        query { ListPhotoAlbums { \(photoAlbumFields) } }
        """
    public static let suggestPhotoAlbums = """
        query { SuggestPhotoAlbums { takenAfter takenBefore placeNames albumTitle photoCount samplePhotoIds } }
        """
    public static let createPhotoAlbum = """
        mutation ($albumTitle: String!, $photoIds: [String!]) { CreatePhotoAlbum(albumTitle: $albumTitle, photoIds: $photoIds) { \(photoAlbumFields) } }
        """
    public static let renamePhotoAlbum = """
        mutation ($photoAlbumId: String!, $albumTitle: String!) { RenamePhotoAlbum(photoAlbumId: $photoAlbumId, albumTitle: $albumTitle) { \(photoAlbumFields) } }
        """
    public static let deletePhotoAlbum = """
        mutation ($photoAlbumId: String!) { DeletePhotoAlbum(photoAlbumId: $photoAlbumId) }
        """
    public static let addPhotosToAlbum = """
        mutation ($photoAlbumId: String!, $photoIds: [String!]!) { AddPhotosToAlbum(photoAlbumId: $photoAlbumId, photoIds: $photoIds) }
        """
    public static let removePhotosFromAlbum = """
        mutation ($photoAlbumId: String!, $photoIds: [String!]!) { RemovePhotosFromAlbum(photoAlbumId: $photoAlbumId, photoIds: $photoIds) }
        """
    public static let setPhotoAlbumCover = """
        mutation ($photoAlbumId: String!, $photoId: String!) { SetPhotoAlbumCover(photoAlbumId: $photoAlbumId, photoId: $photoId) { \(photoAlbumFields) } }
        """
}

extension Connection {
    /// One page of a list of photos, newest first. A page after the first
    /// starts after the last photo of the one before.
    public func photos(in collection: PhotoCollection, beforePhotoId: String? = nil, pageSize: Int = PhotoPaging.pageSize) async throws -> [Photo] {
        struct Answer: Decodable, Sendable { let ListPhotos: [Photo] }
        var variables: [String: JSONValue] = ["pageSize": .number(Double(pageSize))]
        if let beforePhotoId, !beforePhotoId.isEmpty { variables["beforePhotoId"] = .string(beforePhotoId) }
        if let isFavorite = collection.isFavoriteFilter { variables["isFavorite"] = .bool(isFavorite) }
        if let isTrashed = collection.isTrashedFilter { variables["isTrashed"] = .bool(isTrashed) }
        return try await execute(Documents.listPhotos, variables: variables, as: Answer.self).ListPhotos
    }

    /// One page of the photos of an album, newest first, paged like the
    /// library's lists.
    public func photos(inAlbum photoAlbumId: String, beforePhotoId: String? = nil, pageSize: Int = PhotoPaging.pageSize) async throws -> [Photo] {
        struct Answer: Decodable, Sendable { let ListPhotos: [Photo] }
        var variables: [String: JSONValue] = ["pageSize": .number(Double(pageSize)), "albumId": .string(photoAlbumId)]
        if let beforePhotoId, !beforePhotoId.isEmpty { variables["beforePhotoId"] = .string(beforePhotoId) }
        return try await execute(Documents.listPhotos, variables: variables, as: Answer.self).ListPhotos
    }

    /// One photo with the files and previews it is made of.
    public func photoDetail(_ photoId: String) async throws -> PhotoDetail {
        struct Answer: Decodable, Sendable { let ReadPhoto: PhotoDetail }
        return try await execute(Documents.readPhoto, variables: ["photoId": .string(photoId)], as: Answer.self).ReadPhoto
    }

    /// How much the library holds, and how long the trash keeps a photo.
    public func photoLibrary() async throws -> PhotoLibrary {
        struct Answer: Decodable, Sendable { let ReadPhotoLibrary: PhotoLibrary }
        return try await execute(Documents.readPhotoLibrary, as: Answer.self).ReadPhotoLibrary
    }

    /// Marks a photo a favorite, or not, and returns it as it now is.
    public func setPhotoFavorite(_ photoId: String, isFavorite: Bool) async throws -> Photo {
        struct Answer: Decodable, Sendable { let UpdatePhoto: Photo }
        return try await execute(Documents.updatePhoto, variables: ["photoId": .string(photoId), "isFavorite": .bool(isFavorite)], as: Answer.self).UpdatePhoto
    }

    /// Moves photos to the trash; says how many moved.
    @discardableResult
    public func trashPhotos(_ photoIds: [String]) async throws -> Int {
        struct Answer: Decodable, Sendable { let TrashPhotos: Int }
        return try await execute(Documents.trashPhotos, variables: ["photoIds": .array(photoIds.map { .string($0) })], as: Answer.self).TrashPhotos
    }

    /// Takes photos out of the trash; says how many came back.
    @discardableResult
    public func restorePhotos(_ photoIds: [String]) async throws -> Int {
        struct Answer: Decodable, Sendable { let RestorePhotos: Int }
        return try await execute(Documents.restorePhotos, variables: ["photoIds": .array(photoIds.map { .string($0) })], as: Answer.self).RestorePhotos
    }

    /// Deletes photos that are in the trash for good; says how many went.
    @discardableResult
    public func deletePhotos(_ photoIds: [String]) async throws -> Int {
        struct Answer: Decodable, Sendable { let DeletePhotos: Int }
        return try await execute(Documents.deletePhotos, variables: ["photoIds": .array(photoIds.map { .string($0) })], as: Answer.self).DeletePhotos
    }

    /// Deletes everything in the trash for good; says how many photos went.
    @discardableResult
    public func emptyPhotoTrash() async throws -> Int {
        struct Answer: Decodable, Sendable { let EmptyPhotoTrash: Int }
        return try await execute(Documents.emptyPhotoTrash, as: Answer.self).EmptyPhotoTrash
    }
}

/// PhotoPaging is how a list of photos is read a page at a time as the person
/// scrolls toward the end of what is loaded.
public enum PhotoPaging {
    /// How many photos a page holds, as the dashboard's pages do.
    public static let pageSize = 200

    /// How close to the end of the loaded photos a cell comes on screen
    /// before the next page is asked for.
    public static let loadAhead = 60

    /// Whether the page just read may be followed by another: a short page
    /// is the last.
    public static func hasMore(afterPageOf count: Int, pageSize: Int = PhotoPaging.pageSize) -> Bool {
        count >= pageSize
    }

    /// Whether the cell of this photo coming on screen should start the next
    /// page.
    public static func shouldLoadMore(whenShowing photoId: String, in photos: [Photo], loadAhead: Int = PhotoPaging.loadAhead) -> Bool {
        guard let index = photos.lastIndex(where: { $0.id == photoId }) else { return false }
        return index >= photos.count - loadAhead
    }

    /// The photos with a page added after them, leaving out any the list
    /// already holds (a photo moved while the page was read).
    public static func appending(_ page: [Photo], to photos: [Photo]) -> [Photo] {
        let known = Set(photos.map(\.id))
        return photos + page.filter { !known.contains($0.id) }
    }
}

/// PhotoMoment is the time a photo is placed at, with the time zone to read it
/// in.
public struct PhotoMoment: Equatable, Sendable {
    public let date: Date
    public let timeZone: TimeZone
    /// Whether the zone is the one the photo was taken in, rather than the
    /// phone's.
    public let isTimeZoneOfPhoto: Bool
}

/// PhotoDay is the photos of one day, a day being as the wall clock read where
/// they were taken.
public struct PhotoDay: Identifiable, Hashable, Sendable {
    /// "2026-03-11", or "unknown" for photos with no date at all.
    public let id: String
    public let year: Int?
    public let month: Int?
    public let day: Int?
    public let photos: [Photo]

    /// Noon of the day in UTC, to be written with a time zone of UTC so the
    /// text names this very day wherever the phone is. Nil for no date.
    public var noonUTC: Date? {
        guard let year, let month, let day else { return nil }
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(secondsFromGMT: 0) ?? .gmt
        return calendar.date(from: DateComponents(year: year, month: month, day: day, hour: 12))
    }
}

/// PhotoTimeline is how photos are placed on days and times.
///
/// A photo with a `takenAtOffsetMinutes` is placed by the wall clock of where
/// it was taken (its `takenAt` shifted by that offset), so a trip abroad shows
/// its own dates. Without an offset, and for a photo with no `takenAt` (placed
/// by `createdAt`, when it arrived), the phone's time zone is used.
public enum PhotoTimeline {
    /// The time a photo is placed at, and the zone to read it in.
    public static func moment(of photo: Photo, phoneTimeZone: TimeZone = .current) -> PhotoMoment? {
        if let takenAt = ServerTime.date(photo.takenAt) {
            if let offsetMinutes = photo.takenAtOffsetMinutes, let zone = TimeZone(secondsFromGMT: offsetMinutes * 60) {
                return PhotoMoment(date: takenAt, timeZone: zone, isTimeZoneOfPhoto: true)
            }
            return PhotoMoment(date: takenAt, timeZone: phoneTimeZone, isTimeZoneOfPhoto: false)
        }
        if let createdAt = ServerTime.date(photo.createdAt) {
            return PhotoMoment(date: createdAt, timeZone: phoneTimeZone, isTimeZoneOfPhoto: false)
        }
        return nil
    }

    /// The photos, which the server lists newest first, in days. A day keeps
    /// the order the photos came in, and days come in the order of their
    /// first photo.
    public static func days(_ photos: [Photo], phoneTimeZone: TimeZone = .current) -> [PhotoDay] {
        var order: [String] = []
        var members: [String: [Photo]] = [:]
        var parts: [String: DateComponents] = [:]
        for photo in photos {
            var key = "unknown"
            if let moment = moment(of: photo, phoneTimeZone: phoneTimeZone) {
                var calendar = Calendar(identifier: .gregorian)
                calendar.timeZone = moment.timeZone
                let components = calendar.dateComponents([.year, .month, .day], from: moment.date)
                if let year = components.year, let month = components.month, let day = components.day {
                    key = String(format: "%04ld-%02ld-%02ld", year, month, day)
                    parts[key] = components
                }
            }
            if members[key] == nil { order.append(key) }
            members[key, default: []].append(photo)
        }
        return order.map { key in
            PhotoDay(id: key, year: parts[key]?.year, month: parts[key]?.month, day: parts[key]?.day, photos: members[key] ?? [])
        }
    }

    /// The date and time of a photo as the viewer's Info shows them, read in
    /// the zone the photo was taken in when it says one. Nil for no date.
    public static func takenText(of photo: Photo, phoneTimeZone: TimeZone = .current, locale: Locale = .autoupdatingCurrent) -> String? {
        guard let moment = moment(of: photo, phoneTimeZone: phoneTimeZone) else { return nil }
        return moment.date.formatted(Date.FormatStyle(date: .complete, time: .shortened, locale: locale, timeZone: moment.timeZone))
    }

    /// How long a video runs, as m:ss, or h:mm:ss from an hour on. Whole
    /// seconds, rounded to the nearest.
    public static func durationText(milliseconds: Int) -> String {
        let totalSeconds = max(0, (milliseconds + 500) / 1000)
        let hours = totalSeconds / 3600
        let minutes = (totalSeconds % 3600) / 60
        let seconds = totalSeconds % 60
        if hours > 0 { return String(format: "%ld:%02ld:%02ld", hours, minutes, seconds) }
        return String(format: "%ld:%02ld", minutes, seconds)
    }
}

/// PhotoDownload is a photo's file read from the server, to be shared or saved
/// to the phone's Photos.
public struct PhotoDownload: Sendable {
    public let data: Data
    /// The name to give the file, with an extension the phone's Photos knows
    /// the kind of file by.
    public let fileName: String
    /// Whether this is the file the photo was made from. False when that file
    /// could not be read and a preview stands in for it.
    public let isOriginal: Bool
}

extension PhotoDownload {
    /// The extension a file of this content type has, or nil for a kind the
    /// app does not know.
    public static func fileExtension(forContentType contentType: String) -> String? {
        let kind = contentType.split(separator: ";").first.map { $0.trimmingCharacters(in: .whitespaces).lowercased() } ?? ""
        switch kind {
        case "image/jpeg", "image/jpg": return "jpg"
        case "image/png": return "png"
        case "image/heic": return "heic"
        case "image/heif": return "heif"
        case "image/gif": return "gif"
        case "image/webp": return "webp"
        case "video/mp4": return "mp4"
        case "video/quicktime": return "mov"
        default: return nil
        }
    }
}

extension Connection {
    /// The original file of a photo, or, when that cannot be read, the large
    /// preview (the video preview, for a video) in its place.
    public func downloadPhotoFile(_ photo: Photo) async throws -> PhotoDownload {
        let detail = try? await photoDetail(photo.id)
        if let file = detail?.originalFile, let data = try? await download(file.fileUrl) {
            let name = file.originalFilename.isEmpty ? photo.fileName : file.originalFilename
            return PhotoDownload(data: data, fileName: name, isOriginal: true)
        }
        let previewSize = photo.isVideo ? "video" : "large"
        let preview = detail?.photoPreviews.first { $0.photoPreviewSize == previewSize }
        guard let path = preview?.previewUrl ?? (photo.isVideo ? photo.videoPath : photo.viewerPreviewPath) else {
            throw TeaNodeError.refused("This photo has nothing to save yet. Try again in a moment.")
        }
        let data = try await download(path)
        let fileExtension = preview.flatMap { PhotoDownload.fileExtension(forContentType: $0.contentType) } ?? (photo.isVideo ? "mp4" : "jpg")
        let baseName = (photo.fileName as NSString).deletingPathExtension
        return PhotoDownload(data: data, fileName: "\(baseName).\(fileExtension)", isOriginal: false)
    }
}

// MARK: Albums

extension Connection {
    /// The person's albums, the one changed most recently first.
    public func photoAlbums() async throws -> [PhotoAlbum] {
        struct Answer: Decodable, Sendable { let ListPhotoAlbums: [PhotoAlbum] }
        return try await execute(Documents.listPhotoAlbums, as: Answer.self).ListPhotoAlbums
    }

    /// The trips and events found in photos that are in no album.
    public func photoAlbumSuggestions() async throws -> [PhotoAlbumSuggestion] {
        struct Answer: Decodable, Sendable { let SuggestPhotoAlbums: [PhotoAlbumSuggestion] }
        return try await execute(Documents.suggestPhotoAlbums, as: Answer.self).SuggestPhotoAlbums
    }

    /// Makes an album holding the photos, which may be none. The photos go in
    /// batches the server accepts: the first batch makes the album and the
    /// rest are added to it.
    public func createPhotoAlbum(title: String, photoIds: [String] = []) async throws -> PhotoAlbum {
        struct Answer: Decodable, Sendable { let CreatePhotoAlbum: PhotoAlbum }
        let batches = PhotoBatches.batches(of: photoIds)
        let first = batches.first ?? []
        let created = try await execute(
            Documents.createPhotoAlbum, variables: ["albumTitle": .string(PhotoAlbumText.trimmedTitle(title)), "photoIds": .array(first.map { .string($0) })],
            as: Answer.self
        ).CreatePhotoAlbum
        for batch in batches.dropFirst() { try await addPhotos(batch, toAlbum: created.id) }
        return created
    }

    /// Gives an album another title and returns it as it now is.
    public func renamePhotoAlbum(_ photoAlbumId: String, title: String) async throws -> PhotoAlbum {
        struct Answer: Decodable, Sendable { let RenamePhotoAlbum: PhotoAlbum }
        return try await execute(
            Documents.renamePhotoAlbum, variables: ["photoAlbumId": .string(photoAlbumId), "albumTitle": .string(PhotoAlbumText.trimmedTitle(title))], as: Answer.self
        ).RenamePhotoAlbum
    }

    /// Deletes an album. Its photos stay in the library.
    public func deletePhotoAlbum(_ photoAlbumId: String) async throws {
        struct Answer: Decodable, Sendable { let DeletePhotoAlbum: Bool }
        _ = try await execute(Documents.deletePhotoAlbum, variables: ["photoAlbumId": .string(photoAlbumId)], as: Answer.self)
    }

    /// Puts photos in an album; says how many went in, leaving out those that
    /// were in it already.
    @discardableResult
    public func addPhotos(_ photoIds: [String], toAlbum photoAlbumId: String) async throws -> Int {
        struct Answer: Decodable, Sendable { let AddPhotosToAlbum: Int }
        var addedCount = 0
        for batch in PhotoBatches.batches(of: photoIds) {
            addedCount += try await execute(
                Documents.addPhotosToAlbum, variables: ["photoAlbumId": .string(photoAlbumId), "photoIds": .array(batch.map { .string($0) })], as: Answer.self
            ).AddPhotosToAlbum
        }
        return addedCount
    }

    /// Takes photos out of an album, which leaves them in the library; says
    /// how many were taken out.
    @discardableResult
    public func removePhotos(_ photoIds: [String], fromAlbum photoAlbumId: String) async throws -> Int {
        struct Answer: Decodable, Sendable { let RemovePhotosFromAlbum: Int }
        var removedCount = 0
        for batch in PhotoBatches.batches(of: photoIds) {
            removedCount += try await execute(
                Documents.removePhotosFromAlbum, variables: ["photoAlbumId": .string(photoAlbumId), "photoIds": .array(batch.map { .string($0) })], as: Answer.self
            ).RemovePhotosFromAlbum
        }
        return removedCount
    }

    /// Chooses the photo an album is shown by, which must be one of its photos.
    public func setPhotoAlbumCover(_ photoAlbumId: String, photoId: String) async throws -> PhotoAlbum {
        struct Answer: Decodable, Sendable { let SetPhotoAlbumCover: PhotoAlbum }
        return try await execute(
            Documents.setPhotoAlbumCover, variables: ["photoAlbumId": .string(photoAlbumId), "photoId": .string(photoId)], as: Answer.self
        ).SetPhotoAlbumCover
    }

    /// Every photo a suggestion covers, found the way the server found the
    /// run: by its dates. A photo of the range that is in another album comes
    /// with them, which does no harm, an album holds references.
    public func photoIds(of suggestion: PhotoAlbumSuggestion) async throws -> [String] {
        struct Answer: Decodable, Sendable { let ListPhotos: [Photo] }
        var photoIds: [String] = []
        var beforePhotoId: String?
        while true {
            var variables: [String: JSONValue] = [
                "pageSize": .number(Double(PhotoBatches.mostPhotoIdsPerCall)), "takenAfter": .string(suggestion.takenAfter), "takenBefore": .string(suggestion.takenBefore),
            ]
            if let beforePhotoId { variables["beforePhotoId"] = .string(beforePhotoId) }
            let page = try await execute(Documents.listPhotos, variables: variables, as: Answer.self).ListPhotos
            photoIds += page.map(\.id)
            guard page.count >= PhotoBatches.mostPhotoIdsPerCall, let last = page.last else { return photoIds }
            beforePhotoId = last.id
        }
    }
}
