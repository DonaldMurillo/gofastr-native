#if canImport(UIKit)
import AudioToolbox
import AVFoundation
import Chordharp
import UIKit

// The chord harp: a chord grid, a strumplate and a rhythm bar drawn in one
// view. Touches call the bound Go package directly and an audio queue
// pulls samples from it, so nothing waits on a screen render. The layout,
// colors and touch rules follow ChordHarpView.java.
enum ChordHarpComponent: MobileComponentExtensionFactory {
    static let name = "dev.gofastr.chordharp/v1"

    static func capabilities() -> [String: String] { [name: "available"] }

    static func make(payload: String, compact: Bool, emit: @escaping (String) -> Void) throws -> MobileComponentExtensionView {
        ChordHarpInstrument(view: ChordHarpView(layout: try ChordHarpLayout.decode(payload)))
    }
}

final class ChordHarpInstrument: MobileComponentExtensionView {
    private let harp: ChordHarpView
    var view: UIView { harp }

    init(view: ChordHarpView) { harp = view }

    func update(payload: String, emit: @escaping (String) -> Void) throws {
        harp.apply(try ChordHarpLayout.decode(payload))
    }

    func dispose() { harp.dispose() }
}

/// The grid labels and rhythm names Go sends.
struct ChordHarpLayout: Decodable {
    var kinds: [String]
    var chords: [[String]]
    var patterns: [String]

    static func decode(_ payload: String) throws -> ChordHarpLayout {
        let layout = try JSONDecoder().decode(ChordHarpLayout.self, from: Data(payload.utf8))
        guard !layout.kinds.isEmpty, layout.chords.count == layout.kinds.count,
              let columns = layout.chords.first?.count, columns > 0,
              layout.chords.allSatisfy({ $0.count == columns }) else {
            throw MobileComponentFailure.invalidPayload
        }
        return layout
    }
}

/// What the view draws, from the bound package's State.
private struct HarpState: Decodable {
    var held = false, kind = 0, column = 0, latch = false
    var chord = "", short = "", tones = ""
    var zones: [String]? = nil
    var playing = false, pattern = 0, tempo = 0, steps = 0, perBeat = 0

    static func read() -> HarpState {
        (try? JSONDecoder().decode(HarpState.self, from: Data(ChordharpState().utf8))) ?? HarpState()
    }
}

final class ChordHarpView: UIView {
    private static let body = rgb(0x16151a), panel = rgb(0x221f27), line = rgb(0x3a3542)
    private static let ink = rgb(0xf3efe6), muted = rgb(0xa59fae), lit = rgb(0xf0a64a)
    private static let rows = [rgb(0xffc857), rgb(0x5bc0eb), rgb(0xe36bae)] // major, minor, seventh
    private static let zones = 12

    private var layout: ChordHarpLayout
    private var state = HarpState()

    // Layout, rebuilt on every size change.
    private var laidOut = CGSize.zero
    private var grid = CGRect.zero, plate = CGRect.zero
    private var portrait = false
    private var controls: [Control] = []
    private var beatsRect = CGRect.zero, chordRect = CGRect.zero, patternRect = CGRect.zero, tempoRect = CGRect.zero

    // Touches: which pad, zone or control each finger is on.
    private typealias Finger = ObjectIdentifier
    private var padFingers: [Finger: Pad] = [:]
    private var padOrder: [Finger] = []
    private var zoneFingers: [Finger: Int] = [:]
    private var controlFingers: [Finger: String] = [:]
    private var repeatTimers: [Finger: Timer] = [:]
    private var readout: (text: String, size: CGFloat) = ("", 15)
    private var shownStep = -1
    private var glowShown = false
    private var glow = [CFTimeInterval](repeating: 0, count: ChordHarpView.zones)
    private var displayLink: CADisplayLink?
    private let haptics = UIImpactFeedbackGenerator(style: .light)

    // The view fills the screen under the title, so the host's scroll view
    // has nothing to scroll; it is held still so a strum is never a scroll.
    private weak var scroll: UIScrollView?
    private var scrollSettings: (enabled: Bool, delays: Bool)?
    private var fill: NSLayoutConstraint?
    private lazy var standalone = heightAnchor.constraint(equalToConstant: 360)

    private var audio: ChordHarpAudio?
    private var observers: [NSObjectProtocol] = []
    private var disposed = false

