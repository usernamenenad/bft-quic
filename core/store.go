package core

type Store interface {
	AddMessage(msg Message) error
	GetMessagesByKey(key string) ([]Message, error)
}
