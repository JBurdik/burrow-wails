import SwiftUI

/// Liquid Glass where the OS has it (iOS 26+), a material before that.
extension View {
    /// Floating surface (composer bar, scroll-to-end button, cards).
    @ViewBuilder func glassSurface<S: InsettableShape>(_ shape: S, interactive: Bool = false, tint: Color? = nil) -> some View {
        if #available(iOS 26.0, *) {
            self.glassEffect(makeGlass(interactive: interactive, tint: tint), in: shape)
        } else {
            self.background(.regularMaterial, in: shape)
                .overlay(shape.strokeBorder(Color(.separator).opacity(0.35), lineWidth: 0.5))
        }
    }

    /// The composer's bottom bar: glass floating over the transcript on 26, `.bar` before.
    @ViewBuilder func glassBar() -> some View {
        if #available(iOS 26.0, *) {
            self.glassEffect(.regular, in: RoundedRectangle(cornerRadius: 26, style: .continuous))
                .padding(.horizontal, 8).padding(.bottom, 4)
        } else {
            self.background(.bar)
        }
    }

    /// Card fill for in-transcript cards: opaque grouped fill, readable in both appearances.
    func cardBackground(cornerRadius: CGFloat = 16) -> some View {
        self.background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: cornerRadius, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: cornerRadius, style: .continuous).strokeBorder(Color(.separator).opacity(0.3), lineWidth: 0.5))
    }
}

@available(iOS 26.0, *)
private func makeGlass(interactive: Bool, tint: Color?) -> Glass {
    var g = Glass.regular
    if let tint { g = g.tint(tint) }
    return interactive ? g.interactive() : g
}
