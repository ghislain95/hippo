import Foundation
import ImageIO
import UniformTypeIdentifiers

/// The image formats sent as they are, in the order they are preferred (RG-02, design D5).
enum ImageFormat: CaseIterable, Sendable {
  case heic
  case png
  case webp
  case jpeg

  var type: UTType {
    switch self {
    case .heic: .heic
    case .png: .png
    case .webp: .webP
    case .jpeg: .jpeg
    }
  }

  var mimeType: String {
    switch self {
    case .heic: "image/heic"
    case .png: "image/png"
    case .webp: "image/webp"
    case .jpeg: "image/jpeg"
    }
  }

  var fileExtension: String {
    switch self {
    case .heic: "heic"
    case .png: "png"
    case .webp: "webp"
    case .jpeg: "jpg"
    }
  }

  /// The name shown in the form's caption (screen 4).
  var label: String {
    switch self {
    case .heic: "HEIC"
    case .png: "PNG"
    case .webp: "WebP"
    case .jpeg: "JPEG"
    }
  }
}

/// How to load a shared image: one of its types kept as it is, or one converted to JPEG.
enum ImageLoad: Equatable, Sendable {
  case keep(ImageFormat, typeIdentifier: String)
  case convert(typeIdentifier: String)

  /// The type to ask the provider for.
  var typeIdentifier: String {
    switch self {
    case .keep(_, let typeIdentifier), .convert(let typeIdentifier): typeIdentifier
    }
  }
}

extension SharedImage {
  /// The first of HEIC, PNG, WebP, JPEG the image is offered in, by conformance; else its first
  /// image type, to convert; nil when it has no image type (design D5).
  var load: ImageLoad? {
    let types = typeIdentifiers.compactMap { identifier in
      UTType(identifier).map { (identifier, $0) }
    }
    for format in ImageFormat.allCases {
      if let (identifier, _) = types.first(where: { $0.1.conforms(to: format.type) }) {
        return .keep(format, typeIdentifier: identifier)
      }
    }
    if let (identifier, _) = types.first(where: { $0.1.conforms(to: .image) }) {
      return .convert(typeIdentifier: identifier)
    }
    return nil
  }
}

/// An image file ready to send, in the extension's work directory.
struct PreparedImage: Equatable, Sendable {
  let file: URL
  let format: ImageFormat
  let byteCount: Int64

  /// File name of the multipart part (the server ignores it, RG-02).
  var fileName: String { "capture.\(format.fileExtension)" }

  /// Caption of the form: `Image · HEIC · 3,1 Mo` (screen 4).
  var caption: String { "Image · \(format.label) · \(SizeText.megabytes(byteCount))" }
}

/// Turns the file a provider gave into the file to send: copied as it is, or converted to JPEG,
/// then checked against 20 MB (RG-02, design D5). Runs inside the provider's completion handler,
/// where the given file is valid; the bytes are never held in memory.
enum ImagePreparation {
  /// 20 × 1 048 576 bytes, as the server (`image-too-large`).
  static let maximumByteCount: Int64 = 20 * 1_048_576

  enum Result: Equatable, Sendable {
    case ready(PreparedImage)
    case tooLarge(byteCount: Int64)
    case unreadable

    /// The state shown instead of the form, if any.
    var blockingState: ShareState? {
      switch self {
      case .ready: nil
      case .tooLarge(let byteCount): .tooLarge(byteCount: byteCount)
      case .unreadable: .nothingToSave
      }
    }
  }

  /// - Parameter convert: writes a JPEG of its first file into its second; ImageIO by default.
  static func prepare(
    loadedFile: URL, as load: ImageLoad, clientId: Ulid, in directory: URL,
    convert: (URL, URL) throws -> Void = JPEGConversion.convert
  ) -> Result {
    let format: ImageFormat
    if case .keep(let kept, _) = load { format = kept } else { format = .jpeg }
    let file = directory.appendingPathComponent(clientId.value)
      .appendingPathExtension(format.fileExtension)
    let fileManager = FileManager.default
    do {
      try? fileManager.removeItem(at: file)
      if case .keep = load {
        try fileManager.copyItem(at: loadedFile, to: file)
      } else {
        try convert(loadedFile, file)
      }
      let attributes = try fileManager.attributesOfItem(atPath: file.path)
      guard let byteCount = (attributes[.size] as? NSNumber)?.int64Value else {
        try? fileManager.removeItem(at: file)
        return .unreadable
      }
      guard byteCount <= maximumByteCount else {
        try? fileManager.removeItem(at: file)
        return .tooLarge(byteCount: byteCount)
      }
      return .ready(PreparedImage(file: file, format: format, byteCount: byteCount))
    } catch {
      try? fileManager.removeItem(at: file)
      return .unreadable
    }
  }
}

/// JPEG conversion by ImageIO, quality 0.9, the source's properties (orientation) kept.
enum JPEGConversion {
  enum Failure: Error {
    case unreadable
    case unwritable
  }

  static func convert(_ source: URL, to destination: URL) throws {
    guard let imageSource = CGImageSourceCreateWithURL(source as CFURL, nil),
      CGImageSourceGetCount(imageSource) > 0
    else { throw Failure.unreadable }
    guard
      let imageDestination = CGImageDestinationCreateWithURL(
        destination as CFURL, UTType.jpeg.identifier as CFString, 1, nil)
    else { throw Failure.unwritable }
    let options = [kCGImageDestinationLossyCompressionQuality: 0.9] as CFDictionary
    CGImageDestinationAddImageFromSource(imageDestination, imageSource, 0, options)
    guard CGImageDestinationFinalize(imageDestination) else { throw Failure.unreadable }
  }
}
