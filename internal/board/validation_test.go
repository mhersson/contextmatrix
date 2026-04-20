package board

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testProjectConfigForValidation() *ProjectConfig {
	return &ProjectConfig{
		Name:       "test",
		Prefix:     "TEST",
		NextID:     1,
		States:     []string{"todo", "in_progress", "review", "done", "stalled", "not_planned"},
		Types:      []string{"task", "bug", "feature"},
		Priorities: []string{"low", "medium", "high", "critical"},
		Transitions: map[string][]string{
			"todo":        {"in_progress"},
			"in_progress": {"review", "todo"},
			"review":      {"done", "in_progress"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
			"not_planned": {"todo"},
		},
	}
}

func TestValidateType(t *testing.T) {
	v := NewValidator()
	cfg := testProjectConfigForValidation()

	tests := []struct {
		name         string
		cardType     string
		wantErr      bool
		wantSentinel error
	}{
		{"valid task", "task", false, nil},
		{"valid bug", "bug", false, nil},
		{"valid feature", "feature", false, nil},
		// subtask is always valid as a built-in type even though it's not in cfg.Types
		{"built-in subtask", "subtask", false, nil},
		{"invalid type", "epic", true, ErrInvalidType},
		{"empty type", "", true, ErrInvalidType},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateType(cfg, tt.cardType)
			if tt.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.wantSentinel)

				var ve *ValidationError
				require.ErrorAs(t, err, &ve)
				assert.Equal(t, "type", ve.Field)
				assert.Equal(t, tt.cardType, ve.Value)
				assert.Equal(t, cfg.Types, ve.Allowed)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateState(t *testing.T) {
	v := NewValidator()
	cfg := testProjectConfigForValidation()

	tests := []struct {
		name         string
		cardState    string
		wantErr      bool
		wantSentinel error
	}{
		{"valid todo", "todo", false, nil},
		{"valid in_progress", "in_progress", false, nil},
		{"valid stalled", "stalled", false, nil},
		{"invalid state", "cancelled", true, ErrInvalidState},
		{"empty state", "", true, ErrInvalidState},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateState(cfg, tt.cardState)
			if tt.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.wantSentinel)

				var ve *ValidationError
				require.ErrorAs(t, err, &ve)
				assert.Equal(t, "state", ve.Field)
				assert.Equal(t, tt.cardState, ve.Value)
				assert.Equal(t, cfg.States, ve.Allowed)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidatePriority(t *testing.T) {
	v := NewValidator()
	cfg := testProjectConfigForValidation()

	tests := []struct {
		name         string
		priority     string
		wantErr      bool
		wantSentinel error
	}{
		{"valid low", "low", false, nil},
		{"valid medium", "medium", false, nil},
		{"valid high", "high", false, nil},
		{"valid critical", "critical", false, nil},
		{"invalid priority", "urgent", true, ErrInvalidPriority},
		{"empty priority", "", true, ErrInvalidPriority},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidatePriority(cfg, tt.priority)
			if tt.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.wantSentinel)

				var ve *ValidationError
				require.ErrorAs(t, err, &ve)
				assert.Equal(t, "priority", ve.Field)
				assert.Equal(t, tt.priority, ve.Value)
				assert.Equal(t, cfg.Priorities, ve.Allowed)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateTransition_ExplicitRules(t *testing.T) {
	v := NewValidator()
	cfg := testProjectConfigForValidation()

	tests := []struct {
		name    string
		from    string
		to      string
		wantErr bool
	}{
		// Valid explicit transitions
		{"todo to in_progress", "todo", "in_progress", false},
		{"in_progress to review", "in_progress", "review", false},
		{"in_progress to todo", "in_progress", "todo", false},
		{"review to done", "review", "done", false},
		{"review to in_progress", "review", "in_progress", false},
		{"done to todo", "done", "todo", false},

		// Invalid transitions
		{"todo to done", "todo", "done", true},
		{"todo to review", "todo", "review", true},
		{"done to in_progress", "done", "in_progress", true},
		{"done to review", "done", "review", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateTransition(cfg, tt.from, tt.to)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrInvalidTransition)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateTransition_StalledRules(t *testing.T) {
	v := NewValidator()
	cfg := testProjectConfigForValidation()

	tests := []struct {
		name    string
		from    string
		to      string
		wantErr bool
	}{
		// Any state can transition TO stalled
		{"todo to stalled", "todo", "stalled", false},
		{"in_progress to stalled", "in_progress", "stalled", false},
		{"review to stalled", "review", "stalled", false},
		{"done to stalled", "done", "stalled", false},

		// FROM stalled follows Transitions["stalled"]
		{"stalled to todo", "stalled", "todo", false},
		{"stalled to in_progress", "stalled", "in_progress", false},
		{"stalled to review", "stalled", "review", true}, // not in Transitions["stalled"]
		{"stalled to done", "stalled", "done", true},     // not in Transitions["stalled"]

		// stalled to stalled is same-state (valid)
		{"stalled to stalled", "stalled", "stalled", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateTransition(cfg, tt.from, tt.to)
			if tt.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, ErrInvalidTransition)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateTransition_NotPlannedRules(t *testing.T) {
	v := NewValidator()
	cfg := testProjectConfigForValidation()

	tests := []struct {
		name    string
		from    string
		to      string
		wantErr bool
	}{
		// not_planned follows normal transition rules (only explicit transitions)
		// In test config, only todo does NOT list not_planned, so todo->not_planned fails
		{"todo to not_planned", "todo", "not_planned", true},               // not in Transitions["todo"]
		{"in_progress to not_planned", "in_progress", "not_planned", true}, // not in Transitions["in_progress"]
		{"done to not_planned", "done", "not_planned", true},               // not in Transitions["done"]

		// FROM not_planned follows Transitions["not_planned"]
		{"not_planned to todo", "not_planned", "todo", false},
		{"not_planned to in_progress", "not_planned", "in_progress", true}, // not in Transitions["not_planned"]
		{"not_planned to done", "not_planned", "done", true},               // not in Transitions["not_planned"]

		// not_planned to not_planned is same-state (valid)
		{"not_planned to not_planned", "not_planned", "not_planned", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateTransition(cfg, tt.from, tt.to)
			if tt.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, ErrInvalidTransition)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateTransition_SameState(t *testing.T) {
	v := NewValidator()
	cfg := testProjectConfigForValidation()

	for _, state := range cfg.States {
		t.Run(state+" to "+state, func(t *testing.T) {
			err := v.ValidateTransition(cfg, state, state)
			assert.NoError(t, err)
		})
	}
}

func TestValidateTransition_InvalidStates(t *testing.T) {
	v := NewValidator()
	cfg := testProjectConfigForValidation()

	tests := []struct {
		name  string
		from  string
		to    string
		field string
	}{
		{"invalid from_state", "invalid", "todo", "from_state"},
		{"invalid to_state", "todo", "invalid", "to_state"},
		{"both invalid", "invalid1", "invalid2", "from_state"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateTransition(cfg, tt.from, tt.to)
			require.Error(t, err)
			require.ErrorIs(t, err, ErrInvalidState)

			var ve *ValidationError
			require.ErrorAs(t, err, &ve)
			assert.Equal(t, tt.field, ve.Field)
		})
	}
}

func TestValidateCard(t *testing.T) {
	v := NewValidator()
	cfg := testProjectConfigForValidation()
	now := time.Now()

	validCard := &Card{
		ID:       "TEST-001",
		Title:    "Test card",
		Project:  "test",
		Type:     "task",
		State:    "todo",
		Priority: "medium",
		Created:  now,
		Updated:  now,
	}

	t.Run("valid card", func(t *testing.T) {
		err := v.ValidateCard(cfg, validCard)
		assert.NoError(t, err)
	})

	t.Run("invalid type", func(t *testing.T) {
		card := *validCard
		card.Type = "epic"
		err := v.ValidateCard(cfg, &card)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidType)
	})

	t.Run("invalid state", func(t *testing.T) {
		card := *validCard
		card.State = "cancelled"
		err := v.ValidateCard(cfg, &card)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidState)
	})

	t.Run("invalid priority", func(t *testing.T) {
		card := *validCard
		card.Priority = "urgent"
		err := v.ValidateCard(cfg, &card)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidPriority)
	})

	t.Run("malicious source.external_url rejected by ValidateCard", func(t *testing.T) {
		card := *validCard
		card.Source = &Source{System: "jira", ExternalURL: "javascript:alert(1)"}
		err := v.ValidateCard(cfg, &card)
		require.Error(t, err)
		require.ErrorIs(t, err, ErrInvalidExternalURL)

		var ve *ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "source.external_url", ve.Field)
	})

	t.Run("valid https source passes ValidateCard", func(t *testing.T) {
		card := *validCard
		card.Source = &Source{System: "github", ExternalURL: "https://github.com/org/repo/issues/1"}
		err := v.ValidateCard(cfg, &card)
		assert.NoError(t, err)
	})
}

func TestCanTransition(t *testing.T) {
	v := NewValidator()
	cfg := testProjectConfigForValidation()

	assert.True(t, v.CanTransition(cfg, "todo", "in_progress"))
	assert.True(t, v.CanTransition(cfg, "todo", "stalled"))
	assert.False(t, v.CanTransition(cfg, "todo", "not_planned")) // not in Transitions["todo"]
	assert.False(t, v.CanTransition(cfg, "done", "not_planned")) // not in Transitions["done"]
	assert.False(t, v.CanTransition(cfg, "todo", "done"))
	assert.False(t, v.CanTransition(cfg, "invalid", "todo"))
}

func TestAllowedTransitions(t *testing.T) {
	v := NewValidator()
	cfg := testProjectConfigForValidation()

	tests := []struct {
		name     string
		from     string
		expected []string
	}{
		{
			name:     "from todo includes stalled",
			from:     "todo",
			expected: []string{"in_progress", "stalled"},
		},
		{
			name:     "from in_progress includes stalled",
			from:     "in_progress",
			expected: []string{"review", "todo", "stalled"},
		},
		{
			name:     "from stalled does NOT add stalled",
			from:     "stalled",
			expected: []string{"todo", "in_progress"},
		},
		{
			name:     "from not_planned includes stalled (auto-injected)",
			from:     "not_planned",
			expected: []string{"todo", "stalled"},
		},
		{
			name:     "invalid state returns nil",
			from:     "invalid",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed := v.AllowedTransitions(cfg, tt.from)
			assert.ElementsMatch(t, tt.expected, allowed)
		})
	}
}

func TestFindShortestPath(t *testing.T) {
	v := NewValidator()
	cfg := testProjectConfigForValidation()

	tests := []struct {
		name     string
		from     string
		to       string
		wantPath []string
		wantErr  error
	}{
		{"todo to done", "todo", "done", []string{"in_progress", "review", "done"}, nil},
		{"in_progress to done", "in_progress", "done", []string{"review", "done"}, nil},
		{"review to done", "review", "done", []string{"done"}, nil},
		{"already at target", "done", "done", nil, nil},
		{"stalled to done", "stalled", "done", []string{"in_progress", "review", "done"}, nil},
		{"todo to in_progress", "todo", "in_progress", []string{"in_progress"}, nil},
		{"done to in_progress", "done", "in_progress", []string{"todo", "in_progress"}, nil},
		{"invalid from state", "invalid", "done", nil, ErrInvalidState},
		{"invalid to state", "todo", "invalid", nil, ErrInvalidState},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, err := v.FindShortestPath(cfg, tt.from, tt.to)
			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantPath, path)
			}
		})
	}
}

