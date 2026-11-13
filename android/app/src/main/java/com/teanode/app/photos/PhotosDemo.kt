package com.teanode.app.photos

import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.LinearGradient
import android.graphics.Paint
import android.graphics.Shader
import com.teanode.app.demo.DemoAnswerer
import com.teanode.app.demo.DemoResponse
import com.teanode.app.demo.graphQLField
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull
import java.io.ByteArrayOutputStream

/** One invented photo of the demo library. */
internal class DemoPhoto(
    val id: String,
    val takenAt: String,
    val kind: String = "picture",
    val isFavorite: Boolean = false,
    val placeName: String = "",
    val offsetMinutes: Int = 0,
    val albumIds: List<String> = emptyList(),
    val faceGroupIds: List<String> = emptyList(),
    val isTrashed: Boolean = false,
    val isFailed: Boolean = false,
    val isWaiting: Boolean = false,
    val durationMS: Int = 0,
    val hue: Float = 200f,
    val description: String = "",
    val isOwnedByCaller: Boolean = true,
)

internal object PhotosDemoData {
    val photos: List<DemoPhoto> = listOf(
        DemoPhoto("p01", "2026-10-12T16:40:00Z", placeName = "Lakeside", isFavorite = true, albumIds = listOf("a1"), faceGroupIds = listOf("g1"), hue = 210f, description = "A wooden pier over calm water at sunset, with two people standing at the end."),
        DemoPhoto("p02", "2026-10-12T16:35:00Z", kind = "video", durationMS = 65_400, placeName = "Lakeside", albumIds = listOf("a1"), hue = 190f),
        DemoPhoto("p03", "2026-10-12T09:10:00Z", placeName = "Lakeside", albumIds = listOf("a1"), faceGroupIds = listOf("g1", "g2"), hue = 160f),
        DemoPhoto("p04", "2026-10-02T12:00:00Z", placeName = "Hilltown", hue = 30f, isFavorite = true),
        DemoPhoto("p05", "2026-10-02T11:50:00Z", placeName = "Hilltown", hue = 45f, faceGroupIds = listOf("g2")),
        DemoPhoto("p06", "2026-09-20T18:00:00Z", kind = "livePhoto", hue = 280f),
        DemoPhoto("p07", "2026-09-18T07:30:00Z", hue = 330f, isWaiting = true),
        DemoPhoto("p08", "2026-09-05T14:00:00Z", hue = 90f, albumIds = listOf("a2")),
        DemoPhoto("p09", "2026-08-30T10:00:00Z", hue = 120f, isFailed = true),
        DemoPhoto("p10", "2026-08-14T20:15:00Z", hue = 250f, faceGroupIds = listOf("g1")),
        DemoPhoto("p11", "2025-12-24T19:00:00Z", placeName = "Hilltown", hue = 10f, albumIds = listOf("a2"), faceGroupIds = listOf("g1", "g2")),
        DemoPhoto("p12", "2025-12-24T18:00:00Z", hue = 20f),
        DemoPhoto("p13", "2025-07-04T13:00:00Z", offsetMinutes = 120, placeName = "Harbor Town", hue = 180f),
        DemoPhoto("t01", "2026-10-01T10:00:00Z", isTrashed = true, hue = 60f),
        DemoPhoto("t02", "2026-09-28T10:00:00Z", isTrashed = true, hue = 300f),
        DemoPhoto("s01", "2026-09-10T10:00:00Z", isOwnedByCaller = false, albumIds = listOf("a3"), hue = 140f),
        DemoPhoto("s02", "2026-09-10T10:05:00Z", isOwnedByCaller = false, albumIds = listOf("a3"), hue = 100f),
    )

