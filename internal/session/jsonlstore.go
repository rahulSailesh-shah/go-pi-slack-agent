package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

type JSONLStore struct {
	filePath string
	file     *os.File
	isNew    bool
}

func NewJSONLStore(filePath string) (*JSONLStore, error) {
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return nil, err
	}

	isNew := false
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		isNew = true
	}

	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	s := &JSONLStore{filePath: filePath, file: f, isNew: isNew}

	if isNew {
		cwd, _ := os.Getwd()
		header := &Header{
			Type:      "session",
			ID:        uuid.NewString(),
			Timestamp: time.Now(),
			Cwd:       cwd,
		}
		data, err := json.Marshal(header)
		if err != nil {
			f.Close()
			return nil, err
		}
		if _, err := fmt.Fprintf(f, "%s\n", data); err != nil {
			f.Close()
			return nil, err
		}
	}

	return s, nil
}

func (s *JSONLStore) Load() (*Header, []Entry, error) {
	f, err := os.Open(s.filePath)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	var header *Header
	var entries []Entry

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

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
			return nil, nil, fmt.Errorf("line %d: %w", lineNum, err)
		}

		if peek.Type == "session" {
			var h Header
			if err := json.Unmarshal(line, &h); err != nil {
				log.Printf("Malformed line %d header: %v", lineNum, err)
				continue
			}
			header = &h
			continue
		}

		entry, err := parseEntry(peek.Type, line)
		if err != nil {
			log.Printf("Malformed line %d entry: %v", lineNum, err)
			continue
		}
		entries = append(entries, entry)
	}

	return header, entries, scanner.Err()
}

func (s *JSONLStore) Append(entry Entry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(s.file, "%s\n", data)
	return err
}

func (s *JSONLStore) Close() error {
	return s.file.Close()
}

func (s *JSONLStore) IsNew() bool {
	return s.isNew
}

func parseEntry(entryType string, data []byte) (Entry, error) {
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
