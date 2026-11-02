#import <Cocoa/Cocoa.h>

extern void burrowMenuCheckUpdates(void);

@interface BurrowMenuTarget : NSObject
@end
@implementation BurrowMenuTarget
- (void)checkUpdates:(id)sender { burrowMenuCheckUpdates(); }
@end

// Wails' AppMenuRole builds the app menu natively and takes no custom items,
// so "Check for Updates…" is inserted into it after the fact (below About when
// there is one). Idempotent: OnDomReady fires again on a dev reload.
void burrowInstallAppMenuItems(void) {
  dispatch_async(dispatch_get_main_queue(), ^{
    static BurrowMenuTarget *target;
    if (!target) target = [BurrowMenuTarget new];
    NSMenu *menu = [[[NSApp mainMenu] itemAtIndex:0] submenu];
    if (!menu) return;
    for (NSMenuItem *it in menu.itemArray)
      if (it.action == @selector(checkUpdates:)) return;
    NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:@"Check for Updates…"
                                                  action:@selector(checkUpdates:)
                                           keyEquivalent:@""];
    item.target = target;
    NSInteger at = [menu.itemArray.firstObject.title hasPrefix:@"About"] ? 1 : 0;
    [menu insertItem:item atIndex:at];
    if (at == 0) [menu insertItem:[NSMenuItem separatorItem] atIndex:1];
  });
}
