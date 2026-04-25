package board

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCard_FullCard(t *testing.T) {
	created := time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 3, 30, 14, 30, 0, 0, time.UTC)
	heartbeat := time.Date(2026, 3, 30, 14, 30, 0, 0, time.UTC)

	input := `---
id: ALPHA-001
title: Implement user auth
project: project-alpha
type: task
state: in_progress
priority: high
assigned_agent: claude-7a3f
last_heartbeat: 2026-03-30T14:30:00Z
parent: ALPHA-000
subtasks:
  - ALPHA-003
  - ALPHA-004
depends_on:
  - ALPHA-002
context:
  - src/auth/
  - docs/auth-spec.md
labels:
  - backend
  - security
source:
  system: jira
  external_id: PROJ-1234
  external_url: https://company.atlassian.net/browse/PROJ-1234
custom:
  branch_name: feat/user-auth
token_usage:
  prompt_tokens: 12400
  completion_tokens: 3200
  estimated_cost_usd: 0.043
created: 2026-03-30T10:00:00Z
updated: 2026-03-30T14:30:00Z
activity_log:
  - agent: claude-7a3f
    ts: 2026-03-30T14:30:00Z
    action: status_update
    message: JWT middleware done
---
## Plan
This is the plan.

## Progress
- [x] JWT middleware
`

	card, err := ParseCard([]byte(input))
	require.NoError(t, err)

	assert.Equal(t, "ALPHA-001", card.ID)
	assert.Equal(t, "Implement user auth", card.Title)
	assert.Equal(t, "project-alpha", card.Project)
	assert.Equal(t, "task", card.Type)
	assert.Equal(t, "in_progress", card.State)
	assert.Equal(t, "high", card.Priority)
	assert.Equal(t, "claude-7a3f", card.AssignedAgent)
	require.NotNil(t, card.LastHeartbeat)
	assert.Equal(t, heartbeat, *card.LastHeartbeat)
	assert.Equal(t, "ALPHA-000", card.Parent)
	assert.Equal(t, []string{"ALPHA-003", "ALPHA-004"}, card.Subtasks)
	assert.Equal(t, []string{"ALPHA-002"}, card.DependsOn)
	assert.Equal(t, []string{"src/auth/", "docs/auth-spec.md"}, card.Context)
	assert.Equal(t, []string{"backend", "security"}, card.Labels)

	require.NotNil(t, card.Source)
	assert.Equal(t, "jira", card.Source.System)
	assert.Equal(t, "PROJ-1234", card.Source.ExternalID)
	assert.Equal(t, "https://company.atlassian.net/browse/PROJ-1234", card.Source.ExternalURL)

	assert.Equal(t, "feat/user-auth", card.Custom["branch_name"])

	require.NotNil(t, card.TokenUsage)
	assert.Equal(t, int64(12400), card.TokenUsage.PromptTokens)
	assert.Equal(t, int64(3200), card.TokenUsage.CompletionTokens)
	assert.InDelta(t, 0.043, card.TokenUsage.EstimatedCostUSD, 0.0001)

	assert.Equal(t, created, card.Created)
	assert.Equal(t, updated, card.Updated)

	require.Len(t, card.ActivityLog, 1)
	assert.Equal(t, "claude-7a3f", card.ActivityLog[0].Agent)
	assert.Equal(t, "status_update", card.ActivityLog[0].Action)
	assert.Equal(t, "JWT middleware done", card.ActivityLog[0].Message)

	assert.Contains(t, card.Body, "## Plan")
	assert.Contains(t, card.Body, "## Progress")
}

func TestParseCard_MinimalCard(t *testing.T) {
	input := `---
id: TEST-001
title: Test card
project: test-project
type: task
state: todo
priority: medium
created: 2026-03-30T10:00:00Z
updated: 2026-03-30T10:00:00Z
---
`

	card, err := ParseCard([]byte(input))
	require.NoError(t, err)

	assert.Equal(t, "TEST-001", card.ID)
	assert.Equal(t, "Test card", card.Title)
	assert.Empty(t, card.AssignedAgent)
	assert.Nil(t, card.LastHeartbeat)
	assert.Empty(t, card.Parent)
	assert.Nil(t, card.Subtasks)
	assert.Nil(t, card.DependsOn)
	assert.Nil(t, card.Source)
	assert.Nil(t, card.Custom)
	assert.Nil(t, card.TokenUsage)
	assert.Nil(t, card.ActivityLog)
	assert.Empty(t, card.Body)
}

func TestParseCard_EmptyBody(t *testing.T) {
	input := `---
id: TEST-001
title: Test card
project: test-project
type: task
state: todo
priority: medium
created: 2026-03-30T10:00:00Z
updated: 2026-03-30T10:00:00Z
---
`

	card, err := ParseCard([]byte(input))
	require.NoError(t, err)
	assert.Empty(t, card.Body)
}

func TestParseCard_BodyWithMarkdown(t *testing.T) {
	input := "---\nid: TEST-001\ntitle: Test\nproject: test\ntype: task\nstate: todo\npriority: medium\ncreated: 2026-03-30T10:00:00Z\nupdated: 2026-03-30T10:00:00Z\n---\n# Heading\n\n## Subheading\n\n```go\nfunc main() {\n    fmt.Println(\"hello\")\n}\n```\n\n- item 1\n- item 2\n"

	card, err := ParseCard([]byte(input))
	require.NoError(t, err)
	assert.Contains(t, card.Body, "# Heading")
	assert.Contains(t, card.Body, "```go")
	assert.Contains(t, card.Body, "- item 1")
}

func TestParseCard_MissingFrontmatter(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "no delimiters",
			input: "just some text",
		},
		{
			name:  "only opening delimiter",
			input: "---\nid: TEST-001",
		},
		{
			name:  "content before delimiter",
			input: "prefix\n---\nid: TEST-001\n---\nbody",
		},
		{
			name:  "empty frontmatter",
			input: "---\n---\nbody",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseCard([]byte(tt.input))
			assert.ErrorIs(t, err, ErrMissingFrontmatter)
		})
	}
}

func TestParseCard_MalformedYAML(t *testing.T) {
	input := `---
id: TEST-001
title: [unclosed bracket
---
body
`

	_, err := ParseCard([]byte(input))
	assert.ErrorIs(t, err, ErrMalformedFrontmatter)
}

func TestParseCard_ActivityLogMultipleEntries(t *testing.T) {
	input := `---
id: TEST-001
title: Test
project: test
type: task
state: todo
priority: medium
created: 2026-03-30T10:00:00Z
updated: 2026-03-30T10:00:00Z
activity_log:
  - agent: agent-1
    ts: 2026-03-30T10:00:00Z
    action: claimed
    message: Starting work
  - agent: agent-1
    ts: 2026-03-30T11:00:00Z
    action: status_update
    message: Halfway done
  - agent: agent-1
    ts: 2026-03-30T12:00:00Z
    action: completed
    message: Finished
---
`

	card, err := ParseCard([]byte(input))
	require.NoError(t, err)
	require.Len(t, card.ActivityLog, 3)
	assert.Equal(t, "claimed", card.ActivityLog[0].Action)
	assert.Equal(t, "status_update", card.ActivityLog[1].Action)
	assert.Equal(t, "completed", card.ActivityLog[2].Action)
}

func TestParseCard_WindowsLineEndings(t *testing.T) {
	input := "---\r\nid: TEST-001\r\ntitle: Test\r\nproject: test\r\ntype: task\r\nstate: todo\r\npriority: medium\r\ncreated: 2026-03-30T10:00:00Z\r\nupdated: 2026-03-30T10:00:00Z\r\n---\r\n## Body\r\nWith windows endings.\r\n"

	card, err := ParseCard([]byte(input))
	require.NoError(t, err)
	assert.Equal(t, "TEST-001", card.ID)
	assert.Contains(t, card.Body, "## Body")
}

func TestSerializeCard_Basic(t *testing.T) {
	created := time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC)

	card := &Card{
		ID:       "TEST-001",
		Title:    "Test card",
		Project:  "test-project",
		Type:     "task",
		State:    "todo",
		Priority: "medium",
		Created:  created,
		Updated:  created,
		Body:     "## Plan\nDo the thing.\n",
	}

	data, err := SerializeCard(card)
	require.NoError(t, err)

	// Verify structure
	str := string(data)
	assert.True(t, strings.HasPrefix(str, "---\n"), "should start with ---")
	assert.Contains(t, str, "id: TEST-001")
	assert.Contains(t, str, "title: Test card")
	assert.Contains(t, str, "---\n## Plan")
	assert.True(t, strings.HasSuffix(str, "\n"), "should end with newline")
}

