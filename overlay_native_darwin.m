#import <ApplicationServices/ApplicationServices.h>
#import <Cocoa/Cocoa.h>

#import "overlay_native_darwin.h"

static NSApplicationActivationPolicy ponderPreviousActivationPolicy =
    NSApplicationActivationPolicyRegular;
static bool ponderOverlayUsesAccessoryPolicy = false;

static void ponderRestoreActivationPolicy(void) {
    if (!ponderOverlayUsesAccessoryPolicy) {
        return;
    }
    [NSApp setActivationPolicy:ponderPreviousActivationPolicy];
    ponderOverlayUsesAccessoryPolicy = false;
}

bool ponderShowOverlayWindowInactive(
    void *rawWindow,
    int64_t *windowLevel,
    uint64_t *collectionBehavior
) {
    if (rawWindow == NULL) {
        return false;
    }
    if (!ponderOverlayUsesAccessoryPolicy) {
        ponderPreviousActivationPolicy = [NSApp activationPolicy];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
        ponderOverlayUsesAccessoryPolicy = true;
    }
    NSWindow *window = (__bridge NSWindow *)rawWindow;
    // Wails SetBounds positions using the old height before resizing. Set the
    // complete AppKit frame at once so the first show cannot start off-screen.
    NSScreen *screen = [[NSScreen screens] firstObject];
    if (screen == nil) {
        ponderRestoreActivationPolicy();
        return false;
    }
    [window setFrame:[screen frame] display:YES animate:NO];
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
    [window orderFrontRegardless];

    if (windowLevel != NULL) {
        *windowLevel = (int64_t)[window level];
    }
    if (collectionBehavior != NULL) {
        *collectionBehavior = (uint64_t)[window collectionBehavior];
    }
    bool configured =
        ([window collectionBehavior] & requiredBehavior) == requiredBehavior &&
        [window level] == overlayLevel &&
        [window isVisible] &&
        [NSApp activationPolicy] == NSApplicationActivationPolicyAccessory;
    if (!configured) {
        [window orderOut:nil];
        ponderRestoreActivationPolicy();
    }
    return configured;
}

void ponderHideOverlayWindow(void *rawWindow) {
    if (rawWindow != NULL) {
        NSWindow *window = (__bridge NSWindow *)rawWindow;
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
