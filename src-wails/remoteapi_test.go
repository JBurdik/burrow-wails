package main

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// TestRemoteSurfaceIsExhaustive is the security gate: a new App method is off
// the network until somebody puts it in one list or the other on purpose.
func TestRemoteSurfaceIsExhaustive(t *testing.T) {
	allowedMethods := make(map[string]bool, len(remoteAllowed))
	for wire, c := range remoteAllowed {
		if c.Method == "" {
			t.Errorf("remoteAllowed[%q] has no method", wire)
		}
		allowedMethods[c.Method] = true
	}

	appType := reflect.TypeOf(&App{})
	for i := 0; i < appType.NumMethod(); i++ {
		name := appType.Method(i).Name
		if allowedMethods[name] {
			continue
		}
		if _, ok := remoteDenied[name]; ok {
			continue
		}
		t.Errorf("App.%s is in neither remoteAllowed nor remoteDenied — decide "+
			"whether it belongs on the wire, then add it to one of them", name)
	}

	// A denied entry for a method that no longer exists is rot.
	for name := range remoteDenied {
		if _, ok := appType.MethodByName(name); !ok {
			t.Errorf("remoteDenied names App.%s, which does not exist", name)
		}
	}
}

func TestRemoteAllowedArityMatchesMethods(t *testing.T) {
	appType := reflect.TypeOf(&App{})
	for wire, c := range remoteAllowed {
		m, ok := appType.MethodByName(c.Method)
		if !ok {
			t.Errorf("%q: App.%s does not exist", wire, c.Method)
			continue
		}
		// m.Type includes the receiver, the args list does not.
		if want := m.Type.NumIn() - 1; want != len(c.Args) {
			t.Errorf("%q: App.%s takes %d args, table names %d (%v)",
				wire, c.Method, want, len(c.Args), c.Args)
		}
	}
}

func TestRemoteAllowedEveryCommandHasAScope(t *testing.T) {
	for wire, c := range remoteAllowed {
		if c.Scope == "" {
			t.Errorf("%q has no scope; pick the narrowest one that works", wire)
		}
	}
}

func raw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fakeRecv carries one method of each return shape the App surface uses, so
// the marshalling can be tested without a daemon or a database.
//
// Do NOT reach for real App methods here: App.GetPtyForeground and
// App.ListPtySessions both dereference a.daemon with no nil guard, so calling
// them on a zero &App{} panics rather than returning an error.
type fakeRecv struct {
	gotString string
	gotInt    int
}

func (f *fakeRecv) TakeString(s string) string         { f.gotString = s; return s }
func (f *fakeRecv) TakeInt(n int)                      { f.gotInt = n }
func (f *fakeRecv) NoArgsWithError() ([]string, error) { return nil, errors.New("boom") }
func (f *fakeRecv) NoArgsNoError() int                 { return 7 }

func TestCallAppCoercesNumberToString(t *testing.T) {
	// The frontend's pty id is its own numeric counter, and every Go PTY
	// method takes it as an opaque string key. core.ts used to String() it;
	// now the conversion has to.
	f := &fakeRecv{}
	got, err := callApp(f, remoteCmd{Method: "TakeString", Args: []string{"id"}},
		map[string]json.RawMessage{"id": raw(t, 7)})
	if err != nil {
		t.Fatalf("numeric id was not coerced to string: %v", err)
	}
	if got != "7" || f.gotString != "7" {
		t.Fatalf(`want "7", got %q (method saw %q)`, got, f.gotString)
	}
}

func TestCallAppKeepsARealString(t *testing.T) {
	f := &fakeRecv{}
	got, err := callApp(f, remoteCmd{Method: "TakeString", Args: []string{"id"}},
		map[string]json.RawMessage{"id": raw(t, "abc")})
	if err != nil || got != "abc" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestCallAppMissingArgIsZeroValue(t *testing.T) {
	// core.ts passed `args.cwd ?? ""` — an absent optional arg is the zero
	// value, not an error.
	f := &fakeRecv{}
	got, err := callApp(f, remoteCmd{Method: "TakeString", Args: []string{"id"}},
		map[string]json.RawMessage{})
	if err != nil {
		t.Fatalf("missing arg should be the zero value: %v", err)
	}
	if got != "" {
		t.Fatalf("want the zero value, got %q", got)
	}
}

func TestCallAppNullArgIsZeroValue(t *testing.T) {
	f := &fakeRecv{}
	if _, err := callApp(f, remoteCmd{Method: "TakeInt", Args: []string{"n"}},
		map[string]json.RawMessage{"n": json.RawMessage("null")}); err != nil {
		t.Fatalf("explicit null should be the zero value: %v", err)
	}
	if f.gotInt != 0 {
		t.Fatalf("want 0, method saw %d", f.gotInt)
	}
}

func TestCallAppRejectsWrongType(t *testing.T) {
	f := &fakeRecv{}
	_, err := callApp(f, remoteCmd{Method: "TakeInt", Args: []string{"n"}},
		map[string]json.RawMessage{"n": raw(t, "not a number")})
	if err == nil {
		t.Fatal("a string where an int is wanted must be an error")
	}
	if !strings.Contains(err.Error(), "n") {
		t.Errorf("error should name the offending arg, got %q", err)
	}
}

func TestCallAppRejectsUnknownMethod(t *testing.T) {
	if _, err := callApp(&fakeRecv{}, remoteCmd{Method: "NoSuchMethod"}, nil); err == nil {
		t.Fatal("unknown method must be an error")
	}
}

func TestCallAppRejectsArityMismatch(t *testing.T) {
	if _, err := callApp(&fakeRecv{}, remoteCmd{Method: "TakeString", Args: nil}, nil); err == nil {
		t.Fatal("a table entry naming the wrong number of args must be an error")
	}
}

func TestCallAppSplitsTrailingError(t *testing.T) {
	// (T, error): the error becomes the call's error, never a result value.
	got, err := callApp(&fakeRecv{}, remoteCmd{Method: "NoArgsWithError", Args: nil}, nil)
	if err == nil {
		t.Fatal("a method's trailing error must become the call error")
	}
	if got != nil {
		t.Fatalf("a failed call must carry no result, got %v", got)
	}
}

func TestCallAppReturnsAResultWithNoError(t *testing.T) {
	got, err := callApp(&fakeRecv{}, remoteCmd{Method: "NoArgsNoError", Args: nil}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != 7 {
		t.Fatalf("want 7, got %v", got)
	}
}

func TestCallAppHandlesAVoidMethod(t *testing.T) {
	got, err := callApp(&fakeRecv{}, remoteCmd{Method: "TakeInt", Args: []string{"n"}},
		map[string]json.RawMessage{"n": raw(t, 3)})
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("a void method must reply with a null result, got %v", got)
	}
}