func TestSerializeCard_OmitsEmptyFields(t *testing.T) {
	created := time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC)

	card := &Card{
		ID:       "TEST-001",
		Title:    "Test card",
		Project:  "test-project",
		Type:     "task",
		State:    "todo",
		Priority: "medium",
		Created:  created,
		Updated:  created,
	}

	data, err := SerializeCard(card)
	require.NoError(t, err)

	str := string(data)
	assert.NotContains(t, str, "assigned_agent")
	assert.NotContains(t, str, "last_heartbeat")
	assert.NotContains(t, str, "parent")
	assert.NotContains(t, str, "subtasks")
	assert.NotContains(t, str, "source")
	assert.NotContains(t, str, "custom")
	assert.NotContains(t, str, "autonomous")
	assert.NotContains(t, str, "feature_branch")
	assert.NotContains(t, str, "create_pr")
	assert.NotContains(t, str, "branch_name")
	assert.NotContains(t, str, "pr_url")
	assert.NotContains(t, str, "review_attempts")
	assert.NotContains(t, str, "token_usage")
	assert.NotContains(t, str, "activity_log")
}

func TestRoundTrip_FullCard(t *testing.T) {
	created := time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 3, 30, 14, 30, 0, 0, time.UTC)
	heartbeat := time.Date(2026, 3, 30, 14, 30, 0, 0, time.UTC)

	original := &Card{
		ID:            "ALPHA-001",
		Title:         "Implement user auth",
		Project:       "project-alpha",
		Type:          "task",
		State:         "in_progress",
		Priority:      "high",
		AssignedAgent: "claude-7a3f",
		LastHeartbeat: &heartbeat,
		Parent:        "ALPHA-000",
		Subtasks:      []string{"ALPHA-003", "ALPHA-004"},
		DependsOn:     []string{"ALPHA-002"},
		Context:       []string{"src/auth/", "docs/auth-spec.md"},
		Labels:        []string{"backend", "security"},
		Source: &Source{
			System:      "jira",
			ExternalID:  "PROJ-1234",
			ExternalURL: "https://company.atlassian.net/browse/PROJ-1234",
		},
		Custom: map[string]any{
			"branch_name": "feat/user-auth",
		},
		TokenUsage: &TokenUsage{
			PromptTokens:     12400,
			CompletionTokens: 3200,
			EstimatedCostUSD: 0.043,
		},
		Created: created,
		Updated: updated,
		ActivityLog: []ActivityEntry{
			{
				Agent:     "claude-7a3f",
				Timestamp: updated,
				Action:    "status_update",
				Message:   "JWT middleware done",
			},
		},
		Body: "## Plan\nThis is the plan.\n\n## Progress\n- [x] JWT middleware\n",
	}

	// Serialize
	data, err := SerializeCard(original)
	require.NoError(t, err)

	// Parse back
	parsed, err := ParseCard(data)
	require.NoError(t, err)

	// Compare
	assert.Equal(t, original.ID, parsed.ID)
	assert.Equal(t, original.Title, parsed.Title)
	assert.Equal(t, original.Project, parsed.Project)
	assert.Equal(t, original.Type, parsed.Type)
	assert.Equal(t, original.State, parsed.State)
	assert.Equal(t, original.Priority, parsed.Priority)
	assert.Equal(t, original.AssignedAgent, parsed.AssignedAgent)
	require.NotNil(t, parsed.LastHeartbeat)
	assert.Equal(t, *original.LastHeartbeat, *parsed.LastHeartbeat)
	assert.Equal(t, original.Parent, parsed.Parent)
	assert.Equal(t, original.Subtasks, parsed.Subtasks)
	assert.Equal(t, original.DependsOn, parsed.DependsOn)
	assert.Equal(t, original.Context, parsed.Context)
	assert.Equal(t, original.Labels, parsed.Labels)

	require.NotNil(t, parsed.Source)
	assert.Equal(t, original.Source.System, parsed.Source.System)
	assert.Equal(t, original.Source.ExternalID, parsed.Source.ExternalID)
	assert.Equal(t, original.Source.ExternalURL, parsed.Source.ExternalURL)

	assert.Equal(t, original.Custom["branch_name"], parsed.Custom["branch_name"])

	require.NotNil(t, parsed.TokenUsage)
	assert.Equal(t, original.TokenUsage.PromptTokens, parsed.TokenUsage.PromptTokens)
	assert.Equal(t, original.TokenUsage.CompletionTokens, parsed.TokenUsage.CompletionTokens)
	assert.InDelta(t, original.TokenUsage.EstimatedCostUSD, parsed.TokenUsage.EstimatedCostUSD, 0.0001)

	assert.Equal(t, original.Created, parsed.Created)
	assert.Equal(t, original.Updated, parsed.Updated)

	require.Len(t, parsed.ActivityLog, 1)
	assert.Equal(t, original.ActivityLog[0].Agent, parsed.ActivityLog[0].Agent)
	assert.Equal(t, original.ActivityLog[0].Timestamp, parsed.ActivityLog[0].Timestamp)
	assert.Equal(t, original.ActivityLog[0].Action, parsed.ActivityLog[0].Action)
	assert.Equal(t, original.ActivityLog[0].Message, parsed.ActivityLog[0].Message)

	assert.Equal(t, original.Body, parsed.Body)
}

