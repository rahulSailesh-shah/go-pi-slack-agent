# Slack Package Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Extract all Slack connection logic from `main.go` into `internal/slack/`, replacing ~530 lines with ~30, and wiring the new `store` + `media` packages in.

**Architecture:** `internal/slack` exposes a `Handler` interface and a `Client` struct. `Client` owns connection, event filtering, logging, attachment queuing, stop/busy checks, and dispatches to `Handler`. `main.go` becomes pure wiring. `store` and `media` packages are injected via `Config`. Package name `slack` aliases the external `github.com/slack-go/slack` as `slacklib` internally to avoid collision.

**Tech Stack:** Go 1.25, `github.com/slack-go/slack v0.17.3` (socketmode), `slack-agent/internal/store`, `slack-agent/internal/media`

---

## File Map

| Action | Path | Responsibility |
|--------|------|----------------|
| Create | `internal/slack/handler.go` | `Handler` interface |
| Create | `internal/slack/events.go` | Pure conversion: Slack → `store.Message` + `[]store.File` |
| Create | `internal/slack/client.go` | `Client`, `Config`, `New()`, `Run()`, `PostMessage()`, event handlers |
| Create | `internal/slack/events_test.go` | Tests for events.go pure functions |
| Create | `internal/slack/client_test.go` | Tests for `dispatchToHandler` |
| Rewrite | `main.go` | Wire deps, stub handler, call `Run()` |

---

### Task 1: events.go — pure conversion functions

**Files:**
- Create: `internal/slack/events_test.go`
- Create: `internal/slack/events.go`

- [x] **Step 1: Write failing tests**

Create `internal/slack/events_test.go`:

```go
package slack

import (
	"testing"
)

func TestToMessage_SetsAllFields(t *testing.T) {
	msg := toMessage("C001", "U001", "1609459200.000000", "<@UBOT> hello world")
	if msg.ID != "1609459200.000000" {
		t.Fatalf("ID: want 1609459200.000000, got %s", msg.ID)
	}
	if msg.ChannelID != "C001" {
		t.Fatalf("ChannelID: want C001, got %s", msg.ChannelID)
	}
	if msg.UserID != "U001" {
		t.Fatalf("UserID: want U001, got %s", msg.UserID)
	}
	if msg.Text != "hello world" {
		t.Fatalf("Text: want 'hello world', got %q", msg.Text)
	}
	if msg.Platform != "slack" {
		t.Fatalf("Platform: want slack, got %s", msg.Platform)
	}
	if msg.Timestamp.IsZero() {
		t.Fatal("Timestamp should not be zero")
	}
}

func TestStripBotMention(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"<@U123ABC> hello", "hello"},
		{"hello <@U123> world", "hello  world"},
		{"no mention here", "no mention here"},
		{"  <@UBOT>  trimmed  ", "trimmed"},
		{"", ""},
	}
	for _, tc := range cases {
		got := stripBotMention(tc.in)
		if got != tc.want {
			t.Errorf("stripBotMention(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestToFiles_PreferDownloadURL(t *testing.T) {
	files := []rawFile{
		{Name: "img.png", URLPrivateDownload: "http://x.com/dl", URLPrivate: "http://x.com/p", Mimetype: "image/png"},
	}
	result := toFiles(files)
	if len(result) != 1 {
		t.Fatalf("expected 1 file, got %d", len(result))
	}
	if result[0].URL != "http://x.com/dl" {
		t.Fatalf("URL: want http://x.com/dl, got %s", result[0].URL)
	}
	if result[0].Name != "img.png" {
		t.Fatalf("Name: want img.png, got %s", result[0].Name)
	}
	if result[0].ContentType != "image/png" {
		t.Fatalf("ContentType: want image/png, got %s", result[0].ContentType)
	}
}

func TestToFiles_FallsBackToPrivateURL(t *testing.T) {
	files := []rawFile{
		{Name: "doc.pdf", URLPrivateDownload: "", URLPrivate: "http://x.com/p"},
	}
	result := toFiles(files)
	if len(result) != 1 {
		t.Fatalf("expected 1 file, got %d", len(result))
	}
	if result[0].URL != "http://x.com/p" {
		t.Fatalf("URL: want http://x.com/p, got %s", result[0].URL)
	}
}

func TestToFiles_SkipsNoURL(t *testing.T) {
	files := []rawFile{
		{Name: "test.png", URLPrivateDownload: "", URLPrivate: ""},
	}
	if len(toFiles(files)) != 0 {
		t.Fatal("expected 0 files when no URL")
	}
}

func TestToFiles_SkipsNoName(t *testing.T) {
	files := []rawFile{
		{Name: "", URLPrivateDownload: "http://x.com/dl"},
	}
	if len(toFiles(files)) != 0 {
		t.Fatal("expected 0 files when no name")
	}
}

func TestToFiles_EmptyInput(t *testing.T) {
	if len(toFiles(nil)) != 0 {
		t.Fatal("expected 0 files for nil input")
	}
}
```

