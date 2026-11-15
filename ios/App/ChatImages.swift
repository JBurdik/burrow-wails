import SwiftUI
import UIKit

/// The chat a transcript row belongs to: image stubs are fetched per chat.
private struct ChatIdKey: EnvironmentKey { static let defaultValue = 0 }
extension EnvironmentValues {
    var chatId: Int {
        get { self[ChatIdKey.self] }
        set { self[ChatIdKey.self] = newValue }
    }
}

/// Images on a transcript row: inline data URLs, or stubs the page left out
/// ("burrow-image://<index>/<n>", src-wails/chatstore.go) fetched once with
/// `load_chat_image` and cached for the session.
enum ChatImages {
    static let cache = NSCache<NSString, UIImage>()
    static let prefix = "burrow-image://"

    static func decode(_ dataURL: String) -> UIImage? {
        guard let comma = dataURL.firstIndex(of: ","), dataURL.hasPrefix("data:"),
              let data = Data(base64Encoded: String(dataURL[dataURL.index(after: comma)...]), options: .ignoreUnknownCharacters)
        else { return nil }
        return UIImage(data: data)
    }

    @MainActor
    static func load(_ src: String, chatId: Int, model: AppModel) async -> UIImage? {
        let key = "\(chatId)|\(src)" as NSString
        if let hit = cache.object(forKey: key) { return hit }
        var img: UIImage?
        if src.hasPrefix(prefix) {
            let parts = src.dropFirst(prefix.count).split(separator: "/").compactMap { Int($0) }
            guard parts.count == 2, let client = model.client,
                  let url = try? await client.call("load_chat_image", ["chatId": chatId, "index": parts[0], "image": parts[1]]) as? String
            else { return nil }
            img = decode(url)
        } else {
            img = decode(src)
        }
        if let img { cache.setObject(img, forKey: key) }
        return img
    }
}

/// A row of thumbnails; a tap opens the image full screen.
struct ChatImageStrip: View {
    let images: [String]
    @Environment(\.chatId) private var chatId
    @State private var opened: IdentifiedImage?

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 6) {
                ForEach(Array(images.enumerated()), id: \.offset) { _, src in
                    ChatImageThumb(src: src, chatId: chatId) { opened = IdentifiedImage(image: $0) }
                }
            }
        }
        .fullScreenCover(item: $opened) { ImageViewer(image: $0.image) }
    }
}

struct IdentifiedImage: Identifiable {
    let id = UUID()
    let image: UIImage
}

private struct ChatImageThumb: View {
    let src: String
    let chatId: Int
    let open: (UIImage) -> Void
    @Environment(AppModel.self) private var model
    @State private var image: UIImage?
    @State private var failed = false

    var body: some View {
        Group {
            if let image {
                Button { open(image) } label: {
                    Image(uiImage: image).resizable().scaledToFill()
                }
                .buttonStyle(.plain)
            } else {
                ZStack {
                    Color(.tertiarySystemFill)
                    if failed { Image(systemName: "photo").foregroundStyle(.secondary) } else { ProgressView() }
                }
            }
        }
        .frame(width: 120, height: 120)
        .clipShape(RoundedRectangle(cornerRadius: 10))
        .task(id: src) {
            image = await ChatImages.load(src, chatId: chatId, model: model)
            failed = image == nil
        }
    }
}

/// Full screen, pinch to zoom, tap ✕ to close.
private struct ImageViewer: View {
    let image: UIImage
    @Environment(\.dismiss) private var dismiss
    @State private var scale: CGFloat = 1
    @State private var base: CGFloat = 1

    var body: some View {
        ZStack(alignment: .topTrailing) {
            Color.black.ignoresSafeArea()
            Image(uiImage: image).resizable().scaledToFit()
                .scaleEffect(scale)
                .gesture(MagnifyGesture()
                    .onChanged { scale = max(1, base * $0.magnification) }
                    .onEnded { _ in base = scale })
                .onTapGesture(count: 2) { withAnimation { scale = scale > 1 ? 1 : 2.5; base = scale } }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            Button { dismiss() } label: {
                Image(systemName: "xmark").font(.body.weight(.semibold)).foregroundStyle(.white)
                    .frame(width: 40, height: 40).background(.ultraThinMaterial, in: Circle())
            }
            .padding()
        }
    }
}