    fun photoJson(photo: DemoPhoto, withDescription: Boolean = false): String {
        val state = when { photo.isFailed -> "failed"; photo.isWaiting -> "waiting"; else -> "ready" }
        val hasPreviews = state == "ready"
        val trashed = if (photo.isTrashed) """"trashedAt":"2026-11-0${photo.id.last()}T10:00:00Z",""" else ""
        val description = if (withDescription && photo.description.isNotEmpty()) {
            """"photoDescription":"${photo.description}","descriptionVisibleText":"","describedAt":"2026-10-13T08:00:00Z","isDescriptionRequested":false,"photoDocumentKind":"none","""
        } else {
            """"photoDescription":"","descriptionVisibleText":"","describedAt":null,"isDescriptionRequested":${photo.id == "p07" && withDescription},"photoDocumentKind":"","""
        }
        return """{"id":"${photo.id}","createdAt":"${photo.takenAt}","photoKind":"${photo.kind}","takenAt":"${photo.takenAt}","takenAtOffsetMinutes":${if (photo.offsetMinutes != 0) photo.offsetMinutes else "null"},
            "widthPixels":4000,"heightPixels":3000,"durationMS":${photo.durationMS},"latitude":${if (photo.placeName.isNotEmpty()) "46.5" else "null"},"longitude":${if (photo.placeName.isNotEmpty()) "7.5" else "null"},
            "placeName":"${photo.placeName}","cameraMake":"Example","cameraModel":"Phone 3","originalFilename":"IMG_${photo.id}.jpg","isFavorite":${photo.isFavorite},"isScreenshot":false,
            "photoProcessingState":"$state",$trashed"smallPreviewUrl":"${if (hasPreviews) "/demo/photo/${photo.id}/small" else ""}","largePreviewUrl":"${if (hasPreviews) "/demo/photo/${photo.id}/large" else ""}",
            "videoPreviewUrl":"${if (hasPreviews && photo.kind == "video") "/demo/photo/${photo.id}/video" else ""}","photoFileRoles":["original"],"processingErrorMessage":"${if (photo.isFailed) "The picture could not be read." else ""}",
            $description"isOwnedByCaller":${photo.isOwnedByCaller},"ownerName":"${if (photo.isOwnedByCaller) "" else "Alex"}"}""".replace("\n", " ")
    }

    private fun albumJson(id: String, title: String, count: Int, cover: String, extra: String = "") =
        """{"id":"$id","albumTitle":"$title","createdAt":"2026-10-01T00:00:00Z","updatedAt":"2026-10-02T00:00:00Z","coverPhotoId":"$cover","photoCount":$count,"firstTakenAt":"2026-10-02T09:10:00Z","lastTakenAt":"2026-10-12T16:40:00Z",
            "coverPreviewUrl":"/demo/photo/$cover/small",$extra}""".replace("\n", " ")

    val albumsJson = listOf(
        albumJson("a1", "Lake weekend", 3, "p01", """"isOwnedByCaller":true,"canCallerContribute":true,"canCallerManage":true"""),
        albumJson("a2", "Winter", 2, "p11", """"isOwnedByCaller":true,"canCallerContribute":true,"canCallerManage":true"""),
        albumJson("a3", "Garden party", 2, "s01", """"ownerUserId":"user-alex","ownerUsername":"alex","ownerName":"Alex","isOwnedByCaller":false,"shareLevel":"view","canCallerContribute":false,"canCallerManage":false"""),
        albumJson("a4", "Family trip", 5, "p05", """"circleId":"c1","circleName":"Family","ownerUserId":"user-sam","ownerName":"Sam","isOwnedByCaller":false,"canCallerContribute":true,"canCallerManage":false"""),
    )

    fun faceGroupsJson() = listOf(
        """{"id":"g1","createdAt":"2026-10-01T00:00:00Z","updatedAt":"2026-10-01T00:00:00Z","contactId":"k1","contactAddressBookId":"b1","contactName":"Alex Example","isOwnContact":false,"faceCount":14,"photoCount":12,"facePictureUrl":"/demo/face/g1","coverPhotoFaceId":"f1","isHidden":false,"accountUserId":"user-alex","account":{"userId":"user-alex","username":"alex","name":"Alex Example","avatarColorIndex":3}}""",
        """{"id":"g2","createdAt":"2026-10-01T00:00:00Z","updatedAt":"2026-10-01T00:00:00Z","contactId":"","contactAddressBookId":"","contactName":"","faceCount":6,"photoCount":5,"facePictureUrl":"/demo/face/g2","coverPhotoFaceId":"f2","isHidden":false}""",
        """{"id":"g3","createdAt":"2026-10-01T00:00:00Z","updatedAt":"2026-10-01T00:00:00Z","contactId":"k3","contactAddressBookId":"b1","contactName":"Sam Sample","faceCount":4,"photoCount":3,"facePictureUrl":"/demo/face/g3","coverPhotoFaceId":"f3","isHidden":true}""",
    )

