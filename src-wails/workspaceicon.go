package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"strings"
	"sync"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// Project icons are stored as the data URL the user picked — often a full-size
// image (~300 KB). Every client only ever draws them as a small square, but
// list_workspaces and shell_snapshot shipped them whole: ten icons were 2.9 MB
// of a 3.1 MB snapshot, which a phone re-read every 15 s and which outlasted
// its socket checks on a slow link. Clients now get a thumbnail.
const (
	iconThumbPx   = 128      // longest side; a 2x/3x retina square of ~40 pt
	iconThumbOver = 24 << 10 // data URLs at or under this are sent as they are
)

var iconThumbs sync.Map // sha256(original) → thumbnail data URL

// workspaceIconThumb returns a small PNG data URL for a large raster data URL;
// anything else (an emoji / symbol name, a small or undecodable image) is
// returned unchanged.
func workspaceIconThumb(icon string) string {
	if len(icon) <= iconThumbOver || !strings.HasPrefix(icon, "data:image/") {
		return icon
	}
	key := sha256.Sum256([]byte(icon))
	if t, ok := iconThumbs.Load(key); ok {
		return t.(string)
	}
	thumb := icon
	if comma := strings.IndexByte(icon, ','); comma > 0 {
		if raw, err := base64.StdEncoding.DecodeString(icon[comma+1:]); err == nil {
			if src, _, err := image.Decode(bytes.NewReader(raw)); err == nil {
				if t, ok := pngThumb(src); ok {
					thumb = t
				}
			}
		}
	}
	iconThumbs.Store(key, thumb)
	return thumb
}

func pngThumb(src image.Image) (string, bool) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return "", false
	}
	if w > iconThumbPx || h > iconThumbPx {
		if w >= h {
			w, h = iconThumbPx, max(1, h*iconThumbPx/w)
		} else {
			w, h = max(1, w*iconThumbPx/h), iconThumbPx
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return "", false
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), true
}
