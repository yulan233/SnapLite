// SnapLite macOS adapter (ARC). Everything that touches AppKit runs on the
// main thread: the Go side only calls those functions from the event-loop
// thread. Screen capture blocks on a semaphore and is only called from
// goroutines that are not the main thread.

#import <Cocoa/Cocoa.h>
#import <Carbon/Carbon.h>
#import <CoreGraphics/CoreGraphics.h>
#import <ApplicationServices/ApplicationServices.h>
#import <ScreenCaptureKit/ScreenCaptureKit.h>
#import <ServiceManagement/ServiceManagement.h>
#import <objc/runtime.h>
#include <unistd.h>
#include "darwin.h"

extern void goDrainMain(void);
extern void goHotkey(uint32_t id);
extern void goClipboardChanged(void);

#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"

// ---- lifecycle -------------------------------------------------------------

void sl_init(void) {
    // Viva sets the Regular policy when the application begins; a tray tool
    // has no Dock icon.
    [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
    sl_prefetch_content();
}

void sl_wake_main(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        goDrainMain();
    });
}

void sl_activate(void) {
    [NSApp activateIgnoringOtherApps:YES];
    if (@available(macOS 14.0, *)) {
        [NSApp activate];
    }
}

// ---- displays --------------------------------------------------------------

static double sl_display_scale(CGDirectDisplayID id) {
    CGDisplayModeRef mode = CGDisplayCopyDisplayMode(id);
    if (mode == NULL) {
        return 1;
    }
    double points = (double)CGDisplayModeGetWidth(mode);
    double pixels = (double)CGDisplayModeGetPixelWidth(mode);
    CGDisplayModeRelease(mode);
    if (points <= 0 || pixels <= 0) {
        return 1;
    }
    return pixels / points;
}

int sl_displays(sl_display *out, int max) {
    CGDirectDisplayID ids[32];
    uint32_t count = 0;
    if (CGGetActiveDisplayList(32, ids, &count) != kCGErrorSuccess) {
        return -1;
    }
    CGDirectDisplayID primary = CGMainDisplayID();
    int n = 0;
    for (uint32_t i = 0; i < count && n < max; i++) {
        if (CGDisplayIsInMirrorSet(ids[i]) && CGDisplayMirrorsDisplay(ids[i]) != kCGNullDirectDisplay) {
            continue; // a mirror shows another display's content
        }
        CGRect b = CGDisplayBounds(ids[i]);
        out[n].id = ids[i];
        out[n].x = b.origin.x;
        out[n].y = b.origin.y;
        out[n].w = b.size.width;
        out[n].h = b.size.height;
        out[n].scale = sl_display_scale(ids[i]);
        out[n].primary = ids[i] == primary;
        n++;
    }
    return n;
}

// ---- capture ---------------------------------------------------------------

static SCShareableContent *sl_content;

static SCShareableContent *sl_fetch_content(NSError **error) {
    dispatch_semaphore_t done = dispatch_semaphore_create(0);
    __block SCShareableContent *result = nil;
    __block NSError *failure = nil;
    [SCShareableContent getShareableContentExcludingDesktopWindows:NO
                                               onScreenWindowsOnly:YES
                                                 completionHandler:^(SCShareableContent *content, NSError *err) {
        result = content;
        failure = err;
        dispatch_semaphore_signal(done);
    }];
    if (dispatch_semaphore_wait(done, dispatch_time(DISPATCH_TIME_NOW, 5 * NSEC_PER_SEC)) != 0) {
        return nil;
    }
    if (error != NULL) {
        *error = failure;
    }
    if (result != nil) {
        @synchronized([SCShareableContent class]) {
            sl_content = result;
        }
    }
    return result;
}

void sl_prefetch_content(void) {
    if (!CGPreflightScreenCaptureAccess()) {
        return;
    }
    dispatch_async(dispatch_get_global_queue(QOS_CLASS_UTILITY, 0), ^{
        sl_fetch_content(NULL);
    });
}

static SCDisplay *sl_find_display(SCShareableContent *content, uint32_t id) {
    for (SCDisplay *display in content.displays) {
        if (display.displayID == id) {
            return display;
        }
    }
    return nil;
}

