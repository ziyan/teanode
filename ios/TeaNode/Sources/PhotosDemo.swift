#if DEBUG
    import SwiftUI
    import TeaNodeKit
    import UIKit

    /// What the demo server answers for Photos, for `-TeaNodeTab photos` with
    /// `-TeaNodeScreen photos-library`, `photos-viewer`, `photos-viewer-described`, `photos-trash`,
    /// `photos-empty`, `photos-select`, `photos-trash-select`, `photos-search`, `photos-viewer-failed`,
    /// `photos-albums`, `photos-album`, `photos-people` or `photos-person`
    /// (and `photos-faces`, in Settings). The photos are invented, and their pictures are drawn
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
            var placeName = ""
            var isPortrait = false
            /// The files the server holds for the photo, such as "edited".
            var fileRoles = ["original"]
            var failureMessage = ""
        }

        private static let demoPhotos: [DemoPhoto] = [
            DemoPhoto(id: "demo-01", kind: "picture", takenAt: "2026-10-15T18:42:00Z", offsetMinutes: -240, isFavorite: true, placeName: "Riverside Park", isPortrait: true),
            DemoPhoto(id: "demo-02", kind: "picture", takenAt: "2026-10-15T18:40:00Z", offsetMinutes: -240, placeName: "Riverside Park", fileRoles: ["original", "edited"]),
            DemoPhoto(id: "demo-03", kind: "video", takenAt: "2026-10-15T12:05:00Z", offsetMinutes: -240, durationMS: 23_000),
            DemoPhoto(id: "demo-04", kind: "picture", takenAt: "2026-10-15T09:30:00Z", offsetMinutes: -240, processingState: "waiting"),
            DemoPhoto(id: "demo-05", kind: "picture", takenAt: "2026-10-14T21:10:00Z", offsetMinutes: -240, isPortrait: true),
            DemoPhoto(id: "demo-06", kind: "picture", takenAt: "2026-10-14T15:00:00Z", offsetMinutes: -240, isFavorite: true),
            DemoPhoto(id: "demo-07", kind: "video", takenAt: "2026-10-14T14:00:00Z", offsetMinutes: -240, durationMS: 83_000),
            DemoPhoto(
                id: "demo-08", kind: "picture", takenAt: "2026-10-14T10:00:00Z", offsetMinutes: -240, processingState: "failed",
                failureMessage: "The picture could not be read, so no preview was made."),
            DemoPhoto(id: "demo-09", kind: "picture", takenAt: "2026-10-12T08:00:00Z", offsetMinutes: 540, isFavorite: true, placeName: "Harbor Market"),
            DemoPhoto(id: "demo-10", kind: "picture", takenAt: "2026-10-12T05:20:00Z", offsetMinutes: 540, placeName: "Harbor Market", isPortrait: true),
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
            DemoPhoto(id: "demo-t1", kind: "picture", takenAt: "2026-10-09T10:00:00Z", offsetMinutes: -240, isTrashed: true),
            DemoPhoto(id: "demo-t2", kind: "picture", takenAt: "2026-10-09T09:00:00Z", offsetMinutes: -240, isTrashed: true, isPortrait: true),
            DemoPhoto(id: "demo-t3", kind: "video", takenAt: "2026-10-08T17:00:00Z", offsetMinutes: -240, durationMS: 9_000, isTrashed: true),
        ]

        private struct DemoAlbum {
            let id: String
            let title: String
            let photoIds: [String]
            let coverPhotoId: String
        }

        private static let demoAlbums: [DemoAlbum] = [
            DemoAlbum(id: "demo-album-1", title: "Riverside weekend", photoIds: ["demo-01", "demo-02", "demo-03", "demo-04"], coverPhotoId: "demo-01"),
            DemoAlbum(id: "demo-album-2", title: "Harbor Market", photoIds: ["demo-09", "demo-10", "demo-11"], coverPhotoId: "demo-10"),
            DemoAlbum(id: "demo-album-3", title: "Autumn", photoIds: ["demo-05", "demo-06"], coverPhotoId: "demo-06"),
        ]

        /// A person the demo server has found in the photos, with the photos they
        /// are in. Invented, like everything here; an empty name is a person
        /// nobody has named.
        private struct DemoPerson {
            let id: String
            let name: String
            let photoIds: [String]
            var isHidden = false

            var coverFaceId: String { "face-\(id)" }
        }

        private static let demoPeople: [DemoPerson] = [
            DemoPerson(id: "demo-person-1", name: "Robin Example", photoIds: ["demo-01", "demo-02", "demo-05", "demo-06", "demo-09", "demo-10"]),
            DemoPerson(id: "demo-person-2", name: "Sam Placeholder", photoIds: ["demo-01", "demo-02", "demo-09", "demo-11"]),
            DemoPerson(id: "demo-person-3", name: "", photoIds: ["demo-13", "demo-15", "demo-16"]),
        ]

        private static func personJSON(_ person: DemoPerson) -> String {
            let photoCount = person.photoIds.count
            let contactId = person.name.isEmpty ? "" : "contact-" + person.id
            let bookId = person.name.isEmpty ? "" : "book-demo"
            return #"{"id":"\#(person.id)","createdAt":"2026-10-10T10:00:00Z","updatedAt":"2026-10-14T10:00:00Z","contactId":"\#(contactId)","contactAddressBookId":"\#(bookId)","contactName":"\#(person.name)","faceCount":\#(photoCount + 1),"photoCount":\#(photoCount),"facePictureUrl":"/api/v1/photos/faces/\#(person.coverFaceId)/picture","coverPhotoFaceId":"\#(person.coverFaceId)","isHidden":\#(person.isHidden)}"#
        }

        /// The faces the demo server found in one photo.
        private static func facesJSON(photoId: String) -> String {
            demoPeople.filter { $0.photoIds.contains(photoId) }.enumerated().map { index, person in
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
            return #"{"id":"\#(album.id)","albumTitle":"\#(album.title)","createdAt":"2026-10-10T10:00:00Z","updatedAt":"2026-10-14T10:00:00Z","coverPhotoId":null,"photoCount":\#(members.count),"firstTakenAt":\#(first),"lastTakenAt":\#(last),"coverPreviewUrl":"\#(coverPreviewUrl)"}"#
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
            let trashedAt = photo.isTrashed ? #""2026-10-12T10:00:00Z""# : "null"
            let fileRoles = photo.fileRoles.map { "\"" + $0 + "\"" }.joined(separator: ",")
            return #"{"id":"\#(photo.id)","createdAt":"\#(photo.takenAt)","photoKind":"\#(photo.kind)","takenAt":"\#(photo.takenAt)","takenAtOffsetMinutes":\#(photo.offsetMinutes),"widthPixels":\#(photo.isPortrait ? 3024 : 4032),"heightPixels":\#(photo.isPortrait ? 4032 : 3024),"durationMS":\#(photo.durationMS),"latitude":null,"longitude":null,"placeName":"\#(photo.placeName)","cameraMake":"Example","cameraModel":"Phone 1","originalFilename":"IMG_\#(photo.id.suffix(2)).\#(isVideo ? "MOV" : "HEIC")","isFavorite":\#(photo.isFavorite),"isScreenshot":false,"photoProcessingState":"\#(photo.processingState)","trashedAt":\#(trashedAt),"smallPreviewUrl":"\#(smallPreviewUrl)","largePreviewUrl":"\#(largePreviewUrl)","videoPreviewUrl":"\#(videoPreviewUrl)","photoFileRoles":[\#(fileRoles)],"processingErrorMessage":"\#(photo.failureMessage)"\#(extraFields)}"#
        }

        /// The answer to a request about photos: a GraphQL document, or a
        /// preview fetched by its address. Nil for any other request.
        static func photoAnswer(_ request: URLRequest) -> Data? {
            if let url = request.url, url.path.hasPrefix("/api/v1/photos/") {
                return photoPicture(path: url.path)
            }
            let body = String(decoding: request.httpBody ?? Data(), as: UTF8.self)
            let variables = (try? JSONSerialization.jsonObject(with: request.httpBody ?? Data()) as? [String: Any])?["variables"] as? [String: Any] ?? [:]
            let isEmpty = screen == "photos-empty"
            let json: String
            if body.contains("ReadPhotoLibrary") {
                let inLibrary = isEmpty ? 0 : demoPhotos.filter { !$0.isTrashed }.count
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
                let person = demoPeople.first { $0.id == (variables["photoFaceGroupId"] as? String) } ?? demoPeople[0]
                json = #"{"data":{"UnnamePhotoFaceGroup":\#(personJSON(DemoPerson(id: person.id, name: "", photoIds: person.photoIds)))}}"#
            } else if body.contains("NamePhotoFaceGroup") {
                let person = demoPeople.first { $0.id == (variables["photoFaceGroupId"] as? String) } ?? demoPeople[0]
                json = #"{"data":{"NamePhotoFaceGroup":\#(personJSON(DemoPerson(id: person.id, name: "Chris Placeholder", photoIds: person.photoIds)))}}"#
            } else if body.contains("MergePhotoFaceGroups") {
                json = #"{"data":{"MergePhotoFaceGroups":\#(personJSON(demoPeople[0]))}}"#
            } else if body.contains("RemovePhotoFaceFromGroup") {
                json = #"{"data":{"RemovePhotoFaceFromGroup":\#(personJSON(demoPeople[2]))}}"#
            } else if body.contains("HidePhotoFaceGroup") {
                var person = demoPeople.first { $0.id == (variables["photoFaceGroupId"] as? String) } ?? demoPeople[0]
                person.isHidden = variables["isHidden"] as? Bool ?? false
                json = #"{"data":{"HidePhotoFaceGroup":\#(personJSON(person))}}"#
            } else if body.contains("SetPhotoFaceRecognition") {
                json = #"{"data":{"SetPhotoFaceRecognition":\#(variables["isEnabled"] as? Bool ?? true)}}"#
            } else if body.contains("ListPhotoFaceGroups") {
                json = #"{"data":{"ListPhotoFaceGroups":["# + (isEmpty ? [] : demoPeople).map(personJSON).joined(separator: ",") + "]}}"
            } else if body.contains("ReadPhotoFaceGroup") {
                let person = demoPeople.first { $0.id == (variables["photoFaceGroupId"] as? String) } ?? demoPeople[0]
                json = #"{"data":{"ReadPhotoFaceGroup":\#(personJSON(person))}}"#
            } else if body.contains("DescribePhoto") {
                json = #"{"data":{"DescribePhoto":{"photoDescription":"","descriptionVisibleText":"","describedAt":null,"isDescriptionRequested":true}}}"#
            } else if body.contains("RetryPhotoProcessing") {
                json = #"{"data":{"RetryPhotoProcessing":1}}"#
            } else if body.contains("ListPhotos") {
                let isTrashed = variables["isTrashed"] as? Bool == true
                let isFavorite = variables["isFavorite"] as? Bool == true
                var chosen = demoPhotos.filter { $0.isTrashed == isTrashed }
                if isFavorite { chosen = chosen.filter(\.isFavorite) }
                if let albumId = variables["albumId"] as? String {
                    let members = demoAlbums.first { $0.id == albumId }?.photoIds ?? []
                    chosen = chosen.filter { members.contains($0.id) }
                }
                if let faceGroupId = variables["faceGroupId"] as? String {
                    let members = demoPeople.first { $0.id == faceGroupId }?.photoIds ?? []
                    chosen = chosen.filter { members.contains($0.id) }
                }
                if let after = variables["takenAfter"] as? String, let before = variables["takenBefore"] as? String {
                    chosen = chosen.filter { $0.takenAt >= after && $0.takenAt < before }
                }
                // A search keeps the photos whose place or file name has the words.
                if let words = variables["searchText"] as? String, !words.isEmpty {
                    chosen = demoPhotos.filter { !$0.isTrashed && ($0.placeName.localizedCaseInsensitiveContains(words) || $0.id.localizedCaseInsensitiveContains(words)) }
                }
                // Everything fits in one page, so a page after it is empty.
                if isEmpty || variables["beforePhotoId"] is String { chosen = [] }
                json = #"{"data":{"ListPhotos":["# + chosen.map { photoJSON($0) }.joined(separator: ",") + "]}}"
            } else if body.contains("ReadPhoto") {
                let photoId = variables["photoId"] as? String ?? demoPhotos[0].id
                let photo = demoPhotos.first { $0.id == photoId } ?? demoPhotos[0]
                let isVideo = photo.kind == "video"
                let file =
                    #"{"photoFileRole":"original","contentType":"\#(isVideo ? "video/quicktime" : "image/heic")","fileSizeBytes":\#(isVideo ? 48_200_000 : 2_345_678),"originalFilename":"IMG_\#(photo.id.suffix(2)).\#(isVideo ? "MOV" : "HEIC")","fileUrl":"/api/v1/photos/\#(photo.id)/files/original"}"#
                let albums = demoAlbums.filter { $0.photoIds.contains(photo.id) }.map { #"{"id":"\#($0.id)","albumTitle":"\#($0.title)"}"# }.joined(separator: ",")
                json = #"{"data":{"ReadPhoto":{"photo":\#(photoJSON(photo, extraFields: describedFields(photoId: photo.id))),"photoFiles":[\#(file)],"photoPreviews":[],"albums":[\#(albums)],"faces":[\#(facesJSON(photoId: photo.id))]}}}"#
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

        /// A picture for a face's address, `/api/v1/photos/faces/<id>/picture`: a
        /// colored disc with the person's initials, so it is plainly not a real face.
        private static func facePicture(faceId: String) -> Data {
            let personId = faceId.hasPrefix("face-") ? String(faceId.dropFirst(5)) : faceId
            let name = demoPeople.first { $0.id == personId }?.name ?? ""
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
#endif
