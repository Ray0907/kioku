import AppKit

// TextKit owns wrapping, fallback-font line heights and the drawn ellipsis.
final class ReadingText: NSView {
    let textStorage = NSTextStorage()
    let layoutManager = NSLayoutManager()
    let textContainer = NSTextContainer(size: NSSize(width: 300, height: CGFloat.greatestFiniteMagnitude))
    var string: String { textStorage.string }
    override var isFlipped: Bool { true }
    init(lines: Int = 2) {
        super.init(frame: .zero)
        textContainer.lineFragmentPadding = 0
        textContainer.maximumNumberOfLines = lines
        textContainer.lineBreakMode = lines > 0 ? .byTruncatingTail : .byWordWrapping
        layoutManager.addTextContainer(textContainer); textStorage.addLayoutManager(layoutManager)
        setAccessibilityElement(true); setAccessibilityRole(.staticText)
    }
    required init?(coder: NSCoder) { fatalError("Programmatic UI") }
    func setText(_ value: NSAttributedString) {
        textStorage.setAttributedString(value)
        invalidateIntrinsicContentSize(); needsDisplay = true
    }
    override func setFrameSize(_ newSize: NSSize) {
        super.setFrameSize(newSize)
        if textContainer.size.width != newSize.width, newSize.width > 0 {
            textContainer.size = NSSize(width: newSize.width, height: CGFloat.greatestFiniteMagnitude)
            invalidateIntrinsicContentSize()
        }
    }
    override var intrinsicContentSize: NSSize {
        layoutManager.ensureLayout(for: textContainer)
        return NSSize(width: NSView.noIntrinsicMetric, height: ceil(layoutManager.usedRect(for: textContainer).height))
    }
    override func accessibilityValue() -> Any? { string }
    override func draw(_ dirtyRect: NSRect) {
        let range = layoutManager.glyphRange(for: textContainer)
        layoutManager.drawBackground(forGlyphRange: range, at: .zero)
        layoutManager.drawGlyphs(forGlyphRange: range, at: .zero)
    }
    var drawnText: (rect: NSRect, text: String, truncated: Bool, lines: Int) {
        return glyphs(layoutManager, textContainer, string)
    }
}

final class PaddedPopupCell: NSPopUpButtonCell {
    override var cellSize: NSSize { NSSize(width: super.cellSize.width + 16, height: 32) }
    override func titleRect(forBounds rect: NSRect) -> NSRect {
        super.titleRect(forBounds: rect.insetBy(dx: 8, dy: 0))
    }
}
final class PaddedLabelCell: NSTextFieldCell {
    override var cellSize: NSSize { NSSize(width: super.cellSize.width + 24, height: super.cellSize.height + 16) }
    override func drawingRect(forBounds rect: NSRect) -> NSRect {
        super.drawingRect(forBounds: rect.insetBy(dx: 12, dy: 8))
    }
}

@MainActor private func glyphs(_ manager: NSLayoutManager, _ container: NSTextContainer, _ source: String) -> (rect: NSRect, text: String, truncated: Bool, lines: Int) {
    manager.ensureLayout(for: container)
    let range = manager.glyphRange(for: container)
    var end = NSMaxRange(manager.characterRange(forGlyphRange: range, actualGlyphRange: nil))
    var truncated = end < (source as NSString).length
    var lines = 0
    manager.enumerateLineFragments(forGlyphRange: range) { _, _, _, fragment, _ in
        lines += 1
        let hidden = manager.truncatedGlyphRange(inLineFragmentForGlyphAt: fragment.location)
        if hidden.location != NSNotFound {
            end = min(end, manager.characterIndexForGlyph(at: hidden.location))
            truncated = true
        }
    }
    return (manager.usedRect(for: container), (source as NSString).substring(to: end) + (truncated ? "…" : ""), truncated, lines)
}

#if DEBUG
// Measurements come from live AppKit cells / TextKit glyph layout, not AX's
// untruncated semantic value. Tests compare these rectangles themselves.
@MainActor func textMeasurement(_ view: NSView) -> (rect: NSRect, text: String, truncated: Bool, lines: Int)? {
    if let text = view as? ReadingText { return text.drawnText }
    guard let control = view as? NSControl, let cell = control.cell else { return nil }
    let source: NSAttributedString
    if let button = control as? NSButton {
        guard !button.title.isEmpty else { return nil }
        source = NSAttributedString(string: button.title, attributes: [.font: button.font ?? NSFont.systemFont(ofSize: 12)])
    } else if let field = control as? NSTextField {
        source = field.attributedStringValue
    } else { return nil }
    guard source.length > 0 else { return nil }
    let box = control is NSTextField ? cell.drawingRect(forBounds: control.bounds) : cell.titleRect(forBounds: control.bounds)
    let storage = NSTextStorage(attributedString: source)
    let manager = NSLayoutManager()
    let container = NSTextContainer(size: NSSize(width: max(1, box.width), height: CGFloat.greatestFiniteMagnitude))
    container.lineFragmentPadding = 0
    let wrapping = (control as? NSTextField)?.cell?.wraps == true
    container.maximumNumberOfLines = wrapping ? 0 : 1
    container.lineBreakMode = wrapping ? .byWordWrapping : cell.lineBreakMode
    manager.addTextContainer(container); storage.addLayoutManager(manager)
    var result = glyphs(manager, container, source.string)
    switch cell.alignment {
    case .center: result.rect.origin.x += box.midX - result.rect.width / 2
    case .right: result.rect.origin.x += box.maxX - result.rect.width
    default: result.rect.origin.x += box.minX
    }
    result.rect.origin.y += wrapping ? box.minY : box.midY - result.rect.height / 2
    return result
}
#endif