    init(layout: ChordHarpLayout) {
        self.layout = layout
        super.init(frame: .zero)
        isOpaque = false
        backgroundColor = .clear
        contentMode = .redraw
        isMultipleTouchEnabled = true
        standalone.priority = .defaultLow
        standalone.isActive = true
        heightAnchor.constraint(greaterThanOrEqualToConstant: 200).isActive = true
        accessibilityIdentifier = "chordharp.instrument"
        accessibilityElements = voiceOver.all
        let center = NotificationCenter.default
        observers = [
            center.addObserver(forName: UIApplication.didEnterBackgroundNotification, object: nil, queue: .main) { [weak self] _ in self?.pauseAudio() },
            center.addObserver(forName: UIApplication.didBecomeActiveNotification, object: nil, queue: .main) { [weak self] _ in self?.startAudioIfShown() },
            center.addObserver(forName: AVAudioSession.interruptionNotification, object: nil, queue: .main) { [weak self] note in
                guard let raw = note.userInfo?[AVAudioSessionInterruptionTypeKey] as? UInt,
                      let kind = AVAudioSession.InterruptionType(rawValue: raw) else { return }
                // A call or alarm stops the engine; build a new one after it.
                self?.pauseAudio()
                if kind == .ended { self?.startAudioIfShown() }
            },
            center.addObserver(forName: AVAudioSession.mediaServicesWereResetNotification, object: nil, queue: .main) { [weak self] _ in
                self?.restartAudio()
            },
        ]
        refresh()
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) is not supported") }

    deinit {
        observers.forEach(NotificationCenter.default.removeObserver)
        displayLink?.invalidate()
        repeatTimers.values.forEach { $0.invalidate() }
        audio?.stop()
    }

    /// Takes new grid labels and pattern names from Go.
    func apply(_ next: ChordHarpLayout) {
        layout = next
        laidOut = .zero
        setNeedsLayout()
        refresh()
    }

    /// Stops the sound and lets the host's scroll view go. Safe to call again.
    func dispose() {
        disposed = true
        pauseAudio()
        cancelRepeats()
        displayLink?.invalidate()
        displayLink = nil
        releaseScroll()
        observers.forEach(NotificationCenter.default.removeObserver)
        observers.removeAll()
    }

    private func refresh() {
        state = HarpState.read()
        readout = chordReadout()
        voiceOver.update(state, pattern: pattern())
        setNeedsDisplay()
    }

    private func pattern() -> String {
        layout.patterns.indices.contains(state.pattern) ? layout.patterns[state.pattern] : ""
    }

    // MARK: - Size

    override func didMoveToWindow() {
        super.didMoveToWindow()
        if window == nil {
            releaseScroll()
            pauseAudio()
            cancelRepeats()
            displayLink?.invalidate()
            displayLink = nil
        } else if !disposed {
            holdScroll()
            startAudioIfShown()
            refresh()
        }
    }

    private func holdScroll() {
        var parent = superview
        while let view = parent, !(view is UIScrollView) { parent = view.superview }
        guard let found = parent as? UIScrollView, found !== scroll else { return }
        releaseScroll()
        scroll = found
        scrollSettings = (found.isScrollEnabled, found.delaysContentTouches)
        found.isScrollEnabled = false
        found.delaysContentTouches = false
        // Fill the visible height, less whatever else the host put in the
        // content (margins, labels); layoutSubviews measures that.
        let constraint = heightAnchor.constraint(equalTo: found.frameLayoutGuide.heightAnchor, constant: -56)
        constraint.priority = UILayoutPriority(999)
        constraint.isActive = true
        fill = constraint
    }

    private func releaseScroll() {
        fill?.isActive = false
        fill = nil
        if let scroll, let settings = scrollSettings {
            scroll.isScrollEnabled = settings.enabled
            scroll.delaysContentTouches = settings.delays
        }
        scroll = nil
        scrollSettings = nil
    }

    /// Sets the fill constraint's offset to the content the host keeps
    /// around the instrument. That does not depend on the instrument's own
    /// height, so a second pass leaves it unchanged.
    private func fit() {
        guard let scroll, let fill, bounds.height > 0, scroll.contentSize.height > 0 else { return }
        let around = scroll.contentSize.height - bounds.height + scroll.adjustedContentInset.top + scroll.adjustedContentInset.bottom
        if abs(-around - fill.constant) > 0.5 { fill.constant = -around }
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        if bounds.size != laidOut { arrange(bounds.size) }
        fit()
    }