static SCRunningApplication *sl_self_app(SCShareableContent *content) {
    pid_t me = getpid();
    for (SCRunningApplication *application in content.applications) {
        if (application.processID == me) {
            return application;
        }
    }
    return nil;
}

// sl_capture_failure logs a ScreenCaptureKit error and maps it: 1 means the
// screen-recording permission is missing (SCStreamErrorUserDeclined, or any
// failure while the system reports no permission), 2 any other failure.
static int sl_capture_failure(NSString *step, NSError *error) {
    NSLog(@"snaplite: %@ failed: %@", step, error);
    if (error != nil && error.code == -3801) {
        return 1;
    }
    return CGPreflightScreenCaptureAccess() ? 2 : 1;
}

int sl_capture(uint32_t id, uint8_t **pix, int *width, int *height, int *stride) {
    @autoreleasepool {
        *pix = NULL;
        // No CGPreflightScreenCaptureAccess gate here: it caches its answer for
        // the life of the process, so a permission granted while SnapLite runs
        // stayed "denied" until a restart. ScreenCaptureKit itself is asked
        // instead, and sl_capture_failure decides what its error means.
        SCShareableContent *content = nil;
        @synchronized([SCShareableContent class]) {
            content = sl_content;
        }
        SCDisplay *display = content ? sl_find_display(content, id) : nil;
        if (display == nil) {
            NSError *error = nil;
            content = sl_fetch_content(&error);
            if (content == nil) {
                return sl_capture_failure(@"SCShareableContent", error);
            }
            display = sl_find_display(content, id);
            if (display == nil) {
                return 2;
            }
        }
        SCRunningApplication *me = sl_self_app(content);
        NSArray *excluded = me != nil ? @[ me ] : @[];
        SCContentFilter *filter = [[SCContentFilter alloc] initWithDisplay:display
                                                     excludingApplications:excluded
                                                          exceptingWindows:@[]];
        double scale = sl_display_scale(id);
        SCStreamConfiguration *config = [[SCStreamConfiguration alloc] init];
        config.width = (size_t)llround(display.width * scale);
        config.height = (size_t)llround(display.height * scale);
        config.showsCursor = NO;
        config.pixelFormat = kCVPixelFormatType_32BGRA;
        config.captureResolution = SCCaptureResolutionBest;
        config.scalesToFit = NO;

        dispatch_semaphore_t done = dispatch_semaphore_create(0);
        __block CGImageRef image = NULL;
        __block NSError *failure = nil;
        [SCScreenshotManager captureImageWithFilter:filter
                                      configuration:config
                                  completionHandler:^(CGImageRef captured, NSError *err) {
            if (captured != NULL) {
                image = CGImageRetain(captured);
            }
            failure = err;
            dispatch_semaphore_signal(done);
        }];
        if (dispatch_semaphore_wait(done, dispatch_time(DISPATCH_TIME_NOW, 5 * NSEC_PER_SEC)) != 0) {
            return 2;
        }
        if (image == NULL) {
            return sl_capture_failure(@"SCScreenshotManager", failure);
        }
        size_t w = CGImageGetWidth(image);
        size_t h = CGImageGetHeight(image);
        size_t rowBytes = w * 4;
        uint8_t *buffer = malloc(rowBytes * h);
        if (buffer == NULL) {
            CGImageRelease(image);
            return 2;
        }
        CGColorSpaceRef space = CGColorSpaceCreateWithName(kCGColorSpaceSRGB);
        CGContextRef context = CGBitmapContextCreate(buffer, w, h, 8, rowBytes, space,
            kCGImageAlphaPremultipliedLast | kCGBitmapByteOrder32Big);
        CGColorSpaceRelease(space);
        if (context == NULL) {
            free(buffer);
            CGImageRelease(image);
            return 2;
        }
        CGContextDrawImage(context, CGRectMake(0, 0, w, h), image);
        CGContextRelease(context);
        CGImageRelease(image);
        *pix = buffer;
        *width = (int)w;
        *height = (int)h;
        *stride = (int)rowBytes;
        return 0;
    }
}

// ---- permissions and system settings ---------------------------------------

int sl_has_screen_permission(void) { return CGPreflightScreenCaptureAccess() ? 1 : 0; }
void sl_request_screen_permission(void) { CGRequestScreenCaptureAccess(); }

