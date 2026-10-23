package core

import (
	"testing"
	"time"
)

type noticeRetrySession struct {
	*scriptedRetrySession
	content string
}

func (s *noticeRetrySession) Send(prompt, messageID string, images []ImageAttachment, files []FileAttachment) error {
	s.events <- Event{Type: EventText, Content: s.content, SessionID: s.sessionID}
	return s.scriptedRetrySession.Send(prompt, messageID, images, files)
}

func TestProcessInteractiveTurnWithRetry_SessionNoticeDoesNotResetBudget(t *testing.T) {
	oldInitial, oldDelay, oldAttempts := RetriableErrorInitialDelay, RetriableErrorRetryDelay, RetriableErrorMaxAttempts
	SetRetriableErrorPolicy(time.Millisecond, time.Millisecond, 2)
	t.Cleanup(func() { SetRetriableErrorPolicy(oldInitial, oldDelay, oldAttempts) })
	for _, content := range []string{"", "..."} {
		t.Run(content, func(t *testing.T) {
			p := &stubPlatformEngine{n: "test"}
			e := NewEngine("test", &continueStubAgent{prompt: DefaultContinueAfterErrorPrompt}, []Platform{p}, "", LangEnglish)
			key := "test:notice-budget"
			session := e.sessions.GetOrCreateActive(key)
			as := &noticeRetrySession{scriptedRetrySession: newScriptedRetrySession("thread-1", "fail", "fail", "fail", "fail", "ok"), content: content}
			state := &interactiveState{agentSession: as, platform: p, replyCtx: "ctx"}
			e.processInteractiveTurnWithRetry(state, session, e.sessions, key, "hello", "m1", nil, nil, "ctx", time.Now(), key, 5, 0)
			if got := as.sendCount(); got != 2 {
				t.Fatalf("Send calls = %d, want the configured 2 attempts", got)
			}
			if got := session.GetAgentSessionID(); got != "thread-1" {
				t.Fatalf("session ID = %q, want the persisted thread-1", got)
			}
		})
	}
}

func TestProcessInteractiveTurnWithRetry_QueuedTurnHasOwnBudget(t *testing.T) {
	oldInitial, oldDelay, oldAttempts := RetriableErrorInitialDelay, RetriableErrorRetryDelay, RetriableErrorMaxAttempts
	SetRetriableErrorPolicy(time.Millisecond, time.Millisecond, 2)
	t.Cleanup(func() { SetRetriableErrorPolicy(oldInitial, oldDelay, oldAttempts) })
	for _, queuedInput := range []string{"second", ""} {
		t.Run(queuedInput, func(t *testing.T) {
			p := &stubPlatformEngine{n: "test"}
			e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)
			key := "test:queue-budget"
			session := e.sessions.GetOrCreateActive(key)
			as := newScriptedRetrySession("thread-1", "fail", "ok", "fail", "ok")
			state := &interactiveState{agentSession: as, platform: p, replyCtx: "first-ctx",
				pendingMessages: []queuedMessage{{content: queuedInput, messageID: "m2", platform: p, replyCtx: "second-ctx"}},
			}
			e.processInteractiveTurnWithRetry(state, session, e.sessions, key, "first", "m1", nil, nil, "first-ctx", time.Now(), key, 5, 0)
			if got := as.sendCount(); got != 4 {
				t.Fatalf("Send calls = %d, want two attempts for each message", got)
			}
			as.mu.Lock()
			defer as.mu.Unlock()
			if as.prompts[2] != queuedInput || as.prompts[3] != queuedInput {
				t.Fatalf("queued prompts = %q, want queued input %q on both attempts", as.prompts[2:], queuedInput)
			}
		})
	}
}
