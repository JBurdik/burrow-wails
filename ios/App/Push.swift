import UIKit
import UserNotifications

/// APNs (src-wails/apns.go): the core pushes the same notices as Web Push with
/// the app closed. The token rides `push_subscribe_apns` on every socket open
/// (an upsert, so a changed token or a re-pair is covered).
final class PushDelegate: NSObject, UIApplicationDelegate, UNUserNotificationCenterDelegate {
    /// Set by BurrowApp once the scene is up; a tap that launched the app waits here.
    weak var model: AppModel? { didSet { if let c = pendingChat { model?.openChat = c; pendingChat = nil } } }
    private var pendingChat: Int?

    func application(_ app: UIApplication, didFinishLaunchingWithOptions _: [UIApplication.LaunchOptionsKey: Any]? = nil) -> Bool {
        UNUserNotificationCenter.current().delegate = self
        return true
    }

    /// Off until the Apple portal side exists (APNs key, explicit App ID with
    /// Push, its profile — see release.sh APP_PROFILE). Off = no permission
    /// prompt, no token; the core's APNs stays off anyway without apns.p8.
    static let enabled = false

    /// Asks once (iOS remembers the answer), then registers on every launch.
    static func register() {
        guard enabled else { return }
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge]) { granted, _ in
            if granted { DispatchQueue.main.async { UIApplication.shared.registerForRemoteNotifications() } }
        }
    }

    func application(_: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken token: Data) {
        model?.apnsToken = token.map { String(format: "%02x", $0) }.joined()
    }

    func application(_: UIApplication, didFailToRegisterForRemoteNotificationsWithError _: Error) {}

    /// Foreground: no banner for the thread already on screen.
    func userNotificationCenter(_: UNUserNotificationCenter, willPresent n: UNNotification,
                                withCompletionHandler done: @escaping (UNNotificationPresentationOptions) -> Void) {
        let chat = Self.chatId(n.request.content.userInfo)
        done(chat != nil && chat == model?.viewing ? [] : [.banner, .list, .sound])
    }

    func userNotificationCenter(_: UNUserNotificationCenter, didReceive r: UNNotificationResponse,
                                withCompletionHandler done: @escaping () -> Void) {
        if let id = Self.chatId(r.notification.request.content.userInfo) {
            if let model { model.openChat = id } else { pendingChat = id }
        }
        done()
    }

    /// "#/ws/3?chat=7" → 7 (a PTY tab's route has none: the tap just opens the app).
    static func chatId(_ info: [AnyHashable: Any]) -> Int? {
        guard let url = info["url"] as? String, let r = url.range(of: #"chat=\d+"#, options: .regularExpression) else { return nil }
        return Int(url[r].dropFirst(5))
    }

    /// Development-signed builds (release.sh) get sandbox tokens: read the
    /// embedded profile's aps-environment. No profile (App Store) = production.
    static let sandbox: Bool = {
        guard let url = Bundle.main.url(forResource: "embedded", withExtension: "mobileprovision"),
              let d = try? Data(contentsOf: url), let s = String(data: d, encoding: .isoLatin1),
              let r = s.range(of: "<key>aps-environment</key>") else { return false }
        return s[r.upperBound...].prefix(80).contains("development")
    }()
}
