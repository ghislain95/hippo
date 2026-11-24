import CoreGraphics
import Foundation
import Testing
import UniformTypeIdentifiers

@testable import ShareCore

/// The previews of the form: the link card of screen 2 and the image thumbnail of screen 4
/// (design D4, D5).
final class PreviewTests {
  /// A directory of this test only (Swift Testing makes one instance per test), removed after it.
  private let directory: URL

  init() throws {
    directory = try TestFiles.directory()
  }

  deinit {
    try? FileManager.default.removeItem(at: directory)
  }

  @Test("the link card shows the site and the page title")
  func linkWithTitle() {
    let preview = LinkPreview(url: "https://www.example.com/a?x=1", title: "  Un titre\n")

    #expect(preview.site == "www.example.com")
    #expect(preview.headline == "Un titre")
  }

  @Test("the link card shows the URL when there is no title")
  func linkWithoutTitle() {
    let preview = LinkPreview(url: "https://example.com/a", title: " ")

    #expect(preview.site == "example.com")
    #expect(preview.headline == "https://example.com/a")
    #expect(LinkPreview(url: "https://example.com/a", title: nil).headline == preview.headline)
  }

  @Test("the thumbnail is reduced to 600 pixels at most, never upscaled")
  func thumbnail() throws {
    let small = try TestFiles.image(.png, in: directory)
    let image = try #require(Thumbnail.make(from: small))

    #expect(image.width == 64)
    #expect(image.height == 48)
    let reduced = try #require(Thumbnail.make(from: small, maxPixelSize: 32))
    #expect(max(reduced.width, reduced.height) == 32)
  }

  @Test("an unreadable file has no thumbnail")
  func unreadableThumbnail() throws {
    let file = directory.appendingPathComponent("broken.png")
    try Data("not an image".utf8).write(to: file)

    #expect(Thumbnail.make(from: file) == nil)
  }
}