    /** A picture of the demo: a soft gradient in the photo's hue, so a grid is told apart at a glance. */
    fun pictureBytes(hue: Float, size: Int = 96): ByteArray {
        val bitmap = Bitmap.createBitmap(size, size, Bitmap.Config.ARGB_8888)
        val paint = Paint().apply {
            shader = LinearGradient(0f, 0f, size.toFloat(), size.toFloat(), Color.HSVToColor(floatArrayOf(hue, 0.55f, 0.95f)), Color.HSVToColor(floatArrayOf((hue + 40f) % 360f, 0.7f, 0.55f)), Shader.TileMode.CLAMP)
        }
        Canvas(bitmap).drawRect(0f, 0f, size.toFloat(), size.toFloat(), paint)
        val output = ByteArrayOutputStream()
        bitmap.compress(Bitmap.CompressFormat.PNG, 100, output)
        return output.toByteArray()
    }
}

private fun JsonObject.text(name: String): String? = (this[name] as? JsonPrimitive)?.contentOrNull

private fun JsonObject.flag(name: String): Boolean? = (this[name] as? JsonPrimitive)?.booleanOrNull

/**
 * What demo mode answers for Photos: a library of seventeen invented photos (a video, a Live Photo, a favorite, two that
 * are still being made or failed, the Trash, an album shared by somebody else), albums and a suggestion, people, shares,
 * and a picture for each address, so every screen and state can be reached and photographed without a server.
 */
