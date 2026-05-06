package main

import (
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/rahulSailesh-shah/go-pi-ai/openai"

	"slack-agent/internal/handler"
	"slack-agent/internal/media"
	"slack-agent/internal/runner"
	"slack-agent/internal/sandbox"
	slackclient "slack-agent/internal/slack"
	msglog "slack-agent/internal/store"
)

func main() {
	godotenv.Load()

	botToken := os.Getenv("BOT_TOKEN")
	appToken := os.Getenv("APP_TOKEN")

	st, err := msglog.NewJSONLStore(msglog.JSONLStoreConfig{WorkingDir: "data"})
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

	provider, err := openai.NewProvider(openai.Config{
		APIKey:  os.Getenv("NVIDIA_API_KEY"),
		BaseURL: os.Getenv("NVIDIA_BASE_URL"),
	})
	if err != nil {
		log.Fatalf("provider: %v", err)
	}

	exec, err := sandbox.NewDockerExecutor("sandbox", "data")
	if err != nil {
		log.Fatalf("sandbox: %v", err)
	}

	factory := runner.New(runner.Config{
		DataDir:      "data",
		SystemPrompt: "You are a helpful assistant in a Slack workspace.",
		Provider:     provider,
		ModelName:    "openai/gpt-oss-120b",
		Executor:     exec,
	})

	// Assigned after slackclient.New to break initialization cycle.
	var slack *slackclient.Client

	d := handler.NewDispatcher(handler.Config{
		BufferSize:     64,
		SessionFactory: factory.Create,
		Responder: func(channelID, text string) error {
			if slack == nil {
				return nil
			}
			return slack.PostMessage(channelID, text)
		},
	})
	defer d.Close()

	slack, err = slackclient.New(slackclient.Config{
		AppToken:    appToken,
		BotToken:    botToken,
		Store:       st,
		FileHandler: fh,
		Handler:     d,
	})
	if err != nil {
		log.Fatalf("slack: %v", err)
	}

	log.Fatal(slack.Run())
}
