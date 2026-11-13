package com.teanode.app.photos

import android.content.Context
import com.teanode.app.R
import com.teanode.kit.photos.PhotoError

/** The words for why a photo could not be read or saved, in the person's language (the kit has none: ios PhotoErrorText.swift). */
fun photoErrorWords(context: Context, error: PhotoError): String = context.getString(
    when (error) {
        PhotoError.FileCouldNotBeRead -> R.string.photos_error_could_not_read
        PhotoError.FileNotAvailable -> R.string.photos_error_not_available
        PhotoError.NothingToSaveYet -> R.string.photos_error_nothing_to_save
        PhotoError.NoAddressBook -> R.string.photos_error_no_address_book
    },
)
