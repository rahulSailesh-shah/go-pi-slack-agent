package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	gopiai "github.com/rahulSailesh-shah/go-pi-ai"
	"github.com/rahulSailesh-shah/go-pi-ai/openai"
)

type Compactor interface {
	PrepareCompaction(pathEntries []Entry) *CompactionPreparation
	Compact(ctx context.Context, preparation *CompactionPreparation) (*CompactionEntry, error)
}

type CompactionPreparation struct {
	EntriesToSummarize []Entry
	FirstKeptEntryID   string
	TokensBefore       int
	PreviousSummary    string
}

type CompactorConfig struct {
	Complete         func(ctx context.Context, req gopiai.Request) (gopiai.AssistantMessage, error)
	Model            string
	KeepRecentTokens int
	MaxSummaryTokens int
}

type DefaultCompactor struct {
	complete         func(ctx context.Context, req gopiai.Request) (gopiai.AssistantMessage, error)
	model            string
	keepRecentTokens int
	maxSummaryTokens int
}

func NewCompactor(config *CompactorConfig) *DefaultCompactor {
	if config == nil {
		provider, err := openai.NewProvider(openai.Config{
			APIKey:  os.Getenv("CEREBRAS_API_KEY"),
			BaseURL: os.Getenv("CEREBRAS_BASE_URL"),
		})
		if err != nil {
			log.Fatalf("Failed to create provider: %v", err)
		}

		client := gopiai.NewClient(provider)
		config = &CompactorConfig{
			Complete:         client.Complete,
			Model:            "gpt-oss-120b",
			KeepRecentTokens: 1000,
			MaxSummaryTokens: 1000,
		}
	}

	return &DefaultCompactor{
		complete:         config.Complete,
		model:            config.Model,
		keepRecentTokens: config.KeepRecentTokens,
		maxSummaryTokens: config.MaxSummaryTokens,
	}
}

func (c *DefaultCompactor) PrepareCompaction(pathEntries []Entry) *CompactionPreparation {
	var previousSummary string
	activeStart := 0
	for i, entry := range pathEntries {
		if ce, ok := entry.(*CompactionEntry); ok {
			previousSummary = ce.Summary
			activeStart = i + 1
		}
	}

	activeEntries := pathEntries[activeStart:]
	if len(activeEntries) == 0 {
		return nil
	}

	type mapping struct {
		entryIdx int
		msg      gopiai.Message
	}
	var mappings []mapping
	for i, entry := range activeEntries {
		if me, ok := entry.(*MessageEntry); ok {
			mappings = append(mappings, mapping{entryIdx: i, msg: me.Message})
		}
	}
	if len(mappings) == 0 {
		return nil
	}

	messages := make([]gopiai.Message, len(mappings))
	for i, m := range mappings {
		messages[i] = m.msg
	}

	tokensBefore := estimateContextTokens(messages)
	cutIndex := findCutPoint(messages, c.keepRecentTokens)
	if cutIndex <= 0 {
		return nil
	}

	entryCutIdx := mappings[cutIndex].entryIdx

	return &CompactionPreparation{
		EntriesToSummarize: activeEntries[:entryCutIdx],
		FirstKeptEntryID:   activeEntries[entryCutIdx].EntryID(),
		TokensBefore:       tokensBefore,
		PreviousSummary:    previousSummary,
	}
}

func (c *DefaultCompactor) Compact(ctx context.Context, prep *CompactionPreparation) (*CompactionEntry, error) {
	var messages []gopiai.Message
	for _, entry := range prep.EntriesToSummarize {
		if me, ok := entry.(*MessageEntry); ok {
			messages = append(messages, me.Message)
		}
	}

	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages to compact")
	}

	summary, err := generateSummary(ctx, messages, c.complete, c.model, c.maxSummaryTokens, prep.PreviousSummary)
	if err != nil {
		return nil, err
	}

	return &CompactionEntry{
		baseEntry: baseEntry{
			Type:      "compaction",
			ID:        uuid.NewString(),
			Timestamp: time.Now(),
		},
		Summary:          summary,
		FirstKeptEntryID: prep.FirstKeptEntryID,
		TokensBefore:     prep.TokensBefore,
	}, nil
}