    private func arrange(_ size: CGSize) {
        laidOut = size
        let w = size.width, h = size.height
        let pad: CGFloat = 10, gap: CGFloat = 10, row: CGFloat = 48
        portrait = h > w
        controls.removeAll()
        let twoRows = w < 600
        let barBottom = pad + row * (twoRows ? 2 : 1) + (twoRows ? 6 : 0)
        // Row one: transport and pattern.
        var x = pad, y = pad
        controls.append(Control(id: "toggle", rect: CGRect(x: x, y: y, width: 92, height: row)))
        x += 92 + gap
        controls.append(Control(id: "pattern-", rect: CGRect(x: x, y: y, width: 48, height: row)))
        x += 48
        let patternWidth = twoRows ? w - pad - 48 - x - gap - 64 : 124
        patternRect = CGRect(x: x, y: y, width: patternWidth, height: row)
        x += patternWidth
        controls.append(Control(id: "pattern+", rect: CGRect(x: x, y: y, width: 48, height: row)))
        x += 48 + gap
        if twoRows {
            beatsRect = CGRect(x: x, y: y, width: w - pad - x, height: row)
            x = pad
            y += row + 6
        }
        // Tempo, latch, and (one row) the beat lights.
        controls.append(Control(id: "tempo-", rect: CGRect(x: x, y: y, width: 48, height: row)))
        x += 48
        tempoRect = CGRect(x: x, y: y, width: 76, height: row)
        x += 76
        controls.append(Control(id: "tempo+", rect: CGRect(x: x, y: y, width: 48, height: row)))
        x += 48 + gap
        controls.append(Control(id: "latch", rect: CGRect(x: x, y: y, width: 84, height: row)))
        x += 84 + gap
        if !twoRows {
            beatsRect = CGRect(x: x, y: y, width: 76, height: row)
            x += 76 + gap
        }
        chordRect = CGRect(x: x, y: y, width: max(0, w - pad - x), height: row)

        let top = barBottom + gap, bottom = h - pad
        let plateWidth = max(110, (w - 2 * pad) * (portrait ? 0.32 : 0.3))
        plate = CGRect(x: w - pad - plateWidth, y: top, width: plateWidth, height: max(0, bottom - top))
        grid = CGRect(x: pad, y: top, width: max(0, plate.minX - gap - pad), height: max(0, bottom - top))
        readout = chordReadout()
        voiceOver.place(controls: Dictionary(uniqueKeysWithValues: controls.map { ($0.id, $0.rect) }),
                        pattern: patternRect, tempo: tempoRect, readout: chordRect, surface: grid.union(plate))
        setNeedsDisplay()
    }

    // MARK: - Drawing

    override func draw(_ rect: CGRect) {
        let now = CACurrentMediaTime()
        Self.body.setFill()
        UIBezierPath(roundedRect: bounds, cornerRadius: 16).fill()
        drawBar()
        drawGrid()
        let glowing = drawPlate(now)
        glowShown = glowing
        animate(glowing || state.playing)
    }

    private func drawBar() {
        let pressed = Set(controlFingers.values)
        for c in controls {
            let on = c.id == "toggle" && state.playing || c.id == "latch" && state.latch
            (on ? Self.lit : pressed.contains(c.id) ? Self.line : Self.panel).setFill()
            UIBezierPath(roundedRect: c.rect.insetBy(dx: 3, dy: 3), cornerRadius: 10).fill()
            let label: String
            switch c.id {
            case "toggle": label = state.playing ? "■ Stop" : "▶ Start"
            case "pattern-": label = "‹"
            case "pattern+": label = "›"
            case "tempo-": label = "−"
            case "tempo+": label = "+"
            default: label = "Latch"
            }
            let symbol = c.id.hasSuffix("-") || c.id.hasSuffix("+")
            draw(label, in: c.rect, color: on ? Self.body : Self.ink, size: symbol ? 22 : 15)
        }
        draw(pattern(), in: patternRect, color: Self.ink, size: 15)
        draw("\(state.tempo) bpm", in: tempoRect, color: Self.ink, size: 15)
        drawBeats()
        draw(readout.text, in: chordRect, color: state.held ? Self.ink : Self.muted, size: readout.size, right: true)
    }