int sl_has_accessibility(void) {
    NSDictionary *options = @{(__bridge NSString *)kAXTrustedCheckOptionPrompt : @NO};
    return AXIsProcessTrustedWithOptions((__bridge CFDictionaryRef)options) ? 1 : 0;
}

void sl_request_accessibility(void) {
    NSDictionary *options = @{(__bridge NSString *)kAXTrustedCheckOptionPrompt : @YES};
    AXIsProcessTrustedWithOptions((__bridge CFDictionaryRef)options);
}

void sl_open_url(const char *url) {
    NSString *value = [NSString stringWithUTF8String:url];
    NSURL *target = value ? [NSURL URLWithString:value] : nil;
    if (target != nil) {
        [[NSWorkspace sharedWorkspace] openURL:target];
    }
}

void sl_reveal(const char *path) {
    NSString *value = [NSString stringWithUTF8String:path];
    if (value != nil) {
        [[NSWorkspace sharedWorkspace] openURL:[NSURL fileURLWithPath:value isDirectory:YES]];
    }
}

// ---- windows ---------------------------------------------------------------

static NSWindow *sl_find_window(const char *title) {
    NSString *name = [NSString stringWithUTF8String:title];
    for (NSWindow *window in NSApp.windows) {
        if ([window.title isEqualToString:name]) {
            return window;
        }
    }
    return nil;
}

// A borderless NSWindow refuses to become the key window, so an overlay would
// get no keys. Viva creates the window, so instead of subclassing, the
// canBecomeKeyWindow implementation of NSWindow is wrapped once: windows that
// carry the association answer YES, every other window answers as before.
static char sl_keyable;
static BOOL (*sl_can_become_key_original)(id, SEL);

static BOOL sl_can_become_key(id self, SEL command) {
    if (objc_getAssociatedObject(self, &sl_keyable) != nil) {
        return YES;
    }
    return sl_can_become_key_original(self, command);
}

static void sl_install_keyable(void) {
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        Method method = class_getInstanceMethod([NSWindow class], @selector(canBecomeKeyWindow));
        sl_can_become_key_original = (BOOL (*)(id, SEL))method_getImplementation(method);
        method_setImplementation(method, (IMP)sl_can_become_key);
    });
}

