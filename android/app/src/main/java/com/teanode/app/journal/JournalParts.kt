package com.teanode.app.journal

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.LinkAnnotation
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.TextLinkStyles
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.withLink
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.goals.ChatHandoff
import com.teanode.app.knowledge.KnowledgeRoutes
import com.teanode.app.shell.AppNavigator
import com.teanode.app.ui.components.rememberServerPicture
import com.teanode.kit.AppSection
import com.teanode.kit.journal.JournalMoodLevel
import com.teanode.kit.journal.JournalNarrative
import com.teanode.kit.journal.JournalNarrativePiece
import com.teanode.kit.journal.JournalNarrativeText
import com.teanode.kit.journal.JournalPhoto
import com.teanode.kit.journal.JournalRecord
import com.teanode.kit.journal.JournalRecordOpening
import com.teanode.kit.journal.JournalSleepStage

/** The serif face the journal writes in (the person's words and the story), which Android calls Serif. */
val journalWritingFace: FontFamily = FontFamily.Serif

/**
 * Takes the person to a record a day mentions, in the part of the app that shows it. Each part registers its own
 * places, so a record opens its part where the app has no place for the one record (ios JournalRecordOpener).
 */
fun openJournalRecord(navigator: AppNavigator, opening: JournalRecordOpening) {
    when (opening) {
        is JournalRecordOpening.MailItem -> navigator.openSection(AppSection.Mail)
        is JournalRecordOpening.Contact -> navigator.openSection(AppSection.Contacts)
        is JournalRecordOpening.Photo -> navigator.openSection(AppSection.Photos)
        is JournalRecordOpening.Path -> openJournalPath(navigator, opening.path)
        is JournalRecordOpening.Part -> AppSection.fromRouteName(opening.rawName)?.let { section -> navigator.openSection(section) }
        JournalRecordOpening.Nowhere -> Unit
    }
}

/** Follows a path the way the agent's open_page does: a day on the map, a kind of Health data, a recording, a page of knowledge. */
fun openJournalPath(navigator: AppNavigator, path: String) {
    val root = path.substringBefore('?').split("/").firstOrNull { segment -> segment.isNotEmpty() }
    when (root) {
        "recordings" -> navigator.openSection(AppSection.Recordings)
        "location" -> navigator.openSection(AppSection.Location)
        "health" -> navigator.openSection(AppSection.Health)
        "knowledge" -> {
            val pagePath = path.substringBefore('?').removePrefix("/knowledge").trim('/')
            if (pagePath.isEmpty() || pagePath.startsWith("notes")) navigator.openSection(AppSection.Knowledge) else navigator.navigate(KnowledgeRoutes.page(pagePath))
        }
        "agent" -> ChatHandoff.open(navigator, path.substringAfter("conversation=", "").substringBefore('&'))
    }
}

/** A row that opens something when it can: with a chevron at its end, as a row that opens a page has. A row with nowhere to go is only its content. */
@Composable
fun JournalOpenRow(isOpenable: Boolean, onOpen: () -> Unit, modifier: Modifier = Modifier, content: @Composable () -> Unit) {
    if (isOpenable) {
        Row(modifier.fillMaxWidth().clickable(onClick = onOpen).padding(horizontal = 16.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Box(Modifier.weight(1f)) { content() }
            Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, contentDescription = null, tint = MaterialTheme.colorScheme.outline)
        }
    } else {
        Box(modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp)) { content() }
    }
}

/** A row for one record of the day: a symbol, a title, and a line under it, opening the record where the app has a screen for it. */
@Composable
fun JournalRecordRow(
    recordKind: String,
    title: String,
    modifier: Modifier = Modifier,
    detail: String = "",
    opening: JournalRecordOpening = JournalRecordOpening.Nowhere,
    onOpen: (JournalRecordOpening) -> Unit,
    trailing: (@Composable () -> Unit)? = null,
) {
    JournalOpenRow(opening.isOpenable, { onOpen(opening) }, modifier) {
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.Top) {
            Icon(journalRecordIcon(recordKind), contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.width(24.dp))
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(title)
                if (detail.isNotEmpty()) Text(detail, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            trailing?.invoke()
        }
    }
}

/**
 * What the agent wrote, in the writing face, with each record it mentions in bold, as the words the story wrote for it
 * or its title, which opens the record.
 */
