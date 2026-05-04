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
