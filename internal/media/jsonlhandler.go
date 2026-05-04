package media

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"slack-agent/internal/store"
)

type JSONLFileHandler struct {
	workingDir string
	downloader Downloader
	downloadCh chan downloadJob
	ctx        context.Context
	cancel     context.CancelFunc
}

type JSONLFileHandlerConfig struct {
	WorkingDir string
	BotToken   string
	Downloader Downloader
}

type downloadJob struct {
	url      string
	destPath string
}

func NewJSONLFileHandler(cfg JSONLFileHandlerConfig) (*JSONLFileHandler, error) {
	ctx, cancel := context.WithCancel(context.Background())

	downloader := cfg.Downloader
	if downloader == nil {
		client := &http.Client{Timeout: 30 * time.Second}
		downloader = NewHTTPDownloader(client, cfg.BotToken)
	}

	handler := &JSONLFileHandler{
		workingDir: cfg.WorkingDir,
		downloader: downloader,
		downloadCh: make(chan downloadJob, 64),
		ctx:        ctx,
		cancel:     cancel,
	}

	go handler.worker()

	return handler, nil
}

func (h *JSONLFileHandler) ProcessAttachments(channelID string, files []store.File, timestamp string) []store.Attachment {
	attachments := make([]store.Attachment, 0, len(files))

	for _, file := range files {
		if file.URL == "" || file.Name == "" {
			continue
		}

		filename := h.generateLocalFileName(file.Name, timestamp)
		localPath := fmt.Sprintf("%s/Attachments/%s", channelID, filename)

		job := downloadJob{
			url:      file.URL,
			destPath: localPath,
		}

		select {
		case h.downloadCh <- job:
			attachments = append(attachments, store.Attachment{
				Original: file.Name,
				Local:    localPath,
			})
		default:
			log.Printf("download queue full, skipping %s", file.Name)
		}
	}

	return attachments
}

func (h *JSONLFileHandler) Close() error {
	h.cancel()
	close(h.downloadCh)
	return nil
}

func (h *JSONLFileHandler) worker() {
	for {
		select {
		case job, ok := <-h.downloadCh:
			if !ok {
				return
			}
			destPath := h.resolveDestPath(job.destPath)
			if err := h.downloader.Download(h.ctx, job.url, destPath); err != nil {
				log.Printf("failed to download %s to %s: %v", job.url, job.destPath, err)
			}
		case <-h.ctx.Done():
			return
		}
	}
}

func (h *JSONLFileHandler) generateLocalFileName(original string, timestamp string) string {
	ts, _ := strconv.ParseFloat(timestamp, 64)
	tsMs := int64(ts * 1000)
	reg := regexp.MustCompile(`[^a-zA-Z0-9._-]`)
	sanitized := reg.ReplaceAllString(original, "")
	return fmt.Sprintf("%d_%s", tsMs, sanitized)
}

func (h *JSONLFileHandler) resolveDestPath(localPath string) string {
	return h.workingDir + "/" + localPath
}
