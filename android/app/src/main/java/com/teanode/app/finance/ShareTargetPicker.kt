package com.teanode.app.finance

import android.content.Context
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import com.teanode.app.R
import com.teanode.kit.finance.ShareCircle
import com.teanode.kit.finance.SharePerson
import com.teanode.kit.finance.ShareTarget

/** Chooses a person or a whole circle to share with. Used by finance and by shopping lists. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ShareTargetPicker(people: List<SharePerson>, circles: List<ShareCircle>, chosen: ShareTarget?, onChoose: (ShareTarget?) -> Unit) {
    val context = LocalContext.current
    var isOpen by remember { mutableStateOf(false) }
    ExposedDropdownMenuBox(isOpen, { isOpen = it }) {
        OutlinedTextField(
            value = if (chosen == null) stringResource(R.string.finance_share_choose) else nameOfLong(chosen, people, circles, context),
            onValueChange = {}, readOnly = true, label = { Text(stringResource(R.string.finance_share_with)) },
            trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(isOpen) }, modifier = Modifier.menuAnchor().fillMaxWidth(),
        )
        ExposedDropdownMenu(isOpen, { isOpen = false }) {
            for (person in people) DropdownMenuItem(text = { Text(person.pickerLabel) }, onClick = { onChoose(ShareTarget.Person(person.userId)); isOpen = false })
            for (circle in circles) {
                // The size too, so two circles with one name can be told apart.
                DropdownMenuItem(
                    text = { Text(context.getString(R.string.finance_share_everyone_in_circle, circle.name, context.resources.getQuantityString(R.plurals.finance_member_count, circle.memberCount, circle.memberCount))) },
                    onClick = { onChoose(ShareTarget.Circle(circle.id)); isOpen = false },
                )
            }
        }
    }
}

private fun nameOfLong(target: ShareTarget, people: List<SharePerson>, circles: List<ShareCircle>, context: Context): String = when (target) {
    is ShareTarget.Person -> people.firstOrNull { person -> person.userId == target.userId }?.pickerLabel ?: ""
    is ShareTarget.Circle -> circles.firstOrNull { circle -> circle.id == target.circleId }?.let { circle ->
        context.getString(R.string.finance_share_everyone_in_circle, circle.name, context.resources.getQuantityString(R.plurals.finance_member_count, circle.memberCount, circle.memberCount))
    } ?: ""
}

/** What the choice is called in a sentence, such as "Shared with Ben". */
object ShareTargetNames {
    fun nameOf(target: ShareTarget?, people: List<SharePerson>, circles: List<ShareCircle>, context: Context): String = when (target) {
    is ShareTarget.Person -> people.firstOrNull { person -> person.userId == target.userId }?.displayName ?: ""
    is ShareTarget.Circle -> context.getString(R.string.finance_share_everyone_in, circles.firstOrNull { circle -> circle.id == target.circleId }?.name ?: "")
    null -> ""
}
}
