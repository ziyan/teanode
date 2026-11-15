package com.teanode.app.settings

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.calendar.DateWords
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.agent.AgentCorrection
import com.teanode.kit.agent.AgentLearnedFact
import com.teanode.kit.agent.MemoryCheckAnswer
import com.teanode.kit.agent.MemoryCheckQuestion
import com.teanode.kit.agent.MemoryCheckQuestionChange
import com.teanode.kit.agent.MemoryCheckQuestions
import com.teanode.kit.agent.MemoryCheckRun
import com.teanode.kit.agent.MemoryCheckScore
import com.teanode.kit.agent.agentCorrections
import com.teanode.kit.agent.gradeMemoryNow
import com.teanode.kit.agent.isDropped
import com.teanode.kit.agent.learnedFacts
import com.teanode.kit.agent.memoryCheckAnswers
import com.teanode.kit.agent.memoryCheckQuestions
import com.teanode.kit.agent.memoryCheckRuns
import com.teanode.kit.agent.startMemoryCheck
import com.teanode.kit.agent.updateMemoryCheckQuestion
import com.teanode.kit.knowledge.deleteKnowledgeFact
import com.teanode.kit.photos.ServerTime
import kotlinx.coroutines.delay
import java.time.ZoneId

private class MemoryRead(val facts: List<AgentLearnedFact>, val corrections: List<AgentCorrection>, val questions: List<MemoryCheckQuestion>, val runs: List<MemoryCheckRun>)

@Composable
private fun answerSourceName(source: String): String = stringResource(
    when (source) { "memory" -> R.string.settings_memory_from_memory; "sources" -> R.string.settings_memory_from_sources; else -> R.string.settings_memory_from_both },
)

private fun percentText(scorePercent: Double): String = "${Math.round(scorePercent)}%"

private fun dayText(stamp: String): String =
    ServerTime.instant(stamp)?.let { instant -> DateWords.abbreviatedDate(instant.atZone(ZoneId.systemDefault()).toLocalDate()) }.orEmpty()

@Composable
private fun stateTone(questionState: String): TagTone = when (questionState) {
    "confirmed", "corrected" -> TagTone.Good
    "asked", "unsure" -> TagTone.Warning
    else -> TagTone.Plain
}

