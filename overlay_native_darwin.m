#import <ApplicationServices/ApplicationServices.h>
#import <Cocoa/Cocoa.h>

#import "overlay_native_darwin.h"

// The overlay is a HUD for one foreign application, so it may only ever be on
// screen while that application is frontmost: a window at shielding level with
// CanJoinAllSpaces otherwise floats above every other app, including while
// Arena sits on a fullscreen Space somewhere else entirely.
//
// Focus changes are driven by NSWorkspace notifications rather than polling so
// the overlay disappears with the app switch instead of one poll later. Every
// entry point below runs on the main thread (Go calls through
// application.InvokeSync; notifications are delivered on the main queue), so
// this state needs no locking.
static NSApplicationActivationPolicy ponderPreviousActivationPolicy =
    NSApplicationActivationPolicyRegular;
static bool ponderOverlayUsesAccessoryPolicy = false;
static NSWindow *ponderOverlayWindow = nil;
static NSString *ponderOverlayTargetBundleID = nil;
static NSMutableArray *ponderWorkspaceObservers = nil;
static CGWindowLevel ponderOverlayLevel = 0;

static void ponderRestoreActivationPolicy(void) {
    if (!ponderOverlayUsesAccessoryPolicy) {
        return;
    }
    [NSApp setActivationPolicy:ponderPreviousActivationPolicy];
    ponderOverlayUsesAccessoryPolicy = false;
}

// ponderFrontmostTarget returns the tracked application only while it owns the
// active session. Any other frontmost app (including Ponder itself) yields nil.
static NSRunningApplication *ponderFrontmostTarget(void) {
    if (ponderOverlayTargetBundleID == nil) {
        return nil;
    }
    NSRunningApplication *frontmost = [[NSWorkspace sharedWorkspace] frontmostApplication];
    NSString *bundleID = [frontmost bundleIdentifier];
    if (bundleID == nil || ![bundleID isEqualToString:ponderOverlayTargetBundleID]) {
        return nil;
    }
    return frontmost;
}

// ponderTargetWindowFrame reports the AppKit frame of the target's largest
// normal window so the overlay lands on the display and Space the game
// occupies instead of the primary screen. CGWindowList exposes geometry to any
// process; only window titles and pixels require a capture permission.
static bool ponderTargetWindowFrame(pid_t pid, NSRect *frame) {
    CFArrayRef windows = CGWindowListCopyWindowInfo(
        kCGWindowListOptionOnScreenOnly | kCGWindowListExcludeDesktopElements,
        kCGNullWindowID
    );
    if (windows == NULL) {
        return false;
    }

    CGRect best = CGRectZero;
    CGFloat bestArea = 0;
    for (NSDictionary *info in (__bridge NSArray *)windows) {
        NSNumber *ownerPID = info[(__bridge NSString *)kCGWindowOwnerPID];
        NSNumber *layer = info[(__bridge NSString *)kCGWindowLayer];
        // Layer 0 is the normal window layer; anything else is a panel,
        // tooltip, or status item that must not define the overlay's frame.
        if (ownerPID == nil || [ownerPID intValue] != pid || [layer intValue] != 0) {
            continue;
        }
        CFDictionaryRef rawBounds =
            (__bridge CFDictionaryRef)info[(__bridge NSString *)kCGWindowBounds];
        CGRect bounds = CGRectZero;
        if (rawBounds == NULL || !CGRectMakeWithDictionaryRepresentation(rawBounds, &bounds)) {
            continue;
        }
        CGFloat area = bounds.size.width * bounds.size.height;
        if (area > bestArea) {
            bestArea = area;
            best = bounds;
        }
    }
    CFRelease(windows);

    if (bestArea <= 0) {
        return false;
    }
    // CGWindow bounds grow downward from the top-left of the primary display;
    // AppKit frames grow upward from its bottom-left.
    NSScreen *primary = [[NSScreen screens] firstObject];
    if (primary == nil) {
        return false;
    }
    *frame = NSMakeRect(
        best.origin.x,
        NSMaxY([primary frame]) - (best.origin.y + best.size.height),
        best.size.width,
        best.size.height
    );
    return true;
}

static void ponderOverlayApplyVisibility(void) {
    NSWindow *window = ponderOverlayWindow;
    if (window == nil) {
        return;
    }

    NSRunningApplication *target = ponderFrontmostTarget();
    if (target == nil) {
        if ([window isVisible]) {
            [window orderOut:nil];
        }
        return;
    }

    NSRect frame;
    if (!ponderTargetWindowFrame([target processIdentifier], &frame)) {
        // The game is frontmost but owns no listable window (loading, or
        // minimised): cover the active display rather than guessing.
        NSScreen *screen = [NSScreen mainScreen] ?: [[NSScreen screens] firstObject];
        if (screen == nil) {
            return;
        }
        frame = [screen frame];
    }
    if (!NSEqualRects(frame, [window frame])) {
        [window setFrame:frame display:YES animate:NO];
    }
    [window setLevel:ponderOverlayLevel];
    [window orderFrontRegardless];
}

static void ponderInstallWorkspaceObservers(void) {
    if (ponderWorkspaceObservers != nil) {
        return;
    }
    NSArray<NSString *> *names = @[
        NSWorkspaceDidActivateApplicationNotification,
        NSWorkspaceDidDeactivateApplicationNotification,
        NSWorkspaceDidTerminateApplicationNotification,
        NSWorkspaceActiveSpaceDidChangeNotification,
    ];
    NSNotificationCenter *center = [[NSWorkspace sharedWorkspace] notificationCenter];
    ponderWorkspaceObservers = [NSMutableArray arrayWithCapacity:[names count]];
    for (NSString *name in names) {
        id observer = [center addObserverForName:name
                                          object:nil
                                           queue:[NSOperationQueue mainQueue]
                                      usingBlock:^(NSNotification *notification) {
            (void)notification;
            ponderOverlayApplyVisibility();
        }];
        [ponderWorkspaceObservers addObject:observer];
    }
}

