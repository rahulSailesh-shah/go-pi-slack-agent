package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type JSONLStore struct {
	workingDir string
	dedup      Deduplicator
	dirCache   sync.Map
	ctx        context.Context
	cancel     context.CancelFunc
}

type JSONLStoreConfig struct {
	WorkingDir string
	Dedup      Deduplicator
}

func NewJSONLStore(cfg JSONLStoreConfig) (*JSONLStore, error) {
	ctx, cancel := context.WithCancel(context.Background())

	dedup := cfg.Dedup
	if dedup == nil {
		dedup = newTTLDeduplicator(ctx, 60*time.Second)
	}

	return &JSONLStore{
		workingDir: cfg.WorkingDir,
		dedup:      dedup,
		ctx:        ctx,
		cancel:     cancel,
	}, nil
}

func (s *JSONLStore) LogMessage(msg Message) (bool, error) {
	key := fmt.Sprintf("%s:%s", msg.ChannelID, msg.ID)

	if s.dedup.IsDuplicate(key) {
		return false, nil
	}

	channelDir, err := s.getChannelDir(msg.ChannelID)
	if err != nil {
		return false, fmt.Errorf("failed to get channel directory: %w", err)
	}

	logPath := filepath.Join(channelDir, "log.jsonl")

	line, err := json.Marshal(msg)
	if err != nil {
		return false, fmt.Errorf("failed to marshal message: %w", err)
	}

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return false, fmt.Errorf("failed to open log file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(line, '\n')); err != nil {
		return false, fmt.Errorf("failed to write message: %w", err)
	}

	return true, nil
}

func (s *JSONLStore) Close() error {
	s.cancel()
	return nil
}

func (s *JSONLStore) getChannelDir(channelID string) (string, error) {
	if dir, ok := s.dirCache.Load(channelID); ok {
		return dir.(string), nil
	}

	dir := filepath.Join(s.workingDir, channelID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	s.dirCache.Store(channelID, dir)
	return dir, nil
}
