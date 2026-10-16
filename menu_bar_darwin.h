#ifndef PONDER_MENU_BAR_DARWIN_H
#define PONDER_MENU_BAR_DARWIN_H

#include <stdbool.h>

// Main thread only.
void ponderMenuBarCreate(
    const void *onIconPNG,
    int onIconLength,
    const void *offIconPNG,
    int offIconLength,
    const char *title,
    const char *openLabel,
    const char *quitLabel
);
void ponderMenuBarRender(
    const char *status,
    const char *activity,
    bool on,
    bool canToggle,
    const char *indicator,
    const char *tooltip
);

#endif
