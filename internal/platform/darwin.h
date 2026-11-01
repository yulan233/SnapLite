// SnapLite macOS adapter. Implemented in darwin.m; Go callbacks are exported
// from platform_darwin.go.
#include <stdint.h>
#include <stddef.h>

typedef struct {
    uint32_t id;
    double x, y, w, h;
    double scale;
    int primary;
} sl_display;

typedef struct {
    double x, y, w, h;
} sl_rect;

void sl_init(void);
void sl_wake_main(void);

int sl_displays(sl_display *out, int max);

// sl_capture captures a whole display. It blocks and must not run on the main
// thread. On success *pix is malloc'ed RGBA the caller frees.
// Returns 0 on success, 1 when permission is missing, 2 on other failures.
int sl_capture(uint32_t display, uint8_t **pix, int *width, int *height, int *stride);
void sl_prefetch_content(void);

int sl_has_screen_permission(void);
void sl_request_screen_permission(void);
int sl_has_accessibility(void);
void sl_request_accessibility(void);
void sl_open_url(const char *url);
void sl_reveal(const char *path);

int sl_raise_overlay(const char *title, double x, double y, double w, double h);
int sl_float_window(const char *title, double x, double y, double w, double h);
void sl_activate(void);
int sl_list_windows(sl_rect *out, int max, int exclude_own);

int sl_register_hotkey(uint32_t id, uint32_t keycode, uint32_t modifiers);
void sl_unregister_hotkey(uint32_t id);

int sl_set_clipboard_image(const void *png, size_t length);
long sl_change_count(void);
void sl_watch_clipboard(int on);
int sl_clipboard_concealed(void);

void sl_cursor_position(double *x, double *y);

int sl_launch_at_login(void);
int sl_set_launch_at_login(int enabled);

void sl_save_front_app(void);
int sl_paste_to_saved_app(void);
