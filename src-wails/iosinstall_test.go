package main

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIOSManifestNamesRequestHost(t *testing.T) {
	srv := httptest.NewServer(NewHTTPServer(nil).mux())
	defer srv.Close()
	res, err := srv.Client().Get(srv.URL + "/ios/manifest.plist")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	host := strings.TrimPrefix(srv.URL, "http://")
	if !strings.Contains(string(b), "https://"+host+"/ios/Burrow.ipa") || !strings.Contains(string(b), iosBundleID) {
		t.Fatalf("manifest: %s", b)
	}
}

func TestIOSPrefixOnlyPlainPaths(t *testing.T) {
	for in, want := range map[string]string{
		"/burrow": "/burrow", "/burrow/": "/burrow", "": "", "burrow": "",
		"//evil.com": "", "/a:b": "", "/../x": "", `/a"b`: "", "/a b": "",
	} {
		if got := iosPrefix(in); got != want {
			t.Errorf("iosPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIOSManifestKeepsServePrefix(t *testing.T) {
	srv := httptest.NewServer(NewHTTPServer(nil).mux())
	defer srv.Close()
	res, err := srv.Client().Get(srv.URL + "/ios/manifest.plist?p=%2Fburrow")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(b), "/burrow/ios/Burrow.ipa") {
		t.Fatalf("manifest: %s", b)
	}
}
