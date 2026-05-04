package session

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	gopiai "github.com/rahulSailesh-shah/go-pi-ai"
)

func makeTestMessageEntry(text string, parentID *string) *MessageEntry {
	return &MessageEntry{
		baseEntry: baseEntry{
			Type:      "message",
			ID:        uuid.NewString(),
			ParentID:  parentID,
			Timestamp: time.Now(),
		},
		Message: gopiai.UserMessage{
			Contents: []gopiai.Content{gopiai.TextContent{Text: text}},
		},
	}
}

func makeTestCompactionEntry(summary, firstKeptID string, parentID *string) *CompactionEntry {
	return &CompactionEntry{
		baseEntry: baseEntry{
			Type:      "compaction",
			ID:        uuid.NewString(),
			ParentID:  parentID,
			Timestamp: time.Now(),
		},
		Summary:          summary,
		FirstKeptEntryID: firstKeptID,
		TokensBefore:     100,
	}
}

func newTestCompactor() *LLMCompactor {
	return &LLMCompactor{
		keepRecentTokens: 10,
		maxSummaryTokens: 1000,
		model:            "test-model",
		complete: func(ctx context.Context, req gopiai.Request) (gopiai.AssistantMessage, error) {
			return gopiai.AssistantMessage{
				Contents: []gopiai.Content{gopiai.TextContent{Text: "test summary"}},
			}, nil
		},
	}
}

func TestLLMCompactor_PrepareCompaction_NilEntries(t *testing.T) {
	c := newTestCompactor()
	if c.PrepareCompaction(nil) != nil {
		t.Fatal("want nil for nil entries")
	}
}

func TestLLMCompactor_PrepareCompaction_EmptyEntries(t *testing.T) {
	c := newTestCompactor()
	if c.PrepareCompaction([]Entry{}) != nil {
		t.Fatal("want nil for empty entries")
	}
}

func TestLLMCompactor_PrepareCompaction_NoMessageEntries(t *testing.T) {
	// Only CompactionEntry, no MessageEntry — nothing to summarize
	c := newTestCompactor()
	entries := []Entry{makeTestCompactionEntry("old summary", "some-id", nil)}
	if c.PrepareCompaction(entries) != nil {
		t.Fatal("want nil when no MessageEntry present")
	}
}

func TestLLMCompactor_PrepareCompaction_SingleMessageTooSmall(t *testing.T) {
	// Only 1 message — findCutPoint returns 0, PrepareCompaction returns nil
	c := &LLMCompactor{keepRecentTokens: 9999, maxSummaryTokens: 1000}
	id1 := ptr("root")
	entries := []Entry{makeTestMessageEntry("hello", id1)}
	if c.PrepareCompaction(entries) != nil {
		t.Fatal("want nil when cutIndex <= 0 (all messages kept recent)")
	}
}

func TestLLMCompactor_PrepareCompaction_SetsFirstKeptEntryID(t *testing.T) {
	// keepRecentTokens=1: only last message kept, everything before summarized
	c := &LLMCompactor{keepRecentTokens: 1, maxSummaryTokens: 1000}

	// Need enough messages so findCutPoint > 0
	// Each "aaaa" = 1 token. With keepRecentTokens=1:
	// walk back: i=3(acc=1>=1) → find cutPoint >= 3 → cutIndex=3
	// But cutIndex=3 means entryCutIdx in mappings, we need cutIndex > 0
	// Let's use 4 messages, keepRecentTokens=1
	e0 := makeTestMessageEntry("aaaa", nil)
	e1 := makeTestMessageEntry("bbbb", ptr(e0.ID))
	e2 := makeTestMessageEntry("cccc", ptr(e1.ID))
	e3 := makeTestMessageEntry("dddd", ptr(e2.ID))

	entries := []Entry{e0, e1, e2, e3}
	prep := c.PrepareCompaction(entries)

	if prep == nil {
		t.Fatal("expected non-nil CompactionPreparation")
	}
	// The FirstKeptEntryID must be one of our entry IDs
	validIDs := map[string]bool{e0.ID: true, e1.ID: true, e2.ID: true, e3.ID: true}
	if !validIDs[prep.FirstKeptEntryID] {
		t.Fatalf("FirstKeptEntryID %q not in entry set", prep.FirstKeptEntryID)
	}
}

func TestLLMCompactor_PrepareCompaction_SkipsPriorCompaction(t *testing.T) {
	c := &LLMCompactor{keepRecentTokens: 1, maxSummaryTokens: 1000}

	old := makeTestCompactionEntry("previous summary", "old-entry", nil)
	e1 := makeTestMessageEntry("aaaa", ptr(old.ID))
	e2 := makeTestMessageEntry("bbbb", ptr(e1.ID))
	e3 := makeTestMessageEntry("cccc", ptr(e2.ID))
	e4 := makeTestMessageEntry("dddd", ptr(e3.ID))

	entries := []Entry{old, e1, e2, e3, e4}
	prep := c.PrepareCompaction(entries)

	if prep == nil {
		t.Fatal("expected non-nil prep")
	}
	if prep.PreviousSummary != "previous summary" {
		t.Fatalf("want PreviousSummary 'previous summary', got %q", prep.PreviousSummary)
	}
}

func TestLLMCompactor_Compact_EmptyEntriesErrors(t *testing.T) {
	c := newTestCompactor()
	_, err := c.Compact(context.Background(), &CompactionPreparation{
		EntriesToSummarize: []Entry{},
		FirstKeptEntryID:   "kept",
	})
	if err == nil {
		t.Fatal("expected error for empty EntriesToSummarize")
	}
}

func TestLLMCompactor_Compact_CallsCompleteFn(t *testing.T) {
	called := false
	c := &LLMCompactor{
		model:            "test-model",
		maxSummaryTokens: 100,
		complete: func(ctx context.Context, req gopiai.Request) (gopiai.AssistantMessage, error) {
			called = true
			return gopiai.AssistantMessage{
				Contents: []gopiai.Content{gopiai.TextContent{Text: "generated summary"}},
			}, nil
		},
	}

	entry := makeTestMessageEntry("hello world", nil)
	result, err := c.Compact(context.Background(), &CompactionPreparation{
		EntriesToSummarize: []Entry{entry},
		FirstKeptEntryID:   "kept-id",
		TokensBefore:       42,
	})

	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if !called {
		t.Fatal("complete func was not called")
	}
	if result.Summary != "generated summary" {
		t.Fatalf("want 'generated summary', got %q", result.Summary)
	}
	if result.FirstKeptEntryID != "kept-id" {
		t.Fatalf("want FirstKeptEntryID 'kept-id', got %q", result.FirstKeptEntryID)
	}
	if result.TokensBefore != 42 {
		t.Fatalf("want TokensBefore 42, got %d", result.TokensBefore)
	}
	if result.ID == "" {
		t.Fatal("expected non-empty ID on result")
	}
}

func TestLLMCompactor_Compact_PropagatesCompleteError(t *testing.T) {
	c := &LLMCompactor{
		model:            "test-model",
		maxSummaryTokens: 100,
		complete: func(ctx context.Context, req gopiai.Request) (gopiai.AssistantMessage, error) {
			return gopiai.AssistantMessage{}, fmt.Errorf("LLM unavailable")
		},
	}

	entry := makeTestMessageEntry("hello", nil)
	_, err := c.Compact(context.Background(), &CompactionPreparation{
		EntriesToSummarize: []Entry{entry},
		FirstKeptEntryID:   "kept",
	})

	if err == nil {
		t.Fatal("expected error when complete fails")
	}
}

func ptr(s string) *string { return &s }