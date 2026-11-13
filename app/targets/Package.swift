// swift-tools-version: 6.0
// The pure logic of the share extension (design D3). `share/Core` is compiled twice: into the
// extension by the Xcode target that @bacons/apple-targets generates from `share/`, and here as
// the `ShareCore` library that `swift test` checks. Nothing in this file is part of the target.
import PackageDescription

let package = Package(
  name: "HippoShare",
  platforms: [.iOS(.v18), .macOS(.v14)],
  products: [.library(name: "ShareCore", targets: ["ShareCore"])],
  targets: [
    .target(name: "ShareCore", path: "share/Core"),
    .testTarget(name: "ShareCoreTests", dependencies: ["ShareCore"], path: "ShareCoreTests"),
  ]
)
