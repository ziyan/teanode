package com.teanode.kit.photos

import com.teanode.kit.Connection
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive

@Serializable private class ListPhotosAnswer(val ListPhotos: List<Photo>)

private fun photoIdsVariable(photoIds: List<String>): JsonElement = JsonArray(photoIds.map { photoId -> JsonPrimitive(photoId) })

/** One page of photos of a part of the library, newest first, narrowed by the filter. */
suspend fun Connection.photos(
    collection: PhotoCollection,
    filter: PhotoFilter = PhotoFilter(),
    beforePhotoId: String? = null,
    pageSize: Int = PhotoPaging.PAGE_SIZE,
): List<Photo> {
    val variables = mutableMapOf<String, JsonElement>("pageSize" to JsonPrimitive(pageSize))
    if (!beforePhotoId.isNullOrEmpty()) variables["beforePhotoId"] = JsonPrimitive(beforePhotoId)
    collection.isFavoriteFilter?.let { isFavorite -> variables["isFavorite"] = JsonPrimitive(isFavorite) }
    collection.isTrashedFilter?.let { isTrashed -> variables["isTrashed"] = JsonPrimitive(isTrashed) }
    variables.putAll(filter.variables())
    return execute<ListPhotosAnswer>(PhotoDocuments.LIST_PHOTOS, variables).ListPhotos
}

/** The photos the server finds for these words, most relevant first. */
suspend fun Connection.photos(matching: String): List<Photo> {
    val variables = mapOf<String, JsonElement>(
        "pageSize" to JsonPrimitive(PhotoSearch.MOST_RESULT_COUNT), "searchText" to JsonPrimitive(PhotoSearch.words(matching).orEmpty()),
    )
    return execute<ListPhotosAnswer>(PhotoDocuments.LIST_PHOTOS, variables).ListPhotos
}

suspend fun Connection.photosInAlbum(photoAlbumId: String, beforePhotoId: String? = null, pageSize: Int = PhotoPaging.PAGE_SIZE): List<Photo> {
    val variables = mutableMapOf<String, JsonElement>("pageSize" to JsonPrimitive(pageSize), "albumId" to JsonPrimitive(photoAlbumId))
    if (!beforePhotoId.isNullOrEmpty()) variables["beforePhotoId"] = JsonPrimitive(beforePhotoId)
    return execute<ListPhotosAnswer>(PhotoDocuments.LIST_PHOTOS, variables).ListPhotos
}

suspend fun Connection.photosInFaceGroup(photoFaceGroupId: String, beforePhotoId: String? = null, pageSize: Int = PhotoPaging.PAGE_SIZE): List<Photo> {
    val variables = mutableMapOf<String, JsonElement>("pageSize" to JsonPrimitive(pageSize), "faceGroupId" to JsonPrimitive(photoFaceGroupId))
    if (!beforePhotoId.isNullOrEmpty()) variables["beforePhotoId"] = JsonPrimitive(beforePhotoId)
    return execute<ListPhotosAnswer>(PhotoDocuments.LIST_PHOTOS, variables).ListPhotos
}

@Serializable private class ReadPhotoAnswer(val ReadPhoto: PhotoDetail)

suspend fun Connection.photoDetail(photoId: String): PhotoDetail =
    execute<ReadPhotoAnswer>(PhotoDocuments.READ_PHOTO, mapOf("photoId" to JsonPrimitive(photoId))).ReadPhoto

@Serializable private class ReadPhotoPlaceAnswer(val ReadPhotoPlace: PhotoPositionPlace? = null)

suspend fun Connection.photoPlace(photoId: String): PhotoPositionPlace? =
    execute<ReadPhotoPlaceAnswer>(PhotoDocuments.READ_PHOTO_PLACE, mapOf("photoId" to JsonPrimitive(photoId))).ReadPhotoPlace

@Serializable private class DescribePhotoAnswer(val DescribePhoto: Photo)

/** Asks for a description of the photo, or a new one. The description's fields are read from the answer. */
suspend fun Connection.describePhoto(photoId: String, isRedescribed: Boolean = false): PhotoDescription {
    val photoFields = mapOf("photoId" to JsonPrimitive(photoId), "isRedescribed" to JsonPrimitive(isRedescribed))
    return execute<DescribePhotoAnswer>(PhotoDocuments.DESCRIBE_PHOTO, photoFields).DescribePhoto.description
}

@Serializable private class ReadPhotoLibraryAnswer(val ReadPhotoLibrary: PhotoLibrary)

