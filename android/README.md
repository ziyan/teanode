# TeaNode for Android

The Android app: the same sections, screens and refusals as the iPhone app (`ios/`), drawn with Material 3. The plan
is `docs/planning/android-app-execplan.md`.

## Build and test

You need a JDK 17 or newer, the Android SDK (platform 36) and the Gradle wrapper in this directory. Point
`JAVA_HOME` and `ANDROID_HOME` at them, then, from `android/`:

    ./gradlew :kit:test :app:testDebugUnitTest :app:assembleDebug :app:assembleRelease

- `:kit:test` runs the JUnit 5 tests of the logic (the account, the connection, GraphQL, the sign-in) on the JVM.
- `:app:testDebugUnitTest` runs the app's tests under Robolectric with no emulator or phone. They include the
  screenshot tests, which write their pictures to `app/build/outputs/roborazzi/`: open them and look at them.
- `:app:assembleDebug` builds `app/build/outputs/apk/debug`. `:app:assembleRelease` builds the release APK, signed when
  the release key is there (below), unsigned otherwise.
- `-PteanodeVersionCode=<number>` sets the build's `versionCode` (default 1); `versionName` is in `app/build.gradle.kts`.

## Layout

    android/
      kit/                 Kotlin/JVM, mirrors ios/TeaNodeKit: everything that is not a screen
        src/main/kotlin/com/teanode/kit/            the foundation (ServerAddress, ConnectLink, Account, Connection,
                                                    SignIn, Pkce, GraphQLWebSocketMessage, Subscriptions, AppSections)
        src/main/kotlin/com/teanode/kit/<domain>/   a part's models, documents and calls
      app/                 the Compose app, mirrors ios/TeaNode/Sources
        src/main/java/com/teanode/app/
          MainActivity, TeaNodeApplication, AppRoot
          session/         AppSession (accounts, the connection, what the server offers), encrypted storage
          signin/          the sign-in screen, Custom Tabs, the QR scanner, the confirmation sheet, the server guide
          shell/           AppShell, the drawer, the bottom bar, Routes, AppNavigator, Area, Areas
          ui/components/   TeaNodeScreen, rows, states, rememberLoad, snackbars, avatars, dialogs
          ui/theme/        Material 3 theme in TeaNode green, light and dark
          demo/            the invented server, DemoHost, the answering hook
          <domain>/        one package per part of the app (below)
        src/test/java/com/teanode/app/                 tests and screenshot tests

`kit` has no Android dependency, so its tests run in a second. Its documents are the iPhone app's: the server's tests
check every document in `ios/TeaNodeKit/Sources` against the GraphQL schema, so copy a document's text exactly and it is
checked too. Models are `@Serializable` classes read with `TeaNodeJson` (unknown fields are ignored, as a newer
server may send more).

## Sign-in and the account

The app signs in as the iPhone app does: the server's own page in a Custom Tab (OAuth with PKCE and the built-in client,
the redirect `com.teanode.app:/oauth`), or a connect code scanned with the camera (CameraX and ZXing) or pasted as a
link. The iPhone app registers one link for connect codes, `https://teanode.com/app/connect`, and the Android app
claims the same one (an app link, verified by the site) and also `teanode://app/<route>` for the app's own places.
Tokens are kept in a file sealed with an AES-GCM key that lives in the Android Keystore (`KeystoreSecretStore`), never in
plaintext. `Connection` renews a token a day before it runs out, once however many calls are waiting, and tries a call
again with a fresh token after a 401, as the iPhone app does.

## The contract for a part of the app

Every part of TeaNode (`AppSection`: Chats, Journal, Goals, Ideas, Mail, Calendar, Contacts, Photos, Location, Health,
Passwords, Shopping, Knowledge, Finance, Recordings, Coding, Settings) already exists as a placeholder, in its own
package, plugged into the shell. Building a part means filling its package. Nobody edits the shell, `Areas.kt` or
`AndroidManifest.xml` for it.

Chats is `com.teanode.app.chats` (its `AppSection` is `Agent`); every other part is named after its section. Search,
the last item of the bottom bar, is `com.teanode.app.search` and belongs to the Settings agent.

Each package has three files:

| File | What it is |
| --- | --- |
| `<Domain>Section.kt` | `@Composable fun <Domain>Section()`, the part's first screen. Replace the placeholder. |
| `<Domain>Navigation.kt` | `fun NavGraphBuilder.<domain>Routes(navigator)` registers every screen of the part, and `val <domain>Area` hands the shell the routes, the places Search offers, and the demo answerers. |
| `<Domain>Demo.kt` | `val <domain>DemoAnswerers`: what demo mode answers for the part. |

**Routes.** One route per place, all starting with the part's name. `Routes.home(AppSection.Mail)` is `mail/home`, the
part's first screen; `Routes.child(AppSection.Mail, "thread/{threadId}")` is a place inside it. Register each with
`composable(route, deepLinks = Routes.deepLinks(route))`, so every place opens from `teanode://app/<route>` too, and open
one with `LocalAppNavigator.current.navigate(route)`. Only a part's `home` shows the bottom bar. `Area.places` lists the
places Search offers by name (a mail folder, a Settings page), as the iPhone app's `AppPlaces` does.

