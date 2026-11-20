package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"strings"
	"testing"
)

func TestWorkspaceIconThumb(t *testing.T) {
	// A noisy 1024x512 PNG: big enough to be over the threshold.
	src := image.NewRGBA(image.Rect(0, 0, 1024, 512))
	r := rand.New(rand.NewSource(1))
	for y := 0; y < 512; y++ {
		for x := 0; x < 1024; x++ {
			src.Set(x, y, color.RGBA{uint8(r.Intn(256)), uint8(r.Intn(256)), uint8(r.Intn(256)), 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	big := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())

	thumb := workspaceIconThumb(big)
	if len(thumb) >= len(big)/10 || !strings.HasPrefix(thumb, "data:image/png;base64,") {
		t.Fatalf("thumb not small: %d of %d bytes", len(thumb), len(big))
	}
	raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(thumb, "data:image/png;base64,"))
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 128 || b.Dy() != 64 {
		t.Fatalf("thumb size %v, want 128x64 (aspect kept)", b)
	}
	if again := workspaceIconThumb(big); again != thumb {
		t.Fatal("cached thumb differs")
	}

	for _, keep := range []string{"", "rocket", "data:image/png;base64,AAAA", "data:image/png;base64," + strings.Repeat("A", 40000)} {
		if got := workspaceIconThumb(keep); got != keep {
			t.Fatalf("changed %.30q", keep)
		}
	}
}