    private func drawBeats() {
        let steps = max(state.steps, 1), perBeat = max(state.perBeat, 1), beats = steps / perBeat
        let step = ChordharpStep(), beat = step < 0 ? -1 : step / perBeat
        shownStep = step
        let r: CGFloat = 5, spacing = min(16, beatsRect.width / CGFloat(max(beats, 1)))
        let x = beatsRect.midX - spacing * CGFloat(beats - 1) / 2, y = beatsRect.midY
        for i in 0..<max(beats, 0) {
            (i == beat ? (i == 0 ? Self.lit : Self.ink) : Self.line).setFill()
            let radius = i == beat ? r * 1.3 : r
            let center = CGPoint(x: x + CGFloat(i) * spacing, y: y)
            UIBezierPath(ovalIn: CGRect(x: center.x - radius, y: center.y - radius, width: radius * 2, height: radius * 2)).fill()
        }
    }

    private func drawGrid() {
        let rows = layout.kinds.count, cols = layout.chords.first?.count ?? 0
        guard rows > 0, cols > 0, grid.width > 0, grid.height > 0 else { return }
        for k in 0..<rows {
            for c in 0..<cols {
                let r = cell(k, c).insetBy(dx: 3, dy: 3)
                let on = state.held && k == state.kind && c == state.column
                let color = Self.rows[k % Self.rows.count]
                (on ? color : blend(color, Self.panel, 0.18)).setFill()
                UIBezierPath(roundedRect: r, cornerRadius: 10).fill()
                if !on {
                    blend(color, Self.panel, 0.55).setFill()
                    UIRectFill(CGRect(x: r.minX + 10, y: r.maxY - 5, width: max(0, r.width - 20), height: 2))
                }
                let name = c < layout.chords[k].count ? layout.chords[k][c] : ""
                draw(name, in: r, color: on ? Self.body : color, size: min(20, r.height * 0.42))
            }
        }
    }

    /// The pad for chord kind k and grid column c.
    private func cell(_ k: Int, _ c: Int) -> CGRect {
        let rows = CGFloat(layout.kinds.count), cols = CGFloat(layout.chords.first?.count ?? 1)
        if portrait {
            let w = grid.width / rows, h = grid.height / cols
            return CGRect(x: grid.minX + CGFloat(k) * w, y: grid.minY + CGFloat(c) * h, width: w, height: h)
        }
        let w = grid.width / cols, h = grid.height / rows
        return CGRect(x: grid.minX + CGFloat(c) * w, y: grid.minY + CGFloat(k) * h, width: w, height: h)
    }

    private func drawPlate(_ now: CFTimeInterval) -> Bool {
        Self.panel.setFill()
        UIBezierPath(roundedRect: plate, cornerRadius: 14).fill()
        let color = Self.rows[max(state.kind, 0) % Self.rows.count]
        let h = plate.height / CGFloat(Self.zones)
        var glowing = false
        for z in 0..<Self.zones {
            let bottom = plate.maxY - CGFloat(z) * h, top = bottom - h
            let age = (now - glow[z]) / 0.6
            let lit = glow[z] > 0 && age < 1
            if lit {
                glowing = true
                blend(color, Self.panel, 0.85 * (1 - max(age, 0))).setFill()
                UIBezierPath(roundedRect: CGRect(x: plate.minX + 4, y: top + 2, width: plate.width - 8, height: h - 4), cornerRadius: 8).fill()
            }
            if z > 0 {
                (z % 3 == 0 ? blend(color, Self.line, 0.5) : Self.line).setFill()
                UIRectFill(CGRect(x: plate.minX + 12, y: bottom - 1, width: max(0, plate.width - 24), height: 1))
            }
            if let zones = state.zones, zones.indices.contains(z) {
                let attributes = Self.attributes(12, lit && age < 0.6 ? Self.ink : Self.muted)
                let size = (zones[z] as NSString).size(withAttributes: attributes)
                (zones[z] as NSString).draw(at: CGPoint(x: plate.minX + 12, y: top + (h - size.height) / 2), withAttributes: attributes)
            }
        }
        draw("strum \u{2195}\u{FE0E}", in: CGRect(x: plate.midX + 14 - 40, y: plate.minY + 2, width: 80, height: 16), color: Self.muted, size: 11)
        return glowing
    }

