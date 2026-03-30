package storage

import (
	"context"
	"errors"

	"github.com/mhersson/contextmatrix/internal/board"
)

// Sentinel errors for storage operations.
var (
	// ErrProjectNotFound is returned when a project directory or .board.yaml does not exist.
	ErrProjectNotFound = errors.New("project not found")

	// ErrCardNotFound is returned when a card file does not exist.
	ErrCardNotFound = errors.New("card not found")

	// ErrCardExists is returned when attempting to create a card that already exists.
	ErrCardExists = errors.New("card already exists")
)

// CardFilter specifies filtering criteria for listing cards.
// All fields are optional; empty strings mean "no filter".
type CardFilter struct {
	State         string
	Type          string
	Priority      string
	AssignedAgent string
	Label         string
	Parent        string
	ExternalID    string
}

// Store defines the interface for card persistence.
type Store interface {
	// ListProjects returns all discovered projects.
	ListProjects(ctx context.Context) ([]board.ProjectConfig, error)

	// GetProject returns the configuration for a specific project.
	// Returns ErrProjectNotFound if the project does not exist.
	GetProject(ctx context.Context, name string) (*board.ProjectConfig, error)

	// SaveProject persists a project configuration.
	// Creates the project directory if it does not exist.
	SaveProject(ctx context.Context, cfg *board.ProjectConfig) error

	// ListCards returns all cards in a project matching the filter.
	// Returns ErrProjectNotFound if the project does not exist.
	ListCards(ctx context.Context, project string, filter CardFilter) ([]*board.Card, error)

	// GetCard returns a specific card.
	// Returns ErrProjectNotFound if the project does not exist.
	// Returns ErrCardNotFound if the card does not exist.
	GetCard(ctx context.Context, project, id string) (*board.Card, error)

	// CreateCard persists a new card.
	// Returns ErrProjectNotFound if the project does not exist.
	// Returns ErrCardExists if a card with the same ID already exists.
	CreateCard(ctx context.Context, project string, card *board.Card) error

	// UpdateCard persists changes to an existing card.
	// Returns ErrProjectNotFound if the project does not exist.
	// Returns ErrCardNotFound if the card does not exist.
	UpdateCard(ctx context.Context, project string, card *board.Card) error

	// DeleteCard removes a card.
	// Returns ErrProjectNotFound if the project does not exist.
	// Returns ErrCardNotFound if the card does not exist.
	DeleteCard(ctx context.Context, project, id string) error
}
