package com.teanode.kit.photos

/** The GraphQL documents of Photos and photo backup. Copied from the iPhone app's Documents (ios/TeaNodeKit), text unchanged. */
object PhotoDocuments {
    val PHOTO_FIELDS = """
id createdAt photoKind takenAt takenAtOffsetMinutes widthPixels heightPixels durationMS latitude longitude placeName cameraMake cameraModel
originalFilename isFavorite isScreenshot photoProcessingState trashedAt smallPreviewUrl largePreviewUrl videoPreviewUrl
photoFileRoles processingErrorMessage isOwnedByCaller ownerName
""".trimEnd()

    val LIST_PHOTOS = """
query (${'$'}beforePhotoId: String, ${'$'}pageSize: Int, ${'$'}isFavorite: Boolean, ${'$'}isTrashed: Boolean, ${'$'}albumId: String, ${'$'}faceGroupId: String, ${'$'}accountUserId: String, ${'$'}photoKind: String, ${'$'}takenAfter: DateTime, ${'$'}takenBefore: DateTime, ${'$'}searchText: String, ${'$'}nearLatitude: Float, ${'$'}nearLongitude: Float, ${'$'}nearDistanceKilometers: Float) {
  ListPhotos(beforePhotoId: ${'$'}beforePhotoId, pageSize: ${'$'}pageSize, isFavorite: ${'$'}isFavorite, isTrashed: ${'$'}isTrashed, albumId: ${'$'}albumId, faceGroupId: ${'$'}faceGroupId, accountUserId: ${'$'}accountUserId, photoKind: ${'$'}photoKind, takenAfter: ${'$'}takenAfter, takenBefore: ${'$'}takenBefore, searchText: ${'$'}searchText, nearLatitude: ${'$'}nearLatitude, nearLongitude: ${'$'}nearLongitude, nearDistanceKilometers: ${'$'}nearDistanceKilometers) { ${PHOTO_FIELDS} }
}
""".trimEnd()

    val PHOTO_DESCRIPTION_FIELDS = """
photoDescription descriptionVisibleText describedAt isDescriptionRequested photoDocumentKind
""".trimEnd()

    val PHOTO_FACE_FIELDS = """
id photoId boxLeftFraction boxTopFraction boxWidthFraction boxHeightFraction photoFaceGroupId contactId contactName isOwnContact facePictureUrl
""".trimEnd()

    val READ_PHOTO = """
query (${'$'}photoId: String!) {
  ReadPhoto(photoId: ${'$'}photoId) {
    photo { ${PHOTO_FIELDS} ${PHOTO_DESCRIPTION_FIELDS} }
    photoFiles { photoFileRole contentType fileSizeBytes originalFilename fileUrl }
    photoPreviews { photoPreviewSize contentType previewSizeBytes previewUrl }
    albums { id albumTitle }
    faces { ${PHOTO_FACE_FIELDS} }
  }
}
""".trimEnd()

    val LOCATION_PLACE_FIELDS = """
id userId placeName isNamedByPerson placeLabel latitudeDegrees longitudeDegrees radiusMeters osmType osmId osmName osmCategory addressText isAddressTyped
visitCount lastVisitAt namingErrorMessage createdAt updatedAt
""".trimEnd()

    val READ_PHOTO_PLACE = """
query (${'$'}photoId: String!) {
  ReadPhotoPlace(photoId: ${'$'}photoId) {
    locationPlace { ${LOCATION_PLACE_FIELDS} }
    addressText
  }
}
""".trimEnd()

    val DESCRIBE_PHOTO = """
mutation (${'$'}photoId: String!, ${'$'}isRedescribed: Boolean) {
  DescribePhoto(photoId: ${'$'}photoId, isRedescribed: ${'$'}isRedescribed) { ${PHOTO_DESCRIPTION_FIELDS} }
}
""".trimEnd()

    val PHOTO_PLAYBACK_ADDRESS = """
query (${'$'}photoId: String!, ${'$'}photoPreviewSize: String, ${'$'}photoFileRole: String) {
  PhotoPlaybackAddress(photoId: ${'$'}photoId, photoPreviewSize: ${'$'}photoPreviewSize, photoFileRole: ${'$'}photoFileRole)
}
""".trimEnd()

