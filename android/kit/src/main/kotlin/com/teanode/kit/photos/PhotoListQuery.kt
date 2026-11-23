package com.teanode.kit.photos

import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import java.time.ZoneId

/**
 * PhotoListQuery says which photos a list holds, in the arguments ListPhotos and CountPhotos share: a part of the
 * library with a filter, the photos of an album, or the photos of one person.
 */
data class PhotoListQuery(
    val collection: PhotoCollection = PhotoCollection.Library,
    val filter: PhotoFilter = PhotoFilter(),
    val photoAlbumId: String? = null,
    val photoFaceGroupId: String? = null,
) {
    fun variables(zone: ZoneId = ZoneId.systemDefault()): Map<String, JsonElement> {
        val variables = mutableMapOf<String, JsonElement>()
        collection.isFavoriteFilter?.let { isFavorite -> variables["isFavorite"] = JsonPrimitive(isFavorite) }
        collection.isTrashedFilter?.let { isTrashed -> variables["isTrashed"] = JsonPrimitive(isTrashed) }
        photoAlbumId?.let { albumId -> variables["albumId"] = JsonPrimitive(albumId) }
        photoFaceGroupId?.let { faceGroupId -> variables["faceGroupId"] = JsonPrimitive(faceGroupId) }
        variables.putAll(filter.variables(zone))
        return variables
    }
}
