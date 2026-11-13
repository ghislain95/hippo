import SwiftUI

/// The sheet: the form of screens 2, 3, 4 and 9, then one screen per state (screens 5 to 8, 10
/// to 13, and the nothing-to-save and too-long states), from mockups.md. Colours come from the
/// colour sets generated from the app's tokens (design D10); texts use SwiftUI text styles, which
/// follow Dynamic Type. The screen texts come from `Core` (`ShareState.screen`).
struct ShareView: View {
  @Bindable var model: ShareModel

  var body: some View {
    NavigationStack {
      content
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color("background"))
        .navigationTitle("Hippo")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar { toolbar }
    }
    .tint(Color("accent"))
  }

  @ViewBuilder
  private var content: some View {
    switch model.state {
    case .loading:
      ProgressView()
    case .form, .sending:
      if let form = model.form {
        ShareFormView(model: model, form: form)
      }
    default:
      if let text = model.state.screen {
        StatusView(model: model, text: text)
      }
    }
  }

  @ToolbarContentBuilder
  private var toolbar: some ToolbarContent {
    if model.state.showsCancel {
      ToolbarItem(placement: .cancellationAction) {
        Button("Annuler", action: model.cancel)
      }
    }
    if model.state == .form || model.state == .sending {
      ToolbarItem(placement: .confirmationAction) {
        if model.state == .sending {
          // « Enregistrer » shows a progress indicator while sending (mockups.md).
          ProgressView()
            .accessibilityLabel("Enregistrer")
        } else {
          Button("Enregistrer", action: model.save)
            .fontWeight(.semibold)
            .disabled(!model.canSave)
        }
      }
    }
  }
}

/// The form of a link, a text or an image, frozen from the first send.
private struct ShareFormView: View {
  @Bindable var model: ShareModel
  let form: CaptureForm

  var body: some View {
    Form {
      switch form {
      case .link:
        if let preview = model.linkPreview {
          Section {
            LinkCard(preview: preview)
          }
          .listRowBackground(Color("surface"))
        }
      case .image:
        Section {
          ImagePreview(thumbnail: model.thumbnail, caption: model.imageCaption)
        }
        .listRowBackground(Color("surface"))
      case .note:
        Section {
          TextField("", text: $model.noteText, axis: .vertical)
            .lineLimit(3...12)
            .foregroundStyle(Color("text"))
            .accessibilityLabel("Note")
        } header: {
          Text("Note")
            .foregroundStyle(Color("textMuted"))
        }
        .listRowBackground(Color("surface"))
      }
      if form.hasCaptureNote {
        Section {
          TextField("Note (facultative)", text: $model.userNote, axis: .vertical)
            .lineLimit(1...8)
            .foregroundStyle(Color("text"))
            // The placeholder names the field only while it is empty; a prefilled note (screen
            // 9) keeps its name for VoiceOver.
            .accessibilityLabel("Note (facultative)")
        }
        .listRowBackground(Color("surface"))
      }
      Section {
        Toggle("Privé", isOn: $model.isPrivate)
          .foregroundStyle(Color("text"))
      } footer: {
        Text("Un élément privé n'est jamais envoyé à l'IA.")
          .foregroundStyle(Color("textMuted"))
      }
      .listRowBackground(Color("surface"))
    }
    .scrollContentBackground(.hidden)
    .disabled(!model.isEditable)
  }
}

/// The site and the title, or the URL, of the shared link (screen 2).
private struct LinkCard: View {
  let preview: LinkPreview

  var body: some View {
    VStack(alignment: .leading, spacing: 4) {
      Text(verbatim: preview.site)
        .font(.footnote)
        .foregroundStyle(Color("textMuted"))
      Text(verbatim: preview.headline)
        .font(.headline)
        .foregroundStyle(Color("text"))
        .lineLimit(4)
    }
    .padding(.vertical, 4)
    .accessibilityElement(children: .combine)
  }
}

