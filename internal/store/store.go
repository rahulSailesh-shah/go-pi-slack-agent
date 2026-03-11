package store

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"
)

type Attachment struct {
	original string // original filename from uploader
	local    string // path relative to store directory
}

type AttachmentFile struct {
	name               string
	urlPrivate         string
	urlPrivateDownload string
}

type LoggedMessage struct {
	Date        time.Time
	Ts          int64
	User        string
	UserName    string
	DisplayName string
	Text        string
	Attachments []Attachment
	IsBot       bool
}

type ChannelStoreConfig struct {
	workingDir string
	botToken   string
}

type PendingDownloads struct {
	channelID string
	localPath string
	url       string
}

type ChannelStore struct {
	workingDir       string
	botToken         string
	pendingDownloads []PendingDownloads
	isDownloading    bool
	recentlyLogged   map[string]int64
	mu               sync.RWMutex
}

func New(config ChannelStoreConfig) *ChannelStore {
	err := os.MkdirAll(config.workingDir, 0755)
	if err != nil {
		return nil
	}

	return &ChannelStore{
		workingDir:       config.workingDir,
		botToken:         config.botToken,
		pendingDownloads: []PendingDownloads{},
		isDownloading:    false,
		recentlyLogged:   make(map[string]int64),
		mu:               sync.RWMutex{},
	}
}

func (s *ChannelStore) GetChannelDir(channelID string) string {
	dir := filepath.Join(s.workingDir, channelID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return ""
	}
	return dir
}

func (s *ChannelStore) GenerateLocalFileName(original string, timestamp string) string {
	ts, _ := strconv.ParseFloat(timestamp, 64)
	tsMs := int64(ts * 1000)
	reg := regexp.MustCompile(`[^a-zA-Z0-9._-]`)
	sanitized := reg.ReplaceAllString(original, "")
	return fmt.Sprintf("%d_%s", tsMs, sanitized)
}

func (s *ChannelStore) ProcessAttachments(channelId string, files []AttachmentFile, timestamp string) []Attachment {
	attachments := make([]Attachment, 0, len(files))
	s.mu.Lock()
	for _, file := range files {
		url := file.urlPrivateDownload
		if url == "" {
			url = file.urlPrivate
		}

		if url == "" || file.name == "" {
			continue
		}

		filename := s.GenerateLocalFileName(file.name, timestamp)
		localPath := fmt.Sprintf("%s/Attachments/%s", channelId, filename)

		s.pendingDownloads = append(s.pendingDownloads, PendingDownloads{
			channelID: channelId,
			localPath: localPath,
			url:       url,
		})

		attachments = append(attachments, Attachment{
			original: file.name,
			local:    localPath,
		})
	}
	s.mu.Unlock()

	go s.processDownloadQueue()

	return attachments
}

func (s *ChannelStore) LogMessage(channelId string, message LoggedMessage) (bool, error) {
	dedupeKey := fmt.Sprintf("%s:%d", channelId, message.Ts)

	s.mu.Lock()
	if _, exists := s.recentlyLogged[dedupeKey]; exists {
		s.mu.Unlock()
		return false, nil
	}

	s.recentlyLogged[dedupeKey] = time.Now().Unix()
	s.mu.Unlock()

	go func() {
		time.Sleep(60 * time.Second)
		s.mu.Lock()
		delete(s.recentlyLogged, dedupeKey)
		s.mu.Unlock()
	}()

	if message.Date.IsZero() {
		message.Date = time.Unix(message.Ts, 0)
	}

	channelDir := s.GetChannelDir(channelId)
	if channelDir == "" {
		return false, fmt.Errorf("failed to create channel directory")
	}

	logPath := filepath.Join(channelDir, "log.jsonl")

	line, err := json.Marshal(message)
	if err != nil {
		return false, err
	}

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return false, err
	}
	defer f.Close()

	_, err = f.Write(append(line, '\n'))
	if err != nil {
		return false, err
	}

	return true, nil
}

func (s *ChannelStore) processDownloadQueue() {
	s.mu.Lock()
	if s.isDownloading || len(s.pendingDownloads) == 0 {
		s.mu.Unlock()
		return
	}
	s.isDownloading = true

	downloads := s.pendingDownloads
	s.pendingDownloads = nil
	s.mu.Unlock()

	for _, item := range downloads {
		if err := s.downloadAttachment(item.localPath, item.url); err != nil {
			fmt.Printf("Failed to download attachment %s: %v\n", item.localPath, err)
		}
	}

	s.mu.Lock()
	s.isDownloading = false
	s.mu.Unlock()
}

func (s *ChannelStore) downloadAttachment(localPath, url string) error {
	filePath := filepath.Join(s.workingDir, localPath)
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.botToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}
