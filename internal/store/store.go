package store

type Store interface {
	LogMessage(msg Message) (bool, error)
	Close() error
}