// ---------------------------------------------------------------------------
// Unexported helpers
// ---------------------------------------------------------------------------

func findLastUsage(messages []gopiai.Message) *gopiai.AssistantMessage {
	for i := len(messages) - 1; i >= 0; i-- {
		if am, ok := messages[i].(gopiai.AssistantMessage); ok {
			if am.StopReason != gopiai.StopReasonAborted &&
				am.StopReason != gopiai.StopReasonError &&
				am.Usage.TotalTokens > 0 {
				return &am
			}
		}
		if am, ok := messages[i].(*gopiai.AssistantMessage); ok {
			if am.StopReason != gopiai.StopReasonAborted &&
				am.StopReason != gopiai.StopReasonError &&
				am.Usage.TotalTokens > 0 {
				return am
			}
		}
	}
	return nil
}

func estimateTokens(msg gopiai.Message) int {
	var chars int
	for _, c := range msg.GetContents() {
		switch v := c.(type) {
		case gopiai.TextContent:
			chars += len(v.Text)
		case gopiai.ImageContent:
			chars += 4800
		case gopiai.ToolCall:
			chars += len(v.Name)
			if raw, err := json.Marshal(v.Arguments); err == nil {
				chars += len(raw)
			}
		}
	}
	return int(math.Ceil(float64(chars) / 4.0))
}

func estimateContextTokens(messages []gopiai.Message) int {
	lastUsageIdx := -1
	var lastUsage gopiai.Usage
	for i := len(messages) - 1; i >= 0; i-- {
		if am, ok := messages[i].(gopiai.AssistantMessage); ok {
			if am.StopReason != gopiai.StopReasonAborted &&
				am.StopReason != gopiai.StopReasonError &&
				am.Usage.TotalTokens > 0 {
				lastUsageIdx = i
				lastUsage = am.Usage
				break
			}
		}
		if am, ok := messages[i].(*gopiai.AssistantMessage); ok {
			if am.StopReason != gopiai.StopReasonAborted &&
				am.StopReason != gopiai.StopReasonError &&
				am.Usage.TotalTokens > 0 {
				lastUsageIdx = i
				lastUsage = am.Usage
				break
			}
		}
	}

	if lastUsageIdx < 0 {
		total := 0
		for _, m := range messages {
			total += estimateTokens(m)
		}
		return total
	}

	total := lastUsage.TotalTokens
	for i := lastUsageIdx + 1; i < len(messages); i++ {
		total += estimateTokens(messages[i])
	}
	return total
}

func findCutPoint(messages []gopiai.Message, keepRecentTokens int) int {
	if len(messages) == 0 {
		return 0
	}

	var cutPoints []int
	for i, m := range messages {
		role := m.Role()
		if role == "user" || role == "assistant" {
			cutPoints = append(cutPoints, i)
		}
	}
	if len(cutPoints) == 0 {
		return 0
	}

	accumulated := 0
	cutIndex := cutPoints[0]

	for i := len(messages) - 1; i >= 0; i-- {
		accumulated += estimateTokens(messages[i])
		if accumulated >= keepRecentTokens {
			for _, cp := range cutPoints {
				if cp >= i {
					cutIndex = cp
					break
				}
			}
			break
		}
	}

	if cutIndex < len(messages) && messages[cutIndex].Role() == "tool" {
		for cutIndex > 0 {
			cutIndex--
			if messages[cutIndex].Role() != "tool" {
				break
			}
		}
	}

	return cutIndex
}

