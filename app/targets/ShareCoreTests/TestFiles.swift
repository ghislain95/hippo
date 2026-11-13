import CoreGraphics
import Foundation
import ImageIO
import UniformTypeIdentifiers

/// Files made by the tests themselves: no binary fixture in the repository.
enum TestFiles {
  /// A new empty directory; the suite that asks for it removes it after the test (`deinit`).
  static func directory() throws -> URL {
    let url = FileManager.default.temporaryDirectory
      .appendingPathComponent("ShareCoreTests-\(UUID().uuidString)", isDirectory: true)
    try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
    return url
  }

  /// A small image of the given type, drawn by Core Graphics and written by ImageIO.
  static func image(_ type: UTType, in directory: URL, name: String = "source") throws -> URL {
    let width = 64
    let height = 48
    guard
      let context = CGContext(
        data: nil, width: width, height: height, bitsPerComponent: 8, bytesPerRow: 0,
        space: CGColorSpaceCreateDeviceRGB(),
        bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)
    else { throw TestFileError.cannotDraw }
    context.setFillColor(CGColor(red: 0.2, green: 0.5, blue: 0.8, alpha: 1))
    context.fill(CGRect(x: 0, y: 0, width: width, height: height))
    context.setFillColor(CGColor(red: 0.9, green: 0.4, blue: 0.1, alpha: 1))
    context.fill(CGRect(x: 8, y: 8, width: 20, height: 12))
    guard let image = context.makeImage() else { throw TestFileError.cannotDraw }

    let url = directory.appendingPathComponent(name).appendingPathExtension(
      type.preferredFilenameExtension ?? "img")
    guard
      let destination = CGImageDestinationCreateWithURL(
        url as CFURL, type.identifier as CFString, 1, nil)
    else { throw TestFileError.cannotWrite(type.identifier) }
    CGImageDestinationAddImage(destination, image, nil)
    guard CGImageDestinationFinalize(destination) else {
      throw TestFileError.cannotWrite(type.identifier)
    }
    return url
  }

  /// A file of exactly `byteCount` bytes, sparse: fast whatever its size.
  static func file(byteCount: Int64, in directory: URL, name: String = "source.bin") throws -> URL {
    let url = directory.appendingPathComponent(name)
    FileManager.default.createFile(atPath: url.path, contents: nil)
    let handle = try FileHandle(forWritingTo: url)
    try handle.truncate(atOffset: UInt64(byteCount))
    try handle.close()
    return url
  }

  /// A file holding the given bytes.
  static func file(_ data: Data, in directory: URL, name: String) throws -> URL {
    let url = directory.appendingPathComponent(name)
    try data.write(to: url)
    return url
  }

  /// The type ImageIO reads in a file, if any.
  static func imageType(of url: URL) -> String? {
    guard let source = CGImageSourceCreateWithURL(url as CFURL, nil) else { return nil }
    return CGImageSourceGetType(source) as String?
  }
}

enum TestFileError: Error {
  case cannotDraw
  case cannotWrite(String)
}
