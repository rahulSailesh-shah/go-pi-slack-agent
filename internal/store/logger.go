package store

type MessageLogger interface {
	LogMessage(msg Message) (bool, error)
	Close() error
}
