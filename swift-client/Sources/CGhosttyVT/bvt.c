#include "bvt.h"

#include <ghostty/vt.h>
#include <stdlib.h>
#include <string.h>

struct BvtTerm {
  GhosttyTerminal term;
  GhosttyRenderState rs;
  GhosttyRenderStateRowIterator rows;
  GhosttyRenderStateRowCells cells;
  GhosttyKeyEncoder enc;
  GhosttyKeyEvent ev;
  GhosttyMouseEncoder menc;
  GhosttyMouseEvent mev;
  BvtWriteFn write;
  void *userdata;
};

static void on_write_pty(GhosttyTerminal term, void *ud, const uint8_t *data, size_t len) {
  (void)term;
  BvtTerm *t = ud;
  if (t->write) t->write(t->userdata, data, len);
}

BvtTerm *bvt_new(uint16_t cols, uint16_t rows, size_t scrollback_lines, BvtWriteFn fn, void *userdata) {
  BvtTerm *t = calloc(1, sizeof *t);
  if (!t) return NULL;
  t->write = fn;
  t->userdata = userdata;
  if (ghostty_terminal_new(NULL, &t->term, cols ? cols : 1, rows ? rows : 1) != GHOSTTY_SUCCESS ||
      ghostty_render_state_new(NULL, &t->rs) != GHOSTTY_SUCCESS ||
      ghostty_render_state_row_iterator_new(NULL, &t->rows) != GHOSTTY_SUCCESS ||
      ghostty_render_state_row_cells_new(NULL, &t->cells) != GHOSTTY_SUCCESS ||
      ghostty_key_encoder_new(NULL, &t->enc) != GHOSTTY_SUCCESS ||
      ghostty_key_event_new(NULL, &t->ev) != GHOSTTY_SUCCESS ||
      ghostty_mouse_encoder_new(NULL, &t->menc) != GHOSTTY_SUCCESS ||
      ghostty_mouse_event_new(NULL, &t->mev) != GHOSTTY_SUCCESS) {
    bvt_free(t);
    return NULL;
  }
  ghostty_terminal_set(t->term, GHOSTTY_TERMINAL_OPT_USERDATA, t);
  ghostty_terminal_set(t->term, GHOSTTY_TERMINAL_OPT_WRITE_PTY, (const void *)on_write_pty);
  ghostty_terminal_set(t->term, GHOSTTY_TERMINAL_OPT_SCROLLBACK_MAX_LINES, &scrollback_lines);
  return t;
}

void bvt_free(BvtTerm *t) {
  if (!t) return;
  if (t->mev) ghostty_mouse_event_free(t->mev);
  if (t->menc) ghostty_mouse_encoder_free(t->menc);
  if (t->ev) ghostty_key_event_free(t->ev);
  if (t->enc) ghostty_key_encoder_free(t->enc);
  if (t->cells) ghostty_render_state_row_cells_free(t->cells);
  if (t->rows) ghostty_render_state_row_iterator_free(t->rows);
  if (t->rs) ghostty_render_state_free(t->rs);
  if (t->term) ghostty_terminal_free(t->term);
  free(t);
}

void bvt_write(BvtTerm *t, const uint8_t *data, size_t len) { ghostty_terminal_vt_write(t->term, data, len); }

void bvt_reset(BvtTerm *t) { ghostty_terminal_reset(t->term); }

void bvt_resize(BvtTerm *t, uint16_t cols, uint16_t rows, uint32_t cw, uint32_t ch) {
  ghostty_terminal_resize(t->term, cols ? cols : 1, rows ? rows : 1, cw, ch);
}

static GhosttyColorRgb rgb(uint32_t c) { return (GhosttyColorRgb){(uint8_t)(c >> 16), (uint8_t)(c >> 8), (uint8_t)c}; }
static uint32_t hex(GhosttyColorRgb c) { return (uint32_t)c.r << 16 | (uint32_t)c.g << 8 | c.b; }

void bvt_set_colors(BvtTerm *t, uint32_t fg, uint32_t bg, uint32_t cursor) {
  GhosttyColorRgb f = rgb(fg), b = rgb(bg), c = rgb(cursor);
  ghostty_terminal_set(t->term, GHOSTTY_TERMINAL_OPT_COLOR_FOREGROUND, &f);
  ghostty_terminal_set(t->term, GHOSTTY_TERMINAL_OPT_COLOR_BACKGROUND, &b);
  ghostty_terminal_set(t->term, GHOSTTY_TERMINAL_OPT_COLOR_CURSOR, &c);
}