- [x] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/slack/... -run "TestToMessage|TestStripBotMention|TestToFiles" -v
```

Expected: `FAIL` — `toMessage undefined`

- [x] **Step 3: Implement events.go**

Create `internal/slack/events.go`:

```go
package slack

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"slack-agent/internal/store"
)

// rawFile holds file attachment fields extracted from a Slack event payload.
// client.go populates this from slack.Msg.Files before calling toFiles.
type rawFile struct {
	Name               string
	URLPrivate         string
	URLPrivateDownload string
	Mimetype           string
}

func toMessage(channelID, userID, ts, text string) store.Message {
	tsFloat, _ := strconv.ParseFloat(ts, 64)
	return store.Message{
		ID:        ts,
		ChannelID: channelID,
		Timestamp: time.Unix(int64(tsFloat), 0),
		UserID:    userID,
		Text:      stripBotMention(text),
		Platform:  "slack",
	}
}

func toFiles(files []rawFile) []store.File {
	result := make([]store.File, 0, len(files))
	for _, f := range files {
		url := f.URLPrivateDownload
		if url == "" {
			url = f.URLPrivate
		}
		if url == "" || f.Name == "" {
			continue
		}
		result = append(result, store.File{
			Name:        f.Name,
			URL:         url,
			ContentType: f.Mimetype,
		})
	}
	return result
}

var botMentionRe = regexp.MustCompile(`<@[A-Z0-9]+>`)

func stripBotMention(text string) string {
	return strings.TrimSpace(botMentionRe.ReplaceAllString(text, ""))
}
```

- [x] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/slack/... -run "TestToMessage|TestStripBotMention|TestToFiles" -v
```

Expected: all 9 tests PASS

- [x] **Step 5: Commit**

```bash
git add internal/slack/events.go internal/slack/events_test.go
git commit -m "feat(slack): add pure event conversion functions"
```

---

### Task 2: handler.go — Handler interface

**Files:**
- Create: `internal/slack/handler.go`

- [x] **Step 1: Create handler.go**

```go
package slack

import "slack-agent/internal/store"

// Handler is implemented by the caller to respond to Slack events.
// The slack package handles logging, attachment queuing, and goroutine
// dispatch. Per-channel serialization and busy-state are the handler's
// responsibility — required for correctness when multiple platforms share
// the same agent.
type Handler interface {
	HandleStop(channelID string)
	HandleEvent(msg store.Message, files []store.File)
}
```

- [x] **Step 2: Verify package compiles**

```bash
go build ./internal/slack/...
```

Expected: no output (clean compile)

- [x] **Step 3: Commit**

```bash
git add internal/slack/handler.go
git commit -m "feat(slack): add Handler interface"
```

---

### Task 3: client.go + client_test.go

**Files:**
- Create: `internal/slack/client_test.go`
- Create: `internal/slack/client.go`

- [x] **Step 1: Write failing tests**

Create `internal/slack/client_test.go`:

