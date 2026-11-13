package com.teanode.kit.contacts


/** The GraphQL documents, the same text as the iPhone app's (ios/TeaNodeKit): the server's tests check that text against its schema. */
object PersonVisitsDocuments {
    private const val D = "$"

    const val LOCATION_PLACE_FIELDS = """id userId placeName isNamedByPerson placeLabel latitudeDegrees longitudeDegrees radiusMeters osmType osmId osmName osmCategory addressText isAddressTyped
visitCount lastVisitAt namingErrorMessage createdAt updatedAt"""

    const val PLACE_VISIT_FIELDS = """id arrivedAt departedAt durationSeconds latitudeDegrees longitudeDegrees accuracyMeters visitSource
photoCount photosTakenBefore
photos {
  id createdAt photoKind takenAt takenAtOffsetMinutes widthPixels heightPixels durationMS latitude longitude placeName cameraMake cameraModel
  originalFilename isFavorite isScreenshot photoProcessingState trashedAt smallPreviewUrl largePreviewUrl videoPreviewUrl
  photoFileRoles processingErrorMessage isOwnedByCaller ownerName
}
place {
  id userId placeName isNamedByPerson placeLabel latitudeDegrees longitudeDegrees radiusMeters osmType osmId osmName osmCategory addressText isAddressTyped
  visitCount lastVisitAt namingErrorMessage createdAt updatedAt
}"""

    const val LOCATION_VISITS = """query (${D}userId: String, ${D}from: DateTime, ${D}until: DateTime, ${D}placeId: String, ${D}placeLabel: String, ${D}offset: Int, ${D}limitCount: Int) {
  ListLocationVisits(userId: ${D}userId, from: ${D}from, until: ${D}until, placeId: ${D}placeId, placeLabel: ${D}placeLabel, offset: ${D}offset, limitCount: ${D}limitCount) {
    placeVisits { ${PLACE_VISIT_FIELDS} }
    nextOffset
  }
}"""

    const val LOCATION_PLACES = """query (${D}userId: String, ${D}placeLabel: String, ${D}query: String, ${D}offset: Int, ${D}limitCount: Int) {
  ListPlaces(userId: ${D}userId, placeLabel: ${D}placeLabel, query: ${D}query, offset: ${D}offset, limitCount: ${D}limitCount) {
    places { ${LOCATION_PLACE_FIELDS} }
    nextOffset
  }
}"""

}