package codex

import (
	"encoding/json"
	"testing"

	"github.com/chenhg5/cc-connect/core"
)

func TestAppServerSession_CapacityErrorIsRetriable(t *testing.T) {
	for _, tc := range []struct {
		name, info, message string
		want                core.ErrorKind
	}{
		{"capacity message", "", "Selected model is at capacity. Please try a different model.", core.ErrorKindOverloaded},
		{"structured overload", "serverOverloaded", "Try a different model.", core.ErrorKindOverloaded},
		{"structured rate limit", "rateLimitExceeded", "Please retry later.", core.ErrorKindRateLimit},
		{"quota exhausted", "usageLimitExceeded", "Usage limit reached", core.ErrorKindUnknown},
		{"authentication", "unauthorized", "Invalid API key", core.ErrorKindUnknown},
		{"structured quota overrides text", "usageLimitExceeded", "Rate limit exceeded", core.ErrorKindUnknown},
		{"structured authentication overrides text", "unauthorized", "The upstream is at capacity", core.ErrorKindUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &appServerSession{events: make(chan core.Event, 2), currentTurn: "turn-1"}
			raw, err := json.Marshal(map[string]any{
				"threadId": "thread-1",
				"turn": map[string]any{
					"id": "turn-1", "status": "failed",
					"error": map[string]any{"message": tc.message, "codexErrorInfo": tc.info},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			s.handleNotification("turn/completed", raw)
			event := <-s.events
			if event.Type != core.EventError || event.Error == nil || event.Error.Error() != tc.message || event.ErrorKind != tc.want {
				t.Fatalf("event = %#v, want error %q with kind %q", event, tc.message, tc.want)
			}
		})
	}
}

func TestAppServerSession_InternalRetryDoesNotEndTurn(t *testing.T) {
	s := &appServerSession{events: make(chan core.Event, 2), currentTurn: "turn-1"}
	s.handleNotification("error", json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","willRetry":true,"error":{"message":"Selected model is at capacity. Please try a different model.","codexErrorInfo":"serverOverloaded"}}`))
	select {
	case event := <-s.events:
		t.Fatalf("internal retry emitted terminal event: %#v", event)
	default:
	}
	if s.currentTurn != "turn-1" {
		t.Fatalf("currentTurn = %q, want turn-1", s.currentTurn)
	}
	s.handleNotification("turn/completed", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}`))
	event := <-s.events
	if event.Type != core.EventResult || !event.Done {
		t.Fatalf("event = %#v, want completed result", event)
	}
}

func TestAppServerSession_TerminalCapacityErrorEmittedOnce(t *testing.T) {
	s := &appServerSession{events: make(chan core.Event, 4), currentTurn: "turn-1"}
	s.handleNotification("error", json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","willRetry":false,"error":{"message":"Selected model is at capacity. Please try a different model.","codexErrorInfo":"serverOverloaded"}}`))
	select {
	case event := <-s.events:
		if event.Type != core.EventError || event.ErrorKind != core.ErrorKindOverloaded {
			t.Fatalf("event = %#v, want retriable capacity error", event)
		}
	default:
		t.Fatal("terminal error notification was ignored")
	}
	s.handleNotification("turn/completed", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"turn-1","status":"failed","error":{"message":"Selected model is at capacity. Please try a different model.","codexErrorInfo":"serverOverloaded"}}}`))
	select {
	case event := <-s.events:
		t.Fatalf("turn/completed emitted duplicate error: %#v", event)
	default:
	}
}

func TestAppServerSession_LateFailedTurnDoesNotFailRetry(t *testing.T) {
	s := &appServerSession{events: make(chan core.Event, 4), currentTurn: "retry-turn"}
	s.handleNotification("turn/completed", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"old-turn","status":"failed","error":{"message":"Selected model is at capacity.","codexErrorInfo":"serverOverloaded"}}}`))
	s.handleNotification("error", json.RawMessage(`{"threadId":"thread-1","turnId":"old-turn","willRetry":false,"error":{"message":"Selected model is at capacity.","codexErrorInfo":"serverOverloaded"}}`))
	select {
	case event := <-s.events:
		t.Fatalf("late failure emitted an event for the retry: %#v", event)
	default:
	}
	if s.currentTurn != "retry-turn" {
		t.Fatalf("currentTurn = %q, want retry-turn", s.currentTurn)
	}
}

func TestAgent_CapacityRetryKeepsRecordedPrompt(t *testing.T) {
	var agent any = &Agent{}
	keeps, ok := agent.(core.PromptKeptOnErrorAgent)
	if !ok || !keeps.PromptKeptOnRetriableError() {
		t.Fatal("Codex records the prompt before a turn fails; retry must continue instead of appending it again")
	}
	continues, ok := agent.(core.ContinueAfterErrorAgent)
	if !ok || continues.ContinueAfterErrorPrompt() == "" {
		t.Fatal("Codex must provide a continuation prompt for the recorded turn")
	}
}
