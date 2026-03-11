package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// SlackEvent represents a Slack event with all necessary fields
type SlackEvent struct {
	Type        string        `json:"type"`
	Channel     string        `json:"channel"`
	Ts          string        `json:"ts"`
	User        string        `json:"user"`
	Text        string        `json:"text"`
	Files       []FileDetails `json:"files,omitempty"`
	Attachments []string      `json:"attachments,omitempty"`
}

// FileDetails represents file information from Slack events
type FileDetails struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Title              string `json:"title,omitempty"`
	Mimetype           string `json:"mimetype,omitempty"`
	Filetype           string `json:"filetype,omitempty"`
	Size               int64  `json:"size"`
	URLPrivate         string `json:"url_private,omitempty"`
	URLPrivateDownload string `json:"url_private_download,omitempty"`
	Permalink          string `json:"permalink,omitempty"`
	PermalinkPublic    string `json:"permalink_public,omitempty"`
}

// Bot represents the main bot structure
type Bot struct {
	api       *slack.Client
	client    *socketmode.Client
	startupTs string
	botUserID string
}

func main() {
	godotenv.Load()

	appToken := os.Getenv("APP_TOKEN")
	if !strings.HasPrefix(appToken, "xapp-") {
		panic("SLACK_APP_TOKEN must have the prefix \"xapp-\".")
	}

	botToken := os.Getenv("BOT_TOKEN")
	if !strings.HasPrefix(botToken, "xoxb-") {
		panic("SLACK_BOT_TOKEN must have the prefix \"xoxb-\".")
	}

	// Pretty print Slack API configuration
	fmt.Println("=== Slack API Configuration ===")
	fmt.Printf("Bot Token: %s...\n", botToken[:min(len(botToken), 20)])
	fmt.Printf("App Token: %s...\n", appToken[:min(len(appToken), 20)])
	fmt.Printf("Debug Mode: Enabled\n")
	fmt.Printf("Log Format: File:Line + Timestamp\n")
	fmt.Println("================================")

	api := slack.New(
		botToken,
		slack.OptionDebug(true),
		slack.OptionLog(log.New(os.Stdout, "api: ", log.Lshortfile|log.LstdFlags)),
		slack.OptionAppLevelToken(appToken),
	)

	// Pretty print Socketmode client configuration
	fmt.Println("=== Socketmode Client Configuration ===")
	fmt.Printf("API Client: Configured\n")
	fmt.Printf("Debug Mode: Enabled\n")
	fmt.Printf("Log Format: File:Line + Timestamp\n")
	fmt.Println("=====================================")

	client := socketmode.New(
		api,
		socketmode.OptionDebug(true),
		socketmode.OptionLog(log.New(os.Stdout, "socketmode: ", log.Lshortfile|log.LstdFlags)),
	)

	// Get bot user info
	authResp, err := api.AuthTest()
	if err != nil {
		panic(fmt.Sprintf("Failed to get bot info: %v", err))
	}

	// Pretty print bot information
	fmt.Println("=== Bot Information ===")
	fmt.Printf("Bot User ID: %s\n", authResp.UserID)
	fmt.Printf("Bot Username: %s\n", authResp.User)
	fmt.Printf("Team ID: %s\n", authResp.TeamID)
	fmt.Printf("Team Name: %s\n", authResp.Team)
	fmt.Printf("Startup Timestamp: %s\n", strconv.FormatInt(time.Now().Unix(), 10))
	fmt.Println("========================")

	bot := &Bot{
		api:       api,
		client:    client,
		startupTs: strconv.FormatInt(time.Now().Unix(), 10),
		botUserID: authResp.UserID,
	}

	socketmodeHandler := socketmode.NewSocketmodeHandler(client)

	// Pretty print event handler configuration
	fmt.Println("=== Event Handler Configuration ===")
	fmt.Println("Connection Handlers:")
	fmt.Println("  - Connecting")
	fmt.Println("  - Connection Error")
	fmt.Println("  - Connected")
	fmt.Println("  - Hello")
	fmt.Println("")
	fmt.Println("EventsAPI Handlers:")
	fmt.Println("  - EventsAPI (all events)")
	fmt.Println("  - AppMention")
	fmt.Println("  - FileShared")
	fmt.Println("  - Message")
	fmt.Println("")
	fmt.Println("Interactive Handlers:")
	fmt.Println("  - Interactive (all)")
	fmt.Println("  - BlockActions")
	fmt.Println("")
	fmt.Println("Slash Command Handlers:")
	fmt.Println("  - SlashCommand (all)")
	fmt.Println("  - /rocket")
	fmt.Println("===================================")

	socketmodeHandler.Handle(socketmode.EventTypeConnecting, bot.middlewareConnecting)
	socketmodeHandler.Handle(socketmode.EventTypeConnectionError, bot.middlewareConnectionError)
	socketmodeHandler.Handle(socketmode.EventTypeConnected, bot.middlewareConnected)
	socketmodeHandler.Handle(socketmode.EventTypeHello, bot.middlewareHello)

	// \\ EventTypeEventsAPI //\\
	// Handle all EventsAPI
	socketmodeHandler.Handle(socketmode.EventTypeEventsAPI, bot.middlewareEventsAPI)

	// Handle a specific event from EventsAPI
	socketmodeHandler.HandleEvents(slackevents.AppMention, bot.middlewareAppMentionEvent)
	socketmodeHandler.HandleEvents(slackevents.FileShared, bot.middlewareFileShared)
	socketmodeHandler.HandleEvents(slackevents.Message, bot.middlewareMessageEvent)

	// \\ EventTypeInteractive //\\
	// Handle all Interactive Events
	socketmodeHandler.Handle(socketmode.EventTypeInteractive, bot.middlewareInteractive)

	// Handle a specific Interaction
	socketmodeHandler.HandleInteraction(slack.InteractionTypeBlockActions, bot.middlewareInteractionTypeBlockActions)

	// Handle all SlashCommand
	socketmodeHandler.Handle(socketmode.EventTypeSlashCommand, bot.middlewareSlashCommand)
	socketmodeHandler.HandleSlashCommand("/rocket", bot.middlewareSlashCommand)

	// socketmodeHandler.HandleDefault(middlewareDefault)

	fmt.Println("\n🚀 Starting Slack Bot Event Loop...")
	socketmodeHandler.RunEventLoop()
}

