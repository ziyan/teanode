package com.teanode.kit.journal

// A narrative is a few paragraphs separated by blank lines, in which a record the agent mentions is written
// [[kind:id|the words its sentence shows]], or [[kind:id]]. The server answers each mention beside the narrative (its
// title and where it opens), and the screen draws the mention as its words, or the record's title when it has none,
// which opens the record where the app has a screen for it.

/** JournalNarrativePiece is a piece of a paragraph: plain words, or a mention of a record with the words the story wrote for it (empty when it wrote none, for the title to stand in). */
sealed class JournalNarrativePiece {
    data class Words(val text: String) : JournalNarrativePiece()

    data class Mention(val record: JournalRecord, val words: String) : JournalNarrativePiece()
}

/** JournalNarrativeText reads a narrative into paragraphs of pieces. */
object JournalNarrativeText {
    /**
     * The narrative's paragraphs. A mention whose record the server resolved is a piece of its own; one it could not
     * (the record is gone, or no longer the person's, or it is not among the references) is left out of the words,
     * since its title is not known.
     */
    fun paragraphs(narrative: String, references: List<JournalRecord>): List<List<JournalNarrativePiece>> {
        val recordsByReference = references.associateBy { record -> record.reference }
        return narrative.split("\n\n").map { paragraph -> paragraph.trim() }.filter { paragraph -> paragraph.isNotEmpty() }
            .map { paragraph -> pieces(paragraph, recordsByReference) }.filter { pieces -> pieces.isNotEmpty() }
    }

    private fun pieces(paragraph: String, recordsByReference: Map<String, JournalRecord>): List<JournalNarrativePiece> {
        val pieces = ArrayList<JournalNarrativePiece>()
        var rest = paragraph
        while (true) {
            val open = rest.indexOf("[[")
            if (open < 0) break
            val close = rest.indexOf("]]", open + 2)
            if (close < 0) break
            val inside = rest.substring(open + 2, close)
            val bar = inside.indexOf('|')
            val reference = if (bar < 0) inside else inside.substring(0, bar)
            val mentionWords = if (bar < 0) "" else inside.substring(bar + 1).trim()
            val before = rest.substring(0, open)
            if (isReference(reference)) {
                if (before.isNotEmpty()) pieces.add(JournalNarrativePiece.Words(before))
                val record = recordsByReference[reference]
                if (record != null && !record.isUnavailable && record.title.isNotEmpty()) {
                    pieces.add(JournalNarrativePiece.Mention(record, mentionWords))
                }
                rest = rest.substring(close + 2)
            } else {
                // Not a mention: the brackets are the writer's own words.
                pieces.add(JournalNarrativePiece.Words("$before[["))
                rest = rest.substring(open + 2)
            }
        }
        if (rest.isNotEmpty()) pieces.add(JournalNarrativePiece.Words(rest))
        return tidy(pieces)
    }

    /**
     * Whether text is kind:id: a kind in lowercase letters and underscores, then an id, which may itself hold colons
     * and an @ (an occurrence of a repeating event is eventId@start).
     */
    internal fun isReference(text: String): Boolean {
        val colon = text.indexOf(':')
        if (colon < 0) return false
        val kind = text.substring(0, colon)
        val identifier = text.substring(colon + 1)
        return kind.isNotEmpty() && identifier.isNotEmpty() && kind.all { character -> character.isLowerCase() && character.isLetter() || character == '_' } &&
            !identifier.contains(' ')
    }

    /** Joins neighbouring words and takes the space a left-out mention would leave doubled. */
    private fun tidy(pieces: List<JournalNarrativePiece>): List<JournalNarrativePiece> {
        val tidied = ArrayList<JournalNarrativePiece>()
        for (piece in pieces) {
            val previous = tidied.lastOrNull()
            if (piece is JournalNarrativePiece.Words && previous is JournalNarrativePiece.Words) {
                tidied[tidied.size - 1] = JournalNarrativePiece.Words(previous.text + piece.text)
            } else {
                tidied.add(piece)
            }
        }
        return tidied.mapNotNull { piece ->
            if (piece !is JournalNarrativePiece.Words) return@mapNotNull piece
            val collapsed = piece.text.replace("  ", " ")
            if (collapsed.isEmpty()) null else JournalNarrativePiece.Words(collapsed)
        }
    }
}

/**
 * JournalRecordOpening is where the app takes a record a day mentions or links to. The kinds are memory's record kinds,
 * which the dashboard opens with a path; the app has a screen for some of them and a part for others.
 */
sealed class JournalRecordOpening {
    data class MailItem(val itemId: String) : JournalRecordOpening()

    data class Contact(val id: String) : JournalRecordOpening()

    data class Photo(val id: String) : JournalRecordOpening()

    /** A path the app follows as it follows the agent's open_page: a day on the map, a kind of Health data, a Finance part, a page of knowledge. */
    data class Path(val path: String) : JournalRecordOpening()

    /** A part of the app by its raw name ("calendar", "ideas"), when the app has no screen for the one record. */
    data class Part(val rawName: String) : JournalRecordOpening()

    /** The app has no screen for it. */
    data object Nowhere : JournalRecordOpening()

    /** Whether the app has somewhere to take the person. */
    val isOpenable: Boolean get() = this != Nowhere

    companion object {
        /**
         * Where the record opens. The kind decides first, since the app has a screen for that record; the dashboard's
         * path decides for the parts the app shows by day or by kind of data.
         */
        fun of(recordKind: String, recordId: String, dashboardPath: String): JournalRecordOpening {
            when (recordKind) {
                "mail" -> return MailItem(recordId)
                "contact" -> return Contact(recordId)
                "photo" -> return Photo(recordId)
                "recording", "recording_passage" -> return Path("/recordings/$recordId")
                "calendar_event", "calendar_occurrence", "reminder" -> return Part("calendar")
                "finance_transaction", "finance_receipt" -> return Part("finance")
                "idea" -> return Part("ideas")
                "goal" -> return Part("goals")
                "shopping_trip" -> return Part("shopping")
            }
            val root = dashboardPath.substringBefore('?').split("/").firstOrNull { segment -> segment.isNotEmpty() }.orEmpty()
            if (root in listOf("location", "health", "knowledge")) return Path(dashboardPath)
            return Nowhere
        }

        fun of(record: JournalRecord): JournalRecordOpening = of(record.recordKind, record.recordId, record.dashboardPath)

        fun of(moment: JournalMoment): JournalRecordOpening = of(moment.recordKind, moment.recordId, moment.dashboardPath)
    }
}
