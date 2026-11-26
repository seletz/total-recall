// swift-tools-version: 6.0
// Spike #317: the TUI of total-recall as a native macOS app (SwiftUI).
// The app reads the archive only through the CLI (`total-recall … --json`).
import PackageDescription

let package = Package(
    name: "TotalRecallMac",
    platforms: [.macOS(.v15)],
    products: [
        .executable(name: "TotalRecall", targets: ["TotalRecall"]),
    ],
    targets: [
        // Models, JSON decoding, the CLI client, view models, diff, glob.
        // No SwiftUI, no AppKit: the tests run headless.
        .target(name: "TotalRecallCore"),
        // Windows and views.
        .target(name: "TotalRecallUI", dependencies: ["TotalRecallCore"]),
        .executableTarget(name: "TotalRecall", dependencies: ["TotalRecallCore", "TotalRecallUI"]),
        .testTarget(
            name: "TotalRecallCoreTests",
            dependencies: ["TotalRecallCore"],
            resources: [.copy("Fixtures")]
        ),
    ]
)
