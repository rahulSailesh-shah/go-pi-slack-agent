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
