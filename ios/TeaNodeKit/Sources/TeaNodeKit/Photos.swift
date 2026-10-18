import Foundation

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// PhotoError is why a photo could not be read or saved, without any words:
/// the app writes each in the person's language, so the Kit has no English
/// that can reach the screen.
public enum PhotoError: Error, Equatable, Sendable {
    /// The server did not serve the file. The status code is not shown.
    case fileCouldNotBeRead
    /// The server says there is no such file for this person: it is gone, or
    /// they may not have it. Another copy of the photo may stand in for it.
    case fileNotAvailable
    /// The photo has no file and no preview yet.
    case nothingToSaveYet
    /// The person has no address book to put a new contact in.
    case noAddressBook
}

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
    /// The roles of the files the server holds for the photo: "original", and
    /// "edited" and "liveVideo" when it has them. Empty from a server that
    /// does not say.
    public let photoFileRoles: [String]
    /// The server's plain reason the last try at making previews failed;
    /// empty unless it did.
    public let processingErrorMessage: String
    /// Whether the photo is in the person's own library. A photo read through
    /// an album shared with them is not: it has no favorite mark or place of
    /// theirs, and cannot be trashed by them. A server that does not say
    /// shares nothing, so the photo is the person's.
    public let isOwnedByCaller: Bool
    /// Whose the photo is when it is not the person's; empty otherwise.
    public let ownerName: String

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        createdAt = try container.decode(String.self, forKey: .createdAt)
        photoKind = try container.decode(String.self, forKey: .photoKind)
        takenAt = try container.decodeIfPresent(String.self, forKey: .takenAt)
        takenAtOffsetMinutes = try container.decodeIfPresent(Int.self, forKey: .takenAtOffsetMinutes)
        widthPixels = try container.decode(Int.self, forKey: .widthPixels)
        heightPixels = try container.decode(Int.self, forKey: .heightPixels)
        durationMS = try container.decode(Int.self, forKey: .durationMS)
        latitude = try container.decodeIfPresent(Double.self, forKey: .latitude)
        longitude = try container.decodeIfPresent(Double.self, forKey: .longitude)
        placeName = try container.decode(String.self, forKey: .placeName)
        cameraMake = try container.decode(String.self, forKey: .cameraMake)
        cameraModel = try container.decode(String.self, forKey: .cameraModel)
        originalFilename = try container.decode(String.self, forKey: .originalFilename)
        isFavorite = try container.decode(Bool.self, forKey: .isFavorite)
        isScreenshot = try container.decode(Bool.self, forKey: .isScreenshot)
        photoProcessingState = try container.decode(String.self, forKey: .photoProcessingState)
        trashedAt = try container.decodeIfPresent(String.self, forKey: .trashedAt)
        smallPreviewUrl = try container.decode(String.self, forKey: .smallPreviewUrl)
        largePreviewUrl = try container.decode(String.self, forKey: .largePreviewUrl)
        videoPreviewUrl = try container.decode(String.self, forKey: .videoPreviewUrl)
        photoFileRoles = try container.decodeIfPresent([String].self, forKey: .photoFileRoles) ?? []
        processingErrorMessage = try container.decodeIfPresent(String.self, forKey: .processingErrorMessage) ?? ""
        isOwnedByCaller = try container.decodeIfPresent(Bool.self, forKey: .isOwnedByCaller) ?? true
        ownerName = try container.decodeIfPresent(String.self, forKey: .ownerName) ?? ""
    }

    /// Whether the photo has an edited version beside its original.
    public var hasEditedFile: Bool { photoFileRoles.contains("edited") }
    /// Whether the photo is a Live Photo whose video the server holds.
    public var hasLiveVideoFile: Bool { photoFileRoles.contains("liveVideo") }

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
    public var filename: String {
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

/// PhotoDescription is what a model wrote about a picture, as ReadPhoto and
/// DescribePhoto say it. The list of photos leaves it out. The words are text a
/// model wrote from a picture that may carry words meant for it: shown, never
/// obeyed.
public struct PhotoDescription: Codable, Hashable, Sendable {
    /// Empty until somebody works with the photo.
    public let photoDescription: String
    /// The words the model could read in the picture.
    public let descriptionVisibleText: String
    /// When they were written; nil while the photo is undescribed.
    public let describedAt: String?
    /// A description is on its way: asked for, and not yet written.
    public let isDescriptionRequested: Bool

    public init(photoDescription: String = "", descriptionVisibleText: String = "", describedAt: String? = nil, isDescriptionRequested: Bool = false) {
        self.photoDescription = photoDescription
        self.descriptionVisibleText = descriptionVisibleText
        self.describedAt = describedAt
        self.isDescriptionRequested = isDescriptionRequested
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        photoDescription = try container.decodeIfPresent(String.self, forKey: .photoDescription) ?? ""
        descriptionVisibleText = try container.decodeIfPresent(String.self, forKey: .descriptionVisibleText) ?? ""
        describedAt = try container.decodeIfPresent(String.self, forKey: .describedAt)
        isDescriptionRequested = try container.decodeIfPresent(Bool.self, forKey: .isDescriptionRequested) ?? false
    }

    /// Whether a description has been written.
    public var isDescribed: Bool { describedAt != nil }

    /// When it was written, as a date in the phone's zone, or nil.
    public func describedOnText(timeZone: TimeZone = .current, locale: Locale = .autoupdatingCurrent) -> String? {
        guard let date = ServerTime.date(describedAt) else { return nil }
        return date.formatted(Date.FormatStyle(date: .long, time: .omitted, locale: locale, timeZone: timeZone))
    }
}

/// PhotoDescriptionPolling is how a description that is on its way is waited
/// for, as the dashboard does: asked about every few seconds while the person
/// stays on the photo, and every fifteen seconds once two minutes have passed,
/// when the screen also says it is taking longer than usual. It does not give
/// up while the screen is open; closing the screen ends it, and the job goes
/// on without it.
public enum PhotoDescriptionPolling {
    public static let intervalSeconds = 3
    public static let slowAfterSeconds = 120
    public static let slowIntervalSeconds = 15

    /// Whether a screen that has waited this long says it is taking longer
    /// than usual.
    public static func isSlow(afterWaiting waitedSeconds: Int) -> Bool {
        waitedSeconds >= slowAfterSeconds
    }

    /// How long a screen that has waited this long waits before it asks again.
    public static func secondsBeforeAsking(afterWaiting waitedSeconds: Int) -> Int {
        isSlow(afterWaiting: waitedSeconds) ? slowIntervalSeconds : intervalSeconds
    }
}

/// PhotoFace is one face found in a photo. The box is in fractions of the
/// picture from its top left corner. The picture of the face is cut from the
/// photo by the server and needs the app's sign-in, so it is read through the
/// app's connection and its preview cache, never by an address alone.
public struct PhotoFace: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let photoId: String
    public let boxLeftFraction: Double
    public let boxTopFraction: Double
    public let boxWidthFraction: Double
    public let boxHeightFraction: Double
    /// Empty for a face too small or too uncertain to be grouped.
    public let photoFaceGroupId: String
    /// Those of the face's group; empty while the group is unnamed.
    public let contactId: String
    public let contactName: String
    /// Whether the group's contact is the person's own card, so the face is
    /// them. A server that does not say has no such face.
    public let isOwnContact: Bool
    public let facePictureUrl: String

    public init(
        id: String, photoId: String, boxLeftFraction: Double = 0, boxTopFraction: Double = 0, boxWidthFraction: Double = 0, boxHeightFraction: Double = 0,
        photoFaceGroupId: String = "", contactId: String = "", contactName: String = "", isOwnContact: Bool = false, facePictureUrl: String = ""
    ) {
        self.id = id
        self.photoId = photoId
        self.boxLeftFraction = boxLeftFraction
        self.boxTopFraction = boxTopFraction
        self.boxWidthFraction = boxWidthFraction
        self.boxHeightFraction = boxHeightFraction
        self.photoFaceGroupId = photoFaceGroupId
        self.contactId = contactId
        self.contactName = contactName
        self.isOwnContact = isOwnContact
        self.facePictureUrl = facePictureUrl
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        photoId = try container.decode(String.self, forKey: .photoId)
        boxLeftFraction = try container.decode(Double.self, forKey: .boxLeftFraction)
        boxTopFraction = try container.decode(Double.self, forKey: .boxTopFraction)
        boxWidthFraction = try container.decode(Double.self, forKey: .boxWidthFraction)
        boxHeightFraction = try container.decode(Double.self, forKey: .boxHeightFraction)
        photoFaceGroupId = try container.decode(String.self, forKey: .photoFaceGroupId)
        contactId = try container.decode(String.self, forKey: .contactId)
        contactName = try container.decode(String.self, forKey: .contactName)
        isOwnContact = try container.decodeIfPresent(Bool.self, forKey: .isOwnContact) ?? false
        facePictureUrl = try container.decode(String.self, forKey: .facePictureUrl)
    }

    /// Whether the face is in a group, which is a person to open.
    public var isGrouped: Bool { !photoFaceGroupId.isEmpty }
}