// Bot methods

func (b *Bot) middlewareConnecting(evt *socketmode.Event, client *socketmode.Client) {
	fmt.Println("Connecting to Slack with Socket Mode...")
}

func (b *Bot) middlewareConnectionError(evt *socketmode.Event, client *socketmode.Client) {
	fmt.Println("Connection failed. Retrying later...")
}

func (b *Bot) middlewareConnected(evt *socketmode.Event, client *socketmode.Client) {
	fmt.Println("Connected to Slack with Socket Mode.")
}

func (b *Bot) middlewareHello(evt *socketmode.Event, client *socketmode.Client) {
	fmt.Println("Received a hello message. Howdy to you too.")
}

func (b *Bot) middlewareEventsAPI(evt *socketmode.Event, client *socketmode.Client) {
	fmt.Println("middlewareEventsAPI")
	eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
	if !ok {
		fmt.Printf("Ignored %+v\n", evt)
		return
	}

	fmt.Printf("Event received: %+v\n", eventsAPIEvent)

	client.Ack(*evt.Request)

	switch eventsAPIEvent.Type {
	case slackevents.CallbackEvent:
		innerEvent := eventsAPIEvent.InnerEvent
		switch ev := innerEvent.Data.(type) {
		case *slackevents.AppMentionEvent:
			b.handleAppMention(ev)
		case *slackevents.MessageEvent:
			b.handleMessage(ev)
		case *slackevents.MemberJoinedChannelEvent:
			fmt.Printf("user %q joined to channel %q", ev.User, ev.Channel)
		}
	default:
		client.Debugf("unsupported Events API event received")
	}
}

func (b *Bot) middlewareAppMentionEvent(evt *socketmode.Event, client *socketmode.Client) {
	eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
	if !ok {
		fmt.Printf("Ignored %+v\n", evt)
		return
	}

	client.Ack(*evt.Request)

	ev, ok := eventsAPIEvent.InnerEvent.Data.(*slackevents.AppMentionEvent)
	if !ok {
		fmt.Printf("Ignored %+v\n", ev)
		return
	}

	b.handleAppMention(ev)
}