    val READ_PHOTO_LIBRARY = """
query {
  ReadPhotoLibrary {
    photoCount videoCount favoriteCount trashedCount usedBytes maximumFileBytes maximumAccountBytes trashRetentionDays
    isDescriptionAvailable descriptionUnavailableMessage
    isFaceRecognitionAvailable faceRecognitionUnavailableMessage isPhotoFaceRecognitionEnabled isPhotoReadingEnabled
  }
}
""".trimEnd()

    val UPDATE_PHOTO = """
mutation (${'$'}photoId: String!, ${'$'}isFavorite: Boolean, ${'$'}takenAt: DateTime) {
  UpdatePhoto(photoId: ${'$'}photoId, isFavorite: ${'$'}isFavorite, takenAt: ${'$'}takenAt) { ${PHOTO_FIELDS} }
}
""".trimEnd()

    val TRASH_PHOTOS = """
mutation (${'$'}photoIds: [String!]!) { TrashPhotos(photoIds: ${'$'}photoIds) }
""".trimEnd()

    val SEARCH_PHOTO_PLACES = """
query (${'$'}query: String!, ${'$'}nearLatitude: Float, ${'$'}nearLongitude: Float, ${'$'}limitCount: Int) {
  SearchPhotoPlaces(query: ${'$'}query, nearLatitude: ${'$'}nearLatitude, nearLongitude: ${'$'}nearLongitude, limitCount: ${'$'}limitCount) {
    placeName latitude longitude distanceKilometers
  }
}
""".trimEnd()

    val SET_PHOTO_LOCATION = """
mutation (${'$'}photoIds: [String!]!, ${'$'}latitude: Float!, ${'$'}longitude: Float!) {
  SetPhotoLocation(photoIds: ${'$'}photoIds, latitude: ${'$'}latitude, longitude: ${'$'}longitude)
}
""".trimEnd()

    val REMOVE_PHOTO_LOCATION = """
mutation (${'$'}photoIds: [String!]!) { RemovePhotoLocation(photoIds: ${'$'}photoIds) }
""".trimEnd()

    val RESTORE_PHOTOS = """
mutation (${'$'}photoIds: [String!]!) { RestorePhotos(photoIds: ${'$'}photoIds) }
""".trimEnd()

    val DELETE_PHOTOS = """
mutation (${'$'}photoIds: [String!]!) { DeletePhotos(photoIds: ${'$'}photoIds) }
""".trimEnd()

    val RETRY_PHOTO_PROCESSING = """
mutation (${'$'}photoIds: [String!]!) { RetryPhotoProcessing(photoIds: ${'$'}photoIds) }
""".trimEnd()

    val EMPTY_PHOTO_TRASH = """
mutation { EmptyPhotoTrash }
""".trimEnd()

    val PHOTO_ALBUM_FIELDS = """
id albumTitle createdAt updatedAt coverPhotoId photoCount firstTakenAt lastTakenAt coverPreviewUrl
ownerUserId circleId ownerUsername ownerName ownerAvatarUrl ownerAvatarColorIndex circleName
isOwnedByCaller shareLevel canCallerContribute canCallerManage isLocationIncluded
""".trimEnd()

    val LIST_PHOTO_ALBUMS = """
query (${'$'}circleId: String) { ListPhotoAlbums(circleId: ${'$'}circleId) { ${PHOTO_ALBUM_FIELDS} } }
""".trimEnd()

    val SUGGEST_PHOTO_ALBUMS = """
query { SuggestPhotoAlbums { takenAfter takenBefore placeNames albumTitle photoCount samplePhotoIds } }
""".trimEnd()

    val CREATE_PHOTO_ALBUM = """
mutation (${'$'}albumTitle: String!, ${'$'}photoIds: [String!], ${'$'}circleId: String) {
  CreatePhotoAlbum(albumTitle: ${'$'}albumTitle, photoIds: ${'$'}photoIds, circleId: ${'$'}circleId) { ${PHOTO_ALBUM_FIELDS} }
}
""".trimEnd()

    val RENAME_PHOTO_ALBUM = """
mutation (${'$'}photoAlbumId: String!, ${'$'}albumTitle: String!) { RenamePhotoAlbum(photoAlbumId: ${'$'}photoAlbumId, albumTitle: ${'$'}albumTitle) { ${PHOTO_ALBUM_FIELDS} } }
""".trimEnd()

