package slack

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"slack-agent/internal/handler"
	"slack-agent/internal/media"
	msglog "slack-agent/internal/store"

	slacklib "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

type Client struct {
	api         *slacklib.Client
	socket      *socketmode.Client
	store       msglog.MessageLogger
	fileHandler media.FileHandler
	handler     handler.Handler
	botUserID   string
	startupTs   string
	poster      func(channelID, text string) error
}

type Config struct {
	AppToken    string
	BotToken    string
	Store       msglog.MessageLogger
	FileHandler media.FileHandler
	Handler     handler.Handler
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

func (c *Client) PostMessage(channelID, text string) error {
	return c.poster(channelID, text)
}

func (c *Client) Run() error {
	h := socketmode.NewSocketmodeHandler(c.socket)
	h.HandleEvents(slackevents.AppMention, c.onAppMention)
	h.HandleEvents(slackevents.Message, c.onMessage)
	return h.RunEventLoop()
}

func (c *Client) onAppMention(evt *socketmode.Event, client *socketmode.Client) {
	log.Printf("onAppMention: %v", evt)
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
	log.Printf("onMessage: %v", evt)
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

	storeFiles := toFiles(files)
	if len(storeFiles) > 0 {
		msg.Attachments = c.fileHandler.ProcessAttachments(msg.ChannelID, storeFiles, msg.ID)
	}

	if _, err := c.store.LogMessage(msg); err != nil {
		log.Printf("LogMessage: %v", err)
	}
	if msg.ID < c.startupTs {
		return
	}

	if !isDM && ev.SubType != "file_share" {
		return
	}

	c.dispatchToHandler(msg, storeFiles)
}

func (c *Client) dispatchToHandler(msg msglog.Message, files []msglog.File) {
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