void bvt_scroll(BvtTerm *t, intptr_t rows) {
  GhosttyTerminalScrollViewport s = {.tag = GHOSTTY_SCROLL_VIEWPORT_DELTA};
  s.value.delta = rows;
  ghostty_terminal_scroll_viewport(t->term, s);
}

void bvt_scroll_bottom(BvtTerm *t) {
  GhosttyTerminalScrollViewport s = {.tag = GHOSTTY_SCROLL_VIEWPORT_BOTTOM};
  ghostty_terminal_scroll_viewport(t->term, s);
}

size_t bvt_snapshot(BvtTerm *t, BvtCell *out, size_t cap, BvtFrame *f) {
  memset(f, 0, sizeof *f);
  if (ghostty_render_state_update(t->rs, t->term) != GHOSTTY_SUCCESS) return 0;
  ghostty_render_state_get(t->rs, GHOSTTY_RENDER_STATE_DATA_COLS, &f->cols);
  ghostty_render_state_get(t->rs, GHOSTTY_RENDER_STATE_DATA_ROWS, &f->rows);

  GhosttyRenderStateColors colors = GHOSTTY_INIT_SIZED(GhosttyRenderStateColors);
  ghostty_render_state_get(t->rs, GHOSTTY_RENDER_STATE_DATA_COLORS, &colors);
  f->fg = hex(colors.foreground);
  f->bg = hex(colors.background);

  GhosttyRenderStateCursor cur = GHOSTTY_INIT_SIZED(GhosttyRenderStateCursor);
  if (ghostty_render_state_get(t->rs, GHOSTTY_RENDER_STATE_DATA_CURSOR, &cur) == GHOSTTY_SUCCESS) {
    f->cursor_visible = cur.visible && cur.viewport_has_value;
    if (cur.viewport_has_value) {
      f->cursor_x = cur.viewport_x;
      f->cursor_y = cur.viewport_y;
    }
    f->cursor_style = (uint8_t)cur.visual_style;
  }

  GhosttyTerminalScrollbar sb = {0};
  if (ghostty_terminal_get(t->term, GHOSTTY_TERMINAL_DATA_SCROLLBAR, &sb) == GHOSTTY_SUCCESS) {
    f->scroll_total = sb.total;
    f->scroll_offset = sb.offset;
    f->scroll_len = sb.len;
    f->at_bottom = sb.offset + sb.len >= sb.total;
  } else {
    f->at_bottom = true;
  }
  GhosttyTerminalScreen scr = GHOSTTY_TERMINAL_SCREEN_PRIMARY;
  ghostty_terminal_get(t->term, GHOSTTY_TERMINAL_DATA_ACTIVE_SCREEN, &scr);
  f->alt_screen = scr == GHOSTTY_TERMINAL_SCREEN_ALTERNATE;

  size_t n = 0;
  uint16_t y = 0;
  if (ghostty_render_state_get(t->rs, GHOSTTY_RENDER_STATE_DATA_ROW_ITERATOR, &t->rows) != GHOSTTY_SUCCESS) return 0;
  while (y < f->rows && ghostty_render_state_row_iterator_next(t->rows)) {
    if (ghostty_render_state_row_get(t->rows, GHOSTTY_RENDER_STATE_ROW_DATA_CELLS, &t->cells) != GHOSTTY_SUCCESS) break;
    uint16_t x = 0;
    size_t row_start = (size_t)y * f->cols;
    while (x < f->cols && ghostty_render_state_row_cells_next(t->cells)) {
      size_t i = row_start + x;
      if (i >= cap) return n;
      BvtCell *c = &out[i];
      memset(c, 0, sizeof *c);

      GhosttyBuffer buf = {.ptr = (uint8_t *)c->text, .cap = sizeof c->text, .len = 0};
      if (ghostty_render_state_row_cells_get(t->cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_UTF8, &buf) ==
          GHOSTTY_SUCCESS)
        c->len = (uint8_t)buf.len;  // a cluster over 28 bytes is OUT_OF_SPACE → drawn empty

      GhosttyCell raw = 0;
      GhosttyCellWide wide = GHOSTTY_CELL_WIDE_NARROW;
      if (ghostty_render_state_row_cells_get(t->cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_RAW, &raw) == GHOSTTY_SUCCESS)
        ghostty_cell_get(raw, GHOSTTY_CELL_DATA_WIDE, &wide);
      c->wide = wide == GHOSTTY_CELL_WIDE_WIDE ? BVT_WIDE : wide == GHOSTTY_CELL_WIDE_NARROW ? BVT_NARROW : BVT_SPACER;

      bool styled = false;
      ghostty_render_state_row_cells_get(t->cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_HAS_STYLING, &styled);
      if (styled) {
        GhosttyStyle st = GHOSTTY_INIT_SIZED(GhosttyStyle);
        if (ghostty_render_state_row_cells_get(t->cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_STYLE, &st) ==
            GHOSTTY_SUCCESS) {
          if (st.bold) c->flags |= BVT_BOLD;
          if (st.italic) c->flags |= BVT_ITALIC;
          if (st.faint) c->flags |= BVT_FAINT;
          if (st.underline != GHOSTTY_SGR_UNDERLINE_NONE) c->flags |= BVT_UNDERLINE;
          if (st.strikethrough) c->flags |= BVT_STRIKE;
          if (st.inverse) c->flags |= BVT_INVERSE;
          if (st.invisible) c->len = 0;
        }
      }
      GhosttyColorRgb col;
      if (ghostty_render_state_row_cells_get(t->cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_FG_COLOR, &col) ==
          GHOSTTY_SUCCESS) {
        c->fg = hex(col);
        c->flags |= BVT_HAS_FG;
      }
      if (ghostty_render_state_row_cells_get(t->cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_BG_COLOR, &col) ==
          GHOSTTY_SUCCESS) {
        c->bg = hex(col);
        c->flags |= BVT_HAS_BG;
      }
      x++;
      n = i + 1;
    }
    for (; x < f->cols && row_start + x < cap; x++) memset(&out[row_start + x], 0, sizeof *out);
    n = row_start + f->cols < cap ? row_start + f->cols : cap;
    y++;
  }
  ghostty_render_state_clean(t->rs);
  return n;
}

