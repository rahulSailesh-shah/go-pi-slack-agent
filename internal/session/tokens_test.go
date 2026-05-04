package session

import (
	"testing"

	gopiai "github.com/rahulSailesh-shah/go-pi-ai"
)

func TestEstimateTokens_TextContent(t *testing.T) {
	msg := gopiai.UserMessage{
		Contents: []gopiai.Content{gopiai.TextContent{Text: "hello"}}, // 5 chars
	}
	got := estimateTokens(msg)
	want := 2 // int(math.Ceil(5.0 / 4.0)) = 2
	if got != want {
		t.Fatalf("want %d, got %d", want, got)
	}
}

func TestEstimateTokens_EmptyMessage(t *testing.T) {
	msg := gopiai.UserMessage{}
	got := estimateTokens(msg)
	if got != 0 {
		t.Fatalf("want 0, got %d", got)
	}
}

func TestEstimateTokens_ImageContent(t *testing.T) {
	msg := gopiai.UserMessage{
		Contents: []gopiai.Content{gopiai.ImageContent{}}, // 4800 chars flat rate
	}
	got := estimateTokens(msg)
	want := 1200 // int(math.Ceil(4800.0 / 4.0))
	if got != want {
		t.Fatalf("want %d, got %d", want, got)
	}
}

func TestFindCutPoint_EmptyReturnsZero(t *testing.T) {
	got := findCutPoint(nil, 1000)
	if got != 0 {
		t.Fatalf("want 0 for nil, got %d", got)
	}
}

func TestFindCutPoint_LargeKeepReturnsFirst(t *testing.T) {
	msgs := []gopiai.Message{
		gopiai.UserMessage{Contents: []gopiai.Content{gopiai.TextContent{Text: "aaaa"}}},
		gopiai.UserMessage{Contents: []gopiai.Content{gopiai.TextContent{Text: "bbbb"}}},
	}
	// keepRecentTokens=999: accumulated never reaches 999, cutIndex stays cutPoints[0]=0
	got := findCutPoint(msgs, 999)
	if got != 0 {
		t.Fatalf("want 0, got %d", got)
	}
}

func TestFindCutPoint_SmallKeepCutsLater(t *testing.T) {
	msgs := []gopiai.Message{
		gopiai.UserMessage{Contents: []gopiai.Content{gopiai.TextContent{Text: "aaaa"}}}, // 1 token
		gopiai.UserMessage{Contents: []gopiai.Content{gopiai.TextContent{Text: "bbbb"}}}, // 1 token
		gopiai.UserMessage{Contents: []gopiai.Content{gopiai.TextContent{Text: "cccc"}}}, // 1 token
		gopiai.UserMessage{Contents: []gopiai.Content{gopiai.TextContent{Text: "dddd"}}}, // 1 token
	}
	// keepRecentTokens=2: walk back i=3(acc=1), i=2(acc=2 >= 2) → first cutPoint >= 2 is 2
	got := findCutPoint(msgs, 2)
	want := 2
	if got != want {
		t.Fatalf("want %d, got %d", want, got)
	}
}

func TestEstimateContextTokens_NoUsageFallsBackToEstimate(t *testing.T) {
	msgs := []gopiai.Message{
		gopiai.UserMessage{Contents: []gopiai.Content{gopiai.TextContent{Text: "hello"}}}, // 2 tokens
	}
	got := estimateContextTokens(msgs)
	want := 2
	if got != want {
		t.Fatalf("want %d, got %d", want, got)
	}
}

func TestFindLastUsage_NoMessages(t *testing.T) {
	got := findLastUsage(nil)
	if got != nil {
		t.Fatal("want nil for no messages")
	}
}

func TestFindLastUsage_NoAssistantMessage(t *testing.T) {
	msgs := []gopiai.Message{
		gopiai.UserMessage{Contents: []gopiai.Content{gopiai.TextContent{Text: "hi"}}},
	}
	got := findLastUsage(msgs)
	if got != nil {
		t.Fatal("want nil when no assistant messages")
	}
}