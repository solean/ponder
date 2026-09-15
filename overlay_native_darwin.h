#ifndef PONDER_OVERLAY_NATIVE_DARWIN_H
#define PONDER_OVERLAY_NATIVE_DARWIN_H

#include <stdbool.h>
#include <stdint.h>

bool ponderOverlayTargetRunning(const char *targetBundleID);
bool ponderOverlayAttach(
    void *window,
    const char *targetBundleID,
    int64_t *windowLevel,
    uint64_t *collectionBehavior
);
bool ponderOverlaySync(void);
void ponderOverlayDetach(void *window);
bool ponderOverlayPointerPosition(void *window, double *x, double *y);

#endif