/** What the agent remembers: the memory check with its questions and scores, what it learned lately, and what it learned from the person. */
@Composable
fun AgentMemorySettingsScreen(onBack: () -> Unit) {
    val resources = LocalContext.current.resources
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val actions = rememberActionRunner()
    val loaded = rememberLoad {
        MemoryRead(
            facts = runCatching { connection.learnedFacts() }.getOrDefault(emptyList()),
            corrections = runCatching { connection.agentCorrections() }.getOrDefault(emptyList()),
            questions = connection.memoryCheckQuestions(),
            runs = runCatching { connection.memoryCheckRuns() }.getOrDefault(emptyList()),
        )
    }
    var editing by remember { mutableStateOf<MemoryCheckQuestion?>(null) }
    var viewingRun by remember { mutableStateOf<MemoryCheckRun?>(null) }
    var forgetting by remember { mutableStateOf<AgentLearnedFact?>(null) }
    var isShowingDropped by remember { mutableStateOf(false) }
    val checkText = stringResource(R.string.settings_memory_x_check_said)
    val gradingText = stringResource(R.string.settings_memory_x_grading_said)
    val savedText = stringResource(R.string.settings_memory_x_saved)
    val droppedText = stringResource(R.string.settings_memory_x_dropped_said)
    val undoText = stringResource(R.string.settings_memory_x_undo)
    val forgottenText = stringResource(R.string.settings_memory_x_forgotten)
    val hasRunInProgress = loaded.value?.runs?.any { run -> !run.isFinished } == true

    // A grading under way is read again until it has its scores.
    LaunchedEffect(hasRunInProgress) {
        while (hasRunInProgress) {
            delay(10_000)
            loaded.refresh()
        }
    }
    viewingRun?.let { run ->
        MemoryAnswersScreen(run, loaded.value?.questions.orEmpty(), onBack = { viewingRun = null })
        return
    }
    LoadedSettingsPage(stringResource(R.string.settings_memory_title), onBack, loaded) { page ->
        item { TeaNodeSectionHeader(stringResource(R.string.settings_memory_x_header)) }
        item {
            TeaNodeRow(headline = stringResource(R.string.settings_memory_x_check_now), onClick = if (actions.isWorking) null else ({ actions.run(checkText) { connection.startMemoryCheck() } }))
            if (page.questions.isNotEmpty()) {
                TeaNodeRow(
                    headline = stringResource(if (hasRunInProgress) R.string.settings_memory_x_grading else R.string.settings_memory_x_grade_now),
                    onClick = if (actions.isWorking || hasRunInProgress) null else ({ actions.run(gradingText, onSuccess = { loaded.refresh() }) { connection.gradeMemoryNow() } }),
                )
            }
        }
        item { FooterText(stringResource(R.string.settings_memory_x_header_footer)) }
        if (page.questions.isEmpty()) {
            item { TeaNodeRow(headline = stringResource(R.string.settings_memory_x_no_questions), supportingMaxLines = 4) }
        } else {
            val shown = MemoryCheckQuestions.visible(page.questions, isShowingDropped)
            val droppedCount = page.questions.size - MemoryCheckQuestions.visible(page.questions, false).size
            item {
                TeaNodeSectionHeader(stringResource(if (shown.size == 1) R.string.settings_memory_x_questions_one else R.string.settings_memory_x_questions_many, shown.size))
                SwitchRow(stringResource(R.string.settings_memory_x_show_dropped, droppedCount), isShowingDropped, { isOn -> isShowingDropped = isOn })
            }
            if (shown.isEmpty()) item { TeaNodeRow(headline = stringResource(R.string.settings_memory_x_all_dropped)) }
            items(shown.size) { index ->
                val question = shown[index]
                val lines = buildList {
                    add(question.expectedAnswer)
                    if (question.questionKind == "changed" && !question.outdatedAnswer.isNullOrEmpty()) add(stringResource(R.string.settings_memory_x_was, question.outdatedAnswer.orEmpty()))
                    question.answeredAt?.let { stamp -> add(relativeText(stamp)) }
                }
                TeaNodeRow(
                    headline = question.questionText, supporting = lines.joinToString("\n"), supportingMaxLines = 6, onClick = { editing = question },
                    trailing = {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            UpkeepTag(UpkeepWords.questionKind(resources, question.questionKind))
                            UpkeepTag(UpkeepWords.questionState(resources, question.questionState), stateTone(question.questionState))
                            if (question.isAnswerFiledAfter) UpkeepTag(stringResource(R.string.settings_memory_x_filed_after), TagTone.Warning)
                            if (!question.isDropped) {
                                RowMenu(
                                    listOf(
                                        stringResource(R.string.settings_memory_x_drop) to {
                                            val earlierState = question.questionState
                                            actions.run(onSuccess = {
                                                loaded.refresh()
                                                snackbar.show(droppedText, undoText) {
                                                    actions.run(onSuccess = { loaded.refresh() }) { connection.updateMemoryCheckQuestion(question.id, MemoryCheckQuestionChange(questionState = earlierState)) }
                                                }
                                            }) { connection.updateMemoryCheckQuestion(question.id, MemoryCheckQuestionChange(questionState = "dropped")) }
                                        },
                                    ),
                                )
                            }
                        }
                    },
                )
            }
        }
        if (page.questions.isNotEmpty() || page.runs.isNotEmpty()) {
            item { TeaNodeSectionHeader(stringResource(R.string.settings_memory_x_scores_header)) }
            val latest = page.runs.firstOrNull { run -> run.isFinished }
            if (latest != null) {
                MemoryCheckScore.answerSources.forEach { source ->
                    item { ScoreRow(source, latest, MemoryCheckRun.scoreHistory(page.runs, source)) }
                }
                item {
                    Text(
                        stringResource(R.string.settings_memory_x_last_graded, whenText(latest.startedAt), latest.questionCount, moneyText(latest.cost, null)),
                        Modifier.padding(horizontal = 16.dp, vertical = 4.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
            if (page.runs.isEmpty()) item { TeaNodeRow(headline = stringResource(R.string.settings_memory_x_not_graded_yet)) }
            items(page.runs.size) { index ->
                val run = page.runs[index]
                val parts = mutableListOf(stringResource(R.string.settings_memory_x_run_questions, run.questionCount))
                if (run.isFinished) MemoryCheckScore.answerSources.forEach { source -> run.score(source)?.let { score -> parts += "${answerSourceName(source)} ${percentText(score.scorePercent)}" } }
                if (run.cost > 0) parts += moneyText(run.cost, null)
                TeaNodeRow(
                    headline = whenText(run.startedAt), supporting = parts.joinToString(" · "), supportingMaxLines = 4,
                    onClick = if (run.isFinished) ({ viewingRun = run }) else null,
                    trailing = if (!run.isFinished) ({ UpkeepTag(stringResource(R.string.settings_memory_x_grading), TagTone.Good) }) else null,
                )
            }
            item { FooterText(stringResource(R.string.settings_memory_x_scores_footer)) }
        }
        item { TeaNodeSectionHeader(stringResource(R.string.settings_memory_x_learned_header)) }
        if (page.facts.isEmpty()) item { TeaNodeRow(headline = stringResource(R.string.settings_memory_x_learned_none)) }
        items(page.facts.size) { index ->
            val learned = page.facts[index]
            val quote = learned.fact.evidence.firstOrNull()?.quote.orEmpty()
            TeaNodeRow(
                headline = learned.fact.text,
                supporting = listOfNotNull("${learned.path}#${learned.fact.number}", if (quote.isNotEmpty()) "“$quote”" else null).joinToString("\n"), supportingMaxLines = 5,
                trailing = {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        if (learned.fact.inferred) UpkeepTag(stringResource(R.string.settings_memory_x_inferred), TagTone.Warning)
                        TextButton(onClick = { forgetting = learned }) { Text(stringResource(R.string.settings_memory_x_forget)) }
                    }
                },
            )
        }
        item { FooterText(stringResource(R.string.settings_memory_x_learned_footer)) }
        if (page.corrections.isNotEmpty()) {
            item { TeaNodeSectionHeader(stringResource(R.string.settings_memory_x_corrections_header)) }
            items(page.corrections.size) { index ->
                val correction = page.corrections[index]
                TeaNodeRow(headline = correction.said, supporting = whenText(correction.createdAt))
            }
            item { FooterText(stringResource(R.string.settings_memory_x_corrections_footer)) }
        }
    }
    forgetting?.let { learned ->
        ConfirmationDialog(
            title = stringResource(R.string.settings_memory_x_forget), text = stringResource(R.string.settings_memory_x_forget_message, learned.fact.text), confirmLabel = stringResource(R.string.settings_memory_x_forget), isDestructive = true,
            onConfirm = { forgetting = null; actions.run(forgottenText, onSuccess = { loaded.refresh() }) { connection.deleteKnowledgeFact(learned.path, learned.fact.number) } }, onDismiss = { forgetting = null },
        )
    }
    editing?.let { question -> QuestionEditorDialog(question, actions, onDismiss = { editing = null }, onSaved = { editing = null; loaded.refresh() }) }
}

/** One source's latest score, its line over the gradings shown, and how far it moved since the first of them. */
@Composable
private fun ScoreRow(answerFrom: String, latestRun: MemoryCheckRun, history: List<MemoryCheckRun.Point>) {
    val score = latestRun.score(answerFrom)
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(answerSourceName(answerFrom), Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
            if (score != null) Text(percentText(score.scorePercent), style = MaterialTheme.typography.titleMedium)
            else Text(stringResource(R.string.settings_memory_x_not_graded), color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        if (history.size > 1) ScoreChart(history)
        val points = MemoryCheckRun.scoreChangePoints(history)
        if (points != null && history.isNotEmpty()) {
            val day = dayText(history.first().startedAt)
            Text(
                when {
                    points > 0 -> if (points == 1) stringResource(R.string.settings_memory_x_up_one, points, day) else stringResource(R.string.settings_memory_x_up_many, points, day)
                    points < 0 -> if (-points == 1) stringResource(R.string.settings_memory_x_down_one, -points, day) else stringResource(R.string.settings_memory_x_down_many, -points, day)
                    else -> stringResource(R.string.settings_memory_x_unchanged, day)
                },
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        if (score != null && score.filedAfterCount > 0) {
            Text(
                stringResource(R.string.settings_memory_x_without_filed_after, percentText(score.scorePercentWithoutFiledAfter), score.filedAfterCount),
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

/** The score over the gradings shown, on the whole of zero to a hundred, so a line that moved from 70 to 72 looks as flat as it is. */
@Composable
private fun ScoreChart(history: List<MemoryCheckRun.Point>) {
    val lineColor = MaterialTheme.colorScheme.primary
    val gridColor = MaterialTheme.colorScheme.outlineVariant
    val description = history.joinToString(", ") { point -> "${dayText(point.startedAt)} ${percentText(point.scorePercent)}" }
    Canvas(Modifier.fillMaxWidth().height(72.dp).semanticsDescription(description)) {
        val inset = 6.dp.toPx()
        val width = size.width - 2 * inset
        val height = size.height - 2 * inset
        listOf(0.0, 50.0, 100.0).forEach { level ->
            val y = inset + height * (1f - (level / 100.0).toFloat())
            drawLine(gridColor, Offset(inset, y), Offset(size.width - inset, y), strokeWidth = 1.dp.toPx())
        }
        val points = history.mapIndexed { index, point -> Offset(inset + width * index / (history.size - 1), inset + height * (1f - (point.scorePercent / 100.0).toFloat())) }
        val path = Path().apply { points.forEachIndexed { index, point -> if (index == 0) moveTo(point.x, point.y) else lineTo(point.x, point.y) } }
        drawPath(path, lineColor, style = Stroke(width = 2.dp.toPx()))
        points.forEach { point -> drawCircle(lineColor, radius = 3.dp.toPx(), center = point) }
    }
}

private fun Modifier.semanticsDescription(description: String): Modifier = this.then(Modifier.semantics { contentDescription = description })

/** What the agent answered in one grading, to each question from each source, beside the answer on record and what the grader made of it; the missed and wrong ones first by default. */
@Composable
private fun MemoryAnswersScreen(run: MemoryCheckRun, questions: List<MemoryCheckQuestion>, onBack: () -> Unit) {
    val resources = LocalContext.current.resources
    val connection = LocalConnection.current
    val loaded = rememberLoad(run.id) { connection.memoryCheckAnswers(run.id) }
    var isOnlyMissedOrWrong by remember { mutableStateOf(true) }
    LoadedSettingsPage(stringResource(R.string.settings_memory_x_answers_title, whenText(run.startedAt)), onBack, loaded) { answers ->
        val shown = answers.filter { answer -> !isOnlyMissedOrWrong || answer.isMissedOrWrong }
        item { SwitchRow(stringResource(R.string.settings_memory_x_only_missed), isOnlyMissedOrWrong, { isOn -> isOnlyMissedOrWrong = isOn }) }
        item { FooterText(stringResource(R.string.settings_memory_x_answers_footer)) }
        if (shown.isEmpty()) item { TeaNodeRow(headline = stringResource(if (isOnlyMissedOrWrong) R.string.settings_memory_x_nothing_missed else R.string.settings_memory_x_no_answers), supportingMaxLines = 3) }
        items(shown.size) { index ->
            val answer = shown[index]
            val question = questions.firstOrNull { candidate -> candidate.id == answer.questionId }
            Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(question?.questionText.orEmpty(), style = MaterialTheme.typography.bodyLarge)
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    UpkeepTag(UpkeepWords.verdict(resources, answer.answerVerdict), if (answer.isRight) TagTone.Good else if (answer.answerVerdict == "partial") TagTone.Warning else if (answer.isMissedOrWrong) TagTone.Bad else TagTone.Plain)
                    UpkeepTag(answerSourceName(answer.answerFrom))
                }
                LabeledText(stringResource(R.string.settings_memory_x_answer_on_record), question?.expectedAnswer.orEmpty())
                LabeledText(stringResource(R.string.settings_memory_x_agent_answer), answer.answerText)
                LabeledText(stringResource(R.string.settings_memory_x_why), answer.verdictReason)
            }
        }
    }
}

@Composable
private fun LabeledText(title: String, text: String) {
    if (text.isEmpty()) return
    Column {
        Text(title, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(text, style = MaterialTheme.typography.bodySmall)
    }
}

/** Changes a question: its words, the answer on record, its kind, what used to be true for a question about a change, and its state. Only what changed is sent. */
@Composable
private fun QuestionEditorDialog(question: MemoryCheckQuestion, actions: ActionRunner, onDismiss: () -> Unit, onSaved: () -> Unit) {
    val resources = LocalContext.current.resources
    val connection = LocalConnection.current
    val savedText = stringResource(R.string.settings_memory_x_saved)
    var draft by remember(question.id) { mutableStateOf(MemoryCheckQuestionChange(question.questionKind, question.questionText, question.expectedAnswer, question.outdatedAnswer.orEmpty(), question.questionState)) }
    val change = MemoryCheckQuestionChange.between(question, draft)
    FormDialog(
        title = stringResource(R.string.settings_memory_x_edit_title), confirmLabel = stringResource(R.string.settings_save), isConfirmEnabled = draft.questionText.orEmpty().isNotBlank(), isWorking = actions.isWorking,
        onConfirm = { actions.run(savedText, onSuccess = onSaved) { if (!change.isEmpty) connection.updateMemoryCheckQuestion(question.id, change) } },
        onDismiss = onDismiss,
    ) {
        Column(Modifier.verticalScroll(rememberScrollState())) {
            FormField(draft.questionText.orEmpty(), { typed -> draft = draft.copy(questionText = typed) }, stringResource(R.string.settings_memory_x_field_question), isSingleLine = false, minLines = 2)
            FormField(draft.expectedAnswer.orEmpty(), { typed -> draft = draft.copy(expectedAnswer = typed) }, stringResource(R.string.settings_memory_x_answer_on_record), isSingleLine = false, minLines = 2)
            ChoiceRow(stringResource(R.string.settings_memory_x_field_kind), MemoryCheckQuestions.questionKinds, draft.questionKind.orEmpty(), label = { kind -> UpkeepWords.questionKind(resources, kind) }, onSelect = { kind -> draft = draft.copy(questionKind = kind) })
            // What used to be true matters only to a question about a change: an answer that gives it is graded out of date.
            if (draft.questionKind == "changed") FormField(draft.outdatedAnswer.orEmpty(), { typed -> draft = draft.copy(outdatedAnswer = typed) }, stringResource(R.string.settings_memory_x_field_outdated), isSingleLine = false, minLines = 2)
            ChoiceRow(stringResource(R.string.settings_memory_x_field_state), MemoryCheckQuestions.questionStates, draft.questionState.orEmpty(), label = { state -> UpkeepWords.questionState(resources, state) }, onSelect = { state -> draft = draft.copy(questionState = state) })
            FooterText(stringResource(R.string.settings_memory_x_state_footer))
        }
    }
}