/// PhotoFaceGroup is the faces the server believes are one person. It is
/// nobody until the person names it by choosing a contact.
public struct PhotoFaceGroup: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let createdAt: String
    public let updatedAt: String
    /// The contact that names the group; empty while it is nobody.
    public let contactId: String
    public let contactAddressBookId: String
    public let contactName: String
    /// Whether that contact is the person's own card, so the group is them. A
    /// server that does not say has no such group.
    public let isOwnContact: Bool
    /// Counted over photos that are not in the Trash.
    public let faceCount: Int
    public let photoCount: Int
    /// The picture of the group's cover face.
    public let facePictureUrl: String
    public let coverPhotoFaceId: String
    public let isHidden: Bool

    public init(
        id: String, createdAt: String = "", updatedAt: String = "", contactId: String = "", contactAddressBookId: String = "", contactName: String = "",
        isOwnContact: Bool = false, faceCount: Int = 0, photoCount: Int = 0, facePictureUrl: String = "", coverPhotoFaceId: String = "", isHidden: Bool = false
    ) {
        self.id = id
        self.createdAt = createdAt
        self.updatedAt = updatedAt
        self.contactId = contactId
        self.contactAddressBookId = contactAddressBookId
        self.contactName = contactName
        self.isOwnContact = isOwnContact
        self.faceCount = faceCount
        self.photoCount = photoCount
        self.facePictureUrl = facePictureUrl
        self.coverPhotoFaceId = coverPhotoFaceId
        self.isHidden = isHidden
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        createdAt = try container.decode(String.self, forKey: .createdAt)
        updatedAt = try container.decode(String.self, forKey: .updatedAt)
        contactId = try container.decode(String.self, forKey: .contactId)
        contactAddressBookId = try container.decode(String.self, forKey: .contactAddressBookId)
        contactName = try container.decode(String.self, forKey: .contactName)
        isOwnContact = try container.decodeIfPresent(Bool.self, forKey: .isOwnContact) ?? false
        faceCount = try container.decode(Int.self, forKey: .faceCount)
        photoCount = try container.decode(Int.self, forKey: .photoCount)
        facePictureUrl = try container.decode(String.self, forKey: .facePictureUrl)
        coverPhotoFaceId = try container.decode(String.self, forKey: .coverPhotoFaceId)
        isHidden = try container.decode(Bool.self, forKey: .isHidden)
    }

    /// The name of the contact, without the spaces round it.
    public var trimmedContactName: String { contactName.trimmingCharacters(in: .whitespacesAndNewlines) }

    /// Whether a contact names the group.
    public var isNamed: Bool { !trimmedContactName.isEmpty }
}

/// NamedPhotoFaceGroup is what naming a group answered. A contact names one
/// person, so a contact that named another group already merges the two:
/// `photoFaceGroup` is the group that was named already, with the faces of
/// both, and `mergedPhotoFaceGroupId` is the group that went into it, empty
/// when nothing was merged. A contact that had no picture was given one cut
/// from a face of the group.
public struct NamedPhotoFaceGroup: Decodable, Hashable, Sendable {
    public let photoFaceGroup: PhotoFaceGroup
    public let mergedPhotoFaceGroupId: String
    public let isContactPictureSet: Bool

    public init(photoFaceGroup: PhotoFaceGroup, mergedPhotoFaceGroupId: String = "", isContactPictureSet: Bool = false) {
        self.photoFaceGroup = photoFaceGroup
        self.mergedPhotoFaceGroupId = mergedPhotoFaceGroupId
        self.isContactPictureSet = isContactPictureSet
    }

    /// Whether another group was put into this one.
    public var isMerged: Bool { !mergedPhotoFaceGroupId.isEmpty }
}

/// PhotoDetail is one photo with its files, previews, the albums it is in, its
/// description and the faces found in it.
public struct PhotoDetail: Codable, Hashable, Sendable {
    public let photo: Photo
    public let photoFiles: [PhotoFile]
    public let photoPreviews: [PhotoPreview]
    public let albums: [PhotoAlbumReference]
    public let photoDescription: PhotoDescription
    public let faces: [PhotoFace]

    public init(
        photo: Photo, photoFiles: [PhotoFile], photoPreviews: [PhotoPreview], albums: [PhotoAlbumReference] = [],
        photoDescription: PhotoDescription = PhotoDescription(), faces: [PhotoFace] = []
    ) {
        self.photo = photo
        self.photoFiles = photoFiles
        self.photoPreviews = photoPreviews
        self.albums = albums
        self.photoDescription = photoDescription
        self.faces = faces
    }

    private enum DecodingKeys: String, CodingKey {
        case photo, photoFiles, photoPreviews, albums, faces
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: DecodingKeys.self)
        photo = try container.decode(Photo.self, forKey: .photo)
        photoFiles = try container.decode([PhotoFile].self, forKey: .photoFiles)
        photoPreviews = try container.decode([PhotoPreview].self, forKey: .photoPreviews)
        // A server that does not know albums, descriptions or faces yet says
        // nothing of them.
        albums = try container.decodeIfPresent([PhotoAlbumReference].self, forKey: .albums) ?? []
        // The description is fields of the photo itself.
        photoDescription = try container.decode(PhotoDescription.self, forKey: .photo)
        faces = try container.decodeIfPresent([PhotoFace].self, forKey: .faces) ?? []
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: DecodingKeys.self)
        try container.encode(photo, forKey: .photo)
        try container.encode(photoFiles, forKey: .photoFiles)
        try container.encode(photoPreviews, forKey: .photoPreviews)
        try container.encode(albums, forKey: .albums)
        try container.encode(faces, forKey: .faces)
    }

    /// The file Save to the Photos app and Share hand on: the original.
    public var originalFile: PhotoFile? { photoFiles.first { $0.photoFileRole == "original" } }

    /// The file of one role, such as "edited" or "liveVideo".
    public func file(withRole photoFileRole: String) -> PhotoFile? { photoFiles.first { $0.photoFileRole == photoFileRole } }
}

