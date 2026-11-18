import Foundation
import Testing
import UniformTypeIdentifiers

@testable import ShareCore

/// Requirements: an image is kept in its original format from a closed list, else converted to
/// JPEG; an image over 20 MB is refused before any send; a share with nothing to capture is
/// told as such (design D5).
final class ImagePreparationTests {
  /// A directory of this test only (Swift Testing makes one instance per test), removed after it.
  private let directory: URL

  init() throws {
    directory = try TestFiles.directory()
  }

  deinit {
    try? FileManager.default.removeItem(at: directory)
  }

  private let clientId = Ulid(value: "01M7A5K2800000000000000000")

  private func prepare(
    _ file: URL, offeredAs types: [String], in directory: URL
  ) throws -> ImagePreparation.Result {
    let load = try #require(SharedImage(typeIdentifiers: types).load)
    return ImagePreparation.prepare(loadedFile: file, as: load, clientId: clientId, in: directory)
  }

  @Test("HEIC preferred over JPEG")
  func heicPreferredOverJPEG() throws {
    // Any bytes: a kept file is copied, never decoded (and a CI runner may lack an HEVC encoder).
    let source = try TestFiles.file(
      Data("any bytes of an HEIC file".utf8), in: directory, name: "source.heic")
    let image = SharedImage(typeIdentifiers: ["public.jpeg", "public.heic"])

    #expect(image.load == .keep(.heic, typeIdentifier: "public.heic"))
    let result = try prepare(source, offeredAs: image.typeIdentifiers, in: directory)

    guard case .ready(let prepared) = result else {
      Issue.record("expected an image ready to send, got \(result)")
      return
    }
    #expect(prepared.format.mimeType == "image/heic")
    #expect(prepared.fileName == "capture.heic")
    #expect(try Data(contentsOf: prepared.file) == Data(contentsOf: source))
  }

  @Test("PNG screenshot")
  func pngScreenshot() throws {
    let source = try TestFiles.image(.png, in: directory)

    let result = try prepare(source, offeredAs: ["public.png"], in: directory)

    guard case .ready(let prepared) = result else {
      Issue.record("expected an image ready to send, got \(result)")
      return
    }
    #expect(try Data(contentsOf: prepared.file) == Data(contentsOf: source))
    #expect(prepared.caption.hasPrefix("Image · PNG"))
  }

  @Test("TIFF converted to JPEG")
  func tiffConvertedToJPEG() throws {
    let source = try TestFiles.image(.tiff, in: directory)

    #expect(
      SharedImage(typeIdentifiers: ["public.tiff"]).load == .convert(typeIdentifier: "public.tiff"))
    let result = try prepare(source, offeredAs: ["public.tiff"], in: directory)

    guard case .ready(let prepared) = result else {
      Issue.record("expected an image ready to send, got \(result)")
      return
    }
    #expect(TestFiles.imageType(of: prepared.file) == UTType.jpeg.identifier)
    #expect(prepared.format.mimeType == "image/jpeg")
    #expect(prepared.fileName == "capture.jpg")
    #expect(prepared.caption.hasPrefix("Image · JPEG"))
  }

  @Test("Other image types are never sent as they are")
  func otherImageTypesAreNeverSentAsTheyAre() {
    let image = SharedImage(typeIdentifiers: ["com.adobe.raw-image", "public.jpeg"])

    #expect(image.load == .keep(.jpeg, typeIdentifier: "public.jpeg"))
  }

  @Test("Unreadable image")
  func unreadableImage() throws {
    let source = try TestFiles.file(Data("not an image".utf8), in: directory, name: "x.tiff")

    let result = try prepare(source, offeredAs: ["public.tiff"], in: directory)

    #expect(result == .unreadable)
    #expect(result.blockingState == .nothingToSave)
  }

  @Test("Image of exactly 20 MB")
  func imageOfExactly20MB() throws {
    let source = try TestFiles.file(byteCount: 20_971_520, in: directory)

    let result = try prepare(source, offeredAs: ["public.png"], in: directory)

    guard case .ready(let prepared) = result else {
      Issue.record("expected the form, got \(result)")
      return
    }
    #expect(result.blockingState == nil)
    #expect(prepared.caption.hasSuffix("20,0 Mo"))
  }

  @Test("Image one byte over 20 MB")
  func imageOneByteOver20MB() throws {
    let source = try TestFiles.file(byteCount: 20_971_521, in: directory)

    let result = try prepare(source, offeredAs: ["public.png"], in: directory)

    #expect(result == .tooLarge(byteCount: 20_971_521))
    #expect(
      result.blockingState?.screen
        == ScreenText(title: "Image trop lourde", message: "20,1 Mo, la limite est de 20 Mo."))
  }

  @Test("Converted image over the limit")
  func convertedImageOverTheLimit() throws {
    let source = try TestFiles.image(.tiff, in: directory)

    let result = ImagePreparation.prepare(
      loadedFile: source, as: .convert(typeIdentifier: "public.tiff"), clientId: clientId,
      in: directory,
      convert: { _, destination in
        _ = try TestFiles.file(
          byteCount: 22_000_000, in: destination.deletingLastPathComponent(),
          name: destination.lastPathComponent)
      })

    #expect(result == .tooLarge(byteCount: 22_000_000))
    #expect(result.blockingState == .tooLarge(byteCount: 22_000_000))
  }

  @Test("Size of a small image")
  func sizeOfASmallImage() throws {
    let source = try TestFiles.file(byteCount: 3_250_000, in: directory)

    let result = try prepare(source, offeredAs: ["public.jpeg"], in: directory)

    #expect(SizeText.megabytes(3_250_000) == "3,1 Mo")
    guard case .ready(let prepared) = result else {
      Issue.record("expected the form, got \(result)")
      return
    }
    #expect(prepared.caption == "Image · JPEG · 3,1 Mo")
  }

  @Test("sizes round up to one decimal, with a French comma")
  func sizeText() {
    #expect(SizeText.megabytes(0) == "0,0 Mo")
    #expect(SizeText.megabytes(1) == "0,1 Mo")
    #expect(SizeText.megabytes(1_048_576) == "1,0 Mo")
    #expect(SizeText.megabytes(22_000_000) == "21,0 Mo")
  }

  @Test("the kept file is named after the capture id, in the work directory")
  func keptFileName() throws {
    let source = try TestFiles.image(.png, in: directory)

    let result = try prepare(source, offeredAs: ["public.png"], in: directory)

    guard case .ready(let prepared) = result else {
      Issue.record("expected an image ready to send, got \(result)")
      return
    }
    #expect(prepared.file == directory.appendingPathComponent("\(clientId.value).png"))
  }

  @Test("WebP is kept; a type that is no image is never loaded")
  func webpAndNonImage() {
    #expect(
      SharedImage(typeIdentifiers: ["public.jpeg", "org.webmproject.webp"]).load
        == .keep(.webp, typeIdentifier: "org.webmproject.webp"))
    #expect(SharedImage(typeIdentifiers: ["public.plain-text"]).load == nil)
  }
}
