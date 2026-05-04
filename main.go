package main

import (
	"log"
	"os"

	"slack-agent/internal/handler"
	"slack-agent/internal/media"
	slackclient "slack-agent/internal/slack"
	"slack-agent/internal/store"

	"github.com/joho/godotenv"
)

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

	d := handler.NewDispatcher(handler.Config{
		BufferSize: 64,
		Processor: func(msg store.Message, files []store.File) {
			log.Printf("[%s] event: %s", msg.ChannelID, msg.Text)
		},
	})
	defer d.Close()

	c, err := slackclient.New(slackclient.Config{
		AppToken:    appToken,
		BotToken:    botToken,
		Store:       st,
		FileHandler: fh,
		Handler:     d,
	})
	if err != nil {
		log.Fatalf("slack: %v", err)
	}

	log.Fatal(c.Run())
}
