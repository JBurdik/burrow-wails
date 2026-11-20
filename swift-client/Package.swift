// swift-tools-version:5.9
import PackageDescription

// libghostty-vt is the prebuilt static lib `just libghostty` drops into src-wails/third_party
// (same one burrow-daemon's `vt` build uses). ponytail: aarch64-macos only; add an x86_64 slice if ever needed.
let ghostty = Context.packageDirectory + "/../src-wails/third_party/libghostty-vt/aarch64-macos"

let package = Package(
    name: "BurrowSwift",
    platforms: [.macOS(.v14)],
    targets: [
        .target(
            name: "CGhosttyVT",
            cSettings: [.define("GHOSTTY_STATIC"), .unsafeFlags(["-I", ghostty + "/include"])],
            linkerSettings: [.unsafeFlags([ghostty + "/lib/libghostty-vt.a"])]
        ),
        .executableTarget(name: "BurrowSwift", dependencies: ["CGhosttyVT"], resources: [.copy("Resources")]),
    ]
)