func TestRoundTrip_MinimalCard(t *testing.T) {
	created := time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC)

	original := &Card{
		ID:       "TEST-001",
		Title:    "Test card",
		Project:  "test-project",
		Type:     "task",
		State:    "todo",
		Priority: "medium",
		Created:  created,
		Updated:  created,
	}

	data, err := SerializeCard(original)
	require.NoError(t, err)

	parsed, err := ParseCard(data)
	require.NoError(t, err)

	assert.Equal(t, original.ID, parsed.ID)
	assert.Equal(t, original.Title, parsed.Title)
	assert.Empty(t, parsed.AssignedAgent)
	assert.Nil(t, parsed.LastHeartbeat)
	assert.Nil(t, parsed.Source)
	assert.False(t, parsed.Autonomous)
	assert.False(t, parsed.FeatureBranch)
	assert.False(t, parsed.CreatePR)
	assert.Empty(t, parsed.BranchName)
	assert.Empty(t, parsed.PRUrl)
	assert.Zero(t, parsed.ReviewAttempts)
}

func TestRoundTrip_SourceFieldOptional(t *testing.T) {
	created := time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC)

	// Card without source
	cardWithoutSource := &Card{
		ID:       "TEST-001",
		Title:    "No source",
		Project:  "test-project",
		Type:     "task",
		State:    "todo",
		Priority: "medium",
		Created:  created,
		Updated:  created,
	}

	data, err := SerializeCard(cardWithoutSource)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "source:")

	parsed, err := ParseCard(data)
	require.NoError(t, err)
	assert.Nil(t, parsed.Source)

	// Card with source
	cardWithSource := &Card{
		ID:       "TEST-002",
		Title:    "Has source",
		Project:  "test-project",
		Type:     "task",
		State:    "todo",
		Priority: "medium",
		Created:  created,
		Updated:  created,
		Source: &Source{
			System:      "github",
			ExternalID:  "123",
			ExternalURL: "https://github.com/org/repo/issues/123",
		},
	}

	data, err = SerializeCard(cardWithSource)
	require.NoError(t, err)
	assert.Contains(t, string(data), "source:")

	parsed, err = ParseCard(data)
	require.NoError(t, err)
	require.NotNil(t, parsed.Source)
	assert.Equal(t, "github", parsed.Source.System)
}

func TestParseCard_AutonomousFields(t *testing.T) {
	input := `---
id: TEST-001
title: Auto card
project: test
type: task
state: todo
priority: medium
autonomous: true
feature_branch: true
create_pr: true
branch_name: test-001/auto-card
pr_url: https://github.com/org/repo/pull/42
review_attempts: 1
created: 2026-03-30T10:00:00Z
updated: 2026-03-30T10:00:00Z
---
`

	card, err := ParseCard([]byte(input))
	require.NoError(t, err)

	assert.True(t, card.Autonomous)
	assert.True(t, card.FeatureBranch)
	assert.True(t, card.CreatePR)
	assert.Equal(t, "test-001/auto-card", card.BranchName)
	assert.Equal(t, "https://github.com/org/repo/pull/42", card.PRUrl)
	assert.Equal(t, 1, card.ReviewAttempts)
}