// macOS virtual keycodes (Carbon kVK_*) → W3C key codes.
static GhosttyKey mac_key(uint16_t k) {
  switch (k) {
    case 0x00: return GHOSTTY_KEY_A;
    case 0x01: return GHOSTTY_KEY_S;
    case 0x02: return GHOSTTY_KEY_D;
    case 0x03: return GHOSTTY_KEY_F;
    case 0x04: return GHOSTTY_KEY_H;
    case 0x05: return GHOSTTY_KEY_G;
    case 0x06: return GHOSTTY_KEY_Z;
    case 0x07: return GHOSTTY_KEY_X;
    case 0x08: return GHOSTTY_KEY_C;
    case 0x09: return GHOSTTY_KEY_V;
    case 0x0A: return GHOSTTY_KEY_INTL_BACKSLASH;
    case 0x0B: return GHOSTTY_KEY_B;
    case 0x0C: return GHOSTTY_KEY_Q;
    case 0x0D: return GHOSTTY_KEY_W;
    case 0x0E: return GHOSTTY_KEY_E;
    case 0x0F: return GHOSTTY_KEY_R;
    case 0x10: return GHOSTTY_KEY_Y;
    case 0x11: return GHOSTTY_KEY_T;
    case 0x12: return GHOSTTY_KEY_DIGIT_1;
    case 0x13: return GHOSTTY_KEY_DIGIT_2;
    case 0x14: return GHOSTTY_KEY_DIGIT_3;
    case 0x15: return GHOSTTY_KEY_DIGIT_4;
    case 0x16: return GHOSTTY_KEY_DIGIT_6;
    case 0x17: return GHOSTTY_KEY_DIGIT_5;
    case 0x18: return GHOSTTY_KEY_EQUAL;
    case 0x19: return GHOSTTY_KEY_DIGIT_9;
    case 0x1A: return GHOSTTY_KEY_DIGIT_7;
    case 0x1B: return GHOSTTY_KEY_MINUS;
    case 0x1C: return GHOSTTY_KEY_DIGIT_8;
    case 0x1D: return GHOSTTY_KEY_DIGIT_0;
    case 0x1E: return GHOSTTY_KEY_BRACKET_RIGHT;
    case 0x1F: return GHOSTTY_KEY_O;
    case 0x20: return GHOSTTY_KEY_U;
    case 0x21: return GHOSTTY_KEY_BRACKET_LEFT;
    case 0x22: return GHOSTTY_KEY_I;
    case 0x23: return GHOSTTY_KEY_P;
    case 0x24: return GHOSTTY_KEY_ENTER;
    case 0x25: return GHOSTTY_KEY_L;
    case 0x26: return GHOSTTY_KEY_J;
    case 0x27: return GHOSTTY_KEY_QUOTE;
    case 0x28: return GHOSTTY_KEY_K;
    case 0x29: return GHOSTTY_KEY_SEMICOLON;
    case 0x2A: return GHOSTTY_KEY_BACKSLASH;
    case 0x2B: return GHOSTTY_KEY_COMMA;
    case 0x2C: return GHOSTTY_KEY_SLASH;
    case 0x2D: return GHOSTTY_KEY_N;
    case 0x2E: return GHOSTTY_KEY_M;
    case 0x2F: return GHOSTTY_KEY_PERIOD;
    case 0x30: return GHOSTTY_KEY_TAB;
    case 0x31: return GHOSTTY_KEY_SPACE;
    case 0x32: return GHOSTTY_KEY_BACKQUOTE;
    case 0x33: return GHOSTTY_KEY_BACKSPACE;
    case 0x35: return GHOSTTY_KEY_ESCAPE;
    case 0x41: return GHOSTTY_KEY_NUMPAD_DECIMAL;
    case 0x43: return GHOSTTY_KEY_NUMPAD_MULTIPLY;
    case 0x45: return GHOSTTY_KEY_NUMPAD_ADD;
    case 0x47: return GHOSTTY_KEY_NUM_LOCK;
    case 0x4B: return GHOSTTY_KEY_NUMPAD_DIVIDE;
    case 0x4C: return GHOSTTY_KEY_NUMPAD_ENTER;
    case 0x4E: return GHOSTTY_KEY_NUMPAD_SUBTRACT;
    case 0x51: return GHOSTTY_KEY_NUMPAD_EQUAL;
    case 0x52: return GHOSTTY_KEY_NUMPAD_0;
    case 0x53: return GHOSTTY_KEY_NUMPAD_1;
    case 0x54: return GHOSTTY_KEY_NUMPAD_2;
    case 0x55: return GHOSTTY_KEY_NUMPAD_3;
    case 0x56: return GHOSTTY_KEY_NUMPAD_4;
    case 0x57: return GHOSTTY_KEY_NUMPAD_5;
    case 0x58: return GHOSTTY_KEY_NUMPAD_6;
    case 0x59: return GHOSTTY_KEY_NUMPAD_7;
    case 0x5B: return GHOSTTY_KEY_NUMPAD_8;
    case 0x5C: return GHOSTTY_KEY_NUMPAD_9;
    case 0x60: return GHOSTTY_KEY_F5;
    case 0x61: return GHOSTTY_KEY_F6;
    case 0x62: return GHOSTTY_KEY_F7;
    case 0x63: return GHOSTTY_KEY_F3;
    case 0x64: return GHOSTTY_KEY_F8;
    case 0x65: return GHOSTTY_KEY_F9;
    case 0x67: return GHOSTTY_KEY_F11;
    case 0x6D: return GHOSTTY_KEY_F10;
    case 0x6F: return GHOSTTY_KEY_F12;
    case 0x73: return GHOSTTY_KEY_HOME;
    case 0x74: return GHOSTTY_KEY_PAGE_UP;
    case 0x75: return GHOSTTY_KEY_DELETE;
    case 0x76: return GHOSTTY_KEY_F4;
    case 0x77: return GHOSTTY_KEY_END;
    case 0x78: return GHOSTTY_KEY_F2;
    case 0x79: return GHOSTTY_KEY_PAGE_DOWN;
    case 0x7A: return GHOSTTY_KEY_F1;
    case 0x7B: return GHOSTTY_KEY_ARROW_LEFT;
    case 0x7C: return GHOSTTY_KEY_ARROW_RIGHT;
    case 0x7D: return GHOSTTY_KEY_ARROW_DOWN;
    case 0x7E: return GHOSTTY_KEY_ARROW_UP;
    default: return GHOSTTY_KEY_UNIDENTIFIED;
  }
}

