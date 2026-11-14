import Foundation

/// The states of the sheet (design D9). `form` and `sending` show the form; the others show one
/// screen of mockups.md.
enum ShareState: Equatable, Sendable {
  case loading
  case form
  case sending
  /// Screen 5.
  case saved
  /// Screen 12 (RG-12, RG-13).
  case savedPrivate
  /// Screen 6, for a link on its first send.
  case duplicate(withNote: Bool)
  /// Screen 7, with « Réessayer ».
  case unreachable
  /// Screen 8: the image to send weighs `byteCount` bytes.
  case tooLarge(byteCount: Int64)
  /// The text or note to send weighs `byteCount` bytes, over 1 MiB.
  case tooLong(byteCount: Int64)
  /// Screen 10: the problem's `detail` as the server wrote it, if any, and the HTTP status.
  case refused(detail: String?, status: Int)
  /// Screen 11.
  case tokenRefused
  /// Blank text, unreadable image.
  case nothingToSave
  /// Screen 13: the request body could not be written on the phone; nothing is sent.
  case preparationFailed

  /// Only the unreachable screen offers « Réessayer » (RG-06, design D8).
  var offersRetry: Bool { self == .unreachable }

  /// The sheet closes by itself after these screens (design D9).
  var closesBySelf: Bool {
    switch self {
    case .saved, .savedPrivate, .duplicate: true
    default: false
    }
  }

  /// The texts of the screen, nil while the form shows (mockups.md and design, « Decisions to
  /// confirm », validated on 2026-11-13).
  var screen: ScreenText? {
    switch self {
    case .loading, .form, .sending:
      nil
    case .saved:
      ScreenText(title: "Enregistré", message: "Hippo le range dans la nuit.")
    case .savedPrivate:
      ScreenText(
        title: "Enregistré",
        message: "Privé : il attend dans À trier et n'est jamais envoyé à l'IA.")
    case .duplicate(let withNote):
      ScreenText(
        title: "Déjà dans Hippo",
        message: withNote
          ? "Il était déjà dans ta bibliothèque ; ta note l'accompagne."
          : "Il était déjà dans ta bibliothèque.")
    case .unreachable:
      ScreenText(
        title: "Serveur injoignable", message: "Vérifie que Tailscale est actif, puis réessaie.")
    case .tooLarge(let byteCount):
      ScreenText(
        title: "Image trop lourde",
        message: "\(SizeText.megabytes(byteCount)), la limite est de 20 Mo.")
    case .tooLong(let byteCount):
      ScreenText(
        title: "Texte trop long",
        message: "\(SizeText.megabytes(byteCount)), la limite est de 1 Mo.")
    case .refused(let detail?, _):
      ScreenText(
        title: "Hippo a refusé", message: detail, caption: "Message renvoyé par le serveur")
    case .refused(nil, let status):
      ScreenText(
        title: "Hippo a refusé", message: "Le serveur a refusé ce partage (HTTP \(status)).")
    case .tokenRefused:
      ScreenText(
        title: "Accès refusé",
        message: "Le token de l'appli ne correspond plus au serveur. Reconstruis l'appli.")
    case .nothingToSave:
      ScreenText(
        title: "Rien à enregistrer",
        message: "Ce partage ne contient ni lien, ni image, ni texte.")
    case .preparationFailed:
      ScreenText(
        title: "Enregistrement impossible",
        message: "Hippo n'a pas pu préparer l'envoi. Libère un peu d'espace, puis réessaie.")
    }
  }
}

/// The French texts of a screen (ADR-0026).
struct ScreenText: Equatable, Sendable {
  let title: String
  let message: String
  var caption: String? = nil
}

/// A size in megabytes of 1 048 576 bytes, as the server counts, one decimal rounded up, with a
/// French decimal comma: `20,1 Mo` (RG-02, design D5).
enum SizeText {
  static func megabytes(_ byteCount: Int64) -> String {
    let tenths = (max(0, byteCount) * 10 + 1_048_575) / 1_048_576
    return "\(tenths / 10),\(tenths % 10) Mo"
  }
}
