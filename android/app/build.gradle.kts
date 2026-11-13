plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
}

android {
    namespace = "com.teanode.app"
    compileSdk = 36

    defaultConfig {
        applicationId = "com.teanode.app"
        minSdk = 29
        targetSdk = 36
        versionCode = (findProperty("teanodeVersionCode") as String?)?.toInt() ?: 1
        versionName = "0.1.0"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
        androidResources.localeFilters += listOf("en", "ja", "zh-rCN")
    }

    // The release keystore and its password live outside the repository. Without them the release variant stays unsigned.
    val signingDirectory = file(System.getenv("TEANODE_ANDROID_SIGNING_DIRECTORY") ?: "${System.getProperty("user.home")}/.secrets/teanode-android")
    val keystoreFile = signingDirectory.resolve("teanode-release.jks")
    val storePasswordFile = signingDirectory.resolve("store-password.txt")
    val isReleaseSigningAvailable = keystoreFile.isFile && storePasswordFile.isFile

    signingConfigs {
        if (isReleaseSigningAvailable) {
            create("release") {
                val storePasswordText = storePasswordFile.readText().trim()
                storeFile = keystoreFile
                storePassword = storePasswordText
                keyAlias = "teanode"
                keyPassword = storePasswordText
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            if (isReleaseSigningAvailable) signingConfig = signingConfigs.getByName("release")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }

    testOptions {
        unitTests.isReturnDefaultValues = true
        unitTests.isIncludeAndroidResources = true
    }

    lint {
        abortOnError = false
    }
}

// Push needs a Firebase project. Without its config file the app still builds and runs, and push stays off.
if (file("google-services.json").exists()) {
    apply(plugin = libs.plugins.google.services.get().pluginId)
}

kotlin {
    compilerOptions { jvmTarget.set(org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17) }
}

dependencies {
    implementation(project(":kit"))

    implementation(platform(libs.compose.bom))
    implementation(libs.compose.material3)
    implementation(libs.compose.material.icons)
    implementation(libs.compose.ui.tooling.preview)
    debugImplementation(libs.compose.ui.tooling)

    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.navigation.compose)
    implementation(libs.androidx.lifecycle.viewmodel.compose)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.browser)
    implementation(libs.androidx.camera.core)
    implementation(libs.androidx.camera.camera2)
    implementation(libs.androidx.camera.lifecycle)
    implementation(libs.androidx.camera.view)
    implementation(libs.zxing.core)
    implementation(libs.kotlinx.coroutines.android)
    // Location and Health: the fused location provider and geofences, Health Connect, an API-key-free map.
    implementation(libs.play.services.location)
    implementation(libs.health.connect.client)
    implementation(libs.osmdroid.android)

    implementation(libs.androidx.core)

    // Push and calls. Firebase Messaging is used only when the Firebase project's config file is there (see below); without
    // it the library is inert and the code never asks it for anything.
    implementation(platform(libs.firebase.bom))
    implementation(libs.firebase.messaging)
    implementation(libs.stream.webrtc)
    implementation(libs.glance.appwidget)
    implementation(libs.glance.material3)
    implementation(libs.androidx.media3.exoplayer)
    implementation(libs.androidx.media3.ui)
    implementation(libs.androidx.work.runtime)
    implementation(libs.androidx.exifinterface)
    implementation(libs.androidx.credentials)
    // CameraX and WorkManager both hand out ListenableFuture; this keeps the one real class on the classpath.
    implementation(libs.guava)

    debugImplementation(libs.compose.ui.test.manifest)

    testImplementation(libs.junit.jupiter)
    testImplementation(libs.kotlinx.coroutines.test)
    testImplementation(libs.okhttp)
    testRuntimeOnly("org.junit.platform:junit-platform-launcher")
    // Screenshot tests run on the JVM under Robolectric, which speaks JUnit 4; Roborazzi draws the pictures.
    testImplementation(libs.robolectric)
    testImplementation(libs.roborazzi)
    testImplementation(libs.roborazzi.compose)
    testImplementation(platform(libs.compose.bom))
    testImplementation(libs.compose.ui.test.junit4)
    testRuntimeOnly(libs.junit.vintage.engine)
}

tasks.withType<Test> {
    useJUnitPlatform()
    // Without the Roborazzi plugin the pictures are written only when told to be; the screenshot tests name their own files.
    systemProperty("roborazzi.test.record", "true")
    // Robolectric unpacks its native graphics library into the temp directory. Each checkout gets its own, so tests running
    // in two clones at once do not overwrite the library under each other.
    val testTemporaryDirectory = layout.buildDirectory.dir("tmp/test-temporary").get().asFile
    doFirst { testTemporaryDirectory.mkdirs() }
    systemProperty("java.io.tmpdir", testTemporaryDirectory.absolutePath)
}