size_t bvt_encode_key(BvtTerm *t, uint16_t keycode, uint16_t mods, uint16_t consumed, const char *utf8, size_t utf8_len,
                      uint32_t unshifted, bool release, char *out, size_t cap) {
  ghostty_key_encoder_setopt_from_terminal(t->enc, t->term);
  ghostty_key_event_set_action(t->ev, release ? GHOSTTY_KEY_ACTION_RELEASE : GHOSTTY_KEY_ACTION_PRESS);
  ghostty_key_event_set_key(t->ev, mac_key(keycode));
  ghostty_key_event_set_mods(t->ev, mods);
  ghostty_key_event_set_consumed_mods(t->ev, consumed);
  ghostty_key_event_set_composing(t->ev, false);
  ghostty_key_event_set_utf8(t->ev, utf8_len ? utf8 : NULL, utf8_len);
  ghostty_key_event_set_unshifted_codepoint(t->ev, unshifted);
  size_t n = 0;
  if (ghostty_key_encoder_encode(t->enc, t->ev, out, cap, &n) != GHOSTTY_SUCCESS) return 0;
  return n;
}

static bool mode(BvtTerm *t, GhosttyMode m) {
  GhosttyTerminalModeConfig c = {.mode = m, .value = false};
  ghostty_terminal_get(t->term, GHOSTTY_TERMINAL_DATA_MODE, &c);
  return c.value;
}