int sl_raise_overlay(const char *title, double x, double y, double w, double h) {
    NSWindow *window = sl_find_window(title);
    if (window == nil) {
        return 1;
    }
    sl_install_keyable();
    objc_setAssociatedObject(window, &sl_keyable, @YES, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
    window.level = NSScreenSaverWindowLevel;
    window.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces |
                                NSWindowCollectionBehaviorFullScreenAuxiliary |
                                NSWindowCollectionBehaviorStationary |
                                NSWindowCollectionBehaviorIgnoresCycle;
    window.hasShadow = NO;
    window.movable = NO;
    CGFloat primaryHeight = CGDisplayBounds(CGMainDisplayID()).size.height;
    NSRect frame = NSMakeRect(x, primaryHeight - (y + h), w, h);
    [window setFrame:frame display:YES];
    sl_activate();
    [window makeKeyAndOrderFront:nil];
    [window orderFrontRegardless];
    return 0;
}

// sl_float_window keeps a window above other applications' windows without
// activating SnapLite, so the application in front keeps the keyboard. The
// window may become key when it is clicked (a borderless window cannot by
// default), so a pin can take Escape once the user has clicked it.
int sl_float_window(const char *title, double x, double y, double w, double h) {
    NSWindow *window = sl_find_window(title);
    if (window == nil) {
        return 1;
    }
    sl_install_keyable();
    objc_setAssociatedObject(window, &sl_keyable, @YES, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
    window.level = NSStatusWindowLevel;
    window.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces |
                                NSWindowCollectionBehaviorFullScreenAuxiliary |
                                NSWindowCollectionBehaviorIgnoresCycle;
    window.hidesOnDeactivate = NO;
    CGFloat primaryHeight = CGDisplayBounds(CGMainDisplayID()).size.height;
    [window setFrame:NSMakeRect(x, primaryHeight - (y + h), w, h) display:YES];
    [window orderFrontRegardless];
    return 0;
}

int sl_list_windows(sl_rect *out, int max, int exclude_own) {
    CFArrayRef list = CGWindowListCopyWindowInfo(
        kCGWindowListOptionOnScreenOnly | kCGWindowListExcludeDesktopElements, kCGNullWindowID);
    if (list == NULL) {
        return -1;
    }
    pid_t me = getpid();
    int n = 0;
    NSArray *windows = (__bridge_transfer NSArray *)list;
    for (NSDictionary *info in windows) {
        if (n >= max) {
            break;
        }
        if ([info[(__bridge NSString *)kCGWindowLayer] intValue] != 0) {
            continue;
        }
        if (exclude_own && [info[(__bridge NSString *)kCGWindowOwnerPID] intValue] == me) {
            continue;
        }
        NSNumber *alpha = info[(__bridge NSString *)kCGWindowAlpha];
        if (alpha != nil && alpha.doubleValue <= 0) {
            continue;
        }
        CGRect bounds;
        NSDictionary *rect = info[(__bridge NSString *)kCGWindowBounds];
        if (rect == nil || !CGRectMakeWithDictionaryRepresentation((__bridge CFDictionaryRef)rect, &bounds)) {
            continue;
        }
        if (bounds.size.width < 40 || bounds.size.height < 40) {
            continue;
        }
        out[n].x = bounds.origin.x;
        out[n].y = bounds.origin.y;
        out[n].w = bounds.size.width;
        out[n].h = bounds.size.height;
        n++;
    }
    return n;
}

// ---- hotkeys (Carbon; dispatched on the main thread) -----------------------

static const OSType sl_signature = 'SNPL';
static NSMutableDictionary<NSNumber *, NSValue *> *sl_hotkey_refs;

static OSStatus sl_hotkey_handler(EventHandlerCallRef next, EventRef event, void *data) {
    (void)next;
    (void)data;
    EventHotKeyID key;
    if (GetEventParameter(event, kEventParamDirectObject, typeEventHotKeyID, NULL, sizeof(key), NULL, &key) != noErr) {
        return eventNotHandledErr;
    }
    if (key.signature != sl_signature) {
        return eventNotHandledErr;
    }
    goHotkey(key.id);
    return noErr;
}

int sl_register_hotkey(uint32_t id, uint32_t keycode, uint32_t modifiers) {
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        sl_hotkey_refs = [NSMutableDictionary dictionary];
        EventTypeSpec spec = {kEventClassKeyboard, kEventHotKeyPressed};
        InstallApplicationEventHandler(&sl_hotkey_handler, 1, &spec, NULL, NULL);
    });
    EventHotKeyID key = {sl_signature, id};
    EventHotKeyRef ref = NULL;
    OSStatus status = RegisterEventHotKey(keycode, modifiers, key, GetApplicationEventTarget(), 0, &ref);
    if (status != noErr) {
        return (int)status;
    }
    sl_hotkey_refs[@(id)] = [NSValue valueWithPointer:ref];
    return 0;
}

void sl_unregister_hotkey(uint32_t id) {
    NSValue *ref = sl_hotkey_refs[@(id)];
    if (ref != nil) {
        UnregisterEventHotKey((EventHotKeyRef)ref.pointerValue);
        [sl_hotkey_refs removeObjectForKey:@(id)];
    }
}

// ---- clipboard -------------------------------------------------------------

int sl_set_clipboard_image(const void *png, size_t length) {
    @autoreleasepool {
        NSData *data = [NSData dataWithBytes:png length:length];
        NSBitmapImageRep *rep = [NSBitmapImageRep imageRepWithData:data];
        NSData *tiff = rep.TIFFRepresentation;
        NSPasteboard *board = [NSPasteboard generalPasteboard];
        [board clearContents];
        NSPasteboardItem *item = [[NSPasteboardItem alloc] init];
        [item setData:data forType:NSPasteboardTypePNG];
        if (tiff != nil) {
            [item setData:tiff forType:NSPasteboardTypeTIFF];
        }
        return [board writeObjects:@[ item ]] ? 0 : 1;
    }
}

long sl_change_count(void) { return (long)[NSPasteboard generalPasteboard].changeCount; }

// macOS has no clipboard-change notification. This 800 ms timer is the one
// standing timer SnapLite keeps (docs/04 §2.6); its tolerance lets the system
// coalesce the wake-ups, and it calls into Go only when the count changed.
static NSTimer *sl_clipboard_timer;
static NSInteger sl_clipboard_seen;