static void ponderRemoveWorkspaceObservers(void) {
    if (ponderWorkspaceObservers == nil) {
        return;
    }
    NSNotificationCenter *center = [[NSWorkspace sharedWorkspace] notificationCenter];
    for (id observer in ponderWorkspaceObservers) {
        [center removeObserver:observer];
    }
    ponderWorkspaceObservers = nil;
}

bool ponderOverlayTargetRunning(const char *rawTargetBundleID) {
    if (rawTargetBundleID == NULL) {
        return false;
    }
    NSString *bundleID = [NSString stringWithUTF8String:rawTargetBundleID];
    if ([bundleID length] == 0) {
        return false;
    }
    return [[NSRunningApplication runningApplicationsWithBundleIdentifier:bundleID] count] > 0;
}

bool ponderOverlayAttach(
    void *rawWindow,
    const char *rawTargetBundleID,
    int64_t *windowLevel,
    uint64_t *collectionBehavior
) {
    if (rawWindow == NULL || rawTargetBundleID == NULL) {
        return false;
    }
    NSString *targetBundleID = [NSString stringWithUTF8String:rawTargetBundleID];
    if ([targetBundleID length] == 0) {
        return false;
    }
    if (!ponderOverlayUsesAccessoryPolicy) {
        ponderPreviousActivationPolicy = [NSApp activationPolicy];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
        ponderOverlayUsesAccessoryPolicy = true;
    }

    NSWindow *window = (__bridge NSWindow *)rawWindow;
    NSWindowCollectionBehavior requiredBehavior =
        NSWindowCollectionBehaviorCanJoinAllSpaces |
        NSWindowCollectionBehaviorFullScreenAuxiliary;
    // All Spaces and FullScreenAuxiliary alone do not opt into another app's
    // fullscreen Space (or Stage Manager set).
    if (@available(macOS 13.0, *)) {
        requiredBehavior |= NSWindowCollectionBehaviorCanJoinAllApplications;
    }
    CGWindowLevel maximumLevel = CGWindowLevelForKey(kCGMaximumWindowLevelKey);
    CGWindowLevel shieldingLevel = CGShieldingWindowLevel();
    CGWindowLevel overlayLevel =
        shieldingLevel < maximumLevel ? shieldingLevel + 1 : maximumLevel;
    [window setCollectionBehavior:
        requiredBehavior |
        NSWindowCollectionBehaviorStationary |
        NSWindowCollectionBehaviorIgnoresCycle];
    [window setLevel:overlayLevel];
    [window setHidesOnDeactivate:NO];
    // Display-only even while passive cursor updates show card previews.
    [window setIgnoresMouseEvents:YES];

    ponderOverlayWindow = window;
    ponderOverlayTargetBundleID = targetBundleID;
    ponderOverlayLevel = overlayLevel;
    ponderInstallWorkspaceObservers();
    ponderOverlayApplyVisibility();

    if (windowLevel != NULL) {
        *windowLevel = (int64_t)[window level];
    }
    if (collectionBehavior != NULL) {
        *collectionBehavior = (uint64_t)[window collectionBehavior];
    }
    // Visibility is deliberately excluded: the window stays ordered out until
    // the target application is frontmost.
    bool configured =
        ([window collectionBehavior] & requiredBehavior) == requiredBehavior &&
        [window level] == overlayLevel &&
        [NSApp activationPolicy] == NSApplicationActivationPolicyAccessory;
    if (!configured) {
        ponderOverlayDetach(rawWindow);
    }
    return configured;
}

bool ponderOverlaySync(void) {
    if (ponderOverlayWindow == nil) {
        return false;
    }
    // Refreshes geometry for a game window that moved, changed display, or
    // entered fullscreen since the last call.
    ponderOverlayApplyVisibility();
    return [ponderOverlayWindow isVisible];
}

void ponderOverlayDetach(void *rawWindow) {
    NSWindow *window = rawWindow != NULL ? (__bridge NSWindow *)rawWindow : ponderOverlayWindow;
    ponderRemoveWorkspaceObservers();
    ponderOverlayWindow = nil;
    ponderOverlayTargetBundleID = nil;
    if (window != nil) {
        [window orderOut:nil];
    }
    ponderRestoreActivationPolicy();
}

bool ponderOverlayPointerPosition(void *rawWindow, double *x, double *y) {
    if (rawWindow == NULL || x == NULL || y == NULL) {
        return false;
    }
    NSWindow *window = (__bridge NSWindow *)rawWindow;
    NSView *view = [window contentView];
    NSRect bounds = [view bounds];
    if (![window isVisible] || NSIsEmptyRect(bounds)) {
        return false;
    }
    // Stay in AppKit points throughout; mixing global CG coordinates with a
    // separately sampled window frame can misalign passive hover hit-testing.
    NSPoint point = [view convertPoint:
        [window convertPointFromScreen:[NSEvent mouseLocation]] fromView:nil];
    if (!NSPointInRect(point, bounds)) {
        return false;
    }
    *x = (point.x - NSMinX(bounds)) / NSWidth(bounds);
    double relativeY = (point.y - NSMinY(bounds)) / NSHeight(bounds);
    *y = [view isFlipped] ? relativeY : 1.0 - relativeY;
    return true;
}
