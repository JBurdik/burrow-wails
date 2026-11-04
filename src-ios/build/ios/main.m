//go:build ios
// Bootstrap: the delegate comes from the Go archive (WailsAppDelegate).
//
// iOS 27 traps (SIGTRAP in UIApplicationEvaluateRuntimeIssueForNoSceneLifecycleAdoption)
// in an app that has not adopted the UIScene lifecycle, and Wails v3.0.0's
// delegate is window-based. BurrowSceneDelegate (named by
// UIApplicationSceneManifest in Info.plist) adopts it with no change to Wails:
// the delegate still builds its window in didFinishLaunching, which runs before
// a scene connects; this only attaches that window to the scene.
#import <UIKit/UIKit.h>
#import <WebKit/WebKit.h>
#import <objc/runtime.h>
#include <stdio.h>

@interface BurrowSceneDelegate : UIResponder <UIWindowSceneDelegate>
@property (strong, nonatomic) UIWindow *window;
@end

@implementation BurrowSceneDelegate
- (void)scene:(UIScene *)scene willConnectToSession:(UISceneSession *)session options:(UISceneConnectionOptions *)options {
    UIWindow *win = [UIApplication sharedApplication].delegate.window;
    if (!win || ![scene isKindOfClass:[UIWindowScene class]]) return;
    win.windowScene = (UIWindowScene *)scene;
    win.frame = ((UIWindowScene *)scene).coordinateSpace.bounds;
    self.window = win;
    [win makeKeyAndVisible];
}
@end

// Wails lays its WebView out inside the safe area (WailsViewController's
// viewDidLayoutSubviews), leaving black bars under the status bar and the home
// indicator. The page is built for viewport-fit=cover and pads itself with
// env(safe-area-inset-*), so after Wails' layout the WebView gets the whole view.
// ponytail: swizzle, not a Wails fork; drops Wails' native tab bar (unused here).
static IMP wailsLayout;
static void burrowLayout(UIViewController *self, SEL _cmd) {
    ((void (*)(id, SEL))wailsLayout)(self, _cmd);
    UIView *web = [self valueForKey:@"webView"];
    web.frame = self.view.bounds;
}
static void fillScreen(void) {
    Class vc = NSClassFromString(@"WailsViewController");
    Method m = vc ? class_getInstanceMethod(vc, @selector(viewDidLayoutSubviews)) : NULL;
    if (m) wailsLayout = method_setImplementation(m, (IMP)burrowLayout);
}

// iOS kills the WebContent process under memory pressure (switching projects
// remounts a workspace's chats); WKWebView then shows only the window's
// background — the "black screen". Wails' delegate doesn't handle it, so add
// the WKNavigationDelegate method: log it (Console.app, filter "burrow") and
// reload — the page reconnects and restores its route from the hash.
static void burrowWebDied(UIViewController *self, SEL _cmd, WKWebView *webView) {
    NSLog(@"burrow: WebContent process terminated, reloading");
    [webView reload];
}
static void reloadOnCrash(void) {
    Class vc = NSClassFromString(@"WailsViewController");
    if (vc) class_addMethod(vc, @selector(webViewWebContentProcessDidTerminate:), (IMP)burrowWebDied, "v@:@");
}

int main(int argc, char * argv[]) {
    @autoreleasepool {
        fillScreen();
        reloadOnCrash();
        setvbuf(stdout, NULL, _IONBF, 0);
        setvbuf(stderr, NULL, _IONBF, 0);
        // Start nothing else here: WailsAppDelegate starts the Go runtime from
        // didFinishLaunching, after UIKit has delivered the launch.
        return UIApplicationMain(argc, argv, nil, @"WailsAppDelegate");
    }
}