suspend fun Connection.photoLibrary(): PhotoLibrary = execute<ReadPhotoLibraryAnswer>(PhotoDocuments.READ_PHOTO_LIBRARY).ReadPhotoLibrary

@Serializable private class UpdatePhotoAnswer(val UpdatePhoto: Photo)

suspend fun Connection.setPhotoFavorite(photoId: String, isFavorite: Boolean): Photo =
    execute<UpdatePhotoAnswer>(PhotoDocuments.UPDATE_PHOTO, mapOf("photoId" to JsonPrimitive(photoId), "isFavorite" to JsonPrimitive(isFavorite))).UpdatePhoto

suspend fun Connection.changePhotoDate(photoId: String, takenAtText: String): Photo =
    execute<UpdatePhotoAnswer>(PhotoDocuments.UPDATE_PHOTO, mapOf("photoId" to JsonPrimitive(photoId), "takenAt" to JsonPrimitive(takenAtText))).UpdatePhoto

@Serializable private class SearchPhotoPlacesAnswer(val SearchPhotoPlaces: List<PhotoPlace>)

suspend fun Connection.searchPhotoPlaces(words: String, nearLatitude: Double? = null, nearLongitude: Double? = null): List<PhotoPlace> {
    // The server counts the words by Unicode code points, which a cut by characters can exceed.
    val cutWords = String(words.codePoints().limit(PhotoSearch.MOST_PLACE_WORDS_LENGTH.toLong()).toArray(), 0, minOf(words.codePointCount(0, words.length), PhotoSearch.MOST_PLACE_WORDS_LENGTH))
    val variables = mutableMapOf<String, JsonElement>("query" to JsonPrimitive(cutWords))
    if (nearLatitude != null && nearLongitude != null) {
        variables["nearLatitude"] = JsonPrimitive(nearLatitude)
        variables["nearLongitude"] = JsonPrimitive(nearLongitude)
    }
    return execute<SearchPhotoPlacesAnswer>(PhotoDocuments.SEARCH_PHOTO_PLACES, variables).SearchPhotoPlaces
}

@Serializable private class CountAnswer(
    val SetPhotoLocation: Int = 0, val RemovePhotoLocation: Int = 0, val TrashPhotos: Int = 0, val RestorePhotos: Int = 0,
    val DeletePhotos: Int = 0, val RetryPhotoProcessing: Int = 0, val EmptyPhotoTrash: Int = 0, val AddPhotosToAlbum: Int = 0,
    val RemovePhotosFromAlbum: Int = 0,
)

private suspend fun Connection.inBatches(document: String, photoIds: List<String>, extra: Map<String, JsonElement> = emptyMap(), count: (CountAnswer) -> Int): Int {
    var changedCount = 0
    for (batch in PhotoBatches.batches(photoIds)) changedCount += count(execute<CountAnswer>(document, extra + ("photoIds" to photoIdsVariable(batch))))
    return changedCount
}

suspend fun Connection.setPhotoLocation(photoIds: List<String>, latitude: Double, longitude: Double): Int =
    inBatches(PhotoDocuments.SET_PHOTO_LOCATION, photoIds, mapOf("latitude" to JsonPrimitive(latitude), "longitude" to JsonPrimitive(longitude))) { answer -> answer.SetPhotoLocation }

suspend fun Connection.removePhotoLocation(photoIds: List<String>): Int = inBatches(PhotoDocuments.REMOVE_PHOTO_LOCATION, photoIds) { answer -> answer.RemovePhotoLocation }

suspend fun Connection.trashPhotos(photoIds: List<String>): Int = inBatches(PhotoDocuments.TRASH_PHOTOS, photoIds) { answer -> answer.TrashPhotos }

suspend fun Connection.restorePhotos(photoIds: List<String>): Int = inBatches(PhotoDocuments.RESTORE_PHOTOS, photoIds) { answer -> answer.RestorePhotos }

suspend fun Connection.deletePhotos(photoIds: List<String>): Int = inBatches(PhotoDocuments.DELETE_PHOTOS, photoIds) { answer -> answer.DeletePhotos }

suspend fun Connection.retryPhotoProcessing(photoIds: List<String>): Int = inBatches(PhotoDocuments.RETRY_PHOTO_PROCESSING, photoIds) { answer -> answer.RetryPhotoProcessing }

suspend fun Connection.emptyPhotoTrash(): Int = execute<CountAnswer>(PhotoDocuments.EMPTY_PHOTO_TRASH).EmptyPhotoTrash