```go
package slack

import (
	"testing"
	"time"

	"slack-agent/internal/media"
	"slack-agent/internal/store"
)

// --- mocks ---

type mockHandler struct {
	stopCalled         bool
	stopChannel        string
	eventCalled        bool
	eventMsg           store.Message
	eventFiles         []store.File
	panicOnHandleEvent bool
}

func (m *mockHandler) HandleStop(channelID string) {
	m.stopCalled = true
	m.stopChannel = channelID
}
func (m *mockHandler) HandleEvent(msg store.Message, files []store.File) {
	if m.panicOnHandleEvent {
		panic("test panic")
	}
	m.eventCalled = true
	m.eventMsg = msg
	m.eventFiles = files
}

type mockStore struct {
	logCalled bool
	logMsg    store.Message
}

func (m *mockStore) LogMessage(msg store.Message) (bool, error) {
	m.logCalled = true
	m.logMsg = msg
	return true, nil
}
func (m *mockStore) Close() error { return nil }

type mockFileHandler struct {
	called bool
	result []store.Attachment
}

func (m *mockFileHandler) ProcessAttachments(channelID string, files []store.File, ts string) []store.Attachment {
	m.called = true
	return m.result
}
func (m *mockFileHandler) Close() error { return nil }

// newTestClient builds a Client with nil api/socket — safe for dispatchToHandler tests.
// Returns the client and a pointer to the slice of posted messages for assertions.
func newTestClient(h Handler, s store.Store, fh media.FileHandler) (*Client, *[]string) {
	posted := make([]string, 0)
	c := &Client{
		store:       s,
		fileHandler: fh,
		handler:     h,
		botUserID:   "UBOT",
		startupTs:   "0",
	}
	c.poster = func(channelID, text string) error {
		posted = append(posted, text)
		return nil
	}
	return c, &posted
}

// --- tests ---

func TestDispatchToHandler_CallsHandleEvent(t *testing.T) {
	h := &mockHandler{}
	c, _ := newTestClient(h, &mockStore{}, &mockFileHandler{})

	msg := store.Message{ID: "1000", ChannelID: "C001", Text: "hello", Platform: "slack"}
	c.dispatchToHandler(msg, nil)

	// dispatchToHandler spawns a goroutine — wait for it
	time.Sleep(20 * time.Millisecond)

	if !h.eventCalled {
		t.Fatal("HandleEvent should be called")
	}
	if h.eventMsg.Text != "hello" {
		t.Fatalf("HandleEvent text: want 'hello', got %q", h.eventMsg.Text)
	}
}

func TestDispatchToHandler_StopCommand(t *testing.T) {
	h := &mockHandler{}
	c, _ := newTestClient(h, &mockStore{}, &mockFileHandler{})

	msg := store.Message{ID: "1000", ChannelID: "C001", Text: "stop"}
	c.dispatchToHandler(msg, nil)

	time.Sleep(20 * time.Millisecond)

	if !h.stopCalled {
		t.Fatal("HandleStop should be called on 'stop' text")
	}
	if h.stopChannel != "C001" {
		t.Fatalf("HandleStop channel: want C001, got %s", h.stopChannel)
	}
	if h.eventCalled {
		t.Fatal("HandleEvent should NOT be called on stop")
	}
}

func TestDispatchToHandler_ProcessesAttachmentsBeforeDispatch(t *testing.T) {
	h := &mockHandler{}
	fh := &mockFileHandler{result: []store.Attachment{{Original: "f.png", Local: "C001/Attachments/f.png"}}}
	c, _ := newTestClient(h, &mockStore{}, fh)

	files := []store.File{{Name: "f.png", URL: "http://x.com/f.png"}}
	msg := store.Message{ID: "1000", ChannelID: "C001", Text: "hello"}
	c.dispatchToHandler(msg, files)

	time.Sleep(20 * time.Millisecond)

	if !fh.called {
		t.Fatal("ProcessAttachments should be called when files present")
	}
	if len(h.eventMsg.Attachments) != 1 {
		t.Fatalf("msg.Attachments: want 1, got %d", len(h.eventMsg.Attachments))
	}
}

func TestDispatchToHandler_PanicRecovery(t *testing.T) {
	h := &mockHandler{panicOnHandleEvent: true}
	c, _ := newTestClient(h, &mockStore{}, &mockFileHandler{})

	msg := store.Message{ID: "1000", ChannelID: "C001", Text: "hello"}
	c.dispatchToHandler(msg, nil)

	// goroutine panics internally — must not propagate or deadlock
	time.Sleep(50 * time.Millisecond)
}
```