    val DELETE_PHOTO_ALBUM = """
mutation (${'$'}photoAlbumId: String!) { DeletePhotoAlbum(photoAlbumId: ${'$'}photoAlbumId) }
""".trimEnd()

    val ADD_PHOTOS_TO_ALBUM = """
mutation (${'$'}photoAlbumId: String!, ${'$'}photoIds: [String!]!) { AddPhotosToAlbum(photoAlbumId: ${'$'}photoAlbumId, photoIds: ${'$'}photoIds) }
""".trimEnd()

    val REMOVE_PHOTOS_FROM_ALBUM = """
mutation (${'$'}photoAlbumId: String!, ${'$'}photoIds: [String!]!) { RemovePhotosFromAlbum(photoAlbumId: ${'$'}photoAlbumId, photoIds: ${'$'}photoIds) }
""".trimEnd()

    val SET_PHOTO_ALBUM_COVER = """
mutation (${'$'}photoAlbumId: String!, ${'$'}photoId: String!) { SetPhotoAlbumCover(photoAlbumId: ${'$'}photoAlbumId, photoId: ${'$'}photoId) { ${PHOTO_ALBUM_FIELDS} } }
""".trimEnd()

    val PHOTO_FACE_GROUP_FIELDS = """
id createdAt updatedAt contactId contactAddressBookId contactName isOwnContact faceCount photoCount facePictureUrl coverPhotoFaceId isHidden
accountUserId account { userId username name avatarUrl avatarColorIndex }
""".trimEnd()

    val LIST_PHOTO_FACE_GROUPS = """
query (${'$'}isHiddenIncluded: Boolean) { ListPhotoFaceGroups(isHiddenIncluded: ${'$'}isHiddenIncluded) { ${PHOTO_FACE_GROUP_FIELDS} } }
""".trimEnd()

    val READ_PHOTO_FACE_GROUP = """
query (${'$'}photoFaceGroupId: String!) { ReadPhotoFaceGroup(photoFaceGroupId: ${'$'}photoFaceGroupId) { ${PHOTO_FACE_GROUP_FIELDS} } }
""".trimEnd()

    val NAME_PHOTO_FACE_GROUP = """
mutation (${'$'}photoFaceGroupId: String!, ${'$'}contactId: String!) { NamePhotoFaceGroup(photoFaceGroupId: ${'$'}photoFaceGroupId, contactId: ${'$'}contactId) { photoFaceGroup { ${PHOTO_FACE_GROUP_FIELDS} } mergedPhotoFaceGroupId isContactPictureSet } }
""".trimEnd()

    val UNNAME_PHOTO_FACE_GROUP = """
mutation (${'$'}photoFaceGroupId: String!) { UnnamePhotoFaceGroup(photoFaceGroupId: ${'$'}photoFaceGroupId) { ${PHOTO_FACE_GROUP_FIELDS} } }
""".trimEnd()

    val MERGE_PHOTO_FACE_GROUPS = """
mutation (${'$'}photoFaceGroupIds: [String!]!) { MergePhotoFaceGroups(photoFaceGroupIds: ${'$'}photoFaceGroupIds) { ${PHOTO_FACE_GROUP_FIELDS} } }
""".trimEnd()

    val REMOVE_PHOTO_FACE_FROM_GROUP = """
mutation (${'$'}photoFaceId: String!) { RemovePhotoFaceFromGroup(photoFaceId: ${'$'}photoFaceId) { ${PHOTO_FACE_GROUP_FIELDS} } }
""".trimEnd()

    val HIDE_PHOTO_FACE_GROUP = """
mutation (${'$'}photoFaceGroupId: String!, ${'$'}isHidden: Boolean!) { HidePhotoFaceGroup(photoFaceGroupId: ${'$'}photoFaceGroupId, isHidden: ${'$'}isHidden) { ${PHOTO_FACE_GROUP_FIELDS} } }
""".trimEnd()

    val SET_PHOTO_FACE_RECOGNITION = """
mutation (${'$'}isEnabled: Boolean!) { SetPhotoFaceRecognition(isEnabled: ${'$'}isEnabled) }
""".trimEnd()

    val SET_PHOTO_READING = """
mutation (${'$'}isEnabled: Boolean!) { SetPhotoReading(isEnabled: ${'$'}isEnabled) }
""".trimEnd()

