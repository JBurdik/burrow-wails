package progstatus

import (
	"encoding/base64"
	"strings"
	"testing"

	"burrow/internal/agentphase"
)

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func TestParse(t *testing.T) {
	tf := "QXBwbHkgMyB0byBhZGQsIDEgdG8gY2hhbmdlLCAwIHRvIGRlc3Ryb3k/" // spec's terraform example
	cases := []struct {
		name, body string
		ok         bool
		want       Report
	}{
		{"spec example", "state=blocked:kind=permission:app=terraform:msg=" + tf, true,
			Report{State: "blocked", Kind: "permission", Msg: "Apply 3 to add, 1 to change, 0 to destroy?"}},
		{"padding optional", "state=done:msg=" + strings.TrimRight(b64("ok!"), "="), true, Report{State: "done", Msg: "ok!"}},
		{"whitespace trimmed", " state = working ", true, Report{State: "working"}},
		{"last wins", "state=idle:state=error", true, Report{State: "error"}},
		{"unknown key ignored", "state=working:progress=40:future=x", true, Report{State: "working"}},
		{"malformed pairs skipped", "nokey:=v:BAD=x:a=b;c:state=done", true, Report{State: "done"}},
		{"kind only on blocked", "state=working:kind=permission", true, Report{State: "working"}},
		{"unknown kind = absent", "state=blocked:kind=coffee", true, Report{State: "blocked"}},
		{"id kept", "state=done:id=build/test", true, Report{State: "done", ID: "build/test"}},
		{"bad app is absent, not fatal", "state=done:app=" + strings.Repeat("a", 40), true, Report{State: "done"}},
		{"no state", "msg=" + b64("x"), false, Report{}},
		{"unknown state", "state=sleeping", false, Report{}},
		{"feature query", "?", false, Report{}},
		{"bad base64 length", "state=done:msg=A", false, Report{}},
		{"bad base64 alphabet", "state=done:msg=ab-c", false, Report{}},
		{"value outside charset = malformed pair", "state=done:msg=Zm9v!", true, Report{State: "done"}},
		{"control char in msg", "state=done:msg=" + b64("a\nb"), false, Report{}},
		{"C1 control in msg", "state=done:msg=" + b64("a\u0085b"), false, Report{}},
		{"DEL in title", "state=done:title=" + b64("a\x7f"), false, Report{}},
		{"key too long", "state=done:" + strings.Repeat("k", 17) + "=v", false, Report{}},
		{"msg encoded too long", "state=done:msg=" + strings.Repeat("A", maxMsgEnc+4), false, Report{}},
		{"msg decoded too long", "state=done:msg=" + b64(strings.Repeat("x", maxMsgDec+1)), false, Report{}},
		{"title too long", "state=done:title=" + b64(strings.Repeat("x", maxTitleDec+1)), false, Report{}},
		{"bad id", "state=done:id=a//b", false, Report{}},
		{"id too deep", "state=done:id=" + strings.Repeat("a/", 8) + "a", false, Report{}},
		{"sequence too long", "state=done:x=" + strings.Repeat("a", maxSeq), false, Report{}},
	}
	for _, c := range cases {
		got, ok := Parse(c.body)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: Parse(%q) = %+v,%v want %+v,%v", c.name, c.body, got, ok, c.want, c.ok)
		}
	}
}

func TestEvent(t *testing.T) {
	cases := []struct {
		r    Report
		want agentphase.State
	}{
		{Report{State: "working"}, agentphase.Running},
		{Report{State: "blocked", Kind: "permission"}, agentphase.WaitingApproval},
		{Report{State: "blocked", Kind: "auth"}, agentphase.WaitingInput},
		{Report{State: "blocked"}, agentphase.WaitingInput},
		{Report{State: "done"}, agentphase.Done},
		{Report{State: "error"}, agentphase.Failed},
		{Report{State: "idle"}, agentphase.Idle},
		{Report{State: "clear"}, agentphase.Idle},
	}
	for _, c := range cases {
		ev, ok := Event(c.r)
		if !ok || ev.State != c.want || ev.Kind != agentphase.ProgramStatus {
			t.Errorf("Event(%+v) = %+v,%v want %s", c.r, ev, ok, c.want)
		}
	}
	if _, ok := Event(Report{State: "done", ID: "child"}); ok {
		t.Error("child record must not drive the phase")
	}
}

func TestScannerSplitAndTerminators(t *testing.T) {
	stream := "out\x1b[31mred\x1b]7501;state=working\x07more\x1b]7501;state=done\x1b\\tail\x1b]0;title\x07"
	// Every split point must give the same two bodies.
	for cut := 0; cut <= len(stream); cut++ {
		var s Scanner
		got := append(s.Feed([]byte(stream[:cut])), s.Feed([]byte(stream[cut:]))...)
		if len(got) != 2 || got[0] != "state=working" || got[1] != "state=done" {
			t.Fatalf("cut %d: %q", cut, got)
		}
		if len(s.buf) != 0 {
			t.Fatalf("cut %d: leftover %q", cut, s.buf)
		}
	}
	var s Scanner
	if got := s.Feed([]byte("\x1b]7501;state=done\x1b[0m\x1b]7501;state=idle\x07")); len(got) != 1 || got[0] != "state=idle" {
		t.Errorf("aborted sequence: %q", got)
	}
	if got := s.Feed([]byte("\x1b]7501;" + strings.Repeat("a", maxSeq) + "\x07")); len(got) != 0 || len(s.buf) != 0 {
		t.Errorf("oversized sequence: %d bodies, buf %d", len(got), len(s.buf))
	}
}

func TestPhaseFlow(t *testing.T) {
	apply := func(p agentphase.Phase, body string) agentphase.Phase {
		r, _ := Parse(body)
		ev, _ := Event(r)
		return agentphase.Next(p, ev, 1)
	}
	p := apply(agentphase.Phase{}, "state=blocked:kind=permission:msg="+b64("Approve?"))
	if p.State != agentphase.WaitingApproval || p.Detail != "Approve?" || !p.IsAgent {
		t.Fatalf("blocked: %+v", p)
	}
	p = apply(p, "state=done:msg="+b64("Applied"))
	if p.State != agentphase.Done || p.TurnEndedAt == 0 {
		t.Fatalf("done: %+v", p)
	}
	if q := apply(p, "state=idle"); q != p {
		t.Fatalf("idle must not erase an unseen done: %+v", q)
	}
}