- [x] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/slack/... -run TestDispatchToHandler -v
```

Expected: `FAIL` — `Client undefined`

- [x] **Step 3: Implement client.go**

Create `internal/slack/client.go`:

```go
package slack

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	slacklib "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
	"slack-agent/internal/media"
	"slack-agent/internal/store"
)

type Client struct {
	api         *slacklib.Client
	socket      *socketmode.Client
	store       store.Store
	fileHandler media.FileHandler
	handler     Handler
	botUserID   string
	startupTs   string
	poster      func(channelID, text string) error
}

type Config struct {
	AppToken    string
	BotToken    string
	Store       store.Store
	FileHandler media.FileHandler
	Handler     Handler
}

func New(cfg Config) (*Client, error) {
	api := slacklib.New(
		cfg.BotToken,
		slacklib.OptionAppLevelToken(cfg.AppToken),
	)
	socket := socketmode.New(api)

	authResp, err := api.AuthTest()
	if err != nil {
		return nil, fmt.Errorf("slack auth: %w", err)
	}

	c := &Client{
		api:         api,
		socket:      socket,
		store:       cfg.Store,
		fileHandler: cfg.FileHandler,
		handler:     cfg.Handler,
		botUserID:   authResp.UserID,
		startupTs:   strconv.FormatInt(time.Now().Unix(), 10),
	}
	c.poster = func(channelID, text string) error {
		_, _, err := c.api.PostMessage(channelID, slacklib.MsgOptionText(text, false))
		return err
	}
	return c, nil
}

func (c *Client) PostMessage(channelID, text string) {
	if err := c.poster(channelID, text); err != nil {
		log.Printf("PostMessage %s: %v", channelID, err)
	}
}

func (c *Client) Run() error {
	h := socketmode.NewSocketmodeHandler(c.socket)
	h.HandleEvents(slackevents.AppMention, c.onAppMention)
	h.HandleEvents(slackevents.Message, c.onMessage)
	return h.RunEventLoop()
}

func (c *Client) onAppMention(evt *socketmode.Event, client *socketmode.Client) {
	evAPI, ok := evt.Data.(slackevents.EventsAPIEvent)
	if !ok {
		client.Ack(*evt.Request)
		return
	}
	client.Ack(*evt.Request)

	ev, ok := evAPI.InnerEvent.Data.(*slackevents.AppMentionEvent)
	if !ok {
		return
	}
	if strings.HasPrefix(ev.Channel, "D") {
		return
	}

	msg := toMessage(ev.Channel, ev.User, ev.TimeStamp, ev.Text)

	if _, err := c.store.LogMessage(msg); err != nil {
		log.Printf("LogMessage: %v", err)
	}
	if msg.ID < c.startupTs {
		return
	}

	c.dispatchToHandler(msg, nil)
}