    private func draw(_ s: String, in r: CGRect, color: UIColor, size: CGFloat, right: Bool = false) {
        let attributes = Self.attributes(size, color)
        let measured = (s as NSString).size(withAttributes: attributes)
        let x = right ? r.maxX - 4 - measured.width : r.midX - measured.width / 2
        (s as NSString).draw(at: CGPoint(x: x, y: r.midY - measured.height / 2), withAttributes: attributes)
    }

    private static var fonts: [CGFloat: UIFont] = [:]

    private static func attributes(_ size: CGFloat, _ color: UIColor) -> [NSAttributedString.Key: Any] {
        let size = max(size, 1)
        let font = fonts[size] ?? UIFont.systemFont(ofSize: size, weight: .bold)
        fonts[size] = font
        return [.font: font, .foregroundColor: color]
    }

    /// The held chord, as long as the bar has room for: "C major · C E G"
    /// down to "C". A phone's portrait bar is narrow, so the text shrinks
    /// to 13 points before it is cut.
    private func chordReadout() -> (text: String, size: CGFloat) {
        let choices = state.held
            ? ["\(state.chord)  ·  \(state.tones)", "\(state.short)  ·  \(state.tones)", state.chord, state.short]
            : ["Hold a chord, then strum", "Hold a chord", "No chord"]
        let width = chordRect.width - 4
        for size: CGFloat in [15, 13] {
            let attributes = Self.attributes(size, Self.ink)
            for s in choices where (s as NSString).size(withAttributes: attributes).width <= width { return (s, size) }
        }
        let attributes = Self.attributes(13, Self.ink)
        var s = choices[choices.count - 1]
        while s.count > 1 && ((s + "…") as NSString).size(withAttributes: attributes).width > width { s.removeLast() }
        return (s + "…", 13)
    }