/// PhotoLibrary is what the library page shows beside the photos.
public struct PhotoLibrary: Codable, Hashable, Sendable {
    public let photoCount: Int
    public let videoCount: Int
    public let favoriteCount: Int
    public let trashedCount: Int
    public let usedBytes: Int
    /// The largest file an upload may carry, and the most the library may
    /// hold; zero for no limit.
    public let maximumFileBytes: Int
    public let maximumAccountBytes: Int
    public let trashRetentionDays: Int
    /// What the library holds, as the dashboard, the command line and the agent
    /// count it: the videos are counted among the photos, not beside them.
    public var itemCount: Int { photoCount + videoCount }
    /// Whether this server and this person can have a photo described, and
    /// when not, why, in the server's words.
    public let isDescriptionAvailable: Bool
    public let descriptionUnavailableMessage: String
    /// Whether this server can find faces, why not when it cannot, and the
    /// person's own switch for finding them.
    public let isFaceRecognitionAvailable: Bool
    public let faceRecognitionUnavailableMessage: String
    public let isPhotoFaceRecognitionEnabled: Bool

    public init(
        photoCount: Int, videoCount: Int, favoriteCount: Int, trashedCount: Int, usedBytes: Int, trashRetentionDays: Int,
        maximumFileBytes: Int = 0, maximumAccountBytes: Int = 0, isDescriptionAvailable: Bool = false, descriptionUnavailableMessage: String = "", isFaceRecognitionAvailable: Bool = false,
        faceRecognitionUnavailableMessage: String = "", isPhotoFaceRecognitionEnabled: Bool = false
    ) {
        self.photoCount = photoCount
        self.videoCount = videoCount
        self.favoriteCount = favoriteCount
        self.trashedCount = trashedCount
        self.usedBytes = usedBytes
        self.maximumFileBytes = maximumFileBytes
        self.maximumAccountBytes = maximumAccountBytes
        self.trashRetentionDays = trashRetentionDays
        self.isDescriptionAvailable = isDescriptionAvailable
        self.descriptionUnavailableMessage = descriptionUnavailableMessage
        self.isFaceRecognitionAvailable = isFaceRecognitionAvailable
        self.faceRecognitionUnavailableMessage = faceRecognitionUnavailableMessage
        self.isPhotoFaceRecognitionEnabled = isPhotoFaceRecognitionEnabled
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        photoCount = try container.decode(Int.self, forKey: .photoCount)
        videoCount = try container.decode(Int.self, forKey: .videoCount)
        favoriteCount = try container.decode(Int.self, forKey: .favoriteCount)
        trashedCount = try container.decode(Int.self, forKey: .trashedCount)
        usedBytes = try container.decode(Int.self, forKey: .usedBytes)
        maximumFileBytes = try container.decodeIfPresent(Int.self, forKey: .maximumFileBytes) ?? 0
        maximumAccountBytes = try container.decodeIfPresent(Int.self, forKey: .maximumAccountBytes) ?? 0
        trashRetentionDays = try container.decode(Int.self, forKey: .trashRetentionDays)
        // A server older than descriptions and faces says nothing of them,
        // which reads as neither being available.
        isDescriptionAvailable = try container.decodeIfPresent(Bool.self, forKey: .isDescriptionAvailable) ?? false
        descriptionUnavailableMessage = try container.decodeIfPresent(String.self, forKey: .descriptionUnavailableMessage) ?? ""
        isFaceRecognitionAvailable = try container.decodeIfPresent(Bool.self, forKey: .isFaceRecognitionAvailable) ?? false
        faceRecognitionUnavailableMessage = try container.decodeIfPresent(String.self, forKey: .faceRecognitionUnavailableMessage) ?? ""
        isPhotoFaceRecognitionEnabled = try container.decodeIfPresent(Bool.self, forKey: .isPhotoFaceRecognitionEnabled) ?? false
    }
}

/// PhotoAlbum is a named set of photos. It holds references: a photo can be in
/// several albums, and deleting the album leaves the photos. An album belongs
/// to a person or to a circle; the photos in it belong to whoever added them.
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
    /// The person the album belongs to; nil for a circle's album.
    public let ownerUserId: String?
    /// The circle the album belongs to; nil for a person's.
    public let circleId: String?
    public let ownerUsername: String
    public let ownerName: String
    public let ownerAvatarUrl: String?
    public let ownerAvatarColorIndex: Int
    public let circleName: String
    /// Whether the album is the person's own. A server that does not share
    /// albums says nothing, and every album is then the person's.
    public let isOwnedByCaller: Bool
    /// "view" or "contribute" for an album shared with the person; empty for
    /// their own.
    public let shareLevel: String
    /// Whether the person may add photos of their own to the album.
    public let canCallerContribute: Bool
    /// Whether the person may rename, delete and share the album.
    public let canCallerManage: Bool
    /// Whether the share that reaches the person lets them see where the
    /// photos were taken.
    public let isLocationIncluded: Bool

    public init(
        id: String, albumTitle: String, createdAt: String = "", updatedAt: String = "", coverPhotoId: String? = nil, photoCount: Int = 0,
        firstTakenAt: String? = nil, lastTakenAt: String? = nil, coverPreviewUrl: String = "", ownerUserId: String? = nil, circleId: String? = nil,
        ownerUsername: String = "", ownerName: String = "", ownerAvatarUrl: String? = nil, ownerAvatarColorIndex: Int = 0, circleName: String = "",
        isOwnedByCaller: Bool = true, shareLevel: String = "", canCallerContribute: Bool = true, canCallerManage: Bool = true, isLocationIncluded: Bool = true
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
        self.ownerUserId = ownerUserId
        self.circleId = circleId
        self.ownerUsername = ownerUsername
        self.ownerName = ownerName
        self.ownerAvatarUrl = ownerAvatarUrl
        self.ownerAvatarColorIndex = ownerAvatarColorIndex
        self.circleName = circleName
        self.isOwnedByCaller = isOwnedByCaller
        self.shareLevel = shareLevel
        self.canCallerContribute = canCallerContribute
        self.canCallerManage = canCallerManage
        self.isLocationIncluded = isLocationIncluded
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        albumTitle = try container.decode(String.self, forKey: .albumTitle)
        createdAt = try container.decode(String.self, forKey: .createdAt)
        updatedAt = try container.decode(String.self, forKey: .updatedAt)
        coverPhotoId = try container.decodeIfPresent(String.self, forKey: .coverPhotoId)
        photoCount = try container.decode(Int.self, forKey: .photoCount)
        firstTakenAt = try container.decodeIfPresent(String.self, forKey: .firstTakenAt)
        lastTakenAt = try container.decodeIfPresent(String.self, forKey: .lastTakenAt)
        coverPreviewUrl = try container.decode(String.self, forKey: .coverPreviewUrl)
        // A server older than shared albums says nothing of them, which reads
        // as an album that is the person's own.
        ownerUserId = try container.decodeIfPresent(String.self, forKey: .ownerUserId)
        circleId = try container.decodeIfPresent(String.self, forKey: .circleId)
        ownerUsername = try container.decodeIfPresent(String.self, forKey: .ownerUsername) ?? ""
        ownerName = try container.decodeIfPresent(String.self, forKey: .ownerName) ?? ""
        ownerAvatarUrl = try container.decodeIfPresent(String.self, forKey: .ownerAvatarUrl)
        ownerAvatarColorIndex = try container.decodeIfPresent(Int.self, forKey: .ownerAvatarColorIndex) ?? 0
        circleName = try container.decodeIfPresent(String.self, forKey: .circleName) ?? ""
        isOwnedByCaller = try container.decodeIfPresent(Bool.self, forKey: .isOwnedByCaller) ?? true
        shareLevel = try container.decodeIfPresent(String.self, forKey: .shareLevel) ?? ""
        canCallerContribute = try container.decodeIfPresent(Bool.self, forKey: .canCallerContribute) ?? true
        canCallerManage = try container.decodeIfPresent(Bool.self, forKey: .canCallerManage) ?? true
        isLocationIncluded = try container.decodeIfPresent(Bool.self, forKey: .isLocationIncluded) ?? true
    }

    /// The dates the album's photos span, as one date or a range, in the
    /// phone's time zone. Nil for an album with no photos.
    public func dateRangeText(timeZone: TimeZone = .current, locale: Locale = .autoupdatingCurrent) -> String? {
        PhotoAlbumText.dateRange(from: firstTakenAt, to: lastTakenAt, timeZone: timeZone, locale: locale)
    }

    /// Whether the album belongs to a circle, which every member of it adds to.
    public var isCircleAlbum: Bool { circleId != nil }

    /// Whether another person shared the album with this person, as itself and
    /// not as a circle's.
    public var isSharedWithCaller: Bool { !isOwnedByCaller && circleId == nil }

    /// Whether the person may share the album: only a person's own album is
    /// shared, since every member of a circle has a circle's already.
    public var canCallerShare: Bool { canCallerManage && circleId == nil }

    /// Whether the person may take this photo out of the album: their own when
    /// they may contribute to it, and another person's only when they manage
    /// the album (its owner, or an admin of its circle).
    public func canCallerRemove(_ photo: Photo) -> Bool {
        photo.isOwnedByCaller ? canCallerContribute : canCallerManage
    }

    /// What to call whose the album is: the owner's name, or their username
    /// when they have no name.
    public var ownerDisplayName: String { ownerName.isEmpty ? ownerUsername : ownerName }
}

