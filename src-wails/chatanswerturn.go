package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"burrow/internal/control"
)

// chat_answer_turn: the end_turn card's answer, composed here from the stored
// turn_needs so every client (desktop card, Decisions, tray panel, phone, iOS)
// sends the same message. Clients send what was picked, never the text.

// TurnPick is one ask's answer: Options = picked option labels (one unless the
// ask is multi), else Text = a free-text answer. On a Done card (needs
// "nothing") Index is ignored and Options are the picked `next` follow-ups;
// Ask marks them as asked about ("What do you mean by: …?") instead of ticked.
type TurnPick struct {
	Index   int      `json:"index"`
	Options []string `json:"options,omitempty"`
	Text    string   `json:"text,omitempty"`
	Ask     bool     `json:"ask,omitempty"`
}

// ChatAnswerTurn composes the answer to the chat's stored card and submits it
// through the submit door (send if settled, else queue; clientId dedupe).
// Images = the composer's, riding along like extra. The reply's Text is the
// composed message, which the client shows as its echo.
func (a *App) ChatAnswerTurn(chatID int64, picks []TurnPick, extra string, images []string, clientID string) (ChatSubmitResult, error) {
	return a.submit(chatID, images, clientID, func(c Chat) (string, error) {
		var n control.TurnNeeds
		if c.TurnNeeds == "" || json.Unmarshal([]byte(c.TurnNeeds), &n) != nil {
			return "", fmt.Errorf("the card is gone — a new turn has started")
		}
		return composeTurnAnswer(n, picks, extra)
	})
}

// composeTurnAnswer: Needs you = each answered question quoted with its
// answer (a single ask answered alone = just the answer); a run ask with a
// kept reply answers itself with it. Done = the picked follow-ups (one = as
// is, several = a list), then one "What do you mean by: X?" per asked
// follow-up. The composer text goes last, after a blank line.
func composeTurnAnswer(n control.TurnNeeds, picks []TurnPick, extra string) (string, error) {
	var body string
	if n.Needs == "you" {
		answers := make([]string, len(n.Asks))
		seen := map[int]bool{}
		for _, p := range picks {
			if p.Index < 0 || p.Index >= len(n.Asks) || seen[p.Index] {
				return "", fmt.Errorf("no ask %d on the card", p.Index)
			}
			seen[p.Index] = true
			ask := n.Asks[p.Index]
			if len(p.Options) > 1 && !ask.Multi {
				return "", fmt.Errorf("ask %d takes one option", p.Index)
			}
			var picked []string
			for _, o := range ask.Options { // card order, not click order
				if slices.Contains(p.Options, o) {
					picked = append(picked, o)
				}
			}
			if len(picked) != len(p.Options) {
				return "", fmt.Errorf("ask %d has no such option", p.Index)
			}
			if len(picked) > 0 {
				answers[p.Index] = strings.Join(picked, ", ")
			} else {
				answers[p.Index] = strings.TrimSpace(p.Text)
			}
		}
		type qa struct{ q, a string }
		var answered []qa
		for i, ask := range n.Asks {
			a := answers[i]
			if a == "" && ask.Run != nil {
				a = strings.TrimSpace(ask.Run.Reply)
			}
			if a != "" {
				answered = append(answered, qa{ask.Question, a})
			}
		}
		if len(n.Asks) == 1 && len(answered) == 1 {
			body = answered[0].a
		} else {
			blocks := make([]string, len(answered))
			for i, x := range answered {
				blocks[i] = "> " + strings.ReplaceAll(x.q, "\n", "\n> ") + "\n" + x.a
			}
			body = strings.Join(blocks, "\n\n")
		}
	} else {
		var want, asked []string
		for _, p := range picks {
			if p.Ask {
				asked = append(asked, p.Options...)
			} else {
				want = append(want, p.Options...)
			}
		}
		var picked, questions []string
		for _, s := range n.Next {
			if slices.Contains(want, s) {
				picked = append(picked, s)
			}
			if slices.Contains(asked, s) {
				questions = append(questions, "What do you mean by: "+s+"?")
			}
		}
		if len(picked) != len(want) || len(questions) != len(asked) {
			return "", fmt.Errorf("no such follow-up on the card")
		}
		if len(picked) == 1 {
			body = picked[0]
		} else {
			for i := range picked {
				picked[i] = "- " + picked[i]
			}
			body = strings.Join(picked, "\n")
		}
		if len(questions) > 0 {
			body = strings.TrimSpace(body + "\n\n" + strings.Join(questions, "\n"))
		}
	}
	out := strings.Join(slices.DeleteFunc([]string{body, strings.TrimSpace(extra)}, func(s string) bool { return s == "" }), "\n\n")
	if out == "" {
		return "", fmt.Errorf("nothing answered")
	}
	return out, nil
}