void sl_watch_clipboard(int on) {
    [sl_clipboard_timer invalidate];
    sl_clipboard_timer = nil;
    if (!on) {
        return;
    }
    sl_clipboard_seen = [NSPasteboard generalPasteboard].changeCount;
    sl_clipboard_timer = [NSTimer timerWithTimeInterval:0.8 repeats:YES block:^(NSTimer *timer) {
        (void)timer;
        NSInteger count = [NSPasteboard generalPasteboard].changeCount;
        if (count != sl_clipboard_seen) {
            sl_clipboard_seen = count;
            goClipboardChanged();
        }
    }];
    sl_clipboard_timer.tolerance = 0.4;
    [[NSRunLoop mainRunLoop] addTimer:sl_clipboard_timer forMode:NSRunLoopCommonModes];
}

int sl_clipboard_concealed(void) {
    NSArray<NSPasteboardType> *types = [NSPasteboard generalPasteboard].types;
    for (NSString *marker in @[ @"org.nspasteboard.ConcealedType", @"org.nspasteboard.TransientType",
                                @"org.nspasteboard.AutoGeneratedType" ]) {
        if ([types containsObject:marker]) {
            return 1;
        }
    }
    return 0;
}

// ---- pointer position -------------------------------------------------------

void sl_cursor_position(double *x, double *y) {
    CGEventRef event = CGEventCreate(NULL);
    CGPoint point = event ? CGEventGetLocation(event) : CGPointZero;
    if (event) {
        CFRelease(event);
    }
    *x = point.x;
    *y = point.y;
}

// ---- launch at login -------------------------------------------------------

static BOOL sl_bundled(void) {
    return [[NSBundle mainBundle].bundlePath hasSuffix:@".app"] && [NSBundle mainBundle].bundleIdentifier != nil;
}

int sl_launch_at_login(void) {
    if (!sl_bundled()) {
        return -1;
    }
    return SMAppService.mainAppService.status == SMAppServiceStatusEnabled ? 1 : 0;
}

int sl_set_launch_at_login(int enabled) {
    if (!sl_bundled()) {
        return -1;
    }
    NSError *error = nil;
    BOOL ok = enabled ? [SMAppService.mainAppService registerAndReturnError:&error]
                      : [SMAppService.mainAppService unregisterAndReturnError:&error];
    return ok ? 0 : 1;
}

#pragma clang diagnostic pop

// ---- quick paste -------------------------------------------------------------

static NSRunningApplication *sl_saved_app;

// sl_save_front_app remembers the application in front, unless it is SnapLite,
// so a paste can go back to it after the history panel closes.
void sl_save_front_app(void) {
    NSRunningApplication *front = NSWorkspace.sharedWorkspace.frontmostApplication;
    if (front != nil && front.processIdentifier != getpid()) {
        sl_saved_app = front;
    }
}

// sl_paste_to_saved_app hands the focus back to the remembered application and,
// once it is in front, presses Command+V there. Posting keys needs the
// Accessibility permission: 1 without it.
int sl_paste_to_saved_app(void) {
    if (!AXIsProcessTrusted()) {
        return 1;
    }
    [NSApp hide:nil];
    NSRunningApplication *target = sl_saved_app;
    if (target != nil) {
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
        [target activateWithOptions:0];
#pragma clang diagnostic pop
    }
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 150 * NSEC_PER_MSEC), dispatch_get_main_queue(), ^{
        CGEventSourceRef source = CGEventSourceCreate(kCGEventSourceStateCombinedSessionState);
        CGEventRef down = CGEventCreateKeyboardEvent(source, (CGKeyCode)kVK_ANSI_V, true);
        CGEventRef up = CGEventCreateKeyboardEvent(source, (CGKeyCode)kVK_ANSI_V, false);
        CGEventSetFlags(down, kCGEventFlagMaskCommand);
        CGEventSetFlags(up, kCGEventFlagMaskCommand);
        CGEventPost(kCGHIDEventTap, down);
        CGEventPost(kCGHIDEventTap, up);
        CFRelease(down);
        CFRelease(up);
        if (source != NULL) {
            CFRelease(source);
        }
    });
    return 0;
}
