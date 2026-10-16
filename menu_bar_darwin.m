#import <Cocoa/Cocoa.h>

#import "_cgo_export.h"
#import "menu_bar_darwin.h"

// The dropdown opens with a Control Center style header: "Live Tracking" over
// the current status, plus a switch that starts or stops the tracker. Every
// entry point runs on the main thread (Go calls through application.InvokeSync;
// AppKit delivers actions there), so this state needs no locking.

static const CGFloat ponderHeaderWidth = 280;
static const CGFloat ponderHeaderInset = 14;
static const CGFloat ponderHeaderPadding = 6;
static const CGFloat ponderHeaderSwitchGap = 12;

@interface PonderMenuBarHeaderView : NSView
@property(nonatomic, strong) NSTextField *titleLabel;
@property(nonatomic, strong) NSTextField *statusLabel;
@property(nonatomic, strong) NSTextField *activityLabel;
@property(nonatomic, strong) NSSwitch *toggle;
@end

@implementation PonderMenuBarHeaderView

- (instancetype)initWithTitle:(NSString *)title {
    self = [super initWithFrame:NSMakeRect(0, 0, ponderHeaderWidth, 0)];
    if (self == nil) {
        return nil;
    }
    _titleLabel = [self addLabelWithFont:[NSFont systemFontOfSize:13 weight:NSFontWeightSemibold]
                                   color:[NSColor labelColor]];
    _titleLabel.stringValue = title;
    _statusLabel = [self addLabelWithFont:[NSFont systemFontOfSize:12]
                                    color:[NSColor secondaryLabelColor]];
    _activityLabel = [self addLabelWithFont:[NSFont systemFontOfSize:12]
                                      color:[NSColor secondaryLabelColor]];
    _toggle = [[NSSwitch alloc] init];
    _toggle.target = self;
    _toggle.action = @selector(toggled:);
    _toggle.enabled = NO;
    _toggle.accessibilityLabel = title;
    [self addSubview:_toggle];
    [self layoutContent];
    return self;
}

- (BOOL)isFlipped {
    return YES;
}

- (NSTextField *)addLabelWithFont:(NSFont *)font color:(NSColor *)color {
    NSTextField *label = [NSTextField labelWithString:@""];
    label.font = font;
    label.textColor = color;
    label.lineBreakMode = NSLineBreakByTruncatingTail;
    label.maximumNumberOfLines = 1;
    [self addSubview:label];
    return label;
}

- (void)toggled:(NSSwitch *)sender {
    ponderMenuBarDidToggle(sender.state == NSControlStateValueOn);
}

// Frames rather than Auto Layout: NSMenu sizes a custom item from its view's
// frame, so the height must be settled whenever a line appears or disappears.
- (void)layoutContent {
    NSSize switchSize = self.toggle.intrinsicContentSize;
    CGFloat textWidth = ponderHeaderWidth - 2 * ponderHeaderInset - switchSize.width - ponderHeaderSwitchGap;
    CGFloat y = ponderHeaderPadding;
    for (NSTextField *label in @[ self.titleLabel, self.statusLabel, self.activityLabel ]) {
        if (label.hidden) {
            continue;
        }
        CGFloat height = ceil(label.intrinsicContentSize.height);
        label.frame = NSMakeRect(ponderHeaderInset, y, textWidth, height);
        y += height;
    }
    CGFloat height = y + ponderHeaderPadding;
    [self setFrameSize:NSMakeSize(ponderHeaderWidth, height)];
    self.toggle.frame = NSMakeRect(
        ponderHeaderWidth - ponderHeaderInset - switchSize.width,
        round((height - switchSize.height) / 2),
        switchSize.width,
        switchSize.height
    );
}

@end

@interface PonderMenuBarController : NSObject
@property(nonatomic, strong) NSStatusItem *statusItem;
@property(nonatomic, strong) PonderMenuBarHeaderView *header;
@property(nonatomic, strong) NSImage *onIcon;  // Filled orbs: tracking.
@property(nonatomic, strong) NSImage *offIcon; // Hollow rings: stopped.
@end