@Serializable private class PlaybackAddressAnswer(val PhotoPlaybackAddress: String)

/** A signed address the player reads a video or an original from without the app's sign-in. */
suspend fun Connection.photoPlaybackAddress(photoId: String, source: PhotoPlaybackSource): String {
    val variables = mutableMapOf<String, JsonElement>("photoId" to JsonPrimitive(photoId))
    when (source) {
        is PhotoPlaybackSource.Preview -> variables["photoPreviewSize"] = JsonPrimitive(source.photoPreviewSize)
        is PhotoPlaybackSource.File -> variables["photoFileRole"] = JsonPrimitive(source.photoFileRole)
    }
    val path = execute<PlaybackAddressAnswer>(PhotoDocuments.PHOTO_PLAYBACK_ADDRESS, variables).PhotoPlaybackAddress
    return if (path.startsWith("http")) path else account.server.endpoint(path).toString()
}

// Albums

@Serializable private class ListPhotoAlbumsAnswer(val ListPhotoAlbums: List<PhotoAlbum>)

suspend fun Connection.photoAlbums(circleId: String? = null): List<PhotoAlbum> {
    val variables = if (circleId == null) emptyMap() else mapOf<String, JsonElement>("circleId" to JsonPrimitive(circleId))
    return execute<ListPhotoAlbumsAnswer>(PhotoDocuments.LIST_PHOTO_ALBUMS, variables).ListPhotoAlbums
}

@Serializable private class SuggestPhotoAlbumsAnswer(val SuggestPhotoAlbums: List<PhotoAlbumSuggestion>)

suspend fun Connection.photoAlbumSuggestions(): List<PhotoAlbumSuggestion> = execute<SuggestPhotoAlbumsAnswer>(PhotoDocuments.SUGGEST_PHOTO_ALBUMS).SuggestPhotoAlbums

@Serializable private class CreatePhotoAlbumAnswer(val CreatePhotoAlbum: PhotoAlbum)

/** Makes an album; a list of photos too long for one call goes in batches after it is made. */
suspend fun Connection.createPhotoAlbum(title: String, photoIds: List<String> = emptyList(), circleId: String? = null): PhotoAlbum {
    val batches = PhotoBatches.batches(photoIds)
    val variables = mutableMapOf<String, JsonElement>(
        "albumTitle" to JsonPrimitive(PhotoAlbumText.trimmedTitle(title)), "photoIds" to photoIdsVariable(batches.firstOrNull().orEmpty()),
    )
    // An album for a circle belongs to the circle: every member adds to it.
    if (!circleId.isNullOrEmpty()) variables["circleId"] = JsonPrimitive(circleId)
    val created = execute<CreatePhotoAlbumAnswer>(PhotoDocuments.CREATE_PHOTO_ALBUM, variables).CreatePhotoAlbum
    for (batch in batches.drop(1)) addPhotos(batch, created.id)
    return created
}

@Serializable private class RenamePhotoAlbumAnswer(val RenamePhotoAlbum: PhotoAlbum)

suspend fun Connection.renamePhotoAlbum(photoAlbumId: String, title: String): PhotoAlbum =
    execute<RenamePhotoAlbumAnswer>(
        PhotoDocuments.RENAME_PHOTO_ALBUM, mapOf("photoAlbumId" to JsonPrimitive(photoAlbumId), "albumTitle" to JsonPrimitive(PhotoAlbumText.trimmedTitle(title))),
    ).RenamePhotoAlbum

@Serializable private class DeletePhotoAlbumAnswer(val DeletePhotoAlbum: Boolean = true)

suspend fun Connection.deletePhotoAlbum(photoAlbumId: String) {
    execute<DeletePhotoAlbumAnswer>(PhotoDocuments.DELETE_PHOTO_ALBUM, mapOf("photoAlbumId" to JsonPrimitive(photoAlbumId)))
}

suspend fun Connection.addPhotos(photoIds: List<String>, photoAlbumId: String): Int =
    inBatches(PhotoDocuments.ADD_PHOTOS_TO_ALBUM, photoIds, mapOf("photoAlbumId" to JsonPrimitive(photoAlbumId))) { answer -> answer.AddPhotosToAlbum }

suspend fun Connection.removePhotos(photoIds: List<String>, photoAlbumId: String): Int =
    inBatches(PhotoDocuments.REMOVE_PHOTOS_FROM_ALBUM, photoIds, mapOf("photoAlbumId" to JsonPrimitive(photoAlbumId))) { answer -> answer.RemovePhotosFromAlbum }