func TestRoundTrip_AutonomousFields(t *testing.T) {
	created := time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC)

	original := &Card{
		ID:             "TEST-001",
		Title:          "Autonomous card",
		Project:        "test-project",
		Type:           "task",
		State:          "todo",
		Priority:       "medium",
		Created:        created,
		Updated:        created,
		Autonomous:     true,
		FeatureBranch:  true,
		CreatePR:       true,
		BranchName:     "test-001/autonomous-card",
		PRUrl:          "https://github.com/org/repo/pull/1",
		ReviewAttempts: 2,
	}

	data, err := SerializeCard(original)
	require.NoError(t, err)

	str := string(data)
	assert.Contains(t, str, "autonomous: true")
	assert.Contains(t, str, "feature_branch: true")
	assert.Contains(t, str, "create_pr: true")
	assert.Contains(t, str, "branch_name: test-001/autonomous-card")
	assert.Contains(t, str, "pr_url: https://github.com/org/repo/pull/1")
	assert.Contains(t, str, "review_attempts: 2")

	parsed, err := ParseCard(data)
	require.NoError(t, err)

	assert.Equal(t, original.Autonomous, parsed.Autonomous)
	assert.Equal(t, original.FeatureBranch, parsed.FeatureBranch)
	assert.Equal(t, original.CreatePR, parsed.CreatePR)
	assert.Equal(t, original.BranchName, parsed.BranchName)
	assert.Equal(t, original.PRUrl, parsed.PRUrl)
	assert.Equal(t, original.ReviewAttempts, parsed.ReviewAttempts)
}

func TestParseCard_VettedField(t *testing.T) {
	t.Run("vetted true deserializes correctly", func(t *testing.T) {
		input := `---
id: TEST-001
title: Vetted card
project: test
type: task
state: todo
priority: medium
vetted: true
created: 2026-03-30T10:00:00Z
updated: 2026-03-30T10:00:00Z
---
`
		card, err := ParseCard([]byte(input))
		require.NoError(t, err)
		assert.True(t, card.Vetted)
	})

	t.Run("without vetted field defaults to false", func(t *testing.T) {
		input := `---
id: TEST-001
title: Unvetted card
project: test
type: task
state: todo
priority: medium
created: 2026-03-30T10:00:00Z
updated: 2026-03-30T10:00:00Z
---
`
		card, err := ParseCard([]byte(input))
		require.NoError(t, err)
		assert.False(t, card.Vetted)
	})
}

func TestSerializeCard_VettedOmitempty(t *testing.T) {
	created := time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC)

	t.Run("vetted false omitted from YAML", func(t *testing.T) {
		card := &Card{
			ID:       "TEST-001",
			Title:    "Test card",
			Project:  "test-project",
			Type:     "task",
			State:    "todo",
			Priority: "medium",
			Vetted:   false,
			Created:  created,
			Updated:  created,
		}
		data, err := SerializeCard(card)
		require.NoError(t, err)
		assert.NotContains(t, string(data), "vetted")
	})

	t.Run("vetted true present in YAML", func(t *testing.T) {
		card := &Card{
			ID:       "TEST-001",
			Title:    "Test card",
			Project:  "test-project",
			Type:     "task",
			State:    "todo",
			Priority: "medium",
			Vetted:   true,
			Created:  created,
			Updated:  created,
		}
		data, err := SerializeCard(card)
		require.NoError(t, err)
		assert.Contains(t, string(data), "vetted: true")
	})
}

