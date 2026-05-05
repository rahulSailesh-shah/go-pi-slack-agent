package session

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	agent "github.com/rahulSailesh-shah/go-pi-agent"
)

type Manager struct {
	mu      sync.RWMutex
	store   EntryStore
	header  *Header
	entries []Entry
	byID    map[string]Entry
	leafID  *string
}

func NewSessionManager(store EntryStore) (*Manager, error) {
	header, entries, err := store.Load()
	if err != nil {
		return nil, fmt.Errorf("load session: %w", err)
	}

	m := &Manager{
		store: store,
		byID:  make(map[string]Entry),
	}

	if header == nil {
		cwd, _ := os.Getwd()
		header = &Header{
			Type:      "session",
			ID:        uuid.NewString(),
			Timestamp: time.Now(),
			Cwd:       cwd,
		}
	}
	m.header = header

	for _, e := range entries {
		m.entries = append(m.entries, e)
		m.byID[e.EntryID()] = e
		m.leafID = strPtr(e.EntryID())
	}

	return m, nil
}

func (m *Manager) AppendMessage(msg agent.Message) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry := &MessageEntry{
		baseEntry: baseEntry{
			Type:      "message",
			ID:        uuid.NewString(),
			ParentID:  m.leafID,
			Timestamp: time.Now(),
		},
		Message: msg,
	}
	return m.appendEntry(entry)
}

func (m *Manager) AppendCompaction(summary, firstKeptEntryID string, tokensBefore int, details map[string]any) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry := &CompactionEntry{
		baseEntry: baseEntry{
			Type:      "compaction",
			ID:        uuid.NewString(),
			ParentID:  m.leafID,
			Timestamp: time.Now(),
		},
		Summary:          summary,
		FirstKeptEntryID: firstKeptEntryID,
		TokensBefore:     tokensBefore,
		Details:          details,
	}
	return m.appendEntry(entry)
}

func (m *Manager) GetBranch(fromID *string) []Entry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	startID := m.leafID
	if fromID != nil {
		startID = fromID
	}
	return m.getBranchLocked(startID)
}

func (m *Manager) BuildSessionContext() Context {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.leafID == nil {
		return Context{}
	}

	var afterCompaction []agent.Message
	var keptMessages []agent.Message
	var compaction *CompactionEntry

	current := m.byID[*m.leafID]
	done := false

	for current != nil && !done {
		if compaction == nil {
			if ce, ok := current.(*CompactionEntry); ok {
				compaction = ce
			} else if msg := extractMessage(current); msg != nil {
				afterCompaction = append(afterCompaction, *msg)
			}
		} else {
			if msg := extractMessage(current); msg != nil {
				keptMessages = append(keptMessages, *msg)
			}
			if current.EntryID() == compaction.FirstKeptEntryID {
				done = true
			}
		}

		pid := current.EntryParentID()
		if pid == nil {
			break
		}
		current = m.byID[*pid]
	}

	if compaction == nil {
		reverseMessages(afterCompaction)
		return Context{Messages: afterCompaction}
	}

	reverseMessages(keptMessages)
	reverseMessages(afterCompaction)

	messages := make([]agent.Message, 0, 1+len(keptMessages)+len(afterCompaction))
	messages = append(messages, compactionToUserMessage(*compaction))
	messages = append(messages, keptMessages...)
	messages = append(messages, afterCompaction...)

	return Context{Messages: messages}
}

func (m *Manager) GetLatestCompaction() *CompactionEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for i := len(m.entries) - 1; i >= 0; i-- {
		if ce, ok := m.entries[i].(*CompactionEntry); ok {
			return ce
		}
	}
	return nil
}

func (m *Manager) GetEntries() []Entry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Entry, len(m.entries))
	copy(out, m.entries)
	return out
}

func (m *Manager) GetEntry(id string) Entry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.byID[id]
}

func (m *Manager) SessionID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.header.ID
}

func (m *Manager) appendEntry(entry Entry) (string, error) {
	m.entries = append(m.entries, entry)
	m.byID[entry.EntryID()] = entry
	m.leafID = strPtr(entry.EntryID())

	if err := m.store.Append(entry); err != nil {
		return "", fmt.Errorf("persist entry: %w", err)
	}
	return entry.EntryID(), nil
}

func (m *Manager) getBranchLocked(startID *string) []Entry {
	if startID == nil {
		return nil
	}
	var path []Entry
	current := m.byID[*startID]
	for current != nil {
		path = append(path, current)
		pid := current.EntryParentID()
		if pid == nil {
			break
		}
		current = m.byID[*pid]
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

func strPtr(s string) *string { return &s }

func extractMessage(entry Entry) *agent.Message {
	if me, ok := entry.(*MessageEntry); ok {
		return &me.Message
	}
	return nil
}

func reverseMessages(messages []agent.Message) {
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
}

func compactionToUserMessage(cm CompactionEntry) agent.Message {
	return agent.UserMessage{
		Timestamp: cm.Timestamp,
		Contents: []agent.Content{
			agent.TextContent{
				Text: cm.Summary,
			},
		},
	}
}
