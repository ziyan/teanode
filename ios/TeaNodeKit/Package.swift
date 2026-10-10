// swift-tools-version: 6.0

// TeaNodeKit is everything in the iPhone app that is not a screen: the
// server's address, the API, the websocket. It builds on Linux as well, so
// its tests run without a Mac.

import PackageDescription

let package = Package(
    name: "TeaNodeKit",
    platforms: [.iOS(.v18), .macOS(.v15)],
    products: [
        .library(name: "TeaNodeKit", targets: ["TeaNodeKit"])
    ],
    targets: [
        .target(name: "TeaNodeKit"),
        .testTarget(name: "TeaNodeKitTests", dependencies: ["TeaNodeKit"]),
    ]
)