/// PhotoCircleAlbums is the albums one circle keeps.
public struct PhotoCircleAlbums: Identifiable, Hashable, Sendable {
    public let circleId: String
    public let circleName: String
    public let albums: [PhotoAlbum]

    public var id: String { circleId }
}

/// PhotoAlbumSections is the albums as the Albums section lays them out: the
/// person's own, then those shared with them, then one list for each circle,
/// by the circle's name. Each keeps the order the server gave.
public struct PhotoAlbumSections: Hashable, Sendable {
    public let own: [PhotoAlbum]
    public let sharedWithYou: [PhotoAlbum]
    public let circles: [PhotoCircleAlbums]

    public init(_ albums: [PhotoAlbum]) {
        own = albums.filter { $0.isOwnedByCaller && $0.circleId == nil }
        sharedWithYou = albums.filter(\.isSharedWithCaller)
        var order: [String] = []
        var members: [String: [PhotoAlbum]] = [:]
        for album in albums {
            guard let circleId = album.circleId else { continue }
            if members[circleId] == nil { order.append(circleId) }
            members[circleId, default: []].append(album)
        }
        circles = order.compactMap { circleId -> PhotoCircleAlbums? in
            guard let albums = members[circleId], let first = albums.first else { return nil }
            return PhotoCircleAlbums(circleId: circleId, circleName: first.circleName, albums: albums)
        }.sorted { $0.circleName.localizedCaseInsensitiveCompare($1.circleName) == .orderedAscending }
    }

    /// Whether any album is not the person's own, which is what makes the
    /// sections worth titling.
    public var hasOthers: Bool { !sharedWithYou.isEmpty || !circles.isEmpty }
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

