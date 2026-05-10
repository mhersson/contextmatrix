package storage

import (
	"context"
	"errors"

	"github.com/mhersson/contextmatrix/internal/board"
)

// Sentinel errors for storage operations.
var (
	// ErrProjectNotFound is re-exported from the board package so callers can
	// errors.Is against either sentinel; they point at the same underlying
	// error value. board.LoadProjectConfig returns board.ErrProjectNotFound,
	// which would otherwise bubble up through the service layer and miss the
	// storage.ErrProjectNotFound case in handleServiceError.
	ErrProjectNotFound = board.ErrProjectNotFound

	// ErrProjectExists is returned when attempting to create a project that already exists.
	ErrProjectExists = errors.New("project already exists")

	// ErrProjectHasCards is returned when attempting to delete a project that still contains cards.
	ErrProjectHasCards = errors.New("project has cards")

	// ErrCardNotFound is returned when a card file does not exist.
	ErrCardNotFound = errors.New("card not found")

	// ErrCardExists is returned when attempting to create a card that already exists.
	ErrCardExists = errors.New("card already exists")

	// ErrInvalidPath is returned when a path component could cause directory traversal.
	ErrInvalidPath = errors.New("invalid path component")

	// ErrKnowledgeDocNotFound is returned when a knowledge doc file does not exist.
	ErrKnowledgeDocNotFound = errors.New("knowledge doc not found")

	// ErrInvalidKnowledgeDoc is returned when a doc name is not one of the canonical KB doc names.
	ErrInvalidKnowledgeDoc = errors.New("invalid knowledge doc name")

	// ErrKnowledgeDocSymlink is returned when a doc path resolves to a symbolic link.
	ErrKnowledgeDocSymlink = errors.New("knowledge doc is a symlink (rejected)")

	// ErrInvalidInput is returned when caller-supplied parameters are structurally invalid.
	ErrInvalidInput = errors.New("invalid input")
)

// CardFilter specifies filtering criteria for listing cards.
// All fields are optional; empty strings mean "no filter".
// Vetted uses *bool to distinguish "no filter" (nil) from "filter by false".
type CardFilter struct {
	State         string
	Type          string
	Priority      string
	AssignedAgent string
	Label         string
	Parent        string
	ExternalID    string
	Vetted        *bool
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

	// DeleteProject removes a project and its directory from disk.
	// Returns ErrProjectNotFound if the project does not exist.
	DeleteProject(ctx context.Context, name string) error

	// ProjectCardCount returns the number of cards in a project.
	// Returns ErrProjectNotFound if the project does not exist.
	ProjectCardCount(ctx context.Context, name string) (int, error)

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

	// Knowledge base operations

	// ReadKnowledgeMeta returns the .meta.yaml for a project.
	// If the file does not exist (no KB built yet), returns an empty
	// KnowledgeMeta with SchemaVersion=1 and an empty Repos map (not an error).
	ReadKnowledgeMeta(ctx context.Context, project string) (*board.KnowledgeMeta, error)

	// WriteKnowledgeMeta persists the .meta.yaml.
	// Creates the knowledge directory if it does not exist.
	WriteKnowledgeMeta(ctx context.Context, project string, meta *board.KnowledgeMeta) error

	// ReadKnowledgeDoc reads a single KB doc.
	// Returns ErrInvalidKnowledgeDoc if doc is not a canonical name.
	// Returns ErrKnowledgeDocNotFound if the file does not exist.
	ReadKnowledgeDoc(ctx context.Context, project, repo, doc string) ([]byte, error)

	// WriteKnowledgeDoc writes a single KB doc, creating the repo subdirectory if needed.
	// Returns ErrInvalidKnowledgeDoc if doc is not a canonical name.
	WriteKnowledgeDoc(ctx context.Context, project, repo, doc string, content []byte) error

	// DeleteKnowledgeDoc removes a doc file.
	// Returns nil if the file does not exist.
	// Returns ErrInvalidKnowledgeDoc if doc is not a canonical name.
	DeleteKnowledgeDoc(ctx context.Context, project, repo, doc string) error

	// ListKnowledgeRepos returns the repo subdirectory names under <project>/knowledge/.
	// Returns nil (not an error) if the knowledge directory does not exist.
	ListKnowledgeRepos(ctx context.Context, project string) ([]string, error)

	// KnowledgeDocExists reports whether a doc file is present.
	// Returns ErrInvalidKnowledgeDoc if doc is not a canonical name.
	KnowledgeDocExists(ctx context.Context, project, repo, doc string) (bool, error)
}