func (b *Bot) middlewareMessageEvent(evt *socketmode.Event, client *socketmode.Client) {
	eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
	if !ok {
		fmt.Printf("Ignored %+v\n", evt)
		return
	}

	client.Ack(*evt.Request)

	ev, ok := eventsAPIEvent.InnerEvent.Data.(*slackevents.MessageEvent)
	if !ok {
		fmt.Printf("Ignored %+v\n", ev)
		return
	}

	b.handleMessage(ev)
}

func (b *Bot) middlewareFileShared(evt *socketmode.Event, client *socketmode.Client) {
	eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
	if !ok {
		fmt.Printf("Ignored %+v\n", evt)
		return
	}

	client.Ack(*evt.Request)

	ev, ok := eventsAPIEvent.InnerEvent.Data.(*slackevents.FileSharedEvent)
	if !ok {
		fmt.Printf("Ignored %+v\n", ev)
		return
	}

	b.handleFileShared(ev)
}

func (b *Bot) handleAppMention(ev *slackevents.AppMentionEvent) {
	// Skip DMs (handled by message event)
	if strings.HasPrefix(ev.Channel, "D") {
		return
	}

	slackEvent := SlackEvent{
		Type:    "mention",
		Channel: ev.Channel,
		Ts:      ev.TimeStamp,
		User:    ev.User,
		Text:    removeBotMention(ev.Text),
		Files:   []FileDetails{}, // App mentions don't have embedded files
	}

	// Log to log.jsonl (ALWAYS, even for old messages)
	// Also downloads attachments in background and stores local paths
	slackEvent.Attachments = b.logUserMessage(slackEvent)

	// Only process messages AFTER startup (not replayed old messages)
	if b.isOldMessage(ev.TimeStamp) {
		log.Printf("[%s] Logged old message (pre-startup): %s", ev.Channel, slackEvent.Text[:30])
		return
	}

	// Process the event
	b.handleEvent(slackEvent)
}

func (b *Bot) handleMessage(ev *slackevents.MessageEvent) {
	// Skip bot messages, edits, etc.
	if ev.BotID != "" || ev.User == "" || ev.User == b.botUserID {
		return
	}
	if ev.SubType != "" && ev.SubType != "file_share" {
		return
	}
	if ev.Text == "" && ev.SubType != "file_share" {
		return
	}

	isDM := ev.ChannelType == "im"
	isBotMention := strings.Contains(ev.Text, "<@"+b.botUserID+">")

	// Skip channel @mentions - already handled by app_mention event
	if !isDM && isBotMention {
		return
	}

	eventType := "message"
	if isDM {
		eventType = "dm"
	}
	if ev.SubType == "file_share" {
		eventType = "file_share"
	}

	var files []FileDetails
	if ev.SubType == "file_share" && ev.Message != nil {
		// Extract files from the message for file shares
		files = b.extractFilesFromMessage(ev.Message)
	}

	text := removeBotMention(ev.Text)
	if ev.SubType == "file_share" && len(files) > 0 {
		text = fmt.Sprintf("File shared: %s", files[0].Name)
	}

	slackEvent := SlackEvent{
		Type:    eventType,
		Channel: ev.Channel,
		Ts:      ev.TimeStamp,
		User:    ev.User,
		Text:    text,
		Files:   files,
	}

	// Log to log.jsonl (ALL messages - channel chatter and DMs)
	// Also downloads attachments in background and stores local paths
	slackEvent.Attachments = b.logUserMessage(slackEvent)

	// Only process messages AFTER startup (not replayed old messages)
	if b.isOldMessage(ev.TimeStamp) {
		log.Printf("[%s] Skipping old message (pre-startup): %s", ev.Channel, slackEvent.Text[:30])
		return
	}

	// Only process DMs and file shares
	if isDM || ev.SubType == "file_share" {
		b.handleEvent(slackEvent)
	}
}

func (b *Bot) middlewareInteractive(evt *socketmode.Event, client *socketmode.Client) {
	callback, ok := evt.Data.(slack.InteractionCallback)
	if !ok {
		fmt.Printf("Ignored %+v\n", evt)
		return
	}

	fmt.Printf("Interaction received: %+v\n", callback)

	var payload interface{}

	switch callback.Type {
	case slack.InteractionTypeBlockActions:
		// See https://api.slack.com/apis/connections/socket-implement#button
		client.Debugf("button clicked!")
	case slack.InteractionTypeShortcut:
	case slack.InteractionTypeViewSubmission:
		// See https://api.slack.com/apis/connections/socket-implement#modal
	case slack.InteractionTypeDialogSubmission:
	default:

	}

	client.Ack(*evt.Request, payload)
}