    /// Whether the proposed title is made of the places, so that a line
    /// listing them under the dates would say them twice. A place is a "City,
    /// Country" name and a title may say the country once for several cities,
    /// so a place is in the title when its city and its country each appear in
    /// it. A title of dates, or one that stops at "and more", leaves a place
    /// unsaid, and the line stays.
    public var isTitleMadeOfPlaces: Bool {
        !placeNames.isEmpty
            && placeNames.allSatisfy { placeName in
                placeName.components(separatedBy: ", ")
                    .map { $0.trimmingCharacters(in: .whitespaces) }
                    .filter { !$0.isEmpty }
                    .allSatisfy { albumTitle.contains($0) }
            }
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
        photoFileRoles processingErrorMessage isOwnedByCaller ownerName
        """
    public static let listPhotos = """
        query ($beforePhotoId: String, $pageSize: Int, $isFavorite: Boolean, $isTrashed: Boolean, $albumId: String, $faceGroupId: String, $photoKind: String, $takenAfter: DateTime, $takenBefore: DateTime, $searchText: String, $nearLatitude: Float, $nearLongitude: Float, $nearDistanceKilometers: Float) {
          ListPhotos(beforePhotoId: $beforePhotoId, pageSize: $pageSize, isFavorite: $isFavorite, isTrashed: $isTrashed, albumId: $albumId, faceGroupId: $faceGroupId, photoKind: $photoKind, takenAfter: $takenAfter, takenBefore: $takenBefore, searchText: $searchText, nearLatitude: $nearLatitude, nearLongitude: $nearLongitude, nearDistanceKilometers: $nearDistanceKilometers) { \(photoFields) }
        }
        """
    static let photoDescriptionFields = """
        photoDescription descriptionVisibleText describedAt isDescriptionRequested
        """
    static let photoFaceFields = """
        id photoId boxLeftFraction boxTopFraction boxWidthFraction boxHeightFraction photoFaceGroupId contactId contactName isOwnContact facePictureUrl
        """
    public static let readPhoto = """
        query ($photoId: String!) {
          ReadPhoto(photoId: $photoId) {
            photo { \(photoFields) \(photoDescriptionFields) }
            photoFiles { photoFileRole contentType fileSizeBytes originalFilename fileUrl }
            photoPreviews { photoPreviewSize contentType previewSizeBytes previewUrl }
            albums { id albumTitle }
            faces { \(photoFaceFields) }
          }
        }
        """
    public static let describePhoto = """
        mutation ($photoId: String!, $isRedescribed: Boolean) {
          DescribePhoto(photoId: $photoId, isRedescribed: $isRedescribed) { \(photoDescriptionFields) }
        }
        """
    public static let photoPlaybackAddress = """
        query ($photoId: String!, $photoPreviewSize: String, $photoFileRole: String) {
          PhotoPlaybackAddress(photoId: $photoId, photoPreviewSize: $photoPreviewSize, photoFileRole: $photoFileRole)
        }
        """
    public static let readPhotoLibrary = """
        query {
          ReadPhotoLibrary {
            photoCount videoCount favoriteCount trashedCount usedBytes maximumFileBytes maximumAccountBytes trashRetentionDays
            isDescriptionAvailable descriptionUnavailableMessage
            isFaceRecognitionAvailable faceRecognitionUnavailableMessage isPhotoFaceRecognitionEnabled
          }
        }
        """
    public static let updatePhoto = """
        mutation ($photoId: String!, $isFavorite: Boolean, $takenAt: DateTime) {
          UpdatePhoto(photoId: $photoId, isFavorite: $isFavorite, takenAt: $takenAt) { \(photoFields) }
        }
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
    public static let retryPhotoProcessing = """
        mutation ($photoIds: [String!]!) { RetryPhotoProcessing(photoIds: $photoIds) }
        """
    public static let emptyPhotoTrash = """
        mutation { EmptyPhotoTrash }
        """
    static let photoAlbumFields = """
        id albumTitle createdAt updatedAt coverPhotoId photoCount firstTakenAt lastTakenAt coverPreviewUrl
        ownerUserId circleId ownerUsername ownerName ownerAvatarUrl ownerAvatarColorIndex circleName
        isOwnedByCaller shareLevel canCallerContribute canCallerManage isLocationIncluded
        """
    public static let listPhotoAlbums = """
        query { ListPhotoAlbums { \(photoAlbumFields) } }
        """
    public static let suggestPhotoAlbums = """
        query { SuggestPhotoAlbums { takenAfter takenBefore placeNames albumTitle photoCount samplePhotoIds } }
        """
    public static let createPhotoAlbum = """
        mutation ($albumTitle: String!, $photoIds: [String!], $circleId: String) {
          CreatePhotoAlbum(albumTitle: $albumTitle, photoIds: $photoIds, circleId: $circleId) { \(photoAlbumFields) }
        }
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
    static let photoFaceGroupFields = """
        id createdAt updatedAt contactId contactAddressBookId contactName isOwnContact faceCount photoCount facePictureUrl coverPhotoFaceId isHidden
        """
    public static let listPhotoFaceGroups = """
        query ($isHiddenIncluded: Boolean) { ListPhotoFaceGroups(isHiddenIncluded: $isHiddenIncluded) { \(photoFaceGroupFields) } }
        """
    public static let readPhotoFaceGroup = """
        query ($photoFaceGroupId: String!) { ReadPhotoFaceGroup(photoFaceGroupId: $photoFaceGroupId) { \(photoFaceGroupFields) } }
        """
    public static let namePhotoFaceGroup = """
        mutation ($photoFaceGroupId: String!, $contactId: String!) { NamePhotoFaceGroup(photoFaceGroupId: $photoFaceGroupId, contactId: $contactId) { photoFaceGroup { \(photoFaceGroupFields) } mergedPhotoFaceGroupId isContactPictureSet } }
        """
    public static let unnamePhotoFaceGroup = """
        mutation ($photoFaceGroupId: String!) { UnnamePhotoFaceGroup(photoFaceGroupId: $photoFaceGroupId) { \(photoFaceGroupFields) } }
        """
    public static let mergePhotoFaceGroups = """
        mutation ($photoFaceGroupIds: [String!]!) { MergePhotoFaceGroups(photoFaceGroupIds: $photoFaceGroupIds) { \(photoFaceGroupFields) } }
        """
    public static let removePhotoFaceFromGroup = """
        mutation ($photoFaceId: String!) { RemovePhotoFaceFromGroup(photoFaceId: $photoFaceId) { \(photoFaceGroupFields) } }
        """
    public static let hidePhotoFaceGroup = """
        mutation ($photoFaceGroupId: String!, $isHidden: Boolean!) { HidePhotoFaceGroup(photoFaceGroupId: $photoFaceGroupId, isHidden: $isHidden) { \(photoFaceGroupFields) } }
        """
    public static let setPhotoFaceRecognition = """
        mutation ($isEnabled: Boolean!) { SetPhotoFaceRecognition(isEnabled: $isEnabled) }
        """
    public static let createNamedContact = """
        mutation ($addressBookId: String!, $name: String) { SaveContact(addressBookId: $addressBookId, name: $name) { id name } }
        """
}

extension Connection {
    /// One page of a list of photos, newest first. A page after the first
    /// starts after the last photo of the one before.
    public func photos(
        in collection: PhotoCollection, filter: PhotoFilter = PhotoFilter(), beforePhotoId: String? = nil, pageSize: Int = PhotoPaging.pageSize
    ) async throws -> [Photo] {
        struct Answer: Decodable, Sendable { let ListPhotos: [Photo] }
        var variables: [String: JSONValue] = ["pageSize": .number(Double(pageSize))]
        if let beforePhotoId, !beforePhotoId.isEmpty { variables["beforePhotoId"] = .string(beforePhotoId) }
        if let isFavorite = collection.isFavoriteFilter { variables["isFavorite"] = .bool(isFavorite) }
        if let isTrashed = collection.isTrashedFilter { variables["isTrashed"] = .bool(isTrashed) }
        for (name, value) in filter.variables() { variables[name] = value }
        return try await execute(Documents.listPhotos, variables: variables, as: Answer.self).ListPhotos
    }

    /// The photos that match the words, best match first. A search is not
    /// paged: the server answers with at most `PhotoSearch.mostResultCount`.
    public func photos(matching searchText: String) async throws -> [Photo] {
        struct Answer: Decodable, Sendable { let ListPhotos: [Photo] }
        let variables: [String: JSONValue] = [
            "pageSize": .number(Double(PhotoSearch.mostResultCount)), "searchText": .string(PhotoSearch.words(from: searchText) ?? ""),
        ]
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

    /// One page of the photos with a face in one group of faces, one person,
    /// newest first, paged like the library's lists.
    public func photos(inFaceGroup photoFaceGroupId: String, beforePhotoId: String? = nil, pageSize: Int = PhotoPaging.pageSize) async throws -> [Photo] {
        struct Answer: Decodable, Sendable { let ListPhotos: [Photo] }
        var variables: [String: JSONValue] = ["pageSize": .number(Double(pageSize)), "faceGroupId": .string(photoFaceGroupId)]
        if let beforePhotoId, !beforePhotoId.isEmpty { variables["beforePhotoId"] = .string(beforePhotoId) }
        return try await execute(Documents.listPhotos, variables: variables, as: Answer.self).ListPhotos
    }

    /// Asks for a description of a photo, or a new one when isRedescribed. The
    /// answer is how the photo's description now stands: isDescriptionRequested
    /// while it is on its way, and describedAt once it has been written.
    public func describePhoto(_ photoId: String, isRedescribed: Bool = false) async throws -> PhotoDescription {
        struct Answer: Decodable, Sendable { let DescribePhoto: PhotoDescription }
        return try await execute(
            Documents.describePhoto, variables: ["photoId": .string(photoId), "isRedescribed": .bool(isRedescribed)], as: Answer.self
        ).DescribePhoto
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

    /// Corrects the date a photo is placed at. The text is an RFC 3339 time
    /// whose offset is the one the photo is shown at (see
    /// `PhotoDateChange.takenAtText`). Returns the photo as it now is.
    public func changePhotoDate(_ photoId: String, takenAtText: String) async throws -> Photo {
        struct Answer: Decodable, Sendable { let UpdatePhoto: Photo }
        return try await execute(Documents.updatePhoto, variables: ["photoId": .string(photoId), "takenAt": .string(takenAtText)], as: Answer.self).UpdatePhoto
    }

    /// Moves photos to the trash; says how many moved.
    @discardableResult
    public func trashPhotos(_ photoIds: [String]) async throws -> Int {
        struct Answer: Decodable, Sendable { let TrashPhotos: Int }
        var changedCount = 0
        for batch in PhotoBatches.batches(of: photoIds) {
            changedCount += try await execute(Documents.trashPhotos, variables: ["photoIds": .array(batch.map { .string($0) })], as: Answer.self).TrashPhotos
        }
        return changedCount
    }

    /// Takes photos out of the trash; says how many came back.
    @discardableResult
    public func restorePhotos(_ photoIds: [String]) async throws -> Int {
        struct Answer: Decodable, Sendable { let RestorePhotos: Int }
        var changedCount = 0
        for batch in PhotoBatches.batches(of: photoIds) {
            changedCount += try await execute(Documents.restorePhotos, variables: ["photoIds": .array(batch.map { .string($0) })], as: Answer.self).RestorePhotos
        }
        return changedCount
    }

    /// Deletes photos that are in the trash for good; says how many went.
    @discardableResult
    public func deletePhotos(_ photoIds: [String]) async throws -> Int {
        struct Answer: Decodable, Sendable { let DeletePhotos: Int }
        var changedCount = 0
        for batch in PhotoBatches.batches(of: photoIds) {
            changedCount += try await execute(Documents.deletePhotos, variables: ["photoIds": .array(batch.map { .string($0) })], as: Answer.self).DeletePhotos
        }
        return changedCount
    }

    /// Puts photos whose previews failed back in the queue to be made again;
    /// says how many were moved. Those already waiting are left alone.
    @discardableResult
    public func retryPhotoProcessing(_ photoIds: [String]) async throws -> Int {
        struct Answer: Decodable, Sendable { let RetryPhotoProcessing: Int }
        var retriedCount = 0
        for batch in PhotoBatches.batches(of: photoIds) {
            retriedCount += try await execute(Documents.retryPhotoProcessing, variables: ["photoIds": .array(batch.map { .string($0) })], as: Answer.self)
                .RetryPhotoProcessing
        }
        return retriedCount
    }

    /// Deletes everything in the trash for good; says how many photos went.
    @discardableResult
    public func emptyPhotoTrash() async throws -> Int {
        struct Answer: Decodable, Sendable { let EmptyPhotoTrash: Int }
        return try await execute(Documents.emptyPhotoTrash, as: Answer.self).EmptyPhotoTrash
    }
}

/// PhotoSearch is how the words a person types are asked of the server.
public enum PhotoSearch {
    /// The most photos a search answers with, best match first.
    public static let mostResultCount = 500

    /// The words without the spaces round them; nil when nothing is left to
    /// search for.
    public static func words(from typed: String) -> String? {
        let words = typed.trimmingCharacters(in: .whitespacesAndNewlines)
        return words.isEmpty ? nil : words
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

/// PhotoPlacement is a list of photos after one of them was given another
/// date: the photo is in its new place among those held, and the page that
/// follows is still asked for from the right photo.
public struct PhotoPlacement: Equatable, Sendable {
    /// The photos held, with the changed one where its new date puts it.
    public let photos: [Photo]
    /// The photo the next page is asked for after, which is not the changed
    /// photo when that was the one: the server would continue from its new date.
    public let pageCursorPhotoId: String?
    /// Whether the photo now comes after every photo held while more are still
    /// to be read: its true place is among them, so the list is put in order
    /// again when the next page is added.
    public let isBeyondHeldPhotos: Bool

    /// Puts a photo, as the server now has it, in its place among `photos`,
    /// which are the first of a list that has `hasMore` pages to follow.
    /// `pageCursorPhotoId` is the photo the next page is asked after; nil for
    /// the last of `photos`. A photo that is not held leaves the list as it was.
    public static func place(_ photo: Photo, in photos: [Photo], pageCursorPhotoId: String?, hasMore: Bool) -> PhotoPlacement {
        let cursor = pageCursorPhotoId ?? photos.last?.id
        guard let oldIndex = photos.firstIndex(where: { $0.id == photo.id }) else {
            return PhotoPlacement(photos: photos, pageCursorPhotoId: cursor, isBeyondHeldPhotos: false)
        }
        var nextCursor = cursor
        if cursor == photo.id { nextCursor = oldIndex > 0 ? photos[oldIndex - 1].id : nil }
        var others = photos
        others.remove(at: oldIndex)
        let newIndex = others.firstIndex { PhotoTimeline.isListedBefore(photo, $0) } ?? others.count
        others.insert(photo, at: newIndex)
        return PhotoPlacement(photos: others, pageCursorPhotoId: nextCursor, isBeyondHeldPhotos: hasMore && others.count > 1 && newIndex == others.count - 1)
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

/// PhotoTrashDay is the photos of the Trash moved there on one day, with how
/// long the first of them has left before it is deleted forever. What a person
/// looking at the Trash asks is when each photo goes, not when it was taken.
public struct PhotoTrashDay: Identifiable, Hashable, Sendable {
    /// "2026-03-11", the day they were moved to the Trash on the phone's
    /// clock, or "unknown" for photos that do not say.
    public let id: String
    /// When the first photo of the day was moved to the Trash; nil for no date.
    public let trashedAt: Date?
    /// Whole days left, 0 from the last day on; nil when the server keeps
    /// photos for no set time or the photos do not say when they were trashed.
    public let daysLeft: Int?
    public let photos: [Photo]
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

    /// All the photos in one day with no date, in the order they came in: how
    /// a list ranked by match is laid out, since days would break the order.
    public static func rankedDay(_ photos: [Photo]) -> [PhotoDay] {
        photos.isEmpty ? [] : [PhotoDay(id: "ranked", year: nil, month: nil, day: nil, photos: photos)]
    }

    /// How many whole days a photo trashed at a moment stays in the Trash,
    /// counting from now, when the server keeps it for `retentionDays`: zero
    /// from the last day on, and nil when the server keeps it for no set time
    /// or the photo does not say when it was trashed. The dashboard counts the
    /// same way.
    public static func trashDaysLeft(trashedAt: Date?, retentionDays: Int, now: Date) -> Int? {
        guard let trashedAt, retentionDays > 0 else { return nil }
        let elapsedDays = Int((now.timeIntervalSince(trashedAt) / 86_400).rounded(.down))
        return max(0, retentionDays - elapsedDays)
    }

    /// The photos of the Trash in groups by the day they were moved there. The
    /// server lists the Trash newest trashed first, so a group is a run of the
    /// list: photos are not moved between groups, and the order is kept. Each
    /// group says how long it has left, as its last photo does: the list runs
    /// newest first, so that is the oldest, which is deleted first.
    public static func trashDays(_ photos: [Photo], retentionDays: Int, now: Date = Date(), phoneTimeZone: TimeZone = .current) -> [PhotoTrashDay] {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = phoneTimeZone
        var days: [PhotoTrashDay] = []
        var runKey: String?
        var runPhotos: [Photo] = []
        var runTrashedAt: Date?
        var runOldestTrashedAt: Date?
        func closeRun() {
            guard let key = runKey else { return }
            days.append(
                PhotoTrashDay(
                    id: key, trashedAt: runTrashedAt, daysLeft: trashDaysLeft(trashedAt: runOldestTrashedAt, retentionDays: retentionDays, now: now), photos: runPhotos))
        }
        for photo in photos {
            let trashedAt = ServerTime.date(photo.trashedAt)
            var key = "unknown"
            if let trashedAt {
                let components = calendar.dateComponents([.year, .month, .day], from: trashedAt)
                if let year = components.year, let month = components.month, let day = components.day {
                    key = String(format: "%04ld-%02ld-%02ld", year, month, day)
                }
            }
            if key != runKey {
                closeRun()
                runKey = key
                runPhotos = []
                runTrashedAt = trashedAt
            }
            runPhotos.append(photo)
            runOldestTrashedAt = trashedAt
        }
        closeRun()
        return days
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

    /// Whether the first photo is listed before the second: the later moment
    /// first, and for the same moment the larger id first, as the server lists.
    /// A photo with no moment is listed last.
    public static func isListedBefore(_ first: Photo, _ second: Photo) -> Bool {
        let firstDate = moment(of: first)?.date
        let secondDate = moment(of: second)?.date
        if firstDate != secondDate {
            guard let firstDate else { return false }
            guard let secondDate else { return true }
            return firstDate > secondDate
        }
        return first.id > second.id
    }

    /// The photos in the order the server lists them.
    public static func sorted(_ photos: [Photo]) -> [Photo] {
        photos.sorted(by: isListedBefore)
    }

    /// The date and time of a photo as the viewer's Info shows them, read in
    /// the zone the photo was taken in when it says one. Nil for no date.
    public static func takenText(of photo: Photo, phoneTimeZone: TimeZone = .current, locale: Locale = .autoupdatingCurrent) -> String? {
        guard let moment = moment(of: photo, phoneTimeZone: phoneTimeZone) else { return nil }
        return moment.date.formatted(Date.FormatStyle(date: .complete, time: .shortened, locale: locale, timeZone: moment.timeZone))
    }

    /// What the chip of a photo says when the person asks the agent about it:
    /// the place and the day it was taken, on the clock where it was taken, or
    /// the file name. The server words it again when the question is sent, the
    /// same way, so the chip does not change. Empty when the photo has neither.
    public static func referenceTitle(of photo: Photo, phoneTimeZone: TimeZone = .current) -> String {
        var parts: [String] = []
        let placeName = photo.placeName.trimmingCharacters(in: .whitespacesAndNewlines)
        if !placeName.isEmpty { parts.append(placeName) }
        if photo.takenAt != nil, let moment = moment(of: photo, phoneTimeZone: phoneTimeZone) {
            var calendar = Calendar(identifier: .gregorian)
            calendar.timeZone = moment.timeZone
            let components = calendar.dateComponents([.year, .month, .day], from: moment.date)
            if let year = components.year, let month = components.month, let day = components.day {
                parts.append(String(format: "%04ld-%02ld-%02ld", year, month, day))
            }
        }
        if parts.isEmpty { return photo.originalFilename.trimmingCharacters(in: .whitespacesAndNewlines) }
        return parts.joined(separator: " \u{00B7} ")
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

/// PhotoDownload is a photo's file read from the server into a file of the
/// app's own, to be shared or saved to the phone's Photos. The caller removes
/// the folder the file is in when it is done with it.
public struct PhotoDownload: Sendable {
    public let fileURL: URL
    /// The name the file has, with an extension the phone's Photos knows the
    /// kind of file by.
    public let filename: String
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

/// PhotoPlaybackSource is what an address for a player or a download task is
/// asked for: one smaller copy of a photo, or one of the files it is made of.
public enum PhotoPlaybackSource: Sendable, Equatable {
    /// A photoPreviewSize: "small", "large" or "video".
    case preview(String)
    /// A photoFileRole: "original", "edited" or "liveVideo".
    case file(String)
}

extension Connection {
    /// An address for one preview or file of a photo that opens without
    /// signing in and serves byte ranges, for six hours: for a player, which
    /// cannot send the token and must not read a whole video before it plays,
    /// and for a download task. Each playback asks for a fresh one.
    public func photoPlaybackAddress(_ photoId: String, source: PhotoPlaybackSource) async throws -> URL {
        struct Answer: Decodable, Sendable { let PhotoPlaybackAddress: String }
        var variables: [String: JSONValue] = ["photoId": .string(photoId)]
        switch source {
        case .preview(let photoPreviewSize): variables["photoPreviewSize"] = .string(photoPreviewSize)
        case .file(let photoFileRole): variables["photoFileRole"] = .string(photoFileRole)
        }
        let path = try await execute(Documents.photoPlaybackAddress, variables: variables, as: Answer.self).PhotoPlaybackAddress
        return try absoluteAddress(of: path)
    }

    /// The original file of a photo, or, when the server says it has none for
    /// this person, the large preview (the video preview, for a video) in its
    /// place. The bytes go straight to a file with a download task and are
    /// never held in memory, since an original can be hundreds of megabytes.
    ///
    /// Only "there is no such file" falls back to a preview: a network error
    /// or a server that failed is thrown, so that a smaller copy is never
    /// saved because of a bad connection. `isOriginal` says which came.
    public func downloadPhotoFile(_ photo: Photo) async throws -> PhotoDownload {
        let detail = try await photoDetail(photo.id)
        if let file = detail.originalFile {
            do {
                let fileURL = try await downloadSigned(photo.id, source: .file(file.photoFileRole), filename: file.originalFilename.isEmpty ? photo.filename : file.originalFilename)
                return PhotoDownload(fileURL: fileURL, filename: fileURL.lastPathComponent, isOriginal: true)
            } catch PhotoError.fileNotAvailable {
                // The one case a preview stands in.
            }
        }
        let previewSize = photo.isVideo ? "video" : "large"
        let preview = detail.photoPreviews.first { $0.photoPreviewSize == previewSize }
        guard preview != nil || (photo.isVideo ? photo.videoPath : photo.viewerPreviewPath) != nil else {
            throw PhotoError.nothingToSaveYet
        }
        let fileExtension = preview.flatMap { PhotoDownload.fileExtension(forContentType: $0.contentType) } ?? (photo.isVideo ? "mp4" : "jpg")
        let baseName = (photo.filename as NSString).deletingPathExtension
        let fileURL = try await downloadSigned(photo.id, source: .preview(previewSize), filename: "\(baseName).\(fileExtension)")
        return PhotoDownload(fileURL: fileURL, filename: fileURL.lastPathComponent, isOriginal: false)
    }

    /// One file of a photo, such as its edited version or a Live Photo's
    /// video, with no other standing in for it: it throws when the photo has
    /// no such file.
    public func downloadPhotoFile(_ photo: Photo, role photoFileRole: String) async throws -> PhotoDownload {
        let detail = try await photoDetail(photo.id)
        guard let file = detail.file(withRole: photoFileRole) else { throw PhotoError.nothingToSaveYet }
        let fileURL = try await downloadSigned(photo.id, source: .file(photoFileRole), filename: file.originalFilename.isEmpty ? photo.filename : file.originalFilename)
        return PhotoDownload(fileURL: fileURL, filename: fileURL.lastPathComponent, isOriginal: photoFileRole == "original")
    }

    /// Mints an address and reads what it serves into a file named filename,
    /// in a folder of its own so that the file can keep its name.
    private func downloadSigned(_ photoId: String, source: PhotoPlaybackSource, filename: String) async throws -> URL {
        let address = try await photoPlaybackAddress(photoId, source: source)
        return try await downloadFile(from: address, filename: filename)
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

    /// Makes an album holding the photos, which may be none, for the person or,
    /// with a circle's id, for the circle. The photos go in
    /// batches the server accepts: the first batch makes the album and the
    /// rest are added to it.
    public func createPhotoAlbum(title: String, photoIds: [String] = [], circleId: String? = nil) async throws -> PhotoAlbum {
        struct Answer: Decodable, Sendable { let CreatePhotoAlbum: PhotoAlbum }
        let batches = PhotoBatches.batches(of: photoIds)
        let first = batches.first ?? []
        var variables: [String: JSONValue] = ["albumTitle": .string(PhotoAlbumText.trimmedTitle(title)), "photoIds": .array(first.map { .string($0) })]
        // An album for a circle belongs to the circle: every member adds to it.
        if let circleId, !circleId.isEmpty { variables["circleId"] = .string(circleId) }
        let created = try await execute(Documents.createPhotoAlbum, variables: variables, as: Answer.self).CreatePhotoAlbum
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

// MARK: People

extension Connection {
    /// The groups of faces worth showing: those a contact names first, then the
    /// biggest of the rest. The ones the person hid come only when asked for.
    public func photoFaceGroups(isHiddenIncluded: Bool = false) async throws -> [PhotoFaceGroup] {
        struct Answer: Decodable, Sendable { let ListPhotoFaceGroups: [PhotoFaceGroup] }
        return try await execute(
            Documents.listPhotoFaceGroups, variables: ["isHiddenIncluded": .bool(isHiddenIncluded)], as: Answer.self
        ).ListPhotoFaceGroups
    }

    public func photoFaceGroup(_ photoFaceGroupId: String) async throws -> PhotoFaceGroup {
        struct Answer: Decodable, Sendable { let ReadPhotoFaceGroup: PhotoFaceGroup }
        return try await execute(
            Documents.readPhotoFaceGroup, variables: ["photoFaceGroupId": .string(photoFaceGroupId)], as: Answer.self
        ).ReadPhotoFaceGroup
    }

    /// Names a group by a contact from the person's address books. A contact
    /// names one person, so a contact that names another of the person's groups
    /// merges the two; the answer says what happened.
    public func namePhotoFaceGroup(_ photoFaceGroupId: String, contactId: String) async throws -> NamedPhotoFaceGroup {
        struct Answer: Decodable, Sendable { let NamePhotoFaceGroup: NamedPhotoFaceGroup }
        return try await execute(
            Documents.namePhotoFaceGroup, variables: ["photoFaceGroupId": .string(photoFaceGroupId), "contactId": .string(contactId)], as: Answer.self
        ).NamePhotoFaceGroup
    }

    public func unnamePhotoFaceGroup(_ photoFaceGroupId: String) async throws -> PhotoFaceGroup {
        struct Answer: Decodable, Sendable { let UnnamePhotoFaceGroup: PhotoFaceGroup }
        return try await execute(
            Documents.unnamePhotoFaceGroup, variables: ["photoFaceGroupId": .string(photoFaceGroupId)], as: Answer.self
        ).UnnamePhotoFaceGroup
    }

    /// Puts groups into the first, which stays and takes the faces of the
    /// others, and their name when it has none.
    public func mergePhotoFaceGroups(_ photoFaceGroupIds: [String]) async throws -> PhotoFaceGroup {
        struct Answer: Decodable, Sendable { let MergePhotoFaceGroups: PhotoFaceGroup }
        return try await execute(
            Documents.mergePhotoFaceGroups, variables: ["photoFaceGroupIds": .array(photoFaceGroupIds.map { .string($0) })], as: Answer.self
        ).MergePhotoFaceGroups
    }

    /// "This is not them": takes a face out of its group, which it is not put
    /// back into.
    public func removePhotoFaceFromGroup(_ photoFaceId: String) async throws -> PhotoFaceGroup {
        struct Answer: Decodable, Sendable { let RemovePhotoFaceFromGroup: PhotoFaceGroup }
        return try await execute(
            Documents.removePhotoFaceFromGroup, variables: ["photoFaceId": .string(photoFaceId)], as: Answer.self
        ).RemovePhotoFaceFromGroup
    }

    public func hidePhotoFaceGroup(_ photoFaceGroupId: String, isHidden: Bool) async throws -> PhotoFaceGroup {
        struct Answer: Decodable, Sendable { let HidePhotoFaceGroup: PhotoFaceGroup }
        return try await execute(
            Documents.hidePhotoFaceGroup, variables: ["photoFaceGroupId": .string(photoFaceGroupId), "isHidden": .bool(isHidden)], as: Answer.self
        ).HidePhotoFaceGroup
    }

    /// Switches the finding of faces in the person's photos on or off. Off
    /// deletes every face and group found so far, with the names given them.
    @discardableResult
    public func setPhotoFaceRecognition(isEnabled: Bool) async throws -> Bool {
        struct Answer: Decodable, Sendable { let SetPhotoFaceRecognition: Bool }
        return try await execute(
            Documents.setPhotoFaceRecognition, variables: ["isEnabled": .bool(isEnabled)], as: Answer.self
        ).SetPhotoFaceRecognition
    }

    /// The contacts of the person's address books that match the words, for
    /// choosing who a group of faces is. A first few of each book.
    public func photoNamingContacts(matching words: String) async throws -> [ContactSummary] {
        var contacts: [ContactSummary] = []
        for book in try await addressBooks() {
            contacts += try await self.contacts(addressBookId: book.id, matching: words.trimmingCharacters(in: .whitespacesAndNewlines), first: PhotoNaming.contactsPerBook)
        }
        return contacts
    }

    /// Makes a contact that has only a name, in the person's first address
    /// book, for a person who is not in the address book yet, and returns it.
    public func createNamedContact(_ name: String) async throws -> ContactSummary {
        struct Answer: Decodable, Sendable {
            struct Saved: Decodable, Sendable {
                let id: String
                let name: String?
            }
            let SaveContact: Saved
        }
        guard let book = try await addressBooks().first else {
            throw PhotoError.noAddressBook
        }
        let saved = try await execute(
            Documents.createNamedContact, variables: ["addressBookId": .string(book.id), "name": .string(name)], as: Answer.self
        ).SaveContact
        return ContactSummary(id: saved.id, name: saved.name, organization: nil, emails: nil, phones: nil, hasPhoto: nil, addresses: nil)
    }
}

/// PhotoNaming is the rules of the dialog that names a person from the address
/// book.
public enum PhotoNaming {
    /// How many contacts of each address book are listed: more than fits
    /// without scrolling, few enough that the search is how one is found.
    public static let contactsPerBook = 20

    /// The name as it is saved: without the spaces round it.
    public static func trimmedName(_ name: String) -> String {
        name.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// Whether to offer a new contact for what was typed: when something was
    /// typed and no contact listed has exactly that name, which is chosen from
    /// the list instead of being made again.
    public static func offersNewContact(typed: String, contacts: [ContactSummary]) -> Bool {
        let name = trimmedName(typed)
        guard !name.isEmpty else { return false }
        return !contacts.contains { $0.shownName.compare(name, options: [.caseInsensitive, .diacriticInsensitive]) == .orderedSame }
    }

    /// What a face's name is shown as, "Not <name>" for taking it out of the
    /// group: the name, or nil when the group is nobody's.
    public static func shownName(of group: PhotoFaceGroup) -> String? {
        group.isNamed ? group.trimmedContactName : nil
    }
}