    /// Runs a display link while a zone glows or the rhythm runs. It
    /// redraws on a frame only when the last drawing had a glow or the beat
    /// moved. The frame after a glow ends clears it, and that draw stops
    /// the link unless the rhythm is running.
    private func animate(_ on: Bool) {
        if on && displayLink == nil && window != nil && !disposed {
            let link = CADisplayLink(target: DisplayTarget(self), selector: #selector(DisplayTarget.tick))
            link.add(to: .main, forMode: .common)
            displayLink = link
        } else if !on, let link = displayLink {
            link.invalidate()
            displayLink = nil
        }
    }

    fileprivate func tick() {
        if glowShown || ChordharpStep() != shownStep { setNeedsDisplay() }
    }

    // MARK: - Touch

    override func touchesBegan(_ touches: Set<UITouch>, with event: UIEvent?) {
        for touch in touches { down(Finger(touch), touch.location(in: self)) }
    }

    override func touchesMoved(_ touches: Set<UITouch>, with event: UIEvent?) {
        for touch in touches {
            for sample in event?.coalescedTouches(for: touch) ?? [touch] { move(Finger(touch), sample.location(in: self)) }
        }
    }

    override func touchesEnded(_ touches: Set<UITouch>, with event: UIEvent?) {
        for touch in touches { up(Finger(touch), touch.location(in: self), cancel: false) }
    }

    override func touchesCancelled(_ touches: Set<UITouch>, with event: UIEvent?) {
        for touch in touches { up(Finger(touch), nil, cancel: true) }
    }

    private func down(_ finger: Finger, _ point: CGPoint) {
        if let pad = padAt(point) {
            if state.latch && state.held && state.kind == pad.kind && state.column == pad.column { ChordharpRelease() }
            else { ChordharpHold(pad.kind, pad.column) }
            padFingers[finger] = pad
            padOrder.append(finger)
            tap()
            refresh()
            return
        }
        if plate.contains(point) {
            let z = zoneAt(point.y)
            zoneFingers[finger] = z
            pluck(z, delay: 0)
            setNeedsDisplay()
            return
        }
        if let c = controls.first(where: { $0.rect.contains(point) }) {
            controlFingers[finger] = c.id
            if c.repeats {
                press(c.id)
                scheduleRepeat(c.id, for: finger)
            }
            setNeedsDisplay()
        }
    }

    private func move(_ finger: Finger, _ point: CGPoint) {
        if let from = zoneFingers[finger] {
            let to = zoneAt(point.y)
            if to != from {
                // Pluck every zone passed, spaced a few milliseconds apart,
                // so a fast swipe still sounds as a strum.
                let dir = to > from ? 1 : -1
                for (n, z) in stride(from: from + dir, through: to, by: dir).enumerated() { pluck(z, delay: 0.004 * Double(n)) }
                zoneFingers[finger] = to
                setNeedsDisplay()
            }
            return
        }
        if let pad = padFingers[finger], !state.latch, let now = padAt(point), now != pad {
            padFingers[finger] = now
            ChordharpHold(now.kind, now.column)
            tap()
            refresh()
        }
    }

    private func up(_ finger: Finger, _ point: CGPoint?, cancel: Bool) {
        zoneFingers[finger] = nil
        if let id = controlFingers.removeValue(forKey: finger) {
            cancelRepeat(finger)
            if !cancel, let point, let c = controls.first(where: { $0.id == id }), !c.repeats, c.rect.contains(point) { press(id) }
            setNeedsDisplay()
        }
        if padFingers.removeValue(forKey: finger) != nil {
            padOrder.removeAll { $0 == finger }
            if !state.latch {
                // Another finger still on a pad takes over; otherwise let go.
                if let last = padOrder.last, let pad = padFingers[last] { ChordharpHold(pad.kind, pad.column) }
                else { ChordharpRelease() }
            }
            refresh()
        }
    }

    private func press(_ id: String) {
        switch id {
        case "toggle": ChordharpToggle()
        case "pattern-": ChordharpNextPattern(-1)
        case "pattern+": ChordharpNextPattern(1)
        case "tempo-": ChordharpNudgeTempo(-2)
        case "tempo+": ChordharpNudgeTempo(2)
        case "latch":
            let on = !state.latch
            ChordharpSetLatch(on)
            if !on && padOrder.isEmpty { ChordharpRelease() }
        default: return
        }
        tap()
        refresh()
    }

    /// Tempo buttons repeat while held: after 450 ms, then every 90 ms.
    /// Each finger has its own repeat, so lifting one leaves the others.
    private func scheduleRepeat(_ id: String, for finger: Finger) {
        cancelRepeat(finger)
        repeatTimers[finger] = Timer.scheduledTimer(withTimeInterval: 0.45, repeats: false) { [weak self] _ in
            guard let self, self.repeatTimers[finger] != nil else { return }
            self.press(id)
            self.repeatTimers[finger] = Timer.scheduledTimer(withTimeInterval: 0.09, repeats: true) { [weak self] _ in self?.press(id) }
        }
    }

    private func cancelRepeat(_ finger: Finger) {
        repeatTimers.removeValue(forKey: finger)?.invalidate()
    }

    private func cancelRepeats() {
        repeatTimers.values.forEach { $0.invalidate() }
        repeatTimers.removeAll()
    }

    private func tap() {
        haptics.impactOccurred()
        haptics.prepare()
    }

    private func pluck(_ zone: Int, delay: Double) {
        ChordharpPluck(zone, delay)
        glow[zone] = CACurrentMediaTime() + delay
    }

    private func padAt(_ point: CGPoint) -> Pad? {
        let rows = layout.kinds.count, cols = layout.chords.first?.count ?? 0
        guard grid.contains(point), rows > 0, cols > 0 else { return nil }
        let a = Int((point.x - grid.minX) / grid.width * CGFloat(portrait ? rows : cols))
        let b = Int((point.y - grid.minY) / grid.height * CGFloat(portrait ? cols : rows))
        let k = portrait ? a : b, c = portrait ? b : a
        return Pad(kind: min(max(k, 0), rows - 1), column: min(max(c, 0), cols - 1))
    }

    private func zoneAt(_ y: CGFloat) -> Int {
        guard plate.height > 0 else { return 0 }
        let z = Int((plate.maxY - y) / plate.height * CGFloat(Self.zones))
        return min(max(z, 0), Self.zones - 1)
    }

    // MARK: - Audio

    private func startAudioIfShown() {
        guard !disposed, window != nil, audio == nil, UIApplication.shared.applicationState != .background else { return }
        let next = ChordHarpAudio()
        do {
            try next.start()
            audio = next
        } catch {
            next.stop()
            NSLog("gofastr-native: chord harp audio did not start: %@", error.localizedDescription)
        }
    }

    private func pauseAudio() {
        audio?.stop()
        audio = nil
    }

    /// A media server reset kills the queue: build a new one.
    private func restartAudio() {
        pauseAudio()
        startAudioIfShown()
    }

    private lazy var voiceOver = ChordHarpAccessibility(container: self) { [weak self] id in self?.press(id) }

    private struct Pad: Equatable { let kind: Int, column: Int }

    private struct Control {
        let id: String
        let rect: CGRect
        var repeats: Bool { id.hasPrefix("tempo") }
    }
}

/// Holds the view weakly, since a display link retains its target.
private final class DisplayTarget {
    private weak var view: ChordHarpView?
    init(_ view: ChordHarpView) { self.view = view }
    @objc func tick() { view?.tick() }
}

/// VoiceOver's view of the harp: the bar's buttons by name, the rhythm and
/// tempo as adjustable values, the chord readout, and the grid and
/// strumplate as one surface that passes touches straight through, so the
/// harp can be played with VoiceOver on.
final class ChordHarpAccessibility {
    private let toggle: ChordHarpElement, rhythm: ChordHarpElement, tempo: ChordHarpElement
    private let latch: ChordHarpElement, readout: UIAccessibilityElement, surface: UIAccessibilityElement
    let all: [UIAccessibilityElement]