func serializeConversation(messages []gopiai.Message) string {
	var sb strings.Builder
	for _, m := range messages {
		role := strings.ToUpper(m.Role())
		fmt.Fprintf(&sb, "[%s]\n", role)
		for _, c := range m.GetContents() {
			switch v := c.(type) {
			case gopiai.TextContent:
				sb.WriteString(v.Text)
				sb.WriteString("\n")
			case gopiai.ToolCall:
				args, _ := json.Marshal(v.Arguments)
				fmt.Fprintf(&sb, "Tool call: %s(%s)\n", v.Name, string(args))
			case gopiai.ImageContent:
				sb.WriteString("[image]\n")
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

const summarySystemPrompt = `You are a summarization assistant. Your job is to create structured summaries of conversations. Be precise and preserve important details like names, IDs, error messages, and technical specifics.`

const summarizationPrompt = `The messages above are a conversation to summarize. Create a structured context checkpoint summary that another LLM will use to continue the work.

Use this EXACT format:

## Goal
[What is the user trying to accomplish?]

## Constraints & Preferences
- [Any constraints, preferences, or requirements mentioned by user]
- [Or "(none)" if none were mentioned]

## Progress
### Done
- [x] [Completed tasks/changes]

### In Progress
- [ ] [Current work]

### Blocked
- [Issues preventing progress, if any]

## Key Decisions
- **[Decision]**: [Brief rationale]

## Next Steps
1. [Ordered list of what should happen next]

## Critical Context
- [Any data, examples, or references needed to continue]
- [Or "(none)" if not applicable]

Keep each section concise. Preserve exact names, IDs, and error messages.`

const updateSummarizationPrompt = `The messages above are NEW conversation messages to incorporate into the existing summary provided in <previous-summary> tags.

Update the existing structured summary with new information. RULES:
- PRESERVE all existing information from the previous summary
- ADD new progress, decisions, and context from the new messages
- UPDATE the Progress section: move items from "In Progress" to "Done" when completed
- UPDATE "Next Steps" based on what was accomplished
- If something is no longer relevant, you may remove it

Use this EXACT format:

## Goal
[Preserve existing goals, add new ones if the task expanded]

## Constraints & Preferences
- [Preserve existing, add new ones discovered]

## Progress
### Done
- [x] [Include previously done items AND newly completed items]

### In Progress
- [ ] [Current work - update based on progress]

### Blocked
- [Current blockers - remove if resolved]

## Key Decisions
- **[Decision]**: [Brief rationale] (preserve all previous, add new)

## Next Steps
1. [Update based on current state]

## Critical Context
- [Preserve important context, add new if needed]

Keep each section concise. Preserve exact names, IDs, and error messages.`

func generateSummary(
	ctx context.Context,
	messages []gopiai.Message,
	complete func(ctx context.Context, req gopiai.Request) (gopiai.AssistantMessage, error),
	model string,
	maxTokens int,
	previousSummary string,
) (string, error) {
	conversationText := serializeConversation(messages)

	var promptText strings.Builder
	fmt.Fprintf(&promptText, "<conversation>\n%s\n</conversation>\n\n", conversationText)

	if previousSummary != "" {
		fmt.Fprintf(&promptText, "<previous-summary>\n%s\n</previous-summary>\n\n", previousSummary)
		promptText.WriteString(updateSummarizationPrompt)
	} else {
		promptText.WriteString(summarizationPrompt)
	}

	req := gopiai.Request{
		Model:        model,
		SystemPrompt: summarySystemPrompt,
		Messages: []gopiai.Message{
			gopiai.UserMessage{
				Contents: []gopiai.Content{
					gopiai.TextContent{Text: promptText.String()},
				},
			},
		},
		MaxTokens: &maxTokens,
	}

	resp, err := complete(ctx, req)
	if err != nil {
		return "", fmt.Errorf("summarization failed: %w", err)
	}

	if resp.StopReason == gopiai.StopReasonError {
		return "", fmt.Errorf("summarization failed: unknown error")
	}

	var parts []string
	for _, c := range resp.Contents {
		if tc, ok := c.(gopiai.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n"), nil
}
