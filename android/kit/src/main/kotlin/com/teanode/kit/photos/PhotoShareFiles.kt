package com.teanode.kit.photos

/** How photos are handed to the apps the person shares them with: the names of the files, and what kind they are told to be. */
object PhotoShareFiles {
    /**
     * A name for a file in a folder of shared files: the server's name with anything that could leave the folder taken out,
     * and a number put before the extension when another file of the folder has the name already.
     */
    fun fileName(wanted: String, taken: Set<String>): String {
        val cleaned = wanted.substringAfterLast('/').substringAfterLast('\\').trim().trimStart('.').ifEmpty { "photo" }
        if (cleaned !in taken) return cleaned
        val stem = cleaned.substringBeforeLast('.', cleaned)
        val extension = if (cleaned.contains('.')) "." + cleaned.substringAfterLast('.') else ""
        var number = 2
        while ("$stem $number$extension" in taken) number++
        return "$stem $number$extension"
    }

    /** The kind the share is offered as: the kind all the files have, else any kind, so that the apps that take either are listed. */
    fun commonContentType(contentTypes: List<String>): String {
        val kinds = contentTypes.map { contentType -> contentType.substringBefore('/').lowercase().ifEmpty { "*" } }.distinct()
        return if (kinds.size == 1 && kinds.first() != "*") kinds.first() + "/*" else "*/*"
    }
}
