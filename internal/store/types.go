package store

import "time"

type Message struct {
	ID          string
	ChannelID   string
	Timestamp   time.Time
	UserID      string
	UserName    string
	DisplayName string
	Text        string
	Attachments []Attachment
	IsBot       bool
	Platform    string
}

type File struct {
	Name        string
	URL         string
	ContentType string
}

type Attachment struct {
	Original string `json:"original"`
	Local    string `json:"local"`
}
