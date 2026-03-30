package board

import (
	"errors"
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
		States:     []string{"todo", "in_progress", "review", "done", "stalled"},
		Types:      []string{"task", "bug", "feature"},
		Priorities: []string{"low", "medium", "high", "critical"},
		Transitions: map[string][]string{
			"todo":        {"in_progress"},
			"in_progress": {"review", "todo"},
			"review":      {"done", "in_progress"},
			"done":        {"todo"},
			"stalled":     {"todo", "in_progress"},
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
		{"invalid type", "epic", true, ErrInvalidType},
		{"empty type", "", true, ErrInvalidType},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateType(cfg, tt.cardType)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantSentinel)

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
				assert.ErrorIs(t, err, tt.wantSentinel)

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
				assert.ErrorIs(t, err, tt.wantSentinel)

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
		{"stalled to review", "stalled", "review", true},  // not in Transitions["stalled"]
		{"stalled to done", "stalled", "done", true},      // not in Transitions["stalled"]

		// stalled to stalled is same-state (valid)
		{"stalled to stalled", "stalled", "stalled", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateTransition(cfg, tt.from, tt.to)
			if tt.wantErr {
				assert.Error(t, err)
				assert.ErrorIs(t, err, ErrInvalidTransition)
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
			assert.ErrorIs(t, err, ErrInvalidState)

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
}

func TestCanTransition(t *testing.T) {
	v := NewValidator()
	cfg := testProjectConfigForValidation()

	assert.True(t, v.CanTransition(cfg, "todo", "in_progress"))
	assert.True(t, v.CanTransition(cfg, "todo", "stalled"))
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

func TestValidationError_Unwrap(t *testing.T) {
	ve := &ValidationError{
		Err:     ErrInvalidType,
		Field:   "type",
		Value:   "epic",
		Allowed: []string{"task", "bug"},
		Message: "invalid type",
	}

	assert.True(t, errors.Is(ve, ErrInvalidType))
	assert.False(t, errors.Is(ve, ErrInvalidState))

	var unwrapped *ValidationError
	assert.True(t, errors.As(ve, &unwrapped))
	assert.Equal(t, "type", unwrapped.Field)
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