func (c *Client) onMessage(evt *socketmode.Event, client *socketmode.Client) {
	evAPI, ok := evt.Data.(slackevents.EventsAPIEvent)
	if !ok {
		client.Ack(*evt.Request)
		return
	}
	client.Ack(*evt.Request)

	ev, ok := evAPI.InnerEvent.Data.(*slackevents.MessageEvent)
	if !ok {
		return
	}

	if ev.BotID != "" || ev.User == "" || ev.User == c.botUserID {
		return
	}
	if ev.SubType != "" && ev.SubType != "file_share" {
		return
	}
	if ev.Text == "" && ev.SubType != "file_share" {
		return
	}

	isDM := ev.ChannelType == "im"
	isBotMention := strings.Contains(ev.Text, "<@"+c.botUserID+">")
	if !isDM && isBotMention {
		return
	}

	msg := toMessage(ev.Channel, ev.User, ev.TimeStamp, ev.Text)

	if _, err := c.store.LogMessage(msg); err != nil {
		log.Printf("LogMessage: %v", err)
	}
	if msg.ID < c.startupTs {
		return
	}

	if !isDM && ev.SubType != "file_share" {
		return
	}

	// ev.Message is *slack.Msg (slackevents.MessageEvent.Message field type)
	// Files for file_share subtypes live in ev.Message.Files
	var files []rawFile
	if ev.SubType == "file_share" && ev.Message != nil {
		for _, f := range ev.Message.Files {
			files = append(files, rawFile{
				Name:               f.Name,
				URLPrivate:         f.URLPrivate,
				URLPrivateDownload: f.URLPrivateDownload,
				Mimetype:           f.Mimetype,
			})
		}
	}

	c.dispatchToHandler(msg, toFiles(files))
}

func (c *Client) dispatchToHandler(msg store.Message, files []store.File) {
	if len(files) > 0 {
		attachments := c.fileHandler.ProcessAttachments(msg.ChannelID, files, msg.ID)
		msg.Attachments = attachments
	}

	if strings.TrimSpace(strings.ToLower(msg.Text)) == "stop" {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("HandleStop panic: %v", r)
				}
			}()
			c.handler.HandleStop(msg.ChannelID)
		}()
		return
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("HandleEvent panic: %v", r)
			}
		}()
		c.handler.HandleEvent(msg, files)
	}()
}
```

- [x] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/slack/... -v
```

Expected: all tests PASS

- [x] **Step 5: Commit**

```bash
git add internal/slack/client.go internal/slack/client_test.go
git commit -m "feat(slack): implement Client with event dispatch, stop/busy handling, panic recovery"
```

---

### Task 4: Rewrite main.go

**Files:**
- Modify: `main.go` (full rewrite)

- [x] **Step 1: Rewrite main.go**

Replace the entire contents of `main.go` with:

```go
package main

import (
	"log"
	"os"

	"github.com/joho/godotenv"
	ourslack "slack-agent/internal/slack"
	"slack-agent/internal/media"
	"slack-agent/internal/store"
)

// stubHandler satisfies slack.Handler until a real agent is wired in.
type stubHandler struct{}

func (s *stubHandler) HandleStop(channelID string) {}
func (s *stubHandler) HandleEvent(msg store.Message, files []store.File) {
	log.Printf("[%s] event: %s", msg.ChannelID, msg.Text)
}

func main() {
	godotenv.Load()

	botToken := os.Getenv("BOT_TOKEN")
	appToken := os.Getenv("APP_TOKEN")

	st, err := store.NewJSONLStore(store.JSONLStoreConfig{WorkingDir: "data"})
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	fh, err := media.NewJSONLFileHandler(media.JSONLFileHandlerConfig{
		WorkingDir: "data",
		BotToken:   botToken,
	})
	if err != nil {
		log.Fatalf("filehandler: %v", err)
	}
	defer fh.Close()

	c, err := ourslack.New(ourslack.Config{
		AppToken:    appToken,
		BotToken:    botToken,
		Store:       st,
		FileHandler: fh,
		Handler:     &stubHandler{},
	})
	if err != nil {
		log.Fatalf("slack: %v", err)
	}

	log.Fatal(c.Run())
}
```

- [x] **Step 2: Verify full build**

```bash
go build ./...
```

Expected: clean compile, no errors

- [x] **Step 3: Run full test suite**

```bash
go test ./... -v 2>&1 | grep -E "^(ok|FAIL|---)"
```

Expected: all packages `ok`, no `FAIL`

- [x] **Step 4: Commit**

```bash
git add main.go
git commit -m "feat: replace main.go with slim wiring using internal/slack package"
```
