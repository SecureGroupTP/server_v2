package uuidx

import "github.com/google/uuid"

type Generator interface {
	New() uuid.UUID
}

type DefaultGenerator struct{}

func (DefaultGenerator) New() uuid.UUID {
	return uuid.New()
}
