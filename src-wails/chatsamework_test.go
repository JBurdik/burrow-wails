package main

import (
	"strconv"
	"strings"
	"testing"
)

func TestSameWorkScore(t *testing.T) {
	a := sameWorkTokens("Oprav přihlašování přes GitHub OAuth v nastavení")
	b := sameWorkTokens("GitHub OAuth login nefunguje — přihlašování padá")
	c := sameWorkTokens("Přidej tmavý režim do dashboardu")
	if s := sameWorkScore(a, b); s < sameWorkThreshold {
		t.Fatalf("same work scored %.2f", s)
	}
	if s := sameWorkScore(a, c); s != 0 {
		t.Fatalf("different work scored %.2f", s)
	}
	if sameWorkScore(sameWorkTokens("prosím udělej tohle"), sameWorkTokens("prosím udělej tamto")) != 0 {
		t.Fatal("stop words alone must not match")
	}
}

func TestFindSameWork(t *testing.T) {
	a, _, wsID, _ := runtimeApp(t)
	other, _ := a.CreateChat(Chat{WorkspaceID: wsID, Title: "Fix GitHub OAuth login redirect"})
	a.emitChatLine(strconv.FormatInt(other.ID, 10), chatUserKind, "The GitHub OAuth login redirect loops forever after consent")
	_, _ = a.CreateChat(Chat{WorkspaceID: wsID, Title: "Dark mode for the dashboard"})
	sub, _ := a.CreateChat(Chat{WorkspaceID: wsID, Title: "GitHub OAuth login redirect sub", ParentChatID: other.ID})
	mine, _ := a.CreateChat(Chat{WorkspaceID: wsID, Title: "New chat"})
	drainChatStream(t, a, strconv.FormatInt(other.ID, 10))

	hits := a.findSameWork(mine.ID, "OAuth login with GitHub keeps redirecting in a loop")
	if len(hits) != 1 || hits[0].ID != other.ID {
		t.Fatalf("hits %+v (sub-agent %d must not count)", hits, sub.ID)
	}
	if ctx := sameWorkContext(hits); !strings.Contains(ctx, "chat_read") || !strings.Contains(ctx, "Fix GitHub OAuth") {
		t.Fatalf("context: %s", ctx)
	}
	if !a.firstUserSend(mine.ID) || a.firstUserSend(other.ID) {
		t.Fatal("firstUserSend: mine has no prompt yet, other has one")
	}
}
