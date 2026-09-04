package data

import "github.com/google/uuid"

type UUIDv7Generator struct{}

func NewUUIDv7Generator() UUIDv7Generator {
	return UUIDv7Generator{}
}

func (UUIDv7Generator) NewID() (uuid.UUID, error) {
	return uuid.NewV7()
}