@Serializable private class SetPhotoAlbumCoverAnswer(val SetPhotoAlbumCover: PhotoAlbum)

suspend fun Connection.setPhotoAlbumCover(photoAlbumId: String, photoId: String): PhotoAlbum =
    execute<SetPhotoAlbumCoverAnswer>(PhotoDocuments.SET_PHOTO_ALBUM_COVER, mapOf("photoAlbumId" to JsonPrimitive(photoAlbumId), "photoId" to JsonPrimitive(photoId))).SetPhotoAlbumCover

/** Every photo id of a suggested album, read a page at a time. */
suspend fun Connection.photoIds(suggestion: PhotoAlbumSuggestion): List<String> {
    val photoIds = mutableListOf<String>()
    var beforePhotoId: String? = null
    while (true) {
        val variables = mutableMapOf<String, JsonElement>(
            "pageSize" to JsonPrimitive(PhotoBatches.MOST_PHOTO_IDS_PER_CALL), "takenAfter" to JsonPrimitive(suggestion.takenAfter),
            "takenBefore" to JsonPrimitive(suggestion.takenBefore),
        )
        beforePhotoId?.let { cursor -> variables["beforePhotoId"] = JsonPrimitive(cursor) }
        val page = execute<ListPhotosAnswer>(PhotoDocuments.LIST_PHOTOS, variables).ListPhotos
        photoIds.addAll(page.map { photo -> photo.id })
        if (page.size < PhotoBatches.MOST_PHOTO_IDS_PER_CALL) return photoIds
        beforePhotoId = page.last().id
    }
}

// People

@Serializable private class ListFaceGroupsAnswer(val ListPhotoFaceGroups: List<PhotoFaceGroup>)

suspend fun Connection.photoFaceGroups(isHiddenIncluded: Boolean = false): List<PhotoFaceGroup> =
    execute<ListFaceGroupsAnswer>(PhotoDocuments.LIST_PHOTO_FACE_GROUPS, mapOf("isHiddenIncluded" to JsonPrimitive(isHiddenIncluded))).ListPhotoFaceGroups

@Serializable private class ReadFaceGroupAnswer(val ReadPhotoFaceGroup: PhotoFaceGroup)

suspend fun Connection.photoFaceGroup(photoFaceGroupId: String): PhotoFaceGroup =
    execute<ReadFaceGroupAnswer>(PhotoDocuments.READ_PHOTO_FACE_GROUP, mapOf("photoFaceGroupId" to JsonPrimitive(photoFaceGroupId))).ReadPhotoFaceGroup

@Serializable private class NameFaceGroupAnswer(val NamePhotoFaceGroup: NamedPhotoFaceGroup)

suspend fun Connection.namePhotoFaceGroup(photoFaceGroupId: String, contactId: String): NamedPhotoFaceGroup =
    execute<NameFaceGroupAnswer>(
        PhotoDocuments.NAME_PHOTO_FACE_GROUP, mapOf("photoFaceGroupId" to JsonPrimitive(photoFaceGroupId), "contactId" to JsonPrimitive(contactId)),
    ).NamePhotoFaceGroup

@Serializable private class UnnameFaceGroupAnswer(val UnnamePhotoFaceGroup: PhotoFaceGroup)

suspend fun Connection.unnamePhotoFaceGroup(photoFaceGroupId: String): PhotoFaceGroup =
    execute<UnnameFaceGroupAnswer>(PhotoDocuments.UNNAME_PHOTO_FACE_GROUP, mapOf("photoFaceGroupId" to JsonPrimitive(photoFaceGroupId))).UnnamePhotoFaceGroup

@Serializable private class MergeFaceGroupsAnswer(val MergePhotoFaceGroups: PhotoFaceGroup)

suspend fun Connection.mergePhotoFaceGroups(photoFaceGroupIds: List<String>): PhotoFaceGroup =
    execute<MergeFaceGroupsAnswer>(PhotoDocuments.MERGE_PHOTO_FACE_GROUPS, mapOf("photoFaceGroupIds" to photoIdsVariable(photoFaceGroupIds))).MergePhotoFaceGroups

@Serializable private class RemoveFaceAnswer(val RemovePhotoFaceFromGroup: PhotoFaceGroup)

suspend fun Connection.removePhotoFaceFromGroup(photoFaceId: String): PhotoFaceGroup =
    execute<RemoveFaceAnswer>(PhotoDocuments.REMOVE_PHOTO_FACE_FROM_GROUP, mapOf("photoFaceId" to JsonPrimitive(photoFaceId))).RemovePhotoFaceFromGroup

