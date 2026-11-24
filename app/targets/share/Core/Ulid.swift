import Foundation

/// The capture id of a share (`client_id`, RG-08, ADR-0035): a ULID, 48 bits of Unix time in
/// milliseconds then 80 random bits, in Crockford base 32 (26 characters). Drawn once when the
/// sheet opens, kept by « Réessayer » (design D6). No library.
struct Ulid: Hashable, Sendable, CustomStringConvertible {
  let value: String

  var description: String { value }

  private static let alphabet = Array("0123456789ABCDEFGHJKMNPQRSTVWXYZ")

  /// A new id, its randomness from the system's secure generator.
  static func make(now: Date = Date()) -> Ulid {
    var generator = SystemRandomNumberGenerator()
    return make(now: now, using: &generator)
  }

  static func make(now: Date, using generator: inout some RandomNumberGenerator) -> Ulid {
    let milliseconds = UInt64(max(0, (now.timeIntervalSince1970 * 1000).rounded(.down)))
    let fortyBits: UInt64 = (1 << 40) - 1
    let high = generator.next() & fortyBits
    let low = generator.next() & fortyBits
    let text =
      encode(milliseconds & ((1 << 48) - 1), characters: 10)
      + encode(high, characters: 8) + encode(low, characters: 8)
    return Ulid(value: text)
  }

  /// The last `characters` × 5 bits of `number`, most significant first.
  private static func encode(_ number: UInt64, characters: Int) -> String {
    String(
      (0..<characters).reversed().map { index in
        alphabet[Int((number >> UInt64(index * 5)) & 31)]
      })
  }
}