func TestRoundTrip_VettedField(t *testing.T) {
	created := time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC)

	t.Run("vetted true round-trips correctly", func(t *testing.T) {
		original := &Card{
			ID:       "TEST-001",
			Title:    "Vetted card",
			Project:  "test-project",
			Type:     "task",
			State:    "todo",
			Priority: "medium",
			Vetted:   true,
			Created:  created,
			Updated:  created,
		}
		data, err := SerializeCard(original)
		require.NoError(t, err)

		parsed, err := ParseCard(data)
		require.NoError(t, err)
		assert.True(t, parsed.Vetted)
	})

	t.Run("vetted false round-trips correctly", func(t *testing.T) {
		original := &Card{
			ID:       "TEST-001",
			Title:    "Unvetted card",
			Project:  "test-project",
			Type:     "task",
			State:    "todo",
			Priority: "medium",
			Vetted:   false,
			Created:  created,
			Updated:  created,
		}
		data, err := SerializeCard(original)
		require.NoError(t, err)

		parsed, err := ParseCard(data)
		require.NoError(t, err)
		assert.False(t, parsed.Vetted)
	})
}

func TestParseCard_UseOpusOrchestratorField(t *testing.T) {
	t.Run("use_opus_orchestrator true deserializes correctly", func(t *testing.T) {
		input := `---
id: TEST-001
title: Opus card
project: test
type: task
state: todo
priority: medium
use_opus_orchestrator: true
created: 2026-03-30T10:00:00Z
updated: 2026-03-30T10:00:00Z
---
`
		card, err := ParseCard([]byte(input))
		require.NoError(t, err)
		assert.True(t, card.UseOpusOrchestrator)
	})

	t.Run("without use_opus_orchestrator field defaults to false", func(t *testing.T) {
		input := `---
id: TEST-001
title: Normal card
project: test
type: task
state: todo
priority: medium
created: 2026-03-30T10:00:00Z
updated: 2026-03-30T10:00:00Z
---
`
		card, err := ParseCard([]byte(input))
		require.NoError(t, err)
		assert.False(t, card.UseOpusOrchestrator)
	})
}

func TestSerializeCard_UseOpusOrchestratorOmitempty(t *testing.T) {
	created := time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC)

	t.Run("use_opus_orchestrator false omitted from YAML", func(t *testing.T) {
		card := &Card{
			ID:                  "TEST-001",
			Title:               "Test card",
			Project:             "test-project",
			Type:                "task",
			State:               "todo",
			Priority:            "medium",
			UseOpusOrchestrator: false,
			Created:             created,
			Updated:             created,
		}
		data, err := SerializeCard(card)
		require.NoError(t, err)
		assert.NotContains(t, string(data), "use_opus_orchestrator")
	})

	t.Run("use_opus_orchestrator true present in YAML", func(t *testing.T) {
		card := &Card{
			ID:                  "TEST-001",
			Title:               "Test card",
			Project:             "test-project",
			Type:                "task",
			State:               "todo",
			Priority:            "medium",
			UseOpusOrchestrator: true,
			Created:             created,
			Updated:             created,
		}
		data, err := SerializeCard(card)
		require.NoError(t, err)
		assert.Contains(t, string(data), "use_opus_orchestrator: true")
	})
}

func TestRoundTrip_UseOpusOrchestratorField(t *testing.T) {
	created := time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC)

	t.Run("use_opus_orchestrator true round-trips correctly", func(t *testing.T) {
		original := &Card{
			ID:                  "TEST-001",
			Title:               "Opus card",
			Project:             "test-project",
			Type:                "task",
			State:               "todo",
			Priority:            "medium",
			UseOpusOrchestrator: true,
			Created:             created,
			Updated:             created,
		}
		data, err := SerializeCard(original)
		require.NoError(t, err)

		parsed, err := ParseCard(data)
		require.NoError(t, err)
		assert.True(t, parsed.UseOpusOrchestrator)
	})

	t.Run("use_opus_orchestrator false round-trips correctly", func(t *testing.T) {
		original := &Card{
			ID:                  "TEST-001",
			Title:               "Normal card",
			Project:             "test-project",
			Type:                "task",
			State:               "todo",
			Priority:            "medium",
			UseOpusOrchestrator: false,
			Created:             created,
			Updated:             created,
		}
		data, err := SerializeCard(original)
		require.NoError(t, err)

		parsed, err := ParseCard(data)
		require.NoError(t, err)
		assert.False(t, parsed.UseOpusOrchestrator)
	})
}

