//go:build darwin

// Remote control's way into macOS: Quartz events for input, and a Carbon
// hotkey for the key that takes control back. See inject_darwin.go.

#include <ApplicationServices/ApplicationServices.h>
#include <Carbon/Carbon.h>
#include <dispatch/dispatch.h>

extern void tdcRevokeKeyPressed(void);

int tdcTrusted(void) { return AXIsProcessTrusted() ? 1 : 0; }

void tdcMouse(int type, int button, double x, double y) {
  CGEventRef event = CGEventCreateMouseEvent(NULL, (CGEventType)type, CGPointMake(x, y), (CGMouseButton)button);
  if (!event) return;
  CGEventPost(kCGHIDEventTap, event);
  CFRelease(event);
}

void tdcWhere(double *x, double *y) {
  CGEventRef event = CGEventCreate(NULL);
  CGPoint point = event ? CGEventGetLocation(event) : CGPointZero;
  if (event) CFRelease(event);
  *x = point.x;
  *y = point.y;
}

void tdcWheel(int dy, int dx) {
  CGEventRef event = CGEventCreateScrollWheelEvent(NULL, kCGScrollEventUnitLine, 2, dy, dx);
  if (!event) return;
  CGEventPost(kCGHIDEventTap, event);
  CFRelease(event);
}

void tdcKey(int code, int down, unsigned long long flags) {
  CGEventRef event = CGEventCreateKeyboardEvent(NULL, (CGKeyCode)code, down ? true : false);
  if (!event) return;
  CGEventSetFlags(event, (CGEventFlags)flags);
  CGEventPost(kCGHIDEventTap, event);
  CFRelease(event);
}

static EventHotKeyRef tdcHotKey;
static EventHandlerRef tdcHandler;

static OSStatus tdcOnHotKey(EventHandlerCallRef next, EventRef event, void *data) {
  tdcRevokeKeyPressed();
  return noErr;
}

// Control-Option-Command-Escape, registered on the main thread, where Carbon
// delivers application events.
void tdcWatch(void) {
  dispatch_async(dispatch_get_main_queue(), ^{
    EventTypeSpec spec = {kEventClassKeyboard, kEventHotKeyPressed};
    if (InstallApplicationEventHandler(&tdcOnHotKey, 1, &spec, NULL, &tdcHandler) != noErr) return;
    EventHotKeyID id = {'tdcr', 1};
    if (RegisterEventHotKey(kVK_Escape, cmdKey | optionKey | controlKey, id, GetApplicationEventTarget(), 0,
                            &tdcHotKey) != noErr) {
      RemoveEventHandler(tdcHandler);
      tdcHandler = NULL;
    }
  });
}

void tdcUnwatch(void) {
  dispatch_async(dispatch_get_main_queue(), ^{
    if (tdcHotKey) UnregisterEventHotKey(tdcHotKey);
    if (tdcHandler) RemoveEventHandler(tdcHandler);
    tdcHotKey = NULL;
    tdcHandler = NULL;
  });
}
