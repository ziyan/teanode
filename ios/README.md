# The TeaNode iPhone app

An app for talking to your TeaNode agent from your iPhone: chat, notifications you can answer from, the share sheet, calls, Siri and widgets. One app for every TeaNode server: you connect it to your own. The plan, and why it is built the way it is, is `docs/planning/ios-app-execplan.md`.

It is at the start: the build pipeline and the first screen.

## What is here

- `TeaNodeKit/`: a Swift package with everything that is not a screen: the server's address, the API's messages. It builds on Linux too.
- `TeaNode/`: the app's screens, in SwiftUI.
- `project.yml`: the Xcode project, described as text for [XcodeGen](https://github.com/yonaskolb/XcodeGen). The `.xcodeproj` is generated from it and not committed.
- `Configuration/`: build settings. `Identity.xcconfig`, which you write and never commit, says whose developer account signs your builds.

## Building

Without a Mac, test the package in Swift's own container:

    cd ios/TeaNodeKit
    docker run --rm -v "$PWD":/package -w /package swift:6.1 swift test --scratch-path /tmp/build

Every pull request that touches `ios/` is also built on GitHub's macOS runners (`.github/workflows/ios.yml`).

On a Mac with Xcode 26 and XcodeGen (`brew install xcodegen`):

    cd ios
    xcodegen generate
    open TeaNode.xcodeproj

A simulator build needs no developer account. To run it on your own iPhone, copy `Configuration/Identity.example.xcconfig` to `Configuration/Identity.xcconfig` and set your team.

## The developer account that publishes the app

These are done once, by hand, at developer.apple.com and appstoreconnect.apple.com, by whoever holds the account:

- [ ] Under Certificates, Identifiers & Profiles, Identifiers, register the App IDs (Explicit):
  - `com.teanode.app` with Push Notifications, App Groups, Associated Domains and Siri;
  - `com.teanode.app.share` with App Groups;
  - `com.teanode.app.notification` with App Groups;
  - `com.teanode.app.widgets` with App Groups and Push Notifications.
- [ ] Register the App Group `group.com.teanode.app` and attach it to all four.
- [ ] Under Keys, create an Apple Push Notifications service (APNs) key. Its `.p8` file can be downloaded once: keep it with the account's other secrets, never in a conversation or this repository. It belongs to the push relay, not to any TeaNode server.
- [ ] In App Store Connect, under Users and Access, Integrations, create an App Store Connect API key with the App Manager role, for uploads from CI. Same care with its `.p8`.
- [ ] In App Store Connect, create the app: name TeaNode, bundle ID `com.teanode.app`.
- [ ] Keep the membership renewing: if it lapses, the app leaves the App Store and its notifications stop.