    init(container: UIView, press: @escaping (String) -> Void) {
        toggle = ChordHarpElement(container, "chordharp.toggle", activate: { press("toggle") })
        rhythm = ChordHarpElement(container, "chordharp.rhythm", up: { press("pattern+") }, down: { press("pattern-") })
        tempo = ChordHarpElement(container, "chordharp.tempo", up: { press("tempo+") }, down: { press("tempo-") })
        latch = ChordHarpElement(container, "chordharp.latch", activate: { press("latch") })
        readout = UIAccessibilityElement(accessibilityContainer: container)
        readout.accessibilityTraits = .staticText
        readout.accessibilityIdentifier = "chordharp.chord"
        surface = UIAccessibilityElement(accessibilityContainer: container)
        surface.accessibilityTraits = .allowsDirectInteraction
        surface.accessibilityIdentifier = "chordharp.surface"
        surface.accessibilityHint = "Chord pads on the left, strumplate on the right, low notes at the bottom."
        rhythm.accessibilityLabel = "Rhythm"
        tempo.accessibilityLabel = "Tempo"
        latch.accessibilityLabel = "Latch"
        all = [toggle, rhythm, tempo, latch, readout, surface]
    }

    fileprivate func update(_ state: HarpState, pattern: String) {
        toggle.accessibilityLabel = state.playing ? "Stop rhythm" : "Start rhythm"
        rhythm.accessibilityValue = pattern
        tempo.accessibilityValue = "\(state.tempo) beats per minute"
        latch.accessibilityTraits = state.latch ? [.button, .selected] : .button
        readout.accessibilityLabel = state.held ? "\(state.chord) held, \(state.tones)" : "No chord held"
        var label = "Chord pads and strumplate"
        if let zones = state.zones, let low = zones.first, let high = zones.last { label += ", strumplate from \(low) to \(high)" }
        surface.accessibilityLabel = label
    }

    func place(controls: [String: CGRect], pattern: CGRect, tempo tempoRect: CGRect, readout chord: CGRect, surface area: CGRect) {
        let frame = { (id: String) in controls[id] ?? .zero }
        toggle.accessibilityFrameInContainerSpace = frame("toggle")
        rhythm.accessibilityFrameInContainerSpace = frame("pattern-").union(pattern).union(frame("pattern+"))
        tempo.accessibilityFrameInContainerSpace = frame("tempo-").union(tempoRect).union(frame("tempo+"))
        latch.accessibilityFrameInContainerSpace = frame("latch")
        readout.accessibilityFrameInContainerSpace = chord
        surface.accessibilityFrameInContainerSpace = area
    }
}

/// A bar button, or an adjustable value when up and down are given.
final class ChordHarpElement: UIAccessibilityElement {
    private let activate: (() -> Void)?, up: (() -> Void)?, down: (() -> Void)?

    init(_ container: UIView, _ identifier: String, activate: (() -> Void)? = nil, up: (() -> Void)? = nil, down: (() -> Void)? = nil) {
        self.activate = activate
        self.up = up
        self.down = down
        super.init(accessibilityContainer: container)
        accessibilityIdentifier = identifier
        accessibilityTraits = up == nil ? .button : .adjustable
    }

    override func accessibilityActivate() -> Bool {
        guard let activate else { return false }
        activate()
        return true
    }

