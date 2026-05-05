package session

type EntryStore interface {
	Load() (*Header, []Entry, error)
	Append(entry Entry) error
	Close() error
	IsNew() bool
}
