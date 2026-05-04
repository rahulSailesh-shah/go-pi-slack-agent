package session

import (
	"encoding/json"
	"time"

	agent "github.com/rahulSailesh-shah/go-pi-agent"
	gopiai "github.com/rahulSailesh-shah/go-pi-ai"
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

// MarshalJSON implements json.Marshaler for MessageEntry
func (e *MessageEntry) MarshalJSON() ([]byte, error) {
	type Alias MessageEntry
	msgBytes, err := gopiai.MarshalMessage(e.Message)
	if err != nil {
		return nil, err
	}
	return json.Marshal(&struct {
		*Alias
		Message json.RawMessage `json:"message"`
	}{
		Alias:   (*Alias)(e),
		Message: msgBytes,
	})
}

// UnmarshalJSON implements json.Unmarshaler for MessageEntry
func (e *MessageEntry) UnmarshalJSON(data []byte) error {
	type Alias MessageEntry
	aux := &struct {
		*Alias
		Message json.RawMessage `json:"message"`
	}{
		Alias: (*Alias)(e),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	msg, err := gopiai.UnmarshalMessage(aux.Message)
	if err != nil {
		return err
	}
	e.Message = msg
	return nil
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
