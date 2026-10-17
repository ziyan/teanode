#if DEBUG
    import SwiftUI
    import TeaNodeKit
    import UIKit

    /// What the demo server answers for Photos, for `-TeaNodeTab photos` with
    /// `-TeaNodeScreen photos-library`, `photos-viewer`, `photos-trash` or
    /// `photos-empty`. The photos are invented, and their pictures are drawn
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
        }

        private static let demoPhotos: [DemoPhoto] = [
            DemoPhoto(id: "demo-01", kind: "picture", takenAt: "2026-10-15T18:42:00Z", offsetMinutes: -240, isFavorite: true, placeName: "Riverside Park", isPortrait: true),
            DemoPhoto(id: "demo-02", kind: "picture", takenAt: "2026-10-15T18:40:00Z", offsetMinutes: -240, placeName: "Riverside Park"),
            DemoPhoto(id: "demo-03", kind: "video", takenAt: "2026-10-15T12:05:00Z", offsetMinutes: -240, durationMS: 23_000),
            DemoPhoto(id: "demo-04", kind: "picture", takenAt: "2026-10-15T09:30:00Z", offsetMinutes: -240, processingState: "waiting"),
            DemoPhoto(id: "demo-05", kind: "picture", takenAt: "2026-10-14T21:10:00Z", offsetMinutes: -240, isPortrait: true),
            DemoPhoto(id: "demo-06", kind: "picture", takenAt: "2026-10-14T15:00:00Z", offsetMinutes: -240, isFavorite: true),
            DemoPhoto(id: "demo-07", kind: "video", takenAt: "2026-10-14T14:00:00Z", offsetMinutes: -240, durationMS: 83_000),
            DemoPhoto(id: "demo-08", kind: "picture", takenAt: "2026-10-14T10:00:00Z", offsetMinutes: -240, processingState: "failed"),
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

        private static func photoJSON(_ photo: DemoPhoto) -> String {
            let isVideo = photo.kind == "video"
            let isReady = photo.processingState == "ready"
            let previewPath = "/api/v1/photos/\(photo.id)/previews/"
            let smallPreviewUrl = isReady ? previewPath + "small?v=1" : ""
            let largePreviewUrl = isReady ? previewPath + "large?v=1" : ""
            let videoPreviewUrl = isReady && isVideo ? previewPath + "video?v=1" : ""
            let trashedAt = photo.isTrashed ? #""2026-10-12T10:00:00Z""# : "null"
            return #"{"id":"\#(photo.id)","createdAt":"\#(photo.takenAt)","photoKind":"\#(photo.kind)","takenAt":"\#(photo.takenAt)","takenAtOffsetMinutes":\#(photo.offsetMinutes),"widthPixels":\#(photo.isPortrait ? 3024 : 4032),"heightPixels":\#(photo.isPortrait ? 4032 : 3024),"durationMS":\#(photo.durationMS),"latitude":null,"longitude":null,"placeName":"\#(photo.placeName)","cameraMake":"Example","cameraModel":"Phone 1","originalFilename":"IMG_\#(photo.id.suffix(2)).\#(isVideo ? "MOV" : "HEIC")","isFavorite":\#(photo.isFavorite),"isScreenshot":false,"photoProcessingState":"\#(photo.processingState)","trashedAt":\#(trashedAt),"smallPreviewUrl":"\#(smallPreviewUrl)","largePreviewUrl":"\#(largePreviewUrl)","videoPreviewUrl":"\#(videoPreviewUrl)"}"#
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
                json = #"{"data":{"ReadPhotoLibrary":{"photoCount":\#(inLibrary),"videoCount":3,"favoriteCount":3,"trashedCount":\#(trashed),"usedBytes":412000000,"trashRetentionDays":30}}}"#
            } else if body.contains("ListPhotos") {
                let isTrashed = variables["isTrashed"] as? Bool == true
                let isFavorite = variables["isFavorite"] as? Bool == true
                var chosen = demoPhotos.filter { $0.isTrashed == isTrashed }
                if isFavorite { chosen = chosen.filter(\.isFavorite) }
                // Everything fits in one page, so a page after it is empty.
                if isEmpty || variables["beforePhotoId"] is String { chosen = [] }
                json = #"{"data":{"ListPhotos":["# + chosen.map(photoJSON).joined(separator: ",") + "]}}"
            } else if body.contains("ReadPhoto") {
                let photoId = variables["photoId"] as? String ?? demoPhotos[0].id
                let photo = demoPhotos.first { $0.id == photoId } ?? demoPhotos[0]
                let isVideo = photo.kind == "video"
                let file =
                    #"{"photoFileRole":"original","contentType":"\#(isVideo ? "video/quicktime" : "image/heic")","fileSizeBytes":\#(isVideo ? 48_200_000 : 2_345_678),"originalFilename":"IMG_\#(photo.id.suffix(2)).\#(isVideo ? "MOV" : "HEIC")","fileUrl":"/api/v1/photos/\#(photo.id)/files/original"}"#
                json = #"{"data":{"ReadPhoto":{"photo":\#(photoJSON(photo)),"photoFiles":[\#(file)],"photoPreviews":[]}}}"#
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

        /// A picture for a preview address: `/api/v1/photos/<id>/previews/<size>`.
        private static func photoPicture(path: String) -> Data {
            let parts = path.split(separator: "/").map(String.init)
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
