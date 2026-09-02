#ifndef PONDER_OVERLAY_NATIVE_DARWIN_H
#define PONDER_OVERLAY_NATIVE_DARWIN_H

#include <stdbool.h>
#include <stdint.h>

bool ponderShowOverlayWindowInactive(
    void *window,
    int64_t *windowLevel,
    uint64_t *collectionBehavior
);
void ponderHideOverlayWindow(void *window);
bool ponderOverlayPointerPosition(double *x, double *y);

#endif