func (b *Bot) middlewareInteractionTypeBlockActions(evt *socketmode.Event, client *socketmode.Client) {
	client.Debugf("button clicked!")
}

func (b *Bot) middlewareSlashCommand(evt *socketmode.Event, client *socketmode.Client) {
	cmd, ok := evt.Data.(slack.SlashCommand)
	if !ok {
		fmt.Printf("Ignored %+v\n", evt)
		return
	}

	client.Debugf("Slash command received: %+v", cmd)

	payload := map[string]interface{}{
		"blocks": []slack.Block{
			slack.NewSectionBlock(
				&slack.TextBlockObject{
					Type: slack.MarkdownType,
					Text: "foo",
				},
				nil,
				slack.NewAccessory(
					slack.NewButtonBlockElement(
						"",
						"somevalue",
						&slack.TextBlockObject{
							Type: slack.PlainTextType,
							Text: "bar",
						},
					),
				),
			),
		},
	}
	client.Ack(*evt.Request, payload)
}

// Helper methods

func (b *Bot) isOldMessage(ts string) bool {
	if b.startupTs == "" {
		return false
	}
	return ts < b.startupTs
}

func (b *Bot) postMessage(channel, text string) {
	_, _, err := b.api.PostMessage(channel, slack.MsgOptionText(text, false))
	if err != nil {
		log.Printf("Failed to post message: %v", err)
	}
}

func (b *Bot) handleEvent(event SlackEvent) {
	// TODO: Implement actual event processing logic
	log.Printf("Processing event: %+v", event)
	b.postMessage(event.Channel, fmt.Sprintf("Processed: %s", event.Text))
}

func (b *Bot) logUserMessage(event SlackEvent) []string {
	// Log message to log.jsonl
	eventJSON, err := json.Marshal(event)
	if err != nil {
		log.Printf("Failed to marshal event: %v", err)
		return []string{}
	}

	// Append to log.jsonl file
	f, err := os.OpenFile("log.jsonl", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Failed to open log file: %v", err)
		return []string{}
	}
	defer f.Close()

	if _, err := f.WriteString(string(eventJSON) + "\n"); err != nil {
		log.Printf("Failed to write to log file: %v", err)
		return []string{}
	}

	// TODO: Implement file download and attachment processing
	log.Printf("Logged message: %s", event.Text)
	return []string{} // Return local file paths of downloaded attachments
}

func (b *Bot) handleFileShared(ev *slackevents.FileSharedEvent) {
	// Get file details from Slack API
	fileInfo, _, _, err := b.api.GetFileInfo(ev.FileID, 1, 1)
	if err != nil {
		log.Printf("Failed to get file info: %v", err)
		return
	}

	fileDetails := b.convertToFileDetails(fileInfo)

	slackEvent := SlackEvent{
		Type:    "file_shared",
		Channel: ev.ChannelID,
		Ts:      ev.EventTimestamp,
		User:    ev.UserID,
		Text:    fmt.Sprintf("File shared: %s", fileInfo.Name),
		Files:   []FileDetails{fileDetails},
	}

	// Log the file share event
	b.logUserMessage(slackEvent)
}

func (b *Bot) extractFilesFromMessage(msg *slack.Msg) []FileDetails {
	if msg == nil || len(msg.Files) == 0 {
		return []FileDetails{}
	}

	var files []FileDetails
	for _, file := range msg.Files {
		files = append(files, b.convertToFileDetails(&file))
	}
	return files
}

func (b *Bot) convertToFileDetails(file *slack.File) FileDetails {
	return FileDetails{
		ID:                 file.ID,
		Name:               file.Name,
		Title:              file.Title,
		Mimetype:           file.Mimetype,
		Filetype:           file.Filetype,
		Size:               int64(file.Size),
		URLPrivate:         file.URLPrivate,
		URLPrivateDownload: file.URLPrivateDownload,
		Permalink:          file.Permalink,
		PermalinkPublic:    file.PermalinkPublic,
	}
}

func removeBotMention(text string) string {
	// Removes <@U123ABC>
	return strings.TrimSpace(
		regexp.MustCompile(`<@[A-Z0-9]+>`).ReplaceAllString(text, ""),
	)
}
