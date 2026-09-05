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

bool ponderOverlayPointerPosition(double *x, double *y) {
    if (x == NULL || y == NULL) {
        return false;
    }
    CGEventRef event = CGEventCreate(NULL);
    if (event == NULL) {
        return false;
    }
    CGPoint location = CGEventGetLocation(event);
    CFRelease(event);
    *x = location.x;
    *y = location.y;
    return true;
}