func TestFindShortestPath_NoPath(t *testing.T) {
	v := NewValidator()
	// stalled and not_planned have no outgoing transitions, so b -> stalled is a dead end
	cfg := &ProjectConfig{
		Name:       "test",
		Prefix:     "TEST",
		States:     []string{"a", "b", "stalled", "not_planned"},
		Types:      []string{"task"},
		Priorities: []string{"low"},
		Transitions: map[string][]string{
			"a":           {"b"},
			"b":           {},
			"stalled":     {},
			"not_planned": {},
		},
	}

	path, err := v.FindShortestPath(cfg, "b", "a")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNoPath)
	assert.Nil(t, path)
}

func TestValidateType_SubtaskIsBuiltIn(t *testing.T) {
	v := NewValidator()

	// Project config that does NOT include "subtask" in its types list
	cfg := &ProjectConfig{
		Name:       "test",
		Prefix:     "TEST",
		States:     []string{"todo", "in_progress", "done", "stalled", "not_planned"},
		Types:      []string{"task", "bug"},
		Priorities: []string{"low", "medium", "high"},
		Transitions: map[string][]string{
			"todo":        {"in_progress"},
			"in_progress": {"done"},
			"done":        {"todo"},
			"stalled":     {"todo"},
			"not_planned": {"todo"},
		},
	}

	// subtask must be valid even though it's not in cfg.Types
	err := v.ValidateType(cfg, SubtaskType)
	assert.NoError(t, err)

	// Regular types still work
	assert.NoError(t, v.ValidateType(cfg, "task"))
	assert.NoError(t, v.ValidateType(cfg, "bug"))

	// Unknown types still fail
	require.Error(t, v.ValidateType(cfg, "epic"))
	assert.ErrorIs(t, v.ValidateType(cfg, "epic"), ErrInvalidType)
}

