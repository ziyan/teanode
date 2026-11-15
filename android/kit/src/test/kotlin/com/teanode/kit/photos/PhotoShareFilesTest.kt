package com.teanode.kit.photos

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test

class PhotoShareFilesTest {
    @Test
    fun aNameIsKeptWhenNothingHasItAndNumberedWhenSomethingDoes() {
        assertEquals("IMG_1.jpg", PhotoShareFiles.fileName("IMG_1.jpg", emptySet()))
        assertEquals("IMG_1 2.jpg", PhotoShareFiles.fileName("IMG_1.jpg", setOf("IMG_1.jpg")))
        assertEquals("IMG_1 3.jpg", PhotoShareFiles.fileName("IMG_1.jpg", setOf("IMG_1.jpg", "IMG_1 2.jpg")))
        assertEquals("clip 2", PhotoShareFiles.fileName("clip", setOf("clip")))
    }

    @Test
    fun aNameCannotLeaveTheFolderOrBeHidden() {
        assertEquals("passwd", PhotoShareFiles.fileName("../../etc/passwd", emptySet()))
        assertEquals("evil.jpg", PhotoShareFiles.fileName("..\\evil.jpg", emptySet()))
        assertEquals("photo", PhotoShareFiles.fileName("  ", emptySet()))
        assertEquals("hidden.jpg", PhotoShareFiles.fileName(".hidden.jpg", emptySet()))
    }

    @Test
    fun filesOfOneKindAreOfferedAsThatKindAndMixedFilesAsAnything() {
        assertEquals("image/*", PhotoShareFiles.commonContentType(listOf("image/jpeg", "image/heic")))
        assertEquals("video/*", PhotoShareFiles.commonContentType(listOf("video/mp4")))
        assertEquals("*/*", PhotoShareFiles.commonContentType(listOf("image/jpeg", "video/mp4")))
        assertEquals("*/*", PhotoShareFiles.commonContentType(listOf("")))
    }
}
