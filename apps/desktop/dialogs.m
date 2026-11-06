#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#import <objc/runtime.h>

// Wails implements neither panel, so confirm() is always false and alert() shows
// nothing, and it opens no new windows, so links to other sites do nothing. These
// are added to its delegate class, which is internal to Wails: if a release
// renames it, the log below says so. If Wails adds a method itself,
// class_addMethod declines and Wails' own runs.

static NSString *okLabel = @"OK";
static NSString *cancelLabel = @"Cancel";

void setDialogLabels(const char *ok, const char *cancel) {
    okLabel = [NSString stringWithUTF8String:ok];
    cancelLabel = [NSString stringWithUTF8String:cancel];
}

char *preferredLanguage(void) {
    return strdup(NSLocale.preferredLanguages.firstObject.UTF8String ?: "en");
}

static void present(WKWebView *web, NSString *message, BOOL cancel, void (^done)(BOOL)) {
    NSAlert *alert = [NSAlert new];
    alert.messageText = message;
    [alert addButtonWithTitle:okLabel];
    if (cancel) {
        [alert addButtonWithTitle:cancelLabel];
    }
    [alert beginSheetModalForWindow:web.window completionHandler:^(NSModalResponse r) {
        done(r == NSAlertFirstButtonReturn);
    }];
}

static void confirmPanel(id self, SEL _cmd, WKWebView *web, NSString *message, WKFrameInfo *frame, void (^handler)(BOOL)) {
    present(web, message, YES, ^(BOOL ok) { handler(ok); });
}

static void alertPanel(id self, SEL _cmd, WKWebView *web, NSString *message, WKFrameInfo *frame, void (^handler)(void)) {
    present(web, message, NO, ^(BOOL ok) { handler(); });
}

// openWindow hands a link that wants a new window to the browser. The schemes
// are limited because the link may come from a note, which AI can write too.
static WKWebView *openWindow(id self, SEL _cmd, WKWebView *web, WKWebViewConfiguration *config, WKNavigationAction *action, WKWindowFeatures *features) {
    NSURL *url = action.request.URL;
    if ([@[ @"http", @"https", @"mailto" ] containsObject:url.scheme.lowercaseString]) {
        [NSWorkspace.sharedWorkspace openURL:url];
    }
    return nil;
}

char *loadSetting(const char *key) {
    NSString *value = [NSUserDefaults.standardUserDefaults stringForKey:[NSString stringWithUTF8String:key]];
    return strdup(value.UTF8String ?: "");
}

void saveSetting(const char *key, const char *value) {
    [NSUserDefaults.standardUserDefaults setObject:[NSString stringWithUTF8String:value] forKey:[NSString stringWithUTF8String:key]];
}

void installDelegate(void) {
    Class delegate = objc_getClass("WebviewWindowDelegate");
    if (delegate == nil) {
        NSLog(@"ebira: no WebviewWindowDelegate in this version of Wails: confirm, alert and links to other sites will not work");
        return;
    }
    class_addMethod(delegate, @selector(webView:runJavaScriptConfirmPanelWithMessage:initiatedByFrame:completionHandler:), (IMP)confirmPanel, "v@:@@@@?");
    class_addMethod(delegate, @selector(webView:runJavaScriptAlertPanelWithMessage:initiatedByFrame:completionHandler:), (IMP)alertPanel, "v@:@@@@?");
    class_addMethod(delegate, @selector(webView:createWebViewWithConfiguration:forNavigationAction:windowFeatures:), (IMP)openWindow, "@@:@@@@");
}

char *bundleString(const char *key) {
    id value = NSBundle.mainBundle.infoDictionary[[NSString stringWithUTF8String:key]];
    return strdup([value isKindOfClass:NSString.class] ? [value UTF8String] : "");
}

// showAbout shows the standard about panel, with the icon of the web UI and
// with credits under the version of the bundle.
void showAbout(const char *credits) {
    NSString *text = [NSString stringWithUTF8String:credits];
    dispatch_async(dispatch_get_main_queue(), ^{
        NSMutableParagraphStyle *centered = [NSMutableParagraphStyle new];
        centered.alignment = NSTextAlignmentCenter;
        NSDictionary *attrs = @{
            NSFontAttributeName : [NSFont systemFontOfSize:NSFont.smallSystemFontSize],
            NSForegroundColorAttributeName : NSColor.labelColor,
            NSParagraphStyleAttributeName : centered,
        };
        NSAttributedString *c = [[NSAttributedString alloc] initWithString:text attributes:attrs];
        NSMutableDictionary *options = [@{NSAboutPanelOptionCredits : c} mutableCopy];
        NSURL *url = [NSBundle.mainBundle URLForResource:@"favicon" withExtension:@"svg"];
        NSImage *icon = url ? [[NSImage alloc] initWithContentsOfURL:url] : nil;
        if (icon != nil) {
            icon.size = NSMakeSize(128, 128);
            options[NSAboutPanelOptionApplicationIcon] = icon;
        }
        [NSApp orderFrontStandardAboutPanelWithOptions:options];
        [NSApp activateIgnoringOtherApps:YES];
    });
}
