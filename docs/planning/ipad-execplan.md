# The iPhone app on the iPad

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds. It follows `~/.claude/PLAN.md`.

## Purpose / Big Picture

TeaNode's app runs only on the iPhone today: the project builds it for the iPhone alone (`TARGETED_DEVICE_FAMILY: "1"` in `ios/project.yml`) and only upright. After this change the same app installs on an iPad and uses its screen as iPad apps do. On an iPad the side menu (the list of parts of TeaNode, and under it the chats, mail folders, photo collections or Location's lists of the part on screen) stays open as a column on the left, and the screen beside it takes the rest of the width. Turning the iPad between landscape and portrait keeps the menu open and the screen beside it narrows or widens. When the app is given a narrow window (Slide Over, or a third of the screen in Split View), it behaves exactly as on the iPhone: the menu slides out from the left with the menu button or a swipe.

To see it working: install a debug build on an iPad simulator, open the demo (`-TeaNodeDemo`), turn the simulator (Device > Rotate Left), and choose parts and chats from the menu; the menu never closes and the screen beside it changes. CI takes iPad screenshots in both orientations.

## Progress

- [x] Milestone 1: the app builds for the iPad and runs in every orientation there. (2026-10-18; whether `INFOPLIST_KEY_UISupportedInterfaceOrientations_iPad` is accepted is known only from CI's build.)
- [x] Milestone 2: the side menu is a column that stays open at regular width. (2026-10-18; compiles only in CI.)
- [ ] Milestone 3: screens that read badly when wide are capped, and presenters work on the iPad.
- [ ] Milestone 4: CI photographs the iPad in landscape and portrait, and the App Store's iPad set.
- [ ] Review until a round finds nothing; DESIGN.md says what the iPad does.

## Surprises & Discoveries

- Nothing in the sources assumes a phone's screen: there is no `UIScreen.main`, no `userInterfaceIdiom` and no fixed width tied to the iPhone. `UIDevice.current.name` is used only to name the phone to the server, and `Device.modelIdentifier` is sent as the phone's model; the server's table of model names (`internal/models/phone_model_name.go`) already lists the iPads. There is no `TabView` in the app, so the tab bar that iPadOS 26 moves to the top never appears.

## Decision Log

- Decision: the column appears whenever the window's horizontal size class is regular, in both orientations; at compact width the iPhone's sliding menu stays. Rationale: the person asked for the menu to stay open with the screen adjusting to landscape or portrait; the size class is what iPadOS gives an app for "is there room", and it already turns compact in Slide Over and narrow Split View, where a column would leave no room for the screen. Date: 2026-10-18.
- Decision: the menu cannot be hidden on the iPad in this change. Rationale: the person asked for it to stay open; a hide button is a later addition if wanted.
- Decision: the iPhone stays upright only; the iPad allows all four orientations. Rationale: iPadOS requires all four for an app to take part in Split View and Slide Over, and nothing on the iPhone changes.
- Decision: the share extension, the notification extension and the widgets are built for the iPad too. Rationale: an extension must support every device its app does, or App Store Connect refuses the upload.

- Decision: the `onClose` given to `SideMenu` still sets `isMenuOpen` to false when the menu is a column, instead of doing nothing. Rationale: the column ignores `isMenuOpen`, so the observable result is the one the plan asks for, and a window that later turns compact (Slide Over) then finds the sliding menu closed instead of open. Date: 2026-10-18.
- Decision: the chats in the column are read again from the loop that reports presence (`refreshPresence` in `MainView`), which already restarts when the scene phase, the part or the chosen chat changes and repeats every half minute, rather than from separate `onChange` handlers for the scene phase and the chosen chat. Rationale: one place covers the plan's two triggers (the app coming to the front, a new chat chosen) and also keeps the always-open list fresh while nothing is chosen, which the plan's triggers alone would leave stale. Date: 2026-10-18.
- Decision: the column's width is `SidePanel.columnWidth` (320 points) and its rule, the horizontal size class being regular, is `Optional<UserInterfaceSizeClass>.isMenuColumn`, shared by `SidePanel` and `MainView`. Rationale: both must agree on when the menu is a column, and `SidePanel` is generic, so a static on it cannot be called from outside. Date: 2026-10-18.

## Outcomes & Retrospective

To be written when the work is done.

## Context and Orientation

The app's code is in `ios/TeaNode/Sources/`, its shared library (no UI) in `ios/TeaNodeKit/`, and the Xcode project is generated from `ios/project.yml` by XcodeGen in CI. There is no Swift toolchain on the machine this plan is written on: TeaNodeKit can be built and tested in Docker (`swift:6.1`), but the app itself (SwiftUI and UIKit) only compiles in CI, in `.github/workflows/ios.yml`, so write Swift carefully: no ternary mixing `nil` with a method reference or closure inside a view body (move it to a typed computed property), no `==` on SwiftUI types that are not Equatable, and explicit types where an expression grows long.

The "side menu" is `SideMenu` in `ios/TeaNode/Sources/AppShell.swift`. It is drawn by `SidePanel` in `ios/TeaNode/Sources/CommonViews.swift`: a `GeometryReader` with a `ZStack` holding the panel (width `min(width * 0.84, 380)`) and the screen, which slides right by the panel's width when `isOpen` is true, with a UIKit pan gesture (`SidePanelPan`) to drag it. `AppShell` (the view that holds the menu and the screen, also in `AppShell.swift`) owns `isMenuOpen`, passes `onClose` to `SideMenu` (every choice in the menu calls it), counts openings in `menuOpeningCount` (the chats are read again on each opening, `ConversationsSheet(openingCount:)`), and puts an `OpenMenu` action in the environment that `MenuButton` (a `ToolbarContent` at the top left of every screen, used in about eleven files) calls. The demo server (`ios/TeaNode/Sources/DemoServer.swift`, debug builds only) reads launch arguments such as `-TeaNodeDemo`, `-TeaNodeTab <part>`, `-TeaNodeMenu` and `-TeaNodeScreen <name>`; CI's `shoot` function in `ios.yml` launches the app with them on a simulator and takes a picture.

"Size class" is SwiftUI's `@Environment(\.horizontalSizeClass)`: `.regular` on an iPad's full screen and its wider Split View sizes, `.compact` on the iPhone and in Slide Over.

Rules for every screen are in `ios/DESIGN.md` (system components first, semantic colors, strings in the String Catalog `ios/TeaNode/Resources/Localizable.xcstrings` in English, Simplified Chinese and Japanese). Read it before changing a screen.

## Plan of Work

Milestone 1, the project. In `ios/project.yml` set `TARGETED_DEVICE_FAMILY: "1,2"` for the app and for each extension target that has `"1"` today (four places). Keep `INFOPLIST_KEY_UISupportedInterfaceOrientations: UIInterfaceOrientationPortrait` for the iPhone, and add `INFOPLIST_KEY_UISupportedInterfaceOrientations_iPad: "UIInterfaceOrientationPortrait UIInterfaceOrientationPortraitUpsideDown UIInterfaceOrientationLandscapeLeft UIInterfaceOrientationLandscapeRight"` to the app target. If the generated Info.plist needs the `~ipad` key instead, the build tells you (`UISupportedInterfaceOrientations~ipad`); use what the build accepts. Search the sources for anything that assumes a phone (`UIScreen.main.bounds`, fixed widths, `UIDevice.current`) and note each in `Surprises & Discoveries`.

Milestone 2, the column. In `SidePanel` read `@Environment(\.horizontalSizeClass)`. When it is `.regular`, lay out an `HStack(spacing: 0)` of the panel at a fixed width of 320 points, a hairline `Divider()`, and the screen filling the rest; no offset, no shadow, no rounded clip, no pan gesture, no tap-to-close overlay. When it is `.compact`, keep today's code unchanged. Put a new environment value `isMenuPinned` (a `Bool`, `@Entry` in `extension EnvironmentValues`, beside `openMenu` in `AppShell.swift`) to `true` for the screen and the panel at regular width. `MenuButton` renders nothing when `isMenuPinned` is true (an empty `ToolbarItem` is not allowed, so return an `EmptyView` toolbar content, for example by making the item conditional with `if !isMenuPinned`). In `AppShell`, the `onClose` given to `SideMenu` does nothing when pinned (the choice still takes effect, the menu stays), and `menuOpeningCount` is bumped when the scene becomes active and when the chosen conversation changes while pinned, so the chats in the column stay fresh (today they are read again on each opening, which never happens while pinned). The demo's `-TeaNodeMenu` and `conversations-restored` paths keep working on the iPhone and do nothing harmful on the iPad. The side menu's own scroll to the items of the part on screen (`showPartItems`) is unchanged.

Milestone 3, the screens beside it. At regular width the screen beside the column is 700 to 1000 points wide. Lists and forms adapt by themselves. Two kinds of screen need a cap so lines stay readable: the conversation transcript (`ConversationView.swift`) and reading a mail (`MailThreadView.swift`); give their content a maximum width of about 720 points, centered, using `.frame(maxWidth: 720)` on the scroll content (not on the scroll view, so it still scrolls from the edges), only at regular width. Check the photo grid (`PhotosView.swift`, an adaptive grid that already grows), the map screens (`LocationViews.swift`) and the compose screen still fill sensibly. On the iPad, UIKit presents a share sheet as a popover, which needs an anchor: in `ShareSheetPresenter` (`MediaViews.swift`) set `popoverPresentationController?.sourceRect` to the center of `top.view.bounds` and `permittedArrowDirections` to `[]` as well as the existing `sourceView`, so it appears in the middle instead of pointing at the corner. Look for other `UIActivityViewController` or `UIAlertController(... .actionSheet)` uses and anchor them the same way.

Milestone 4, the pictures. Add a launch argument `-TeaNodeOrientation landscape|portrait` to `DemoServer` (debug only) that, once the window scene exists, calls `windowScene.requestGeometryUpdate(.iOS(interfaceOrientations: .landscapeRight))` (or `.portrait`). In `ios.yml`, in the "Pick the simulators" step, also pick the newest available iPad simulator whose name starts with `iPad Pro 13` (fall back to any `iPad Pro`), export it as `IPAD`. In the fourth screenshot machine (shard 3, the one that already takes the App Store's iPhone set at the end), after the iPhone store set, boot the iPad, install the app, and take with the same `shoot` function: `ipad-conversation-landscape` (light), `ipad-conversation-portrait` (light), `ipad-mail-landscape` (dark), `ipad-photos-landscape` (light), `ipad-location-portrait` (light), and an App Store iPad set `store-ipad/1-conversation` to `store-ipad/4-goals` in landscape with the same status bar override. `shoot` uses `$device`; set `device=$IPAD` before these lines. Keep the iPhone shots exactly as they are.

## Concrete Steps

Work in a clone, never in `~/projects/ziyan/teanode`. From the clone:

    git fetch origin && git checkout feat/ipad
    # after editing Swift: there is no compiler here; read the diff twice for the hazards above
    node ios/scripts/check-button-colors.mjs
    node ios/scripts/check-catalogs.mjs   # if present; the repo's catalog check otherwise
    make lint-ci

TeaNodeKit, if touched (it should not need to be):

    git archive HEAD ios/TeaNodeKit | tar -x -C <dir>
    cd <dir>/ios/TeaNodeKit && docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD":/kit -w /kit swift:6.1 bash -c 'swift build --build-tests -q; swift test --skip-build'

## Validation and Acceptance

CI's iOS workflow compiles the app (App on macOS) and takes the screenshots. Acceptance: in `ipad-conversation-landscape` and `ipad-conversation-portrait` the menu is a column on the left with the chats, the conversation beside it, no menu button at the top left, and the messages no wider than about 720 points; in `ipad-photos-landscape` the grid fills the width beside the column; every iPhone screenshot is unchanged from main's. On a real iPad or the simulator: rotating keeps the menu open; choosing a chat or a part changes the screen and leaves the menu; Slide Over shows the iPhone layout with the menu button.

## Idempotence and Recovery

Every change is in source and CI; nothing touches the server or the database. Reverting the commit returns the app to iPhone only.

## Interfaces and Dependencies

No new libraries. New: `EnvironmentValues.isMenuPinned: Bool` (default `false`), and the demo's `-TeaNodeOrientation` argument. `SidePanel`'s public shape (`isOpen`, `isOpenedFromAnywhere`, `panel`, `content`) is unchanged.
