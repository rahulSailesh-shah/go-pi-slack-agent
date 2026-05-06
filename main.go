package main

import (
	"context"
	"log"
	"os"
	"sync/atomic"

	"github.com/joho/godotenv"
	agent "github.com/rahulSailesh-shah/go-pi-agent"
	"github.com/rahulSailesh-shah/go-pi-ai/openai"

	"slack-agent/internal/handler"
	"slack-agent/internal/media"
	"slack-agent/internal/prompt"
	"slack-agent/internal/runner"
	"slack-agent/internal/sandbox"
	slackclient "slack-agent/internal/slack"
	"slack-agent/internal/tools"
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

	dataDir := "data"
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("getwd: %v", err)
	}

	var slackHolder atomic.Pointer[slackclient.Client]

	systemPromptBuilder := func(channelID string) string {
		cl := slackHolder.Load()
		if cl == nil {
			return ""
		}
		ctx := context.Background()
		users, err := cl.ListUsers(ctx)
		if err != nil {
			log.Printf("system prompt list users: %v", err)
		}
		channels, err := cl.ListChannels(ctx)
		if err != nil {
			log.Printf("system prompt list channels: %v", err)
		}
		mem := prompt.LoadMemory(dataDir, channelID)
		skills := prompt.DiscoverSkills(dataDir, channelID, dataDir)
		return prompt.BuildSystemPrompt(prompt.Options{
			WorkspacePath: dataDir,
			ChannelID:     channelID,
			Memory:        mem,
			IsDocker:      true,
			HostCwd:       cwd,
			Channels:      channels,
			Users:         users,
			Skills:        skills,
		})
	}

	factory := runner.New(runner.Config{
		DataDir:      dataDir,
		SystemPrompt: "You are a helpful assistant in a Slack workspace.",
		ExtraTools: func(channelID string) []agent.AgentTool {
			cl := slackHolder.Load()
			if cl == nil {
				return nil
			}
			return []agent.AgentTool{tools.NewSlackPostTool(cl)}
		},
		SystemPromptBuilder: systemPromptBuilder,
		Provider:            provider,
		ModelName:           "openai/gpt-oss-120b",
		Executor:            exec,
	})

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
		SystemPromptBuilder: systemPromptBuilder,
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
	slackHolder.Store(slack)

	log.Fatal(slack.Run())
}
