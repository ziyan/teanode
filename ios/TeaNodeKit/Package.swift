// swift-tools-version: 6.0

// TeaNodeKit is everything in the iPhone app that is not a screen: the
// server's address, the API, the websocket. It builds on Linux as well, so
// its tests run without a Mac.

import PackageDescription

let package = Package(
    name: "TeaNodeKit",
    platforms: [.iOS("26.0"), .macOS("26.0")],
    products: [
        .library(name: "TeaNodeKit", targets: ["TeaNodeKit"])
    ],
    dependencies: [
        // CryptoKit is Apple's, so Linux (where these tests run without a Mac)
        // gets the same API from swift-crypto, and _CryptoExtras for PBKDF2.
        .package(url: "https://github.com/apple/swift-crypto.git", from: "3.12.0")
    ],
    targets: [
        .target(
            name: "TeaNodeKit",
            dependencies: [
                .product(name: "Crypto", package: "swift-crypto", condition: .when(platforms: [.linux])),
                .product(name: "_CryptoExtras", package: "swift-crypto", condition: .when(platforms: [.linux])),
            ]),
        .testTarget(name: "TeaNodeKitTests", dependencies: ["TeaNodeKit"]),
    ]
)
