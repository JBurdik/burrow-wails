// A tiny C facade over libghostty-vt for the Swift terminal view. All libghostty
// use lives in bvt.c, so an upstream API bump (no stability promise yet) touches one file.
#ifndef BVT_H
#define BVT_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

typedef struct BvtTerm BvtTerm;

/// Replies the emulator owes the PTY (DA / DSR / mode reports).
typedef void (*BvtWriteFn)(void *userdata, const uint8_t *data, size_t len);

enum {
  BVT_BOLD = 1 << 0,
  BVT_ITALIC = 1 << 1,
  BVT_UNDERLINE = 1 << 2,
  BVT_STRIKE = 1 << 3,
  BVT_INVERSE = 1 << 4,
  BVT_FAINT = 1 << 5,
  BVT_HAS_FG = 1 << 6,
  BVT_HAS_BG = 1 << 7,
};

enum { BVT_NARROW = 0, BVT_WIDE = 1, BVT_SPACER = 2 };

typedef struct {
  char text[28];   // UTF-8 grapheme, not NUL-terminated
  uint8_t len;     // 0 = empty cell
  uint8_t wide;    // BVT_NARROW / BVT_WIDE / BVT_SPACER (tail or head spacer: draw nothing)
  uint8_t flags;   // BVT_*
  uint32_t fg, bg; // 0xRRGGBB, valid when BVT_HAS_FG / BVT_HAS_BG
} BvtCell;

typedef struct {
  uint16_t cols, rows;
  uint32_t fg, bg;          // default colours
  bool cursor_visible;      // visible AND inside the viewport
  uint16_t cursor_x, cursor_y;
  uint8_t cursor_style;     // 0 bar, 1 block, 2 underline, 3 hollow block
  bool at_bottom;           // viewport follows the active area
  uint64_t scroll_total, scroll_offset, scroll_len;
  bool alt_screen;
} BvtFrame;

BvtTerm *bvt_new(uint16_t cols, uint16_t rows, size_t scrollback_lines, BvtWriteFn fn, void *userdata);
void bvt_free(BvtTerm *t);
void bvt_write(BvtTerm *t, const uint8_t *data, size_t len);
void bvt_reset(BvtTerm *t);
void bvt_resize(BvtTerm *t, uint16_t cols, uint16_t rows, uint32_t cell_w, uint32_t cell_h);
void bvt_set_colors(BvtTerm *t, uint32_t fg, uint32_t bg, uint32_t cursor);
/// Negative = up (into history). `INTPTR_MAX` jumps to the bottom.
void bvt_scroll(BvtTerm *t, intptr_t rows);
void bvt_scroll_bottom(BvtTerm *t);

/// Fills `cells` (row-major, `cols` per row, at most `cap`) and `frame`. Returns cells written.
size_t bvt_snapshot(BvtTerm *t, BvtCell *cells, size_t cap, BvtFrame *frame);

/// macOS virtual keycode + GHOSTTY_MODS_* bits → bytes for the PTY (0 = nothing to send).
/// `utf8` is the text the key produced (may be empty), `unshifted` the codepoint with no modifiers.
size_t bvt_encode_key(BvtTerm *t, uint16_t mac_keycode, uint16_t mods, uint16_t consumed_mods,
                      const char *utf8, size_t utf8_len, uint32_t unshifted, bool release,
                      char *out, size_t cap);

/// Paste text encoded for the PTY (bracketed when the program asked for it). Returns bytes written,
/// or the needed size when `cap` is too small. `data` is modified in place.
size_t bvt_encode_paste(BvtTerm *t, char *data, size_t len, char *out, size_t cap);

/// Focus in/out report (CSI I / CSI O) when the program enabled mode 1004, else 0.
size_t bvt_encode_focus(BvtTerm *t, bool focused, char *out, size_t cap);

/// True while the program asked for mouse reports (modes 9 / 1000 / 1002 / 1003).
bool bvt_mouse_tracking(BvtTerm *t);

/// Mouse event → report bytes (format/tracking from the terminal's modes). action: 0 press, 1 release,
/// 2 motion; button: 0 none, 1 left, 2 right, 3 middle, 4/5 wheel up/down. x/y in view pixels, `pad` the
/// view's inset on every side. Returns bytes written (0 = nothing to send).
size_t bvt_encode_mouse(BvtTerm *t, int action, int button, uint16_t mods, float x, float y,
                        uint32_t width, uint32_t height, uint32_t cell_w, uint32_t cell_h, uint32_t pad,
                        bool any_pressed, char *out, size_t cap);

/// OSC 0/2 title the program set, copied into `out` (UTF-8, not NUL-terminated). Returns its length.
size_t bvt_title(BvtTerm *t, char *out, size_t cap);

#endif
