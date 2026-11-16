//go:build darwin && cgo

#import <Cocoa/Cocoa.h>
#include <stdbool.h>

// Called synchronously on the main thread. NSData copies the Go-owned bytes
// before this function returns; no asynchronous block retains a Go pointer.
bool ponderSetAppIcon(const void *bytes, int length) {
    @autoreleasepool {
        NSImage *image = [[NSImage alloc] initWithData:[NSData dataWithBytes:bytes length:length]];
        if (!image || !image.valid) return false;
        NSApp.applicationIconImage = image;
        return true;
    }
}