@Composable
fun JournalNarrativeView(narrative: JournalNarrative, onOpen: (JournalRecord) -> Unit, modifier: Modifier = Modifier) {
    val paragraphs = JournalNarrativeText.paragraphs(narrative.narrative, narrative.references)
    Column(modifier, verticalArrangement = Arrangement.spacedBy(12.dp)) {
        for (pieces in paragraphs) {
            Text(attributed(pieces, onOpen), style = MaterialTheme.typography.bodyLarge.copy(fontFamily = journalWritingFace, lineHeight = MaterialTheme.typography.bodyLarge.lineHeight * 1.15f))
        }
    }
}

private fun attributed(pieces: List<JournalNarrativePiece>, onOpen: (JournalRecord) -> Unit): AnnotatedString = buildAnnotatedString {
    for (piece in pieces) {
        when (piece) {
            is JournalNarrativePiece.Words -> append(piece.text)
            is JournalNarrativePiece.Mention -> {
                val words = piece.words.ifEmpty { piece.record.title }
                // A record the app has no screen for is only its title.
                if (JournalRecordOpening.of(piece.record).isOpenable) {
                    withLink(LinkAnnotation.Clickable("record", TextLinkStyles(SpanStyle(fontWeight = FontWeight.Bold))) { onOpen(piece.record) }) { append(words) }
                } else {
                    append(words)
                }
            }
        }
    }
}

/** A small preview of a photo, filling the space it is given. */
@Composable
fun JournalPhotoImage(photoId: String, modifier: Modifier = Modifier) {
    val picture = rememberServerPicture(JournalPhoto.smallPreviewPath(photoId))
    Box(modifier.background(MaterialTheme.colorScheme.surfaceVariant)) {
        if (picture != null) {
            androidx.compose.foundation.Image(picture, contentDescription = null, contentScale = ContentScale.Crop, modifier = Modifier.matchParentSize())
        }
    }
}

// MARK: Moods

/**
 * One of the five faces a day can have: a circle in the mood's color, two eyes and a mouth that turns more upward with
 * each level. A face that is not chosen is grey.
 */
@Composable
fun JournalMoodFace(level: JournalMoodLevel, modifier: Modifier = Modifier, isColored: Boolean = true) {
    val features = MaterialTheme.colorScheme.background
    val greyFace = MaterialTheme.colorScheme.surfaceVariant
    val greyFeatures = MaterialTheme.colorScheme.onSurfaceVariant
    Canvas(modifier) {
        val width = size.width
        val height = size.height
        // Where the mouth sits and how far its middle is pulled, as fractions of the face: positive pulls it down, into a smile.
        val (mouthHeight, pull) = when (level) {
            JournalMoodLevel.Awful -> 0.68f to -0.18f
            JournalMoodLevel.Low -> 0.64f to -0.09f
            JournalMoodLevel.Okay -> 0.62f to 0f
            JournalMoodLevel.Good -> 0.56f to 0.12f
            JournalMoodLevel.Great -> 0.52f to 0.22f
        }
        drawCircle(if (isColored) journalMoodColor(level) else greyFace, radius = width / 2, center = Offset(width / 2, height / 2))
        val featureColor = if (isColored) features else greyFeatures
        val eyeRadius = width * 0.055f
        for (eyeX in listOf(0.37f, 0.63f)) drawCircle(featureColor, radius = eyeRadius, center = Offset(width * eyeX, height * 0.38f))
        val mouth = Path().apply {
            moveTo(width * 0.32f, height * mouthHeight)
            quadraticTo(width * 0.5f, height * (mouthHeight + pull), width * 0.68f, height * mouthHeight)
        }
        drawPath(mouth, featureColor, style = Stroke(width = maxOf(1.5f, width * 0.06f), cap = StrokeCap.Round))
    }
}