**What a screen can ask for.** Composition locals the shell provides: `LocalConnection.current` (the `Connection` of the
account in use: `execute`, `upload`, `get`, `subscribe`), `LocalAppSession.current` (the account, the parts the server
offers, `setBadgeCount(section, count)` for the red count on the bar and the drawer, `noteCodingComputers()` and
`noteRecordingOwner()` for the two parts that appear only once there is something to show), `LocalSnackbar.current`,
`LocalAppNavigator.current`. Put logic in `kit` (`com.teanode.kit.<domain>`, with the iPhone app's tests ported) and keep
screens thin.

**Shared components** (`ui/components`). Do not hand-roll these:

- `TeaNodeScreen(title, onBack, actions, floatingActionButton)`: the top app bar, the way back (or the drawer button on a
  part's first screen) and the snackbars. Every screen is drawn in one.
- `TeaNodeRow`, `TeaNodeNavigationRow`, `TeaNodeSectionHeader`, `TeaNodeDivider`, `CountBadge`: list rows.
- `LoadingState`, `EmptyState`, `ErrorState(message, onRetry)`.
- `rememberLoad { ... }` with `LoadedContent(state, isEmpty, empty) { value -> ... }`: reads when the screen opens, shows
  loading, error with "Try again" and empty, pulls to refresh, and tells the person in a snackbar when a refresh fails.
- `LocalSnackbar.current.show("Done")`, `.showProblem(...)`, `.showError(error)`: successes and errors are snackbars,
  never text stuck in a card.
- `AvatarView(name, avatarUrl, colorIndex)`, `relativeTimeText(epochMS)`, `ConfirmationDialog(...)`, `openWebPage(...)`.

**Demo mode.** A debug build started with `--ez com.teanode.app.extra.DEMO true` signs in to an invented server and
answers from canned JSON, so every screen and state can be reached and photographed without a server. To open a screen
at launch add `--es com.teanode.app.extra.ROUTE mail/home`:

    adb shell am start -n com.teanode.app/.MainActivity --ez com.teanode.app.extra.DEMO true --es com.teanode.app.extra.ROUTE mail/home

Add the part's answers in `<Domain>Demo.kt`:

    val mailDemoAnswerers: List<DemoAnswerer> = listOf(
        graphQLField("ListMailThreads") { variables -> """[{"id":"t1","subject":"Hello"}]""" },
    )

`graphQLField(name, json)` answers any document that asks for the field `name` (matched as a whole word) with
`{"data":{"<name>": <json>}}`. For anything else (a picture, a REST path, a status code) write a `DemoAnswerer` that reads
`DemoRequest.path` or `.document` and returns a `DemoResponse`, or null when the request is not its to answer. The
answerers of all parts are asked in the order `Areas` lists them, then the foundation's, so put a document whose field
name contains another's (`ReadAgentSettings` before `ReadAgent`) first. A document nobody answers fails with
`demo mode has no answer for ...`, so a missing canned answer shows up as an error state, not as an empty screen. The
live feed (`Connection.subscribe`) never connects in demo mode.

**Strings.** Every user-facing string is in English, `zh-rCN` and `ja`, with real translations (the iPhone app's
`Localizable.xcstrings` has most of the words). Put a part's strings in files of its own so parts never conflict:
`res/values/<domain>_strings.xml`, `res/values-zh-rCN/<domain>_strings.xml`, `res/values-ja/<domain>_strings.xml`, every
name prefixed `<domain>_`. `StringsTest` fails when a language lacks a string or its placeholders differ.

**Screenshot tests.** Put a part's in `app/src/test/java/com/teanode/app/<domain>/`: Robolectric with native graphics,
the `DemoHost` that provides the theme, session and connection, and `photograph` from
`com.teanode.app.screenshots`:

    @RunWith(RobolectricTestRunner::class)
    @GraphicsMode(GraphicsMode.Mode.NATIVE)
    @Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
    class MailScreenshotTest {
        @get:Rule val composeRule = createComposeRule()

        @Test fun `inbox dark`() {
            composeRule.setContent { DemoHost(appearance = AppearanceChoice.Dark) { MailSection() } }
            composeRule.photograph("mail-inbox-dark")
        }
    }

Take every screen and state light and dark, and the dense ones at the largest text (`WithFontScale(2f)`), name the
pictures `<domain>-<screen>-<look>`, and open each one: wrong data, clipped text, overlap, no way back, a blank screen.
To photograph the whole app with its bar and drawer use `AppRoot(rememberDemoSession())`.

**A change to something shared** (the shell, the components, the theme, the kit's foundation) is small, additive, and
said in the pull request.

## Release signing and distribution

The release key is the project's own and is never committed (`*.jks` is ignored). `app/build.gradle.kts` reads
`teanode-release.jks` (alias `teanode`) and `store-password.txt` from the directory in `TEANODE_ANDROID_SIGNING_DIRECTORY`
(default `~/.secrets/teanode-android`); without them the release build is unsigned. Push needs a Firebase project: the
google-services plugin is applied only when `app/google-services.json` exists (also ignored), so a clean checkout builds
and runs with push off. Testers get signed builds through Firebase App Distribution; the distribution script is added
with the release workflow (Milestone 0 of the plan).