    val CREATE_NAMED_CONTACT = """
mutation (${'$'}addressBookId: String!, ${'$'}name: String) { SaveContact(addressBookId: ${'$'}addressBookId, name: ${'$'}name) { id name } }
""".trimEnd()

    val PHOTO_ALBUM_SHARE_FIELDS = """
id photoAlbumId granteeUserId granteeUsername granteeName granteeAvatarUrl granteeAvatarColorIndex granteeCircleId granteeCircleName
shareLevel isLocationIncluded isFaceNamesIncluded createdAt
""".trimEnd()

    val LIST_PHOTO_ALBUM_SHARES = """
query (${'$'}photoAlbumId: String!) { ListPhotoAlbumShares(photoAlbumId: ${'$'}photoAlbumId) { ${PHOTO_ALBUM_SHARE_FIELDS} } }
""".trimEnd()

    val SHARE_PHOTO_ALBUM = """
mutation (${'$'}photoAlbumId: String!, ${'$'}granteeUserId: String, ${'$'}granteeCircleId: String, ${'$'}shareLevel: String!, ${'$'}isLocationIncluded: Boolean, ${'$'}isFaceNamesIncluded: Boolean) {
  SharePhotoAlbum(photoAlbumId: ${'$'}photoAlbumId, granteeUserId: ${'$'}granteeUserId, granteeCircleId: ${'$'}granteeCircleId, shareLevel: ${'$'}shareLevel, isLocationIncluded: ${'$'}isLocationIncluded, isFaceNamesIncluded: ${'$'}isFaceNamesIncluded) { ${PHOTO_ALBUM_SHARE_FIELDS} }
}
""".trimEnd()

    val UPDATE_PHOTO_ALBUM_SHARE = """
mutation (${'$'}photoAlbumShareId: String!, ${'$'}shareLevel: String!, ${'$'}isLocationIncluded: Boolean, ${'$'}isFaceNamesIncluded: Boolean) {
  UpdatePhotoAlbumShare(photoAlbumShareId: ${'$'}photoAlbumShareId, shareLevel: ${'$'}shareLevel, isLocationIncluded: ${'$'}isLocationIncluded, isFaceNamesIncluded: ${'$'}isFaceNamesIncluded) { ${PHOTO_ALBUM_SHARE_FIELDS} }
}
""".trimEnd()

    val REMOVE_PHOTO_ALBUM_SHARE = """
mutation (${'$'}photoAlbumShareId: String!) { RemovePhotoAlbumShare(photoAlbumShareId: ${'$'}photoAlbumShareId) }
""".trimEnd()

    val SAVE_PHOTO_TO_LIBRARY = """
mutation (${'$'}photoId: String!) { SavePhotoToLibrary(photoId: ${'$'}photoId) { ${PHOTO_FIELDS} } }
""".trimEnd()

    val SAVE_PHOTO_FROM_ATTACHMENT = """
mutation (${'$'}agentAttachmentId: String!) { SavePhotoFromAttachment(agentAttachmentId: ${'$'}agentAttachmentId) { ${PHOTO_FIELDS} } }
""".trimEnd()

    val CREATE_PHOTO_BACKUP_CREDENTIAL = """
mutation (${'$'}credentialName: String!) {
  CreateSyncCredential(credentialName: ${'$'}credentialName, hasCalendarAccess: false, hasRemindersAccess: false, hasContactsAccess: false, hasPhotosAccess: true) {
    credential { id credentialName }
    username password
  }
}
""".trimEnd()

    val FIND_PHOTOS = """
query (${'$'}clientPhotoIds: [String!]!) { FindPhotos(clientPhotoIds: ${'$'}clientPhotoIds) { id clientPhotoId photoKind photoFileRoles } }
""".trimEnd()

    val ADDRESS_BOOKS = """
query { ListAddressBooks { id name contacts } }
""".trimEnd()

    val CONTACTS = """
query (${'$'}addressBookId: String!, ${'$'}query: String, ${'$'}first: Int) {
  ListContacts(addressBookId: ${'$'}addressBookId, query: ${'$'}query, first: ${'$'}first) {
    id name organization emails phones hasPhoto addresses { written } accountUserId
  }
}
""".trimEnd()

}
