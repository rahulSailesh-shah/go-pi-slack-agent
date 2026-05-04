package session

// Store is the persistence contract for a session log.
// Implementations handle durability; Manager owns domain logic.
type Store interface {
	Load() (*Header, []Entry, error)
	Append(entry Entry) error
	Close() error
}
