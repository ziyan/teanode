package com.teanode.kit.contacts


/** The GraphQL documents, the same text as the iPhone app's (ios/TeaNodeKit): the server's tests check that text against its schema. */
object PersonCardDocuments {
    private const val D = "$"

    const val PERSON_CARD_VISIT_FIELDS = """id arrivedAt departedAt durationSeconds latitudeDegrees longitudeDegrees accuracyMeters visitSource
place {
  id userId placeName isNamedByPerson placeLabel latitudeDegrees longitudeDegrees radiusMeters osmType osmId osmName osmCategory addressText isAddressTyped
  visitCount lastVisitAt namingErrorMessage createdAt updatedAt
}"""

    const val PERSON_LOCATION_FIELDS = """userId username name avatarUrl avatarColorIndex shareLevel grantedShareLevel
pause { id startedAt endedAt }
notificationMute { mutedUntil }
latestPoint { latitudeDegrees longitudeDegrees accuracyMeters altitudeMeters speedMetersPerSecond courseDegrees measuredAt locationSource }
openVisit {
  id arrivedAt departedAt durationSeconds latitudeDegrees longitudeDegrees accuracyMeters visitSource
  place {
    id userId placeName isNamedByPerson placeLabel latitudeDegrees longitudeDegrees radiusMeters osmType osmId osmName osmCategory addressText isAddressTyped
    visitCount lastVisitAt namingErrorMessage createdAt updatedAt
  }
}"""

    const val HEALTH_TYPE_FIELDS = """healthType healthCategory typeName valueKind quantityUnit summaryKind firstDay lastDay sampleCount
latestDaySummary { summaryDay quantityUnit sumValue averageValue minimumValue maximumValue sampleCount durationSeconds categorySeconds }"""

    const val PHOTO_FIELDS = """id createdAt photoKind takenAt takenAtOffsetMinutes widthPixels heightPixels durationMS latitude longitude placeName cameraMake cameraModel
originalFilename isFavorite isScreenshot photoProcessingState trashedAt smallPreviewUrl largePreviewUrl videoPreviewUrl
photoFileRoles processingErrorMessage isOwnedByCaller ownerName"""

    const val PHOTO_ALBUM_FIELDS = """id albumTitle createdAt updatedAt coverPhotoId photoCount firstTakenAt lastTakenAt coverPreviewUrl
ownerUserId circleId ownerUsername ownerName ownerAvatarUrl ownerAvatarColorIndex circleName
isOwnedByCaller shareLevel canCallerContribute canCallerManage isLocationIncluded"""

    const val PERSON_CARD = """query (${D}userId: String, ${D}addressBookId: String, ${D}contactId: String, ${D}itemLimit: Int) {
  ReadPersonCard(userId: ${D}userId, addressBookId: ${D}addressBookId, contactId: ${D}contactId, itemLimit: ${D}itemLimit) {
    person { userId username name avatarUrl avatarColorIndex memberRole joinedAt relationshipKind relationshipTermGender relationshipWord linkedContactId }
    contact {
      id addressBookId name organization emails phones note hasPhoto
      addresses { street locality region postalCode country }
      accountUserId account { userId username name avatarUrl avatarColorIndex }
    }
    sharedCircles { circleId circleName circleKind }
    canMessage
    location { sectionState location { ${PERSON_LOCATION_FIELDS} } todayVisits { ${PERSON_CARD_VISIT_FIELDS} } hasMoreTodayVisits }
    health { sectionState healthTypes { ${HEALTH_TYPE_FIELDS} } hasMore }
    finance {
      sectionState hasMore allFinanceShareLevel
      financeAccounts {
        financeAccountId ownerUserId ownerUsername ownerName financeShareLevel accountName accountMask accountKind currencyCode currentBalance balanceAt
        creditUsage { owedAmount creditLimitAmount creditLimitSource usageShare }
      }
    }
    photos { photosState photos { ${PHOTO_FIELDS} } hasMorePhotos albumsState albums { ${PHOTO_ALBUM_FIELDS} } hasMoreAlbums }
    mail { sectionState mails { mailboxItemId mailboxId folderPath subject fromAddress fromName isFromPerson isUnread receivedAt snippet } hasMore }
    recordings { sectionState recordings { recordingId title recordedAt durationMS spokenPassageCount } hasMore }
    memory { sectionState pages { nodePath nodeName nodeKind summary updatedAt isMatchedByName } hasMore }
  }
}"""

}