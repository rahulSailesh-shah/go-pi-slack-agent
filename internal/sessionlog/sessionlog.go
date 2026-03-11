package sessionlog

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	agent "github.com/rahulSailesh-shah/go-pi-agent"
)

type Header struct {
	Type      string    `json:"type"`
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Cwd       string    `json:"cwd"`
}

type Entry interface {
	EntryType() string
	EntryID() string
	EntryTimestamp() string
	EntryParentID() *string
}
type baseEntry struct {
	Type      string    `json:"type"`
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	ParentID  *string   `json:"parent_id"`
}

func (b baseEntry) EntryType() string      { return b.Type }
func (b baseEntry) EntryID() string        { return b.ID }
func (b baseEntry) EntryTimestamp() string { return b.Timestamp.Format(time.RFC3339) }
func (b baseEntry) EntryParentID() *string { return b.ParentID }

type MessageEntry struct {
	baseEntry
	Message agent.Message `json:"message"`
}

type CompactionEntry struct {
	baseEntry
	Summary          string         `json:"summary"`
	FirstKeptEntryID string         `json:"firstKeptEntryId"`
	TokensBefore     int            `json:"tokensBefore"`
	Details          map[string]any `json:"details,omitempty"`
}

type Context struct {
	Messages []agent.Message `json:"messages"`
}

type Manager interface {
	AppendMessage(msg agent.Message) (string, error)
	AppendCompaction(summary string, firstKeptEntryID string, tokensBefore int,
		details map[string]interface{}) (string, error)
	GetBranch(fromID *string) []Entry
	GetEntries() []Entry
	BuildSessionContext() Context
}

type DefaultManager struct {
	mu          sync.RWMutex
	sessionID   string
	sessionFile string
	header      *Header
	entries     []Entry
	byID        map[string]Entry
	leafID      *string
}

func Open(filePath string) (*DefaultManager, error) {
	sm := &DefaultManager{
		sessionFile: filePath,
		byID:        make(map[string]Entry),
	}

	if _, err := os.Stat(filePath); err == nil {
		if err := sm.loadFromFile(filePath); err != nil {
			return nil, fmt.Errorf("load session: %w", err)
		}
	} else {
		sm.newSession()
		if err := sm.flushAll(); err != nil {
			return nil, fmt.Errorf("create session: %w", err)
		}
	}

	return sm, nil
}

func (sm *DefaultManager) GetBranch(fromID *string) []Entry {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	startID := sm.leafID
	if fromID != nil {
		startID = fromID
	}

	return sm.getBranchLocked(startID)
}

func (sm *DefaultManager) BuildSessionContext() Context {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.leafID == nil {
		return Context{}
	}

	path := sm.getBranchLocked(sm.leafID)
	if len(path) == 0 {
		return Context{}
	}

	// Find the last compaction entry on the path
	var compaction *CompactionEntry
	compactionIdx := -1
	for i, entry := range path {
		if ce, ok := entry.(*CompactionEntry); ok {
			compaction = ce
			compactionIdx = i
		}
	}

	var messages []agent.Message

	if compaction != nil {
		// 1. Emit compaction summary as a synthetic user message
		summaryMsg := compactionToUserMessage(*compaction)
		messages = append(messages, summaryMsg)

		// 2. Emit kept messages (before compaction, from FirstKeptEntryID onward)
		foundFirstKept := false
		for i := 0; i < compactionIdx; i++ {
			if path[i].EntryID() == compaction.FirstKeptEntryID {
				foundFirstKept = true
			}
			if foundFirstKept {
				if msg := extractMessage(path[i]); msg != nil {
					messages = append(messages, *msg)
				}
			}
		}

		// 3. Emit messages after compaction
		for i := compactionIdx + 1; i < len(path); i++ {
			if msg := extractMessage(path[i]); msg != nil {
				messages = append(messages, *msg)
			}
		}
	} else {
		for _, entry := range path {
			if msg := extractMessage(entry); msg != nil {
				messages = append(messages, *msg)
			}
		}
	}

	return Context{Messages: messages}
}