    override func accessibilityIncrement() { up?() }
    override func accessibilityDecrement() { down?() }
}

/// Plays 16-bit mono PCM from Go through an audio queue: three 256-frame
/// buffers, about 16 ms at 48 kHz, as the terminal app does on macOS. The
/// queue calls Fill on its own thread, not the real-time I/O thread, and
/// the buffers queued ahead cover a short stall in Go. The queue follows
/// output route changes by itself.
final class ChordHarpAudio {
    private static let frames = 256, buffers = 3
    private var queue: AudioQueueRef?

    func start() throws {
        let session = AVAudioSession.sharedInstance()
        try session.setCategory(.playback, mode: .default, options: [.mixWithOthers])
        try? session.setPreferredIOBufferDuration(0.005)
        try session.setActive(true)
        let rate = session.sampleRate > 0 ? session.sampleRate : 48000
        ChordharpOpen(Int(rate.rounded()))
        var format = AudioStreamBasicDescription(
            mSampleRate: rate, mFormatID: kAudioFormatLinearPCM,
            mFormatFlags: kLinearPCMFormatFlagIsSignedInteger | kLinearPCMFormatFlagIsPacked,
            mBytesPerPacket: 2, mFramesPerPacket: 1, mBytesPerFrame: 2, mChannelsPerFrame: 1,
            mBitsPerChannel: 16, mReserved: 0)
        var created: AudioQueueRef?
        try check(AudioQueueNewOutput(&format, chordHarpRefill, nil, nil, nil, 0, &created), "create the queue")
        guard let created else { throw failure("create the queue", -1) }
        queue = created
        for _ in 0..<Self.buffers {
            var buffer: AudioQueueBufferRef?
            try check(AudioQueueAllocateBuffer(created, UInt32(Self.frames * 2), &buffer), "allocate a buffer")
            if let buffer { chordHarpRefill(nil, created, buffer) }
        }
        try check(AudioQueueStart(created, nil), "start the queue")
    }

    func stop() {
        guard let queue else { return }
        self.queue = nil
        AudioQueueStop(queue, true)
        AudioQueueDispose(queue, true)
    }

    private func check(_ status: OSStatus, _ step: String) throws {
        if status != noErr { throw failure(step, status) }
    }

    private func failure(_ step: String, _ status: OSStatus) -> NSError {
        NSError(domain: NSOSStatusErrorDomain, code: Int(status), userInfo: [NSLocalizedDescriptionKey: "could not \(step) (\(status))"])
    }
}

/// Fills one queue buffer from Go and queues it again.
private func chordHarpRefill(_: UnsafeMutableRawPointer?, _ queue: AudioQueueRef, _ buffer: AudioQueueBufferRef) {
    let bytes = Int(buffer.pointee.mAudioDataBytesCapacity) & ~1
    let pcm = ChordharpFill(bytes / 2) ?? Data()
    let out = buffer.pointee.mAudioData.assumingMemoryBound(to: UInt8.self)
    let copied = min(pcm.count, bytes)
    pcm.copyBytes(to: out, count: copied)
    if copied < bytes { (out + copied).initialize(repeating: 0, count: bytes - copied) }
    buffer.pointee.mAudioDataByteSize = UInt32(bytes)
    AudioQueueEnqueueBuffer(queue, buffer, 0, nil)
}

private func rgb(_ value: UInt32) -> UIColor {
    UIColor(red: CGFloat((value >> 16) & 0xff) / 255, green: CGFloat((value >> 8) & 0xff) / 255, blue: CGFloat(value & 0xff) / 255, alpha: 1)
}

/// Mixes amount of color into base, like ChordHarpView.java's blend.
private func blend(_ color: UIColor, _ base: UIColor, _ amount: CGFloat) -> UIColor {
    var (r1, g1, b1, a1): (CGFloat, CGFloat, CGFloat, CGFloat) = (0, 0, 0, 0)
    var (r0, g0, b0, a0): (CGFloat, CGFloat, CGFloat, CGFloat) = (0, 0, 0, 0)
    color.getRed(&r1, green: &g1, blue: &b1, alpha: &a1)
    base.getRed(&r0, green: &g0, blue: &b0, alpha: &a0)
    let t = min(max(amount, 0), 1)
    return UIColor(red: r0 + (r1 - r0) * t, green: g0 + (g1 - g0) * t, blue: b0 + (b1 - b0) * t, alpha: 1)
}
#endif
