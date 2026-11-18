import Foundation
import Testing

@testable import ShareCore

/// Requirement: Each share carries its own capture id, kept by « Réessayer » (design D6).
struct UlidTests {
  @Test("Shape of a capture id")
  func shapeOfACaptureId() throws {
    let now = try Date("2026-11-13T10:00:00.000Z", strategy: .iso8601)

    let id = Ulid.make(now: now).value

    #expect(id.wholeMatch(of: /[0-7][0-9A-HJKMNP-TV-Z]{25}/) != nil)
    #expect(id.prefix(10) == "01M7A5K280")
    #expect(Int64(now.timeIntervalSince1970 * 1000) == 1_794_564_000_000)
  }

  @Test("Two shares, two ids")
  func twoSharesTwoIds() {
    let now = Date()

    let first = Ulid.make(now: now)
    let second = Ulid.make(now: now)

    #expect(first != second)
    #expect(first.value.prefix(10) == second.value.prefix(10))
  }

  @Test("the random part takes 80 bits, the largest values included")
  func randomPartBounds() {
    var zeros = FixedGenerator(value: 0)
    var ones = FixedGenerator(value: .max)
    let epoch = Date(timeIntervalSince1970: 0)

    #expect(Ulid.make(now: epoch, using: &zeros).value == String(repeating: "0", count: 26))
    #expect(
      Ulid.make(now: epoch, using: &ones).value
        == String(repeating: "0", count: 10) + String(repeating: "Z", count: 16))
  }
}

/// A generator that always returns the same value.
struct FixedGenerator: RandomNumberGenerator {
  let value: UInt64

  mutating func next() -> UInt64 { value }
}