func (sm *DefaultManager) loadFromFile(filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // 10 MB max line

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var peek struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &peek); err != nil {
			return fmt.Errorf("parse line %d peek: %w", lineNum, err)
		}

		if peek.Type == "session" {
			var h Header
			if err := json.Unmarshal(line, &h); err != nil {
				return fmt.Errorf("parse line %d header: %w", lineNum, err)
			}
			sm.header = &h
			sm.sessionID = h.ID
			continue
		}

		entry, err := sm.parseEntry(peek.Type, line)
		if err != nil {
			return fmt.Errorf("parse line %d entry: %w", lineNum, err)
		}
		sm.entries = append(sm.entries, entry)
		sm.byID[entry.EntryID()] = entry
		sm.leafID = strPtr(entry.EntryID())
	}

	return scanner.Err()
}

func (sm *DefaultManager) newSession() {
	sm.sessionID = uuid.NewString()
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	sm.header = &Header{
		Type:      "session",
		ID:        sm.sessionID,
		Timestamp: time.Now(),
		Cwd:       cwd,
	}
	sm.entries = nil
	sm.byID = make(map[string]Entry)
	sm.leafID = nil
}

func (sm *DefaultManager) flushAll() error {
	dir := filepath.Dir(sm.sessionFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	f, err := os.Create(sm.sessionFile)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(sm.header)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(f, "%s\n", data); err != nil {
		return err
	}

	for _, entry := range sm.entries {
		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(f, "%s\n", data); err != nil {
			return err
		}
	}

	return nil
}

func (sm *DefaultManager) parseEntry(entryType string, data []byte) (Entry, error) {
	switch entryType {
	case "message":
		var e MessageEntry
		if err := json.Unmarshal(data, &e); err != nil {
			return nil, err
		}
		return &e, nil
	case "compaction":
		var e CompactionEntry
		if err := json.Unmarshal(data, &e); err != nil {
			return nil, err
		}
		return &e, nil
	default:
		return nil, fmt.Errorf("unknown entry type: %s", entryType)
	}
}

func (sm *DefaultManager) getBranchLocked(startID *string) []Entry {
	if startID == nil {
		return nil
	}
	var path []Entry
	current := sm.byID[*startID]
	for current != nil {
		path = append(path, current)
		pid := current.EntryParentID()
		if pid == nil {
			break
		}
		current = sm.byID[*pid]
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

// --------------------------------------------------------------
// Getters
// --------------------------------------------------------------

func (sm *DefaultManager) SessionID() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.sessionID
}

func (sm *DefaultManager) SessionFile() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.sessionFile
}

func (sm *DefaultManager) LeafID() *string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.leafID
}

func (sm *DefaultManager) GetEntry(id string) Entry {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.byID[id]
}

func (sm *DefaultManager) GetEntries() []Entry {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	out := make([]Entry, len(sm.entries))
	copy(out, sm.entries)
	return out
}

// ---------------------------------------------------------------------------
// Write operations (append-only)
// ---------------------------------------------------------------------------

func (sm *DefaultManager) AppendMessage(msg agent.Message) (string, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	entry := &MessageEntry{
		baseEntry: baseEntry{
			Type:      "message",
			ID:        uuid.NewString(),
			ParentID:  sm.leafID,
			Timestamp: time.Now(),
		},
		Message: msg,
	}

	return sm.appendEntry(entry)
}

func (sm *DefaultManager) AppendCompaction(
	summary string,
	firstKeptEntryID string,
	tokensBefore int,
	details map[string]interface{},
) (string, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	entry := &CompactionEntry{
		baseEntry: baseEntry{
			Type:      "compaction",
			ID:        uuid.NewString(),
			ParentID:  sm.leafID,
			Timestamp: time.Now(),
		},
		Summary:          summary,
		FirstKeptEntryID: firstKeptEntryID,
		TokensBefore:     tokensBefore,
		Details:          details,
	}

	return sm.appendEntry(entry)
}

func (sm *DefaultManager) appendEntry(entry Entry) (string, error) {
	sm.entries = append(sm.entries, entry)
	sm.byID[entry.EntryID()] = entry
	sm.leafID = strPtr(entry.EntryID())

	if err := sm.appendToFile(entry); err != nil {
		return "", fmt.Errorf("persist entry: %w", err)
	}

	return entry.EntryID(), nil
}

func (sm *DefaultManager) appendToFile(entry Entry) error {
	f, err := os.OpenFile(sm.sessionFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(f, "%s\n", data)
	return err
}

// --------------------------------------------------------------
// Helpers
// --------------------------------------------------------------

func strPtr(s string) *string {
	return &s
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

func extractMessage(entry Entry) *agent.Message {
	if me, ok := entry.(*MessageEntry); ok {
		return &me.Message
	}
	return nil
}