func TestRoundTrip_CustomFields(t *testing.T) {
	created := time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC)

	original := &Card{
		ID:       "TEST-001",
		Title:    "Custom fields test",
		Project:  "test-project",
		Type:     "task",
		State:    "todo",
		Priority: "medium",
		Created:  created,
		Updated:  created,
		Custom: map[string]any{
			"string_field": "value",
			"int_field":    42,
			"bool_field":   true,
			"nested": map[string]any{
				"inner": "data",
			},
		},
	}

	data, err := SerializeCard(original)
	require.NoError(t, err)

	parsed, err := ParseCard(data)
	require.NoError(t, err)

	assert.Equal(t, "value", parsed.Custom["string_field"])
	assert.Equal(t, 42, parsed.Custom["int_field"])
	assert.Equal(t, true, parsed.Custom["bool_field"])

	nested, ok := parsed.Custom["nested"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "data", nested["inner"])
}

func TestCard_SkillsFieldRoundtrip(t *testing.T) {
	t.Run("nil skills (field absent)", func(t *testing.T) {
		input := `---
id: ALPHA-001
title: t
project: p
type: task
state: todo
priority: low
created: 2026-04-25T10:00:00Z
updated: 2026-04-25T10:00:00Z
---
body
`
		card, err := ParseCard([]byte(input))
		require.NoError(t, err)
		assert.Nil(t, card.Skills, "absent skills field should parse as nil pointer")

		out, err := SerializeCard(card)
		require.NoError(t, err)
		assert.NotContains(t, string(out), "skills", "nil skills should be omitted from YAML")
	})

	t.Run("empty skills list", func(t *testing.T) {
		input := `---
id: ALPHA-002
title: t
project: p
type: task
state: todo
priority: low
skills: []
created: 2026-04-25T10:00:00Z
updated: 2026-04-25T10:00:00Z
---
body
`
		card, err := ParseCard([]byte(input))
		require.NoError(t, err)
		require.NotNil(t, card.Skills, "empty list should parse as non-nil pointer")
		assert.Empty(t, *card.Skills, "value should be an empty slice")

		out, err := SerializeCard(card)
		require.NoError(t, err)
		assert.Contains(t, string(out), "skills: []", "empty skills should serialize as 'skills: []'")
	})

	t.Run("populated skills list", func(t *testing.T) {
		input := `---
id: ALPHA-003
title: t
project: p
type: task
state: todo
priority: low
skills:
  - go-development
  - documentation
created: 2026-04-25T10:00:00Z
updated: 2026-04-25T10:00:00Z
---
body
`
		card, err := ParseCard([]byte(input))
		require.NoError(t, err)
		require.NotNil(t, card.Skills)
		assert.Equal(t, []string{"go-development", "documentation"}, *card.Skills)

		out, err := SerializeCard(card)
		require.NoError(t, err)
		assert.Contains(t, string(out), "go-development")
		assert.Contains(t, string(out), "documentation")
	})
}

func TestActivityEntry_SkillField(t *testing.T) {
	t.Run("skill field omitted when empty", func(t *testing.T) {
		input := `---
id: ALPHA-001
title: t
project: p
type: task
state: todo
priority: low
created: 2026-04-25T10:00:00Z
updated: 2026-04-25T10:00:00Z
activity_log:
  - agent: claude-7a3f
    ts: 2026-04-25T10:00:00Z
    action: status_update
    message: working
---
body
`
		card, err := ParseCard([]byte(input))
		require.NoError(t, err)
		require.Len(t, card.ActivityLog, 1)
		assert.Empty(t, card.ActivityLog[0].Skill)

		out, err := SerializeCard(card)
		require.NoError(t, err)
		assert.NotContains(t, string(out), "skill:")
	})

	t.Run("skill field round-trips", func(t *testing.T) {
		input := `---
id: ALPHA-002
title: t
project: p
type: task
state: todo
priority: low
created: 2026-04-25T10:00:00Z
updated: 2026-04-25T10:00:00Z
activity_log:
  - agent: claude-7a3f
    ts: 2026-04-25T10:00:00Z
    action: skill_engaged
    message: engaged go-development
    skill: go-development
---
body
`
		card, err := ParseCard([]byte(input))
		require.NoError(t, err)
		require.Len(t, card.ActivityLog, 1)
		assert.Equal(t, "go-development", card.ActivityLog[0].Skill)
	})
}