/** The five faces in a row, each with its word under it. A tap chooses that mood. The one chosen is in color and the others grey. */
@Composable
fun JournalMoodPicker(chosenLevel: JournalMoodLevel?, onChoose: (JournalMoodLevel) -> Unit, modifier: Modifier = Modifier) {
    val text = rememberJournalText()
    val isLarge = LocalDensity.current.fontScale >= 1.5f
    if (isLarge) {
        // Five faces side by side do not fit at the largest text sizes, so each mood is a row of its own there, its face beside its name.
        Column(modifier) {
            for (level in JournalMoodLevel.entries) {
                Row(
                    Modifier.fillMaxWidth().clickable { onChoose(level) }.height(48.dp).semantics { selected = chosenLevel == level },
                    verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    JournalMoodFace(level, Modifier.size(36.dp), isColored = chosenLevel == null || chosenLevel == level)
                    Text(text.moodName(level), color = if (chosenLevel == level) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.weight(1f))
                }
            }
        }
    } else {
        Row(modifier.fillMaxWidth()) {
            for (level in JournalMoodLevel.entries) {
                Column(
                    Modifier.weight(1f).clickable { onChoose(level) }.padding(vertical = 4.dp).semantics { selected = chosenLevel == level; contentDescription = text.moodName(level) },
                    horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(4.dp),
                ) {
                    JournalMoodFace(level, Modifier.size(36.dp), isColored = chosenLevel == null || chosenLevel == level)
                    Text(
                        text.moodName(level), style = MaterialTheme.typography.labelSmall, maxLines = 1,
                        color = if (chosenLevel == level) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    }
}

// MARK: Bars

/** One part of a bar: a color, and how much of the whole it is. */
class JournalProportionPart(val color: Color, val weight: Int)

/** Parts side by side, each as long as its share of the whole. */
@Composable
fun JournalProportionBar(parts: List<JournalProportionPart>, modifier: Modifier = Modifier) {
    val total = maxOf(1, parts.sumOf { part -> part.weight })
    Canvas(modifier.fillMaxWidth().height(10.dp).clip(RoundedCornerShape(3.dp))) {
        var left = 0f
        for (part in parts) {
            val width = size.width * part.weight / total
            drawRect(part.color, topLeft = Offset(left, 0f), size = Size(width, size.height))
            left += width
        }
    }
}

/** A night's stages side by side, as long as the time in each. */
@Composable
fun JournalSleepBar(stages: List<JournalSleepStage>, modifier: Modifier = Modifier) {
    val text = rememberJournalText()
    val description = stages.joinToString(", ") { stage -> "${text.sleepStageName(stage.sleepStage)} ${text.durationText(stage.minutes)}" }
    JournalProportionBar(stages.map { stage -> JournalProportionPart(journalSleepStageColor(stage.sleepStage), stage.minutes) }, modifier.semantics { contentDescription = description })
}

/** "2 Low, 5 Okay, 13 Good, 5 Great". */
fun journalMoodCountsText(text: JournalText, moodCounts: List<Int>): String = JournalMoodLevel.entries.mapNotNull { level ->
    val count = moodCounts.getOrElse(level.moodLevel - 1) { 0 }
    if (count > 0) "$count ${text.moodName(level)}" else null
}.joinToString(", ")

/** How many days had each mood, side by side. */
@Composable
fun JournalMoodBar(moodCounts: List<Int>, modifier: Modifier = Modifier) {
    val text = rememberJournalText()
    val parts = JournalMoodLevel.entries.mapNotNull { level ->
        val count = moodCounts.getOrElse(level.moodLevel - 1) { 0 }
        if (count > 0) JournalProportionPart(journalMoodColor(level), count) else null
    }
    Column(modifier, verticalArrangement = Arrangement.spacedBy(6.dp)) {
        JournalProportionBar(parts)
        Text(journalMoodCountsText(text, moodCounts), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** The days of a month as small bars in their mood's color, in order. */
@Composable
fun JournalMoodStrip(dayMoods: List<Int>, modifier: Modifier = Modifier) {
    val none = MaterialTheme.colorScheme.surfaceVariant
    Row(modifier.fillMaxWidth().height(6.dp).clip(RoundedCornerShape(2.dp)).clearAndSetSemantics { }, horizontalArrangement = Arrangement.spacedBy(1.dp)) {
        for (moodLevel in dayMoods) Box(Modifier.weight(1f).height(6.dp).background(journalDayMoodColor(moodLevel) ?: none))
    }
}


@Composable
internal fun Heading(title: String) {
    Text(title, style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary, modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 16.dp, bottom = 8.dp))
}

@Composable
internal fun Footnote(text: String) {
    Text(text, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
}

@Composable
internal fun stringOf(resource: Int): String = stringResource(resource)