@implementation PonderMenuBarController

- (void)open:(id)sender {
    ponderMenuBarDidOpen();
}

- (void)quit:(id)sender {
    ponderMenuBarDidQuit();
}

@end

static PonderMenuBarController *ponderMenuBar = nil;

// Template images let macOS tint the icon for light, dark, and highlighted
// menu bars.
static NSImage *ponderMenuBarImage(const void *png, int length) {
    NSImage *image = [[NSImage alloc] initWithData:[NSData dataWithBytes:png length:length]];
    CGFloat thickness = [[NSStatusBar systemStatusBar] thickness];
    image.size = NSMakeSize(thickness, thickness);
    image.template = YES;
    return image;
}

static NSString *ponderString(const char *value) {
    return value == NULL ? @"" : [NSString stringWithUTF8String:value];
}

void ponderMenuBarCreate(
    const void *onIconPNG,
    int onIconLength,
    const void *offIconPNG,
    int offIconLength,
    const char *title,
    const char *openLabel,
    const char *quitLabel
) {
    if (ponderMenuBar != nil) {
        return;
    }
    PonderMenuBarController *controller = [[PonderMenuBarController alloc] init];
    controller.statusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];
    controller.onIcon = ponderMenuBarImage(onIconPNG, onIconLength);
    controller.offIcon = ponderMenuBarImage(offIconPNG, offIconLength);
    controller.statusItem.button.image = controller.offIcon;
    controller.statusItem.button.imagePosition = NSImageOnly;

    NSMenu *menu = [[NSMenu alloc] init];
    menu.autoenablesItems = NO;
    menu.showsStateColumn = NO;

    controller.header = [[PonderMenuBarHeaderView alloc] initWithTitle:ponderString(title)];
    NSMenuItem *headerItem = [[NSMenuItem alloc] init];
    headerItem.view = controller.header;
    [menu addItem:headerItem];
    [menu addItem:[NSMenuItem separatorItem]];

    NSMenuItem *openItem = [[NSMenuItem alloc] initWithTitle:ponderString(openLabel)
                                                      action:@selector(open:)
                                               keyEquivalent:@""];
    openItem.target = controller;
    [menu addItem:openItem];
    [menu addItem:[NSMenuItem separatorItem]];

    NSMenuItem *quitItem = [[NSMenuItem alloc] initWithTitle:ponderString(quitLabel)
                                                      action:@selector(quit:)
                                               keyEquivalent:@"q"];
    quitItem.target = controller;
    [menu addItem:quitItem];

    controller.statusItem.menu = menu;
    ponderMenuBar = controller;
}

void ponderMenuBarRender(
    const char *status,
    const char *activity,
    bool on,
    bool canToggle,
    const char *indicator,
    const char *tooltip
) {
    if (ponderMenuBar == nil) {
        return;
    }
    PonderMenuBarHeaderView *header = ponderMenuBar.header;
    header.statusLabel.stringValue = ponderString(status);
    NSString *activityText = ponderString(activity);
    header.activityLabel.stringValue = activityText;
    header.activityLabel.hidden = activityText.length == 0;
    header.toggle.state = on ? NSControlStateValueOn : NSControlStateValueOff;
    header.toggle.enabled = canToggle;
    [header layoutContent];

    NSString *tooltipText = ponderString(tooltip);
    header.toolTip = tooltipText;

    NSStatusBarButton *button = ponderMenuBar.statusItem.button;
    NSImage *icon = on ? ponderMenuBar.onIcon : ponderMenuBar.offIcon;
    if (button.image != icon) {
        button.image = icon;
    }
    NSString *indicatorText = ponderString(indicator);
    button.title = indicatorText;
    button.imagePosition = indicatorText.length == 0 ? NSImageOnly : NSImageLeading;
    button.toolTip = tooltipText;
}