size_t bvt_encode_paste(BvtTerm *t, char *data, size_t len, char *out, size_t cap) {
  size_t n = 0;
  ghostty_paste_encode(data, len, mode(t, GHOSTTY_MODE_BRACKETED_PASTE), out, cap, &n);
  return n;
}

size_t bvt_encode_focus(BvtTerm *t, bool focused, char *out, size_t cap) {
  if (!mode(t, GHOSTTY_MODE_FOCUS_EVENT)) return 0;
  size_t n = 0;
  if (ghostty_focus_encode(focused ? GHOSTTY_FOCUS_GAINED : GHOSTTY_FOCUS_LOST, out, cap, &n) != GHOSTTY_SUCCESS)
    return 0;
  return n;
}

bool bvt_mouse_tracking(BvtTerm *t) {
  bool on = false;
  ghostty_terminal_get(t->term, GHOSTTY_TERMINAL_DATA_MOUSE_TRACKING, &on);
  return on;
}

size_t bvt_encode_mouse(BvtTerm *t, int action, int button, uint16_t mods, float x, float y, uint32_t width,
                        uint32_t height, uint32_t cell_w, uint32_t cell_h, uint32_t pad, bool any_pressed, char *out,
                        size_t cap) {
  ghostty_mouse_encoder_setopt_from_terminal(t->menc, t->term);
  GhosttyMouseEncoderSize sz = {.size = sizeof sz,
                                .screen_width = width,
                                .screen_height = height,
                                .cell_width = cell_w ? cell_w : 1,
                                .cell_height = cell_h ? cell_h : 1,
                                .padding_top = pad,
                                .padding_bottom = pad,
                                .padding_right = pad,
                                .padding_left = pad};
  ghostty_mouse_encoder_setopt(t->menc, GHOSTTY_MOUSE_ENCODER_OPT_SIZE, &sz);
  ghostty_mouse_encoder_setopt(t->menc, GHOSTTY_MOUSE_ENCODER_OPT_ANY_BUTTON_PRESSED, &any_pressed);
  bool track = true;
  ghostty_mouse_encoder_setopt(t->menc, GHOSTTY_MOUSE_ENCODER_OPT_TRACK_LAST_CELL, &track);
  ghostty_mouse_event_set_action(t->mev, (GhosttyMouseAction)action);
  if (button > 0) ghostty_mouse_event_set_button(t->mev, (GhosttyMouseButton)button);
  else ghostty_mouse_event_clear_button(t->mev);
  ghostty_mouse_event_set_mods(t->mev, mods);
  ghostty_mouse_event_set_position(t->mev, (GhosttyMousePosition){x, y});
  size_t n = 0;
  if (ghostty_mouse_encoder_encode(t->menc, t->mev, out, cap, &n) != GHOSTTY_SUCCESS) return 0;
  return n;
}

size_t bvt_title(BvtTerm *t, char *out, size_t cap) {
  GhosttyString s = {0};
  if (ghostty_terminal_get(t->term, GHOSTTY_TERMINAL_DATA_TITLE, &s) != GHOSTTY_SUCCESS || !s.ptr) return 0;
  size_t n = s.len < cap ? s.len : cap;
  memcpy(out, s.ptr, n);
  return n;
}
