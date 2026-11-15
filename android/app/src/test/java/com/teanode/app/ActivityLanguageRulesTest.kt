package com.teanode.app

import java.io.File
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/**
 * Below Android 13 the language chosen in the app reaches only the activities that are an AppCompatActivity, so each
 * activity of the manifest must be one. Checked over the source, as the accessibility rules are.
 */
class ActivityLanguageRulesTest {
    @Test
    fun everyActivityIsAnAppCompatActivity() {
        val manifest = File("src/main/AndroidManifest.xml").readText()
        val names = Regex("<activity\\s+android:name=\"\\.([A-Za-z0-9_.]+)\"").findAll(manifest).map { match -> match.groupValues[1] }.toList()
        assertTrue(names.isNotEmpty(), "no activity found in the manifest")
        val sourceText = File("src/main/java").walkTopDown().filter { file -> file.extension == "kt" }.joinToString("\n") { file -> file.readText() }
        val notAppCompat = ArrayList<String>()
        for (name in names) {
            val declaration = Regex("class ${name.substringAfterLast('.')}\\s*:\\s*([A-Za-z.]+)\\(").find(sourceText)
            if (declaration == null || !declaration.groupValues[1].endsWith("AppCompatActivity")) notAppCompat.add(name)
        }
        assertTrue(notAppCompat.isEmpty(), "activities that do not follow the app language below Android 13: $notAppCompat")
    }
}
