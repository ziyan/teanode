#if DEBUG
    import SwiftUI
    import TeaNodeKit
    import UIKit

    /// What the demo server answers for Photos, for `-TeaNodeTab photos` with
    /// `-TeaNodeScreen photos-library` (every photo under Library, All), `photos-library-months` and
    /// `photos-library-years` (the same under Months and Years), `photos-viewer`, `photos-viewer-info`
    /// and `photos-viewer-described` (Info over the viewer), `photos-viewer-hidden` (the viewer after a
    /// tap, with the bars gone), `photos-video-viewer` (a video, stopped a third of the way in, with its
    /// controls), `photos-trash`,
    /// `photos-empty`, `photos-select`, `photos-trash-select`, `photos-search`, `photos-viewer-failed`,
    /// `photos-albums`, `photos-album`, `photos-people`, `photos-person`,
    /// `photos-shared-albums`, `photos-share`, `photos-shared-album`, `photos-shared-viewer`,
    /// `photos-filter`, `photos-filter-dates`, `photos-near`, `photos-change-date`, `photos-location` (the cities
    /// to choose from, over Info),
    /// `photos-shared-info` (Info over a photo another person shared), `photos-share-stop` (the question
    /// before a share is stopped) or `photos-new-circle-album` (the question for the title of a circle's album)
    /// (and `photos-faces`, `photos-conversation-card`, `photos-backup-background`, `photos-backup-paused`, `photos-backup-other-account` and
    /// `settings-photo-backup-other-account`, in Settings). The photos are invented, and their pictures are drawn
    /// here: a gradient and a few shapes in a color taken from the photo's id.
    extension DemoServer {
        private struct DemoPhoto {
            let id: String
            let kind: String
            let takenAt: String
            let offsetMinutes: Int
            var durationMS = 0
            var isFavorite = false
            var processingState = "ready"
            var isTrashed = false
            /// How long ago it was moved to the Trash, so that the Trash's groups and the days left do not
            /// depend on the day the screenshots are taken.
            var trashedDaysAgo = 2
            var placeName = ""
            var isPortrait = false
            /// Where it was taken, if the server tells the reader.
            var latitude: Double?
            var longitude: Double?
            /// The files the server holds for the photo, such as "edited".
            var fileRoles = ["original"]
            var failureMessage = ""
            /// Whose the photo is when it is another person's, seen through a shared album.
            var ownerName = ""

            var isForeign: Bool { !ownerName.isEmpty }
        }

        /// An invented place for the photos of Riverside Park.
        static let demoRiversideLatitude = 10.5
        static let demoRiversideLongitude = 20.5
        private static var riversideLatitude: Double { demoRiversideLatitude }
        private static var riversideLongitude: Double { demoRiversideLongitude }

        /// The distance in kilometers between two places, near enough for a demo.
        private static func kilometers(fromLatitude: Double, fromLongitude: Double, toLatitude: Double, toLongitude: Double) -> Double {
            let radians = Double.pi / 180
            let latitudeSpan = (toLatitude - fromLatitude) * radians
            let longitudeSpan = (toLongitude - fromLongitude) * radians
            let haversine =
                pow(sin(latitudeSpan / 2), 2) + cos(fromLatitude * radians) * cos(toLatitude * radians) * pow(sin(longitudeSpan / 2), 2)
            return 2 * 6371 * asin(min(1, sqrt(haversine)))
        }

        private static let demoPhotos: [DemoPhoto] = [
            DemoPhoto(id: "demo-01", kind: "picture", takenAt: "2026-10-15T18:42:00Z", offsetMinutes: -240, isFavorite: true, placeName: "Riverside Park", isPortrait: true,
                latitude: riversideLatitude, longitude: riversideLongitude),
            DemoPhoto(
                id: "demo-02", kind: "picture", takenAt: "2026-10-15T18:40:00Z", offsetMinutes: -240, placeName: "Riverside Park", latitude: riversideLatitude,
                longitude: riversideLongitude, fileRoles: ["original", "edited"]),
            DemoPhoto(id: "demo-03", kind: "video", takenAt: "2026-10-15T12:05:00Z", offsetMinutes: -240, durationMS: 23_000),
            DemoPhoto(id: "demo-04", kind: "picture", takenAt: "2026-10-15T09:30:00Z", offsetMinutes: -240, processingState: "waiting"),
            DemoPhoto(id: "demo-05", kind: "picture", takenAt: "2026-10-14T21:10:00Z", offsetMinutes: -240, isPortrait: true),
            DemoPhoto(
                id: "demo-06", kind: "picture", takenAt: "2026-10-14T15:00:00Z", offsetMinutes: -240, isFavorite: true, placeName: "Riverside Park",
                latitude: riversideLatitude + 0.003, longitude: riversideLongitude + 0.003),
            DemoPhoto(id: "demo-07", kind: "video", takenAt: "2026-10-14T14:00:00Z", offsetMinutes: -240, durationMS: 83_000),
            DemoPhoto(
                id: "demo-08", kind: "picture", takenAt: "2026-10-14T10:00:00Z", offsetMinutes: -240, processingState: "failed",
                failureMessage: "The picture could not be read, so no preview was made."),
            DemoPhoto(id: "demo-09", kind: "picture", takenAt: "2026-10-12T08:00:00Z", offsetMinutes: 540, isFavorite: true, placeName: "Harbor Market", latitude: 10.9, longitude: 20.9),
            DemoPhoto(
                id: "demo-10", kind: "picture", takenAt: "2026-10-12T05:20:00Z", offsetMinutes: 540, placeName: "Harbor Market", isPortrait: true, latitude: 10.9,
                longitude: 20.9),
            DemoPhoto(id: "demo-11", kind: "picture", takenAt: "2026-10-12T03:45:00Z", offsetMinutes: 540),
            DemoPhoto(id: "demo-12", kind: "video", takenAt: "2026-10-11T22:10:00Z", offsetMinutes: 540, durationMS: 3_725_000),
            DemoPhoto(id: "demo-13", kind: "picture", takenAt: "2026-10-02T16:00:00Z", offsetMinutes: -240),
            DemoPhoto(id: "demo-14", kind: "picture", takenAt: "2026-10-02T15:30:00Z", offsetMinutes: -240, isPortrait: true),
            DemoPhoto(id: "demo-15", kind: "picture", takenAt: "2026-10-02T11:00:00Z", offsetMinutes: -240),
            DemoPhoto(id: "demo-16", kind: "picture", takenAt: "2026-10-02T10:00:00Z", offsetMinutes: -240),
            DemoPhoto(id: "demo-17", kind: "picture", takenAt: "2026-10-01T09:00:00Z", offsetMinutes: -240),
            DemoPhoto(id: "demo-18", kind: "picture", takenAt: "2026-10-01T08:00:00Z", offsetMinutes: -240),
            DemoPhoto(id: "demo-19", kind: "picture", takenAt: "2026-09-30T19:00:00Z", offsetMinutes: -240),
            DemoPhoto(id: "demo-20", kind: "picture", takenAt: "2026-09-30T18:00:00Z", offsetMinutes: -240),
            // Older photos, so that Months and Years have more than one picture each.
            DemoPhoto(id: "demo-21", kind: "picture", takenAt: "2026-08-14T17:00:00Z", offsetMinutes: -240, isPortrait: true),
            DemoPhoto(id: "demo-22", kind: "picture", takenAt: "2026-08-02T10:00:00Z", offsetMinutes: -240),
            DemoPhoto(id: "demo-23", kind: "picture", takenAt: "2026-03-02T12:00:00Z", offsetMinutes: -240),
            DemoPhoto(id: "demo-24", kind: "video", takenAt: "2025-11-23T09:00:00Z", offsetMinutes: -300, durationMS: 12_000),
            DemoPhoto(id: "demo-25", kind: "picture", takenAt: "2025-07-04T15:00:00Z", offsetMinutes: -240, isPortrait: true),
            DemoPhoto(id: "demo-s1", kind: "picture", takenAt: "2026-09-20T10:00:00Z", offsetMinutes: -240, isPortrait: true, ownerName: "Sam Placeholder"),
            DemoPhoto(id: "demo-s2", kind: "picture", takenAt: "2026-09-20T09:00:00Z", offsetMinutes: -240, ownerName: "Sam Placeholder"),
            DemoPhoto(id: "demo-s3", kind: "video", takenAt: "2026-09-19T16:00:00Z", offsetMinutes: -240, durationMS: 41_000, ownerName: "Sam Placeholder"),
            DemoPhoto(id: "demo-s4", kind: "picture", takenAt: "2026-09-19T12:00:00Z", offsetMinutes: -240, ownerName: "Sam Placeholder"),
            DemoPhoto(id: "demo-s5", kind: "picture", takenAt: "2026-09-27T17:00:00Z", offsetMinutes: -240, ownerName: "Casey Example"),
            DemoPhoto(id: "demo-s6", kind: "picture", takenAt: "2026-09-27T16:00:00Z", offsetMinutes: -240, isPortrait: true, ownerName: "Casey Example"),
            DemoPhoto(id: "demo-t1", kind: "picture", takenAt: "2026-10-09T10:00:00Z", offsetMinutes: -240, isTrashed: true),
            DemoPhoto(id: "demo-t2", kind: "picture", takenAt: "2026-10-09T09:00:00Z", offsetMinutes: -240, isTrashed: true, isPortrait: true),
            DemoPhoto(id: "demo-t3", kind: "video", takenAt: "2026-10-08T17:00:00Z", offsetMinutes: -240, durationMS: 9_000, isTrashed: true, trashedDaysAgo: 9),
        ]

        private struct DemoAlbum {
            let id: String
            let title: String
            let photoIds: [String]
            let coverPhotoId: String
            /// Whose the album is when it is another person's, and the circle it belongs to.
            var ownerName = ""
            var circleName = ""
            var shareLevel = ""
            var canManage = true
            var canContribute = true

            var isOwned: Bool { ownerName.isEmpty && circleName.isEmpty }
        }

        private static let demoAlbums: [DemoAlbum] = [
            DemoAlbum(id: "demo-album-1", title: "Riverside weekend", photoIds: ["demo-01", "demo-02", "demo-03", "demo-04"], coverPhotoId: "demo-01"),
            DemoAlbum(id: "demo-album-2", title: "Harbor Market", photoIds: ["demo-09", "demo-10", "demo-11"], coverPhotoId: "demo-10"),
            DemoAlbum(id: "demo-album-3", title: "Autumn", photoIds: ["demo-05", "demo-06"], coverPhotoId: "demo-06"),
            DemoAlbum(
                id: "demo-album-4", title: "Lake weekend", photoIds: ["demo-s1", "demo-s2", "demo-s3", "demo-s4"], coverPhotoId: "demo-s1", ownerName: "Sam Placeholder",
                shareLevel: "view", canManage: false, canContribute: false),
            DemoAlbum(
                id: "demo-album-5", title: "Block party", photoIds: ["demo-s5", "demo-14", "demo-s6"], coverPhotoId: "demo-s5", circleName: "Neighbors", shareLevel: "contribute",
                canManage: false),
        ]

        /// The people and circles the demo account shares with.
        private static let demoPeopleJSON =
            #"[{"userId":"user-sam","username":"sam","name":"Sam Placeholder","avatarUrl":null,"avatarColorIndex":4},{"userId":"user-casey","username":"casey","name":"Casey Example","avatarUrl":null,"avatarColorIndex":7}]"#
        private static let demoCirclesJSON =
            #"[{"id":"circle-family","name":"Family","circleKind":"family","createdAt":"2026-01-01T00:00:00Z","memberRole":"admin","members":[],"pendingInvitations":[]},{"id":"circle-neighbors","name":"Neighbors","circleKind":"friends","createdAt":"2026-01-01T00:00:00Z","memberRole":"member","members":[],"pendingInvitations":[]}]"#
        private static let demoSharesJSON =
            #"[{"id":"share-1","photoAlbumId":"demo-album-1","granteeUserId":"user-sam","granteeUsername":"sam","granteeName":"Sam Placeholder","granteeAvatarUrl":null,"granteeAvatarColorIndex":4,"granteeCircleId":null,"granteeCircleName":"","shareLevel":"contribute","isLocationIncluded":true,"createdAt":"2026-10-12T10:00:00Z"},{"id":"share-2","photoAlbumId":"demo-album-1","granteeUserId":null,"granteeUsername":"","granteeName":"","granteeAvatarUrl":null,"granteeAvatarColorIndex":0,"granteeCircleId":"circle-family","granteeCircleName":"Family","shareLevel":"view","isLocationIncluded":false,"createdAt":"2026-10-12T10:05:00Z"}]"#

        /// The names in the photos of other people, by photo.
        private static let demoForeignNames: [String: [String]] = ["demo-s1": ["Sam Placeholder", "Robin Example"]]

        /// A person the demo server has found in the photos, with the photos they
        /// are in. Invented, like everything here; an empty name is a person
        /// nobody has named.
        private struct DemoFaceGroup {
            let id: String
            let name: String
            let photoIds: [String]
            var isHidden = false

            var coverFaceId: String { "face-\(id)" }
        }

        private static let demoFaceGroups: [DemoFaceGroup] = [
            DemoFaceGroup(id: "demo-person-1", name: "Robin Example", photoIds: ["demo-01", "demo-02", "demo-05", "demo-06", "demo-09", "demo-10"]),
            DemoFaceGroup(id: "demo-person-2", name: "Sam Placeholder", photoIds: ["demo-01", "demo-02", "demo-09", "demo-11"]),
            DemoFaceGroup(id: "demo-person-3", name: "", photoIds: ["demo-13", "demo-15", "demo-16"]),
        ]

        private static func faceGroupJSON(_ person: DemoFaceGroup) -> String {
            let photoCount = person.photoIds.count
            let contactId = person.name.isEmpty ? "" : "contact-" + person.id
            let bookId = person.name.isEmpty ? "" : "book-demo"
            return #"{"id":"\#(person.id)","createdAt":"2026-10-10T10:00:00Z","updatedAt":"2026-10-14T10:00:00Z","contactId":"\#(contactId)","contactAddressBookId":"\#(bookId)","contactName":"\#(person.name)","faceCount":\#(photoCount + 1),"photoCount":\#(photoCount),"facePictureUrl":"/api/v1/photos/faces/\#(person.coverFaceId)/picture","coverPhotoFaceId":"\#(person.coverFaceId)","isHidden":\#(person.isHidden)}"#
        }

        /// The faces the demo server found in one photo.
        private static func facesJSON(photoId: String) -> String {
            // The people in a photo another person shared are named as that person named them, and the
            // reader gets no group to open and no face picture.
            if let names = demoForeignNames[photoId] {
                return names.enumerated().map { index, name in
                    #"{"id":"face-shared-\#(photoId)-\#(index)","photoId":"\#(photoId)","boxLeftFraction":\#(0.2 + 0.3 * Double(index)),"boxTopFraction":0.3,"boxWidthFraction":0.2,"boxHeightFraction":0.2,"photoFaceGroupId":"","contactId":"","contactName":"\#(name)","facePictureUrl":""}"#
                }.joined(separator: ",")
            }
            return demoFaceGroups.filter { $0.photoIds.contains(photoId) }.enumerated().map { index, person in
                let contactId = person.name.isEmpty ? "" : "contact-" + person.id
                return #"{"id":"\#(person.coverFaceId)","photoId":"\#(photoId)","boxLeftFraction":\#(0.2 + 0.3 * Double(index)),"boxTopFraction":0.3,"boxWidthFraction":0.2,"boxHeightFraction":0.2,"photoFaceGroupId":"\#(person.id)","contactId":"\#(contactId)","contactName":"\#(person.name)","facePictureUrl":"/api/v1/photos/faces/\#(person.coverFaceId)/picture"}"#
            }.joined(separator: ",")
        }

        /// What the demo server says a photo shows: only the first photo has been
        /// described.
        private static func describedFields(photoId: String) -> String {
            if photoId == "demo-01" {
                return #","photoDescription":"A person in a green jacket stands on a riverside path at sunset, with a footbridge and a few trees behind them.","descriptionVisibleText":"RIVERSIDE PARK","describedAt":"2026-10-16T09:30:00Z","isDescriptionRequested":false"#
            }
            return #","photoDescription":"","descriptionVisibleText":"","describedAt":null,"isDescriptionRequested":false"#
        }

        private static func albumJSON(_ album: DemoAlbum) -> String {
            let members = album.photoIds.compactMap { photoId in demoPhotos.first { $0.id == photoId } }.filter { !$0.isTrashed }
            let dates = members.map(\.takenAt).sorted()
            let first = dates.first.map { #""\#($0)""# } ?? "null"
            let last = dates.last.map { #""\#($0)""# } ?? "null"
            let cover = members.first { $0.id == album.coverPhotoId } ?? members.first
            let coverPreviewUrl = cover.map { "/api/v1/photos/\($0.id)/previews/small?v=1" } ?? ""
            let circleId = album.circleName.isEmpty ? "null" : #""circle-neighbors""#
            let ownerUserId = album.ownerName.isEmpty ? "null" : #""user-sam""#
            let sharingFields =
                #","ownerUserId":\#(ownerUserId),"circleId":\#(circleId),"ownerUsername":"\#(album.ownerName.isEmpty ? "" : "sam")","ownerName":"\#(album.ownerName)","ownerAvatarUrl":null,"ownerAvatarColorIndex":4,"circleName":"\#(album.circleName)","isOwnedByCaller":\#(album.isOwned),"shareLevel":"\#(album.shareLevel)","canCallerContribute":\#(album.canContribute),"canCallerManage":\#(album.canManage),"isLocationIncluded":true"#
            return #"{"id":"\#(album.id)","albumTitle":"\#(album.title)","createdAt":"2026-10-10T10:00:00Z","updatedAt":"2026-10-14T10:00:00Z","coverPhotoId":null,"photoCount":\#(members.count),"firstTakenAt":\#(first),"lastTakenAt":\#(last),"coverPreviewUrl":"\#(coverPreviewUrl)"\#(sharingFields)}"#
        }

        /// The trips and events the demo server offers as albums.
        private static let demoSuggestionJSON =
            #"{"takenAfter":"2026-10-02T09:00:00Z","takenBefore":"2026-10-02T17:00:01Z","placeNames":["Fixture Lake"],"albumTitle":"Fixture Lake","photoCount":12,"samplePhotoIds":["demo-13","demo-14","demo-15","demo-16"]}"#

        private static func photoJSON(_ photo: DemoPhoto, extraFields: String = "") -> String {
            let isVideo = photo.kind == "video"
            let isReady = photo.processingState == "ready"
            let previewPath = "/api/v1/photos/\(photo.id)/previews/"
            let smallPreviewUrl = isReady ? previewPath + "small?v=1" : ""
            let largePreviewUrl = isReady ? previewPath + "large?v=1" : ""
            let videoPreviewUrl = isReady && isVideo ? previewPath + "video?v=1" : ""
            let trashedAt = photo.isTrashed ? "\"" + ISO8601DateFormatter().string(from: Date().addingTimeInterval(-Double(photo.trashedDaysAgo) * 86_400)) + "\"" : "null"
            // A photo another person shared without places reaches the reader with no file name and
            // no files of its own, only previews, as the server sends it.
            let fileRoles = (photo.isForeign ? [] : photo.fileRoles).map { "\"" + $0 + "\"" }.joined(separator: ",")
            let originalFilename = photo.isForeign ? "" : "IMG_\(photo.id.suffix(2)).\(isVideo ? "MOV" : "HEIC")"
            let latitude = photo.latitude.map { String($0) } ?? "null"
            let longitude = photo.longitude.map { String($0) } ?? "null"
            return #"{"id":"\#(photo.id)","createdAt":"\#(photo.takenAt)","photoKind":"\#(photo.kind)","takenAt":"\#(photo.takenAt)","takenAtOffsetMinutes":\#(photo.offsetMinutes),"widthPixels":\#(photo.isPortrait ? 3024 : 4032),"heightPixels":\#(photo.isPortrait ? 4032 : 3024),"durationMS":\#(photo.durationMS),"latitude":\#(latitude),"longitude":\#(longitude),"placeName":"\#(photo.placeName)","cameraMake":"Example","cameraModel":"Phone 1","originalFilename":"\#(originalFilename)","isFavorite":\#(photo.isFavorite),"isScreenshot":false,"photoProcessingState":"\#(photo.processingState)","trashedAt":\#(trashedAt),"smallPreviewUrl":"\#(smallPreviewUrl)","largePreviewUrl":"\#(largePreviewUrl)","videoPreviewUrl":"\#(videoPreviewUrl)","photoFileRoles":[\#(fileRoles)],"processingErrorMessage":"\#(photo.failureMessage)","isOwnedByCaller":\#(!photo.isForeign),"ownerName":"\#(photo.ownerName)"\#(extraFields)}"#
        }

        /// The photos of a visit in the Location demo, as the list of them the server sends with the visit.
        static func demoVisitPhotosJSON(_ photoIds: [String]) -> String {
            let photos = photoIds.compactMap { photoId in demoPhotos.first { $0.id == photoId } }
            return "[" + photos.map { photoJSON($0) }.joined(separator: ",") + "]"
        }

        /// What ReadPhotoPlace answers: the first photo was taken inside the person's Home place, any other
        /// photo with a position at a street, and a photo with none at nothing.
        private static func photoPositionPlaceJSON(photoId: String) -> String {
            guard let photo = demoPhotos.first(where: { $0.id == photoId }), photo.latitude != nil, !photo.isForeign else { return "null" }
            if photo.id == "demo-01" {
                return #"{"locationPlace":\#(LocationDemo.place(LocationDemo.homePlaceId).json),"addressText":""}"#
            }
            return #"{"locationPlace":null,"addressText":"3 Fixture Row, Exampleton"}"#
        }

        /// The answer to a request about photos: a GraphQL document, or a
        /// preview fetched by its address. Nil for any other request.
        static func photoAnswer(_ request: URLRequest) -> Data? {
            if let url = request.url, url.path.hasPrefix("/api/v1/photos/") {
                return photoPicture(path: url.path)
            }
            // The picture on the card of `photos-conversation-card`.
            if let url = request.url, url.path == "/api/v1/agent/attachments/demo-picture" {
                return photoPicture(path: "/api/v1/photos/demo-09/previews/large")
            }
            let body = String(decoding: request.httpBody ?? Data(), as: UTF8.self)
            let variables = (try? JSONSerialization.jsonObject(with: request.httpBody ?? Data()) as? [String: Any])?["variables"] as? [String: Any] ?? [:]
            let isEmpty = screen == "photos-empty"
            let json: String
            let isPhotosScreen = screen?.hasPrefix("photos-") == true
            if body.contains("ListCirclePeople"), isPhotosScreen {
                json = #"{"data":{"ListCirclePeople":\#(demoPeopleJSON)}}"#
            } else if body.contains("ListCircles"), isPhotosScreen {
                json = #"{"data":{"ListCircles":\#(demoCirclesJSON)}}"#
            } else if body.contains("ListPhotoAlbumShares") {
                json = #"{"data":{"ListPhotoAlbumShares":\#(demoSharesJSON)}}"#
            } else if body.contains("SharePhotoAlbum") {
                json = #"{"data":{"SharePhotoAlbum":{"id":"share-3","photoAlbumId":"demo-album-1","granteeUserId":"user-casey","granteeUsername":"casey","granteeName":"Casey Example","granteeAvatarUrl":null,"granteeAvatarColorIndex":7,"granteeCircleId":null,"granteeCircleName":"","shareLevel":"\#(variables["shareLevel"] as? String ?? "view")","isLocationIncluded":\#(variables["isLocationIncluded"] as? Bool ?? false),"createdAt":"2026-10-17T10:00:00Z"}}}"#
            } else if body.contains("UpdatePhotoAlbumShare") {
                json = #"{"data":{"UpdatePhotoAlbumShare":{"id":"share-1","photoAlbumId":"demo-album-1","granteeUserId":"user-sam","granteeUsername":"sam","granteeName":"Sam Placeholder","granteeAvatarUrl":null,"granteeAvatarColorIndex":4,"granteeCircleId":null,"granteeCircleName":"","shareLevel":"\#(variables["shareLevel"] as? String ?? "view")","isLocationIncluded":\#(variables["isLocationIncluded"] as? Bool ?? false),"createdAt":"2026-10-12T10:00:00Z"}}}"#
            } else if body.contains("RemovePhotoAlbumShare") {
                json = #"{"data":{"RemovePhotoAlbumShare":true}}"#
            } else if body.contains("SavePhotoToLibrary") || body.contains("SavePhotoFromAttachment") {
                json = #"{"data":{"\#(body.contains("SavePhotoToLibrary") ? "SavePhotoToLibrary" : "SavePhotoFromAttachment")":\#(photoJSON(demoPhotos[0]))}}"#
            } else if body.contains("PhotoPlaybackAddress") {
                // The one demo video, for any photo that asks to be played.
                json = #"{"data":{"PhotoPlaybackAddress":"\#(PhotosDemoVideo.fileURL().absoluteString)"}}"#
            } else if body.contains("ReadPhotoLibrary") {
                let inLibrary = isEmpty ? 0 : demoPhotos.filter { !$0.isTrashed && !$0.isForeign }.count
                let trashed = isEmpty ? 0 : demoPhotos.filter(\.isTrashed).count
                json = #"{"data":{"ReadPhotoLibrary":{"photoCount":\#(inLibrary),"videoCount":3,"favoriteCount":3,"trashedCount":\#(trashed),"usedBytes":412000000,"maximumFileBytes":2000000000,"maximumAccountBytes":50000000000,"trashRetentionDays":30,"isDescriptionAvailable":true,"descriptionUnavailableMessage":"","isFaceRecognitionAvailable":true,"faceRecognitionUnavailableMessage":"","isPhotoFaceRecognitionEnabled":true}}}"#
            } else if body.contains("ListPhotoAlbums") {
                json = #"{"data":{"ListPhotoAlbums":["# + (isEmpty ? [] : demoAlbums).map(albumJSON).joined(separator: ",") + "]}}"
            } else if body.contains("SuggestPhotoAlbums") {
                json = #"{"data":{"SuggestPhotoAlbums":["# + (isEmpty ? "" : demoSuggestionJSON) + "]}}"
            } else if body.contains("CreatePhotoAlbum") {
                let title = variables["albumTitle"] as? String ?? "New album"
                json = #"{"data":{"CreatePhotoAlbum":\#(albumJSON(DemoAlbum(id: "demo-album-new", title: title, photoIds: [], coverPhotoId: "")))}}"#
            } else if body.contains("RenamePhotoAlbum") {
                let album = demoAlbums.first { $0.id == (variables["photoAlbumId"] as? String) } ?? demoAlbums[0]
                json = #"{"data":{"RenamePhotoAlbum":\#(albumJSON(DemoAlbum(id: album.id, title: variables["albumTitle"] as? String ?? album.title, photoIds: album.photoIds, coverPhotoId: album.coverPhotoId)))}}"#
            } else if body.contains("DeletePhotoAlbum") {
                json = #"{"data":{"DeletePhotoAlbum":true}}"#
            } else if body.contains("AddPhotosToAlbum") {
                json = #"{"data":{"AddPhotosToAlbum":\#((variables["photoIds"] as? [Any])?.count ?? 0)}}"#
            } else if body.contains("RemovePhotosFromAlbum") {
                json = #"{"data":{"RemovePhotosFromAlbum":\#((variables["photoIds"] as? [Any])?.count ?? 0)}}"#
            } else if body.contains("SetPhotoAlbumCover") {
                let album = demoAlbums.first { $0.id == (variables["photoAlbumId"] as? String) } ?? demoAlbums[0]
                json = #"{"data":{"SetPhotoAlbumCover":\#(albumJSON(album))}}"#
            } else if body.contains("UnnamePhotoFaceGroup") {
                let person = demoFaceGroups.first { $0.id == (variables["photoFaceGroupId"] as? String) } ?? demoFaceGroups[0]
                json = #"{"data":{"UnnamePhotoFaceGroup":\#(faceGroupJSON(DemoFaceGroup(id: person.id, name: "", photoIds: person.photoIds)))}}"#
            } else if body.contains("NamePhotoFaceGroup") {
                let person = demoFaceGroups.first { $0.id == (variables["photoFaceGroupId"] as? String) } ?? demoFaceGroups[0]
                json = #"{"data":{"NamePhotoFaceGroup":{"photoFaceGroup":\#(faceGroupJSON(DemoFaceGroup(id: person.id, name: "Chris Placeholder", photoIds: person.photoIds))),"mergedPhotoFaceGroupId":"","isContactPictureSet":false}}}"#
            } else if body.contains("MergePhotoFaceGroups") {
                json = #"{"data":{"MergePhotoFaceGroups":\#(faceGroupJSON(demoFaceGroups[0]))}}"#
            } else if body.contains("RemovePhotoFaceFromGroup") {
                json = #"{"data":{"RemovePhotoFaceFromGroup":\#(faceGroupJSON(demoFaceGroups[2]))}}"#
            } else if body.contains("HidePhotoFaceGroup") {
                var person = demoFaceGroups.first { $0.id == (variables["photoFaceGroupId"] as? String) } ?? demoFaceGroups[0]
                person.isHidden = variables["isHidden"] as? Bool ?? false
                json = #"{"data":{"HidePhotoFaceGroup":\#(faceGroupJSON(person))}}"#
            } else if body.contains("SetPhotoFaceRecognition") {
                json = #"{"data":{"SetPhotoFaceRecognition":\#(variables["isEnabled"] as? Bool ?? true)}}"#
            } else if body.contains("ListPhotoFaceGroups") {
                json = #"{"data":{"ListPhotoFaceGroups":["# + (isEmpty ? [] : demoFaceGroups).map(faceGroupJSON).joined(separator: ",") + "]}}"
            } else if body.contains("ReadPhotoFaceGroup") {
                let person = demoFaceGroups.first { $0.id == (variables["photoFaceGroupId"] as? String) } ?? demoFaceGroups[0]
                json = #"{"data":{"ReadPhotoFaceGroup":\#(faceGroupJSON(person))}}"#
            } else if body.contains("DescribePhoto") {
                json = #"{"data":{"DescribePhoto":{"photoDescription":"","descriptionVisibleText":"","describedAt":null,"isDescriptionRequested":true}}}"#
            } else if body.contains("SearchPhotoPlaces") {
                json = #"{"data":{"SearchPhotoPlaces":\#(demoPlacesJSON(words: variables["query"] as? String ?? "", variables: variables))}}"#
            } else if body.contains("SetPhotoLocation") || body.contains("RemovePhotoLocation") {
                let name = body.contains("SetPhotoLocation") ? "SetPhotoLocation" : "RemovePhotoLocation"
                json = #"{"data":{"\#(name)":\#((variables["photoIds"] as? [Any])?.count ?? 0)}}"#
            } else if body.contains("RetryPhotoProcessing") {
                json = #"{"data":{"RetryPhotoProcessing":1}}"#
            } else if body.contains("ListPhotos") {
                let isTrashed = variables["isTrashed"] as? Bool == true
                let isFavorite = variables["isFavorite"] as? Bool == true
                // Photos of other people are seen only through the album that shares them.
                let isAlbumAsked = variables["albumId"] is String
                var chosen = demoPhotos.filter { $0.isTrashed == isTrashed && (isAlbumAsked || !$0.isForeign) }
                if isFavorite { chosen = chosen.filter(\.isFavorite) }
                if let photoKind = variables["photoKind"] as? String { chosen = chosen.filter { $0.kind == photoKind } }
                if let albumId = variables["albumId"] as? String {
                    let members = demoAlbums.first { $0.id == albumId }?.photoIds ?? []
                    chosen = chosen.filter { members.contains($0.id) }
                }
                if let faceGroupId = variables["faceGroupId"] as? String {
                    let members = demoFaceGroups.first { $0.id == faceGroupId }?.photoIds ?? []
                    chosen = chosen.filter { members.contains($0.id) }
                }
                if let latitude = variables["nearLatitude"] as? Double, let longitude = variables["nearLongitude"] as? Double,
                    let distance = variables["nearDistanceKilometers"] as? Double
                {
                    chosen = chosen.filter { photo in
                        guard let photoLatitude = photo.latitude, let photoLongitude = photo.longitude else { return false }
                        return kilometers(fromLatitude: latitude, fromLongitude: longitude, toLatitude: photoLatitude, toLongitude: photoLongitude) <= distance
                    }
                }
                if let after = variables["takenAfter"] as? String { chosen = chosen.filter { $0.takenAt >= after } }
                if let before = variables["takenBefore"] as? String { chosen = chosen.filter { $0.takenAt < before } }
                // A search keeps the photos whose place or file name has the words.
                if let words = variables["searchText"] as? String, !words.isEmpty {
                    chosen = demoPhotos.filter { !$0.isTrashed && !$0.isForeign && ($0.placeName.localizedCaseInsensitiveContains(words) || $0.id.localizedCaseInsensitiveContains(words)) }
                }
                // Everything fits in one page, so a page after it is empty.
                if isEmpty || variables["beforePhotoId"] is String { chosen = [] }
                json = #"{"data":{"ListPhotos":["# + chosen.map { photoJSON($0) }.joined(separator: ",") + "]}}"
            } else if body.contains("ReadPhotoPlace") {
                json = #"{"data":{"ReadPhotoPlace":\#(photoPositionPlaceJSON(photoId: variables["photoId"] as? String ?? ""))}}"#
            } else if body.contains("ReadPhoto") {
                let photoId = variables["photoId"] as? String ?? demoPhotos[0].id
                let photo = demoPhotos.first { $0.id == photoId } ?? demoPhotos[0]
                let isVideo = photo.kind == "video"
                let file =
                    #"{"photoFileRole":"original","contentType":"\#(isVideo ? "video/quicktime" : "image/heic")","fileSizeBytes":\#(isVideo ? 48_200_000 : 2_345_678),"originalFilename":"IMG_\#(photo.id.suffix(2)).\#(isVideo ? "MOV" : "HEIC")","fileUrl":"/api/v1/photos/\#(photo.id)/files/original"}"#
                let albums = demoAlbums.filter { $0.photoIds.contains(photo.id) }.map { #"{"id":"\#($0.id)","albumTitle":"\#($0.title)"}"# }.joined(separator: ",")
                json = #"{"data":{"ReadPhoto":{"photo":\#(photoJSON(photo, extraFields: describedFields(photoId: photo.id))),"photoFiles":[\#(photo.isForeign ? "" : file)],"photoPreviews":[],"albums":[\#(albums)],"faces":[\#(facesJSON(photoId: photo.id))]}}}"#
            } else if body.contains("UpdatePhoto") {
                var photo = demoPhotos.first { $0.id == (variables["photoId"] as? String) } ?? demoPhotos[0]
                photo.isFavorite = variables["isFavorite"] as? Bool ?? photo.isFavorite
                json = #"{"data":{"UpdatePhoto":\#(photoJSON(photo))}}"#
            } else if body.contains("TrashPhotos") {
                json = #"{"data":{"TrashPhotos":1}}"#
            } else if body.contains("RestorePhotos") {
                json = #"{"data":{"RestorePhotos":1}}"#
            } else if body.contains("DeletePhotos") {
                json = #"{"data":{"DeletePhotos":1}}"#
            } else if body.contains("EmptyPhotoTrash") {
                json = #"{"data":{"EmptyPhotoTrash":3}}"#
            } else {
                return nil
            }
            return Data(json.utf8)
        }

        private struct DemoCity {
            let placeName: String
            let latitude: Double
            let longitude: Double
        }

        /// Cities the demo can place a photo at, as the server names them.
        private static let demoCities: [DemoCity] = [
            DemoCity(placeName: "Lisbon, Portugal", latitude: 38.72, longitude: -9.14),
            DemoCity(placeName: "Lima, Peru", latitude: -12.05, longitude: -77.04),
            DemoCity(placeName: "Lille, France", latitude: 50.63, longitude: 3.06),
            DemoCity(placeName: "Liverpool, United Kingdom", latitude: 53.41, longitude: -2.99),
            DemoCity(placeName: "Lyon, France", latitude: 45.76, longitude: 4.84),
            DemoCity(placeName: "Leipzig, Germany", latitude: 51.34, longitude: 12.37),
        ]

        /// The cities whose name has the words, nearest to the point asked about first when one was
        /// given (and then each says how far it is), otherwise in the order above.
        private static func demoPlacesJSON(words: String, variables: [String: Any]) -> String {
            var cities = demoCities.filter { $0.placeName.localizedCaseInsensitiveContains(words) }
            var distances: [String: Double] = [:]
            if let nearLatitude = variables["nearLatitude"] as? Double, let nearLongitude = variables["nearLongitude"] as? Double {
                for city in cities {
                    distances[city.placeName] = kilometers(fromLatitude: nearLatitude, fromLongitude: nearLongitude, toLatitude: city.latitude, toLongitude: city.longitude)
                }
                cities.sort { (distances[$0.placeName] ?? 0) < (distances[$1.placeName] ?? 0) }
            }
            var objects: [String] = []
            for city in cities {
                let distanceText = distances[city.placeName].map { String(format: "%.1f", $0) } ?? "null"
                objects.append(#"{"placeName":"\#(city.placeName)","latitude":\#(city.latitude),"longitude":\#(city.longitude),"distanceKilometers":\#(distanceText)}"#)
            }
            return "[" + objects.joined(separator: ",") + "]"
        }

        /// A picture for a face's address, `/api/v1/photos/faces/<id>/picture`: a
        /// colored disc with the person's initials, so it is plainly not a real face.
        private static func facePicture(faceId: String) -> Data {
            let personId = faceId.hasPrefix("face-") ? String(faceId.dropFirst(5)) : faceId
            let name = demoFaceGroups.first { $0.id == personId }?.name ?? ""
            let initials = name.split(separator: " ").compactMap(\.first).prefix(2).map(String.init).joined()
            let hue = CGFloat(personId.utf8.reduce(0) { ($0 &* 31 &+ Int($1)) % 360 }) / 360
            let canvas = CGSize(width: 240, height: 240)
            let format = UIGraphicsImageRendererFormat()
            format.scale = 1
            return UIGraphicsImageRenderer(size: canvas, format: format).pngData { context in
                UIColor(hue: hue, saturation: 0.35, brightness: 0.95, alpha: 1).setFill()
                context.fill(CGRect(origin: .zero, size: canvas))
                UIColor(hue: hue, saturation: 0.6, brightness: 0.8, alpha: 1).setFill()
                context.cgContext.fillEllipse(in: CGRect(x: 30, y: 30, width: 180, height: 180))
                let text = (initials.isEmpty ? "?" : initials) as NSString
                let attributes: [NSAttributedString.Key: Any] = [.font: UIFont.systemFont(ofSize: 72, weight: .semibold), .foregroundColor: UIColor.white]
                let size = text.size(withAttributes: attributes)
                text.draw(at: CGPoint(x: (canvas.width - size.width) / 2, y: (canvas.height - size.height) / 2), withAttributes: attributes)
            }
        }

        /// A picture for a preview address: `/api/v1/photos/<id>/previews/<size>`.
        private static func photoPicture(path: String) -> Data {
            let parts = path.split(separator: "/").map(String.init)
            // /api/v1/photos/faces/<face id>/picture
            if parts.count > 5, parts[3] == "faces" { return facePicture(faceId: parts[4]) }
            let photoId = parts.count > 3 ? parts[3] : "demo"
            let size = parts.last ?? "small"
            let photo = demoPhotos.first { $0.id == photoId }
            let isPortrait = photo?.isPortrait ?? false
            let longSide: CGFloat = size == "small" ? 320 : 960
            let canvas = isPortrait ? CGSize(width: longSide * 0.75, height: longSide) : CGSize(width: longSide, height: longSide * 0.75)
            // A hue from the id's bytes, the same every time.
            let hue = CGFloat(photoId.utf8.reduce(0) { ($0 &* 31 &+ Int($1)) % 360 }) / 360
            let format = UIGraphicsImageRendererFormat()
            format.scale = 1
            return UIGraphicsImageRenderer(size: canvas, format: format).pngData { context in
                let space = CGColorSpaceCreateDeviceRGB()
                let top = UIColor(hue: hue, saturation: 0.35, brightness: 0.95, alpha: 1).cgColor
                let bottom = UIColor(hue: (hue + 0.08).truncatingRemainder(dividingBy: 1), saturation: 0.6, brightness: 0.6, alpha: 1).cgColor
                if let gradient = CGGradient(colorsSpace: space, colors: [top, bottom] as CFArray, locations: [0, 1]) {
                    context.cgContext.drawLinearGradient(gradient, start: .zero, end: CGPoint(x: 0, y: canvas.height), options: [])
                }
                // A sun and two hills.
                UIColor(white: 1, alpha: 0.85).setFill()
                context.cgContext.fillEllipse(in: CGRect(x: canvas.width * 0.6, y: canvas.height * 0.15, width: canvas.width * 0.18, height: canvas.width * 0.18))
                UIColor(hue: hue, saturation: 0.55, brightness: 0.4, alpha: 0.9).setFill()
                context.cgContext.fillEllipse(in: CGRect(x: -canvas.width * 0.2, y: canvas.height * 0.62, width: canvas.width * 1.1, height: canvas.height * 0.9))
                UIColor(hue: hue, saturation: 0.6, brightness: 0.28, alpha: 0.95).setFill()
                context.cgContext.fillEllipse(in: CGRect(x: canvas.width * 0.3, y: canvas.height * 0.72, width: canvas.width * 1.1, height: canvas.height * 0.9))
            }
        }
    }

    /// PhotoConversationCardDemo is the cards a file gets in a conversation, on their own:
    /// `-TeaNodeScreen photos-conversation-card`. A picture shows its name, size and caption with no
    /// buttons (a tap opens it; Save to my library, Share and Copy are in its long-press menu), and a
    /// document shows a chevron, since a tap on the card opens it. The picture is drawn by the demo
    /// server.
    struct PhotoConversationCardDemo: View {
        private let files = [
            #"{"attachment_id":"demo-picture","url":"/api/v1/agent/attachments/demo-picture","name":"harbor-market.jpg","content_type":"image/jpeg","size":2345678,"caption":"The market by the harbor."}"#,
            #"{"attachment_id":"demo-document","url":"/api/v1/agent/attachments/demo-document","name":"market-stalls-2024-summary.pdf","content_type":"application/pdf","size":183500,"caption":""}"#,
        ].compactMap { AgentFile.of(toolResult: $0).first }

        var body: some View {
            ScrollView {
                VStack(spacing: 12) {
                    ForEach(files, id: \.attachmentId) { file in
                        FileCard(file: file)
                    }
                }
                .padding()
            }
            .navigationBarTitleDisplayMode(.inline)
        }
    }
#endif
