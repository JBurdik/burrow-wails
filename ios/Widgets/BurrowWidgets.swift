import ActivityKit
import SwiftUI
import WidgetKit

@main
struct BurrowWidgets: WidgetBundle {
    var body: some Widget {
        ThreadsWidget()
        ChatLiveActivity()
    }
}

// MARK: home / lock screen widget

struct SnapshotEntry: TimelineEntry {
    var date: Date
    var snap: WidgetSnapshot?
}

struct SnapshotProvider: TimelineProvider {
    func placeholder(in context: Context) -> SnapshotEntry {
        SnapshotEntry(date: .now, snap: WidgetSnapshot(waiting: 1, working: 2, top: ["Fix login", "Refactor"], at: .now))
    }
    func getSnapshot(in context: Context, completion: @escaping (SnapshotEntry) -> Void) {
        completion(SnapshotEntry(date: .now, snap: WidgetSnapshot.load()))
    }
    // ponytail: the app reloads timelines whenever phases change; the widget
    // itself never connects. Shows what the app last saw (timestamped).
    func getTimeline(in context: Context, completion: @escaping (Timeline<SnapshotEntry>) -> Void) {
        completion(Timeline(entries: [SnapshotEntry(date: .now, snap: WidgetSnapshot.load())], policy: .never))
    }
}

struct ThreadsWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "threads", provider: SnapshotProvider()) { entry in
            ThreadsWidgetView(entry: entry).containerBackground(.fill.tertiary, for: .widget)
        }
        .configurationDisplayName("Burrow threads")
        .description("Threads waiting for you and working.")
        .supportedFamilies([.systemSmall, .systemMedium, .accessoryCircular, .accessoryRectangular])
    }
}

struct ThreadsWidgetView: View {
    @Environment(\.widgetFamily) private var family
    let entry: SnapshotEntry

    var body: some View {
        let s = entry.snap
        switch family {
        case .accessoryCircular:
            VStack(spacing: 0) {
                Image(systemName: "hand.raised.fill").font(.caption)
                Text("\(s?.waiting ?? 0)").font(.title2.bold())
            }
        case .accessoryRectangular:
            VStack(alignment: .leading) {
                Text("\(s?.waiting ?? 0) need you").font(.headline)
                Text("\(s?.working ?? 0) working").font(.caption)
            }
        default:
            VStack(alignment: .leading, spacing: 6) {
                HStack {
                    count(s?.waiting ?? 0, "need you", .orange)
                    Spacer()
                    count(s?.working ?? 0, "working", .blue)
                }
                if family == .systemMedium {
                    ForEach(s?.top ?? [], id: \.self) { Text($0).font(.caption).lineLimit(1) }
                }
                Spacer(minLength: 0)
                if let at = s?.at {
                    Text(at, style: .relative).font(.caption2).foregroundStyle(.secondary)
                } else {
                    Text("Open Burrow to pair").font(.caption2).foregroundStyle(.secondary)
                }
            }
        }
    }

    private func count(_ n: Int, _ label: String, _ color: Color) -> some View {
        VStack(alignment: .leading) {
            Text("\(n)").font(.largeTitle.bold()).foregroundStyle(n > 0 ? color : .secondary)
            Text(label).font(.caption).foregroundStyle(.secondary)
        }
    }
}

// MARK: Live Activity

struct ChatLiveActivity: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: ChatActivityAttributes.self) { ctx in
            HStack(spacing: 12) {
                Image(systemName: ctx.state.symbol).font(.title2).foregroundStyle(tint(ctx.state.state))
                VStack(alignment: .leading, spacing: 2) {
                    Text(ctx.attributes.title).font(.headline).lineLimit(1)
                    Text(ctx.state.detail.isEmpty ? "\(ctx.state.label) · \(ctx.attributes.project)" : ctx.state.detail)
                        .font(.caption).foregroundStyle(.secondary).lineLimit(1)
                }
                Spacer()
                if ctx.state.inFlight {
                    Text(ctx.state.startedAt, style: .timer).font(.caption.monospacedDigit()).frame(width: 52)
                }
            }
            .padding()
            .widgetURL(URL(string: "burrow://chat/\(ctx.attributes.chatId)"))
        } dynamicIsland: { ctx in
            DynamicIsland {
                DynamicIslandExpandedRegion(.leading) {
                    Image(systemName: ctx.state.symbol).foregroundStyle(tint(ctx.state.state))
                }
                DynamicIslandExpandedRegion(.trailing) {
                    if ctx.state.inFlight { Text(ctx.state.startedAt, style: .timer).monospacedDigit().frame(width: 52) }
                }
                DynamicIslandExpandedRegion(.bottom) {
                    VStack(alignment: .leading) {
                        Text(ctx.attributes.title).font(.headline).lineLimit(1)
                        Text(ctx.state.detail.isEmpty ? ctx.state.label : ctx.state.detail).font(.caption).lineLimit(1)
                    }
                }
            } compactLeading: {
                Image(systemName: ctx.state.symbol).foregroundStyle(tint(ctx.state.state))
            } compactTrailing: {
                if ctx.state.inFlight { Text(ctx.state.startedAt, style: .timer).monospacedDigit().frame(width: 40) }
                else { Text(ctx.state.label).font(.caption2) }
            } minimal: {
                Image(systemName: ctx.state.symbol).foregroundStyle(tint(ctx.state.state))
            }
            .widgetURL(URL(string: "burrow://chat/\(ctx.attributes.chatId)"))
        }
    }

    private func tint(_ state: String) -> Color {
        switch state {
        case "running": .blue
        case "waiting_input", "waiting_approval": .orange
        case "done": .green
        case "failed": .red
        default: .secondary
        }
    }
}
