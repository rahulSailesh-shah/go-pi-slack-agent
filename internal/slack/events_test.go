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