/// The thumbnail and the caption `Image · HEIC · 3,1 Mo` (screen 4).
private struct ImagePreview: View {
  let thumbnail: UIImage?
  let caption: String?

  var body: some View {
    VStack(alignment: .leading, spacing: 8) {
      if let thumbnail {
        Image(uiImage: thumbnail)
          .resizable()
          .scaledToFit()
          .frame(maxWidth: .infinity, maxHeight: 240)
          // The card radius of the app's tokens (radius.card); only colours are generated.
          .clipShape(RoundedRectangle(cornerRadius: 16, style: .continuous))
          .accessibilityHidden(true)
      }
      if let caption {
        Text(verbatim: caption)
          .font(.footnote)
          .foregroundStyle(Color("textMuted"))
      }
    }
    .padding(.vertical, 4)
  }
}

/// One screen per state after the form: icon, title, message, caption and actions.
private struct StatusView: View {
  let model: ShareModel
  let text: ScreenText

  var body: some View {
    ScrollView {
      VStack(spacing: 16) {
        Image(systemName: model.state.symbol.name)
          .font(.system(.largeTitle))
          .imageScale(.large)
          .foregroundStyle(Color(model.state.symbol.color))
          .accessibilityHidden(true)
        Text(verbatim: text.title)
          .font(.title2.bold())
          .foregroundStyle(Color("text"))
          .accessibilityAddTraits(.isHeader)
        Text(verbatim: text.message)
          .font(.body)
          .foregroundStyle(Color("text"))
        if let caption = text.caption {
          Text(verbatim: caption)
            .font(.footnote)
            .foregroundStyle(Color("textMuted"))
        }
        actions
          .padding(.top, 8)
      }
      .multilineTextAlignment(.center)
      .frame(maxWidth: .infinity)
      .padding(24)
      .padding(.top, 32)
    }
  }

  @ViewBuilder
  private var actions: some View {
    VStack(spacing: 12) {
      if model.state.offersRetry {
        Button(action: model.retry) {
          // The background colour on the accent: 9.2:1 in dark, 5.4:1 in light (white on the
          // dark accent would be 2.3:1).
          Text("Réessayer")
            .foregroundStyle(Color("background"))
            .frame(maxWidth: .infinity)
        }
        .buttonStyle(.borderedProminent)
        .controlSize(.large)
        if let copy = model.copyAction {
          Button(action: model.copy) {
            // The text colour: the tinted label of a bordered button is under 4.5:1 in light.
            Text(verbatim: copy.label)
              .foregroundStyle(Color("text"))
              .frame(maxWidth: .infinity)
          }
          .buttonStyle(.bordered)
          .controlSize(.large)
        }
      } else if !model.state.closesBySelf {
        Button(action: model.close) {
          Text("Fermer")
            .foregroundStyle(Color("text"))
            .frame(maxWidth: .infinity)
        }
        .buttonStyle(.bordered)
        .controlSize(.large)
      }
    }
  }
}

extension ShareState {
  /// « Annuler » stays in the bar while loading, editing, sending and on the unreachable screen,
  /// which has no « Fermer » (design D9).
  var showsCancel: Bool {
    switch self {
    case .loading, .form, .sending, .unreachable: true
    default: false
    }
  }

  /// The icon of each screen and the colour set it takes (mockups.md).
  var symbol: (name: String, color: String) {
    switch self {
    case .saved: ("checkmark.circle.fill", "success")
    case .savedPrivate: ("lock.fill", "success")
    case .duplicate: ("tray.full.fill", "accent")
    case .unreachable: ("wifi.slash", "danger")
    case .tooLarge, .tooLong: ("exclamationmark.triangle.fill", "warning")
    case .refused: ("nosign", "danger")
    case .tokenRefused: ("key.slash", "danger")
    case .preparationFailed: ("xmark.octagon.fill", "danger")
    case .nothingToSave, .loading, .form, .sending: ("tray", "textMuted")
    }
  }
}