@Serializable private class HideFaceGroupAnswer(val HidePhotoFaceGroup: PhotoFaceGroup)

suspend fun Connection.hidePhotoFaceGroup(photoFaceGroupId: String, isHidden: Boolean): PhotoFaceGroup =
    execute<HideFaceGroupAnswer>(
        PhotoDocuments.HIDE_PHOTO_FACE_GROUP, mapOf("photoFaceGroupId" to JsonPrimitive(photoFaceGroupId), "isHidden" to JsonPrimitive(isHidden)),
    ).HidePhotoFaceGroup

@Serializable private class SettingAnswer(val SetPhotoFaceRecognition: Boolean = false, val SetPhotoReading: Boolean = false)

suspend fun Connection.setPhotoFaceRecognition(isEnabled: Boolean): Boolean =
    execute<SettingAnswer>(PhotoDocuments.SET_PHOTO_FACE_RECOGNITION, mapOf("isEnabled" to JsonPrimitive(isEnabled))).SetPhotoFaceRecognition

suspend fun Connection.setPhotoReading(isEnabled: Boolean): Boolean =
    execute<SettingAnswer>(PhotoDocuments.SET_PHOTO_READING, mapOf("isEnabled" to JsonPrimitive(isEnabled))).SetPhotoReading

// Sharing

@Serializable private class ListSharesAnswer(val ListPhotoAlbumShares: List<PhotoAlbumShare>)

suspend fun Connection.photoAlbumShares(photoAlbumId: String): List<PhotoAlbumShare> =
    execute<ListSharesAnswer>(PhotoDocuments.LIST_PHOTO_ALBUM_SHARES, mapOf("photoAlbumId" to JsonPrimitive(photoAlbumId))).ListPhotoAlbumShares

@Serializable private class ShareAnswer(val SharePhotoAlbum: PhotoAlbumShare)

suspend fun Connection.sharePhotoAlbum(photoAlbumId: String, draft: PhotoShareDraft): PhotoAlbumShare =
    execute<ShareAnswer>(PhotoDocuments.SHARE_PHOTO_ALBUM, draft.variables(photoAlbumId)).SharePhotoAlbum

@Serializable private class UpdateShareAnswer(val UpdatePhotoAlbumShare: PhotoAlbumShare)

suspend fun Connection.updatePhotoAlbumShare(
    photoAlbumShareId: String,
    photoShareLevel: PhotoShareLevel,
    isLocationIncluded: Boolean,
    isFaceNamesIncluded: Boolean,
): PhotoAlbumShare = execute<UpdateShareAnswer>(
    PhotoDocuments.UPDATE_PHOTO_ALBUM_SHARE,
    mapOf(
        "photoAlbumShareId" to JsonPrimitive(photoAlbumShareId), "shareLevel" to JsonPrimitive(photoShareLevel.serverName),
        "isLocationIncluded" to JsonPrimitive(isLocationIncluded), "isFaceNamesIncluded" to JsonPrimitive(isFaceNamesIncluded),
    ),
).UpdatePhotoAlbumShare

@Serializable private class RemoveShareAnswer(val RemovePhotoAlbumShare: Boolean = true)

suspend fun Connection.removePhotoAlbumShare(photoAlbumShareId: String) {
    execute<RemoveShareAnswer>(PhotoDocuments.REMOVE_PHOTO_ALBUM_SHARE, mapOf("photoAlbumShareId" to JsonPrimitive(photoAlbumShareId)))
}

@Serializable private class SavePhotoAnswer(val SavePhotoToLibrary: Photo? = null, val SavePhotoFromAttachment: Photo? = null)

/** Keeps a photo somebody shared in the person's own library. */
suspend fun Connection.savePhotoToLibrary(photoId: String): Photo =
    execute<SavePhotoAnswer>(PhotoDocuments.SAVE_PHOTO_TO_LIBRARY, mapOf("photoId" to JsonPrimitive(photoId))).SavePhotoToLibrary
        ?: throw com.teanode.kit.TeaNodeError.UnreadableAnswer

suspend fun Connection.savePhotoFromAttachment(agentAttachmentId: String): Photo =
    execute<SavePhotoAnswer>(PhotoDocuments.SAVE_PHOTO_FROM_ATTACHMENT, mapOf("agentAttachmentId" to JsonPrimitive(agentAttachmentId))).SavePhotoFromAttachment
        ?: throw com.teanode.kit.TeaNodeError.UnreadableAnswer