func TestValidationError_Unwrap(t *testing.T) {
	ve := &ValidationError{
		Err:     ErrInvalidType,
		Field:   "type",
		Value:   "epic",
		Allowed: []string{"task", "bug"},
		Message: "invalid type",
	}

	require.ErrorIs(t, ve, ErrInvalidType)
	require.NotErrorIs(t, ve, ErrInvalidState)

	var unwrapped *ValidationError
	require.ErrorAs(t, ve, &unwrapped)
	assert.Equal(t, "type", unwrapped.Field)
}

func TestValidateAutonomousFields(t *testing.T) {
	v := NewValidator()

	tests := []struct {
		name    string
		card    Card
		wantErr bool
	}{
		{
			name:    "all false is valid",
			card:    Card{},
			wantErr: false,
		},
		{
			name:    "autonomous alone is valid",
			card:    Card{Autonomous: true},
			wantErr: false,
		},
		{
			name:    "feature_branch alone is valid",
			card:    Card{FeatureBranch: true},
			wantErr: false,
		},
		{
			name:    "create_pr with feature_branch is valid",
			card:    Card{FeatureBranch: true, CreatePR: true},
			wantErr: false,
		},
		{
			name:    "create_pr without feature_branch is invalid",
			card:    Card{CreatePR: true},
			wantErr: true,
		},
		{
			name:    "all enabled is valid",
			card:    Card{Autonomous: true, FeatureBranch: true, CreatePR: true, BranchName: "test-001/card"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateAutonomousFields(&tt.card)
			if tt.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, ErrInvalidAutonomousConfig)

				var ve *ValidationError
				require.ErrorAs(t, err, &ve)
				assert.Equal(t, "create_pr", ve.Field)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateCard_AutonomousFields(t *testing.T) {
	v := NewValidator()
	cfg := testProjectConfigForValidation()
	now := time.Now()

	t.Run("create_pr without feature_branch rejected by ValidateCard", func(t *testing.T) {
		card := &Card{
			ID: "TEST-001", Title: "Test", Project: "test",
			Type: "task", State: "todo", Priority: "medium",
			Created: now, Updated: now,
			CreatePR: true,
		}
		err := v.ValidateCard(cfg, card)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidAutonomousConfig)
	})

	t.Run("valid autonomous card passes ValidateCard", func(t *testing.T) {
		card := &Card{
			ID: "TEST-001", Title: "Test", Project: "test",
			Type: "task", State: "todo", Priority: "medium",
			Created: now, Updated: now,
			Autonomous: true, FeatureBranch: true, CreatePR: true,
		}
		err := v.ValidateCard(cfg, card)
		assert.NoError(t, err)
	})
}

func TestValidateSource(t *testing.T) {
	v := NewValidator()

	tests := []struct {
		name         string
		source       *Source
		wantErr      bool
		wantSentinel error
	}{
		{"nil source", nil, false, nil},
		{"empty external_url", &Source{System: "jira", ExternalURL: ""}, false, nil},
		{"https url", &Source{ExternalURL: "https://example.com/x"}, false, nil},
		{"http url", &Source{ExternalURL: "http://example.com/x"}, false, nil},
		{"uppercase HTTPS scheme", &Source{ExternalURL: "HTTPS://EXAMPLE.COM"}, false, nil},
		{"mixed case Https scheme", &Source{ExternalURL: "Https://example.com"}, false, nil},
		{"javascript scheme", &Source{ExternalURL: "javascript:alert(1)"}, true, ErrInvalidExternalURL},
		{"data scheme", &Source{ExternalURL: "data:text/html,<h1>hi</h1>"}, true, ErrInvalidExternalURL},
		{"vbscript scheme", &Source{ExternalURL: "vbscript:msgbox(1)"}, true, ErrInvalidExternalURL},
		{"no scheme (plain text)", &Source{ExternalURL: "not a url at all"}, true, ErrInvalidExternalURL},
		{"ftp scheme", &Source{ExternalURL: "ftp://example.com/file"}, true, ErrInvalidExternalURL},
		{"file scheme", &Source{ExternalURL: "file:///etc/passwd"}, true, ErrInvalidExternalURL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			card := &Card{Source: tt.source}

			err := v.ValidateSource(card)
			if tt.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.wantSentinel)

				var ve *ValidationError
				require.ErrorAs(t, err, &ve)
				assert.Equal(t, "source.external_url", ve.Field)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidationError_Message(t *testing.T) {
	ve := &ValidationError{
		Err:     ErrInvalidType,
		Field:   "type",
		Value:   "epic",
		Allowed: []string{"task", "bug"},
		Message: "invalid type \"epic\"; valid types: [task bug]",
	}

	assert.Equal(t, "invalid type \"epic\"; valid types: [task bug]", ve.Error())
}

func TestValidateRunnerCallbackStatus(t *testing.T) {
	v := NewValidator()

	t.Run("accepts running", func(t *testing.T) {
		assert.NoError(t, v.ValidateRunnerCallbackStatus("running"))
	})

	t.Run("accepts failed", func(t *testing.T) {
		assert.NoError(t, v.ValidateRunnerCallbackStatus("failed"))
	})

	t.Run("accepts completed", func(t *testing.T) {
		assert.NoError(t, v.ValidateRunnerCallbackStatus("completed"))
	})

	t.Run("rejects queued", func(t *testing.T) {
		assert.Error(t, v.ValidateRunnerCallbackStatus("queued"))
	})

	t.Run("rejects killed", func(t *testing.T) {
		assert.Error(t, v.ValidateRunnerCallbackStatus("killed"))
	})

	t.Run("rejects unknown", func(t *testing.T) {
		assert.Error(t, v.ValidateRunnerCallbackStatus("unknown"))
	})
}

func TestValidateRunnerStatus(t *testing.T) {
	v := NewValidator()

	t.Run("accepts empty string", func(t *testing.T) {
		assert.NoError(t, v.ValidateRunnerStatus(""))
	})

	t.Run("accepts queued", func(t *testing.T) {
		assert.NoError(t, v.ValidateRunnerStatus("queued"))
	})

	t.Run("accepts running", func(t *testing.T) {
		assert.NoError(t, v.ValidateRunnerStatus("running"))
	})

	t.Run("accepts failed", func(t *testing.T) {
		assert.NoError(t, v.ValidateRunnerStatus("failed"))
	})

	t.Run("accepts completed", func(t *testing.T) {
		assert.NoError(t, v.ValidateRunnerStatus("completed"))
	})

	t.Run("accepts killed", func(t *testing.T) {
		assert.NoError(t, v.ValidateRunnerStatus("killed"))
	})

	t.Run("rejects invalid status", func(t *testing.T) {
		err := v.ValidateRunnerStatus("invalid")
		require.Error(t, err)

		var ve *ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "runner_status", ve.Field)
	})
}