val photosDemoAnswerers: List<DemoAnswerer> = listOf(
    // A picture: /demo/photo/<id>/<size> and /demo/face/<group>.
    DemoAnswerer { request ->
        val parts = request.path.split('/')
        when {
            request.path.startsWith("/demo/photo/") && parts.size >= 5 -> {
                val photo = PhotosDemoData.photos.firstOrNull { candidate -> candidate.id == parts[3] }
                DemoResponse(PhotosDemoData.pictureBytes(photo?.hue ?: 200f, if (parts[4] == "large") 360 else 96))
            }
            request.path.startsWith("/demo/face/") -> DemoResponse(PhotosDemoData.pictureBytes(((parts.last().hashCode() and 0xff) % 360).toFloat(), 96))
            else -> null
        }
    },
    graphQLField("ListPhotos") { variables ->
        var photos = PhotosDemoData.photos
        val search = variables.text("searchText")
        photos = when {
            variables.flag("isTrashed") == true -> photos.filter { photo -> photo.isTrashed }
            else -> photos.filter { photo -> !photo.isTrashed }
        }
        if (variables.flag("isFavorite") == true) photos = photos.filter { photo -> photo.isFavorite }
        variables.text("albumId")?.let { albumId -> photos = PhotosDemoData.photos.filter { photo -> albumId in photo.albumIds } }
        variables.text("faceGroupId")?.let { groupId -> photos = PhotosDemoData.photos.filter { photo -> groupId in photo.faceGroupIds } }
        variables.text("photoKind")?.let { kind -> photos = photos.filter { photo -> photo.kind == kind } }
        if (!search.isNullOrEmpty()) photos = photos.filter { photo -> photo.placeName.contains(search, ignoreCase = true) || photo.description.contains(search, ignoreCase = true) }
        photos.sortedByDescending { photo -> photo.takenAt }.joinToString(",", "[", "]") { photo -> PhotosDemoData.photoJson(photo) }
    },
    graphQLField("ReadPhotoLibrary", """{"photoCount":12,"videoCount":1,"favoriteCount":2,"trashedCount":2,"usedBytes":5400000000,"maximumFileBytes":4294967296,"maximumAccountBytes":107374182400,"trashRetentionDays":30,
        "isDescriptionAvailable":true,"descriptionUnavailableMessage":"","isFaceRecognitionAvailable":true,"faceRecognitionUnavailableMessage":"","isPhotoFaceRecognitionEnabled":true,"isPhotoReadingEnabled":true}""".replace("\n", " ")),
    graphQLField("ReadPhotoPlace", """{"locationPlace":null,"addressText":"1 Pier Road, Lakeside"}"""),
    graphQLField("ReadPhoto") { variables ->
        val photo = PhotosDemoData.photos.firstOrNull { candidate -> candidate.id == variables.text("photoId") } ?: PhotosDemoData.photos.first()
        val albums = PhotosDemoData.albumsJson.mapIndexedNotNull { index, _ -> "a${index + 1}".takeIf { albumId -> albumId in photo.albumIds } }
            .joinToString(",", "[", "]") { albumId -> """{"id":"$albumId","albumTitle":"${if (albumId == "a1") "Lake weekend" else if (albumId == "a2") "Winter" else "Garden party"}"}""" }
        val faces = photo.faceGroupIds.joinToString(",", "[", "]") { groupId ->
            """{"id":"f-${photo.id}-$groupId","photoId":"${photo.id}","boxLeftFraction":0.3,"boxTopFraction":0.2,"boxWidthFraction":0.2,"boxHeightFraction":0.25,"photoFaceGroupId":"$groupId","contactId":"","contactName":"${if (groupId == "g1") "Alex Example" else ""}","facePictureUrl":"/demo/face/$groupId"}"""
        }
        """{"photo":${PhotosDemoData.photoJson(photo, withDescription = true)},"photoFiles":[{"photoFileRole":"original","contentType":"image/jpeg","fileSizeBytes":3200000,"originalFilename":"IMG_${photo.id}.jpg","fileUrl":"/demo/photo/${photo.id}/large"}],"photoPreviews":[],"albums":$albums,"faces":$faces}"""
    },
    graphQLField("ListPhotoAlbums") { _ -> PhotosDemoData.albumsJson.joinToString(",", "[", "]") },
    graphQLField("SuggestPhotoAlbums", """[{"takenAfter":"2026-10-12T00:00:00Z","takenBefore":"2026-10-13T00:00:00Z","placeNames":["Lakeside"],"albumTitle":"Lakeside","photoCount":3,"samplePhotoIds":["p01","p02","p03"]}]"""),
    graphQLField("ListPhotoFaceGroups") { variables -> PhotosDemoData.faceGroupsJson().filter { json -> variables.flag("isHiddenIncluded") == true || !json.contains(""""isHidden":true""") }.joinToString(",", "[", "]") },
    graphQLField("ReadPhotoFaceGroup") { variables -> PhotosDemoData.faceGroupsJson().firstOrNull { json -> json.contains("\"id\":\"${variables.text("photoFaceGroupId")}\"") } ?: PhotosDemoData.faceGroupsJson().first() },
    graphQLField("ListPhotoAlbumShares", """[{"id":"s1","photoAlbumId":"a1","granteeUserId":"user-alex","granteeUsername":"alex","granteeName":"Alex Example","granteeAvatarColorIndex":3,"shareLevel":"contribute","isLocationIncluded":true,"isFaceNamesIncluded":false,"createdAt":"2026-10-13T00:00:00Z"},
        {"id":"s2","photoAlbumId":"a1","granteeCircleId":"c1","granteeCircleName":"Family","shareLevel":"view","isLocationIncluded":false,"isFaceNamesIncluded":false,"createdAt":"2026-10-14T00:00:00Z"}]""".replace("\n", " ")),
    graphQLField("ListCircles", """[{"id":"c1","name":"Family","members":[{"userId":"user-alex","username":"alex","name":"Alex Example","avatarColorIndex":3},{"userId":"user-sam","username":"sam","name":"Sam Sample","avatarColorIndex":6}]},{"id":"c2","name":"Book club","members":[{"userId":"user-robin","username":"robin","name":"Robin Reader","avatarColorIndex":9}]}]"""),
    graphQLField("ListAddressBooks", """[{"id":"b1","name":"Contacts","contacts":3}]"""),
    graphQLField("ListContacts", """[{"id":"k1","name":"Alex Example","emails":["alex@example.com"]},{"id":"k2","name":"Robin Reader","emails":["robin@example.com"]}]"""),
    graphQLField("FindPhotos", "[]"),
)
