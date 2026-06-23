package ai

import (
	"context"
	"encoding/json"
	"iot-dashboard/internal/database"
	"time"

	"github.com/jackc/pgx/v5"
)

type Conversation struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Count     struct {
		Messages int `json:"messages"`
	} `json:"_count"`
}

type Message struct {
	ID             string          `json:"id"`
	ConversationID string          `json:"conversationId"`
	Role           string          `json:"role"`
	Content        string          `json:"content"`
	ToolName       *string         `json:"toolName,omitempty"`
	ToolInput      json.RawMessage `json:"toolInput,omitempty"`
	ToolResult     json.RawMessage `json:"toolResult,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
}

type Repository struct{}

func (r *Repository) CreateConversation(ctx context.Context, userID, title string) (*Conversation, error) {
	var c Conversation
	err := database.Pool.QueryRow(ctx, `
		INSERT INTO ai_conversations (id, user_id, title, context, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, $2, '{}', NOW(), NOW())
		RETURNING id, user_id, title, created_at, updated_at
	`, userID, title).Scan(&c.ID, &c.UserID, &c.Title, &c.CreatedAt, &c.UpdatedAt)
	return &c, err
}

func (r *Repository) ListConversations(ctx context.Context, userID string) ([]Conversation, error) {
	rows, err := database.Pool.Query(ctx, `
		SELECT c.id, c.user_id, c.title, c.created_at, c.updated_at,
		       COUNT(m.id)::int AS message_count
		FROM ai_conversations c
		LEFT JOIN ai_messages m ON m.conversation_id = c.id
		WHERE c.user_id = $1
		GROUP BY c.id
		ORDER BY c.updated_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var convs []Conversation
	for rows.Next() {
		var c Conversation
		if err := rows.Scan(&c.ID, &c.UserID, &c.Title, &c.CreatedAt, &c.UpdatedAt, &c.Count.Messages); err != nil {
			return nil, err
		}
		convs = append(convs, c)
	}
	return convs, nil
}

func (r *Repository) GetMessages(ctx context.Context, conversationID string) ([]Message, error) {
	rows, err := database.Pool.Query(ctx, `
		SELECT id, conversation_id, role, content, tool_name, tool_input, tool_result, created_at
		FROM ai_messages
		WHERE conversation_id = $1
		ORDER BY created_at ASC
	`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content,
			&m.ToolName, &m.ToolInput, &m.ToolResult, &m.CreatedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}

func (r *Repository) AddMessage(ctx context.Context, conversationID, role, content string, toolName *string, toolInput, toolResult json.RawMessage) (*Message, error) {
	var m Message
	err := database.Pool.QueryRow(ctx, `
		INSERT INTO ai_messages (id, conversation_id, role, content, tool_name, tool_input, tool_result, created_at)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, NOW())
		RETURNING id, conversation_id, role, content, tool_name, tool_input, tool_result, created_at
	`, conversationID, role, content, toolName, nullableJSON(toolInput), nullableJSON(toolResult)).Scan(
		&m.ID, &m.ConversationID, &m.Role, &m.Content,
		&m.ToolName, &m.ToolInput, &m.ToolResult, &m.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	_, _ = database.Pool.Exec(ctx, `UPDATE ai_conversations SET updated_at = NOW() WHERE id = $1`, conversationID)
	return &m, nil
}

// ── Preview drafts ──────────────────────────────────────────────────────────

// UpsertDraft stores a preview as the view state, clearing any selected dashboard.
func (r *Repository) UpsertDraft(ctx context.Context, userID, conversationID string, data json.RawMessage) error {
	var convID interface{}
	if conversationID != "" {
		convID = conversationID
	}
	_, err := database.Pool.Exec(ctx, `
		INSERT INTO ai_preview_drafts (user_id, conversation_id, dashboard_id, data, updated_at)
		VALUES ($1, $2, NULL, $3, NOW())
		ON CONFLICT (user_id) DO UPDATE
		SET conversation_id = EXCLUDED.conversation_id,
		    dashboard_id    = NULL,
		    data            = EXCLUDED.data,
		    updated_at      = NOW()
	`, userID, convID, []byte(data))
	return err
}

// UpsertDashboard stores a selected dashboard as the view state, clearing any preview.
func (r *Repository) UpsertDashboard(ctx context.Context, userID, dashboardID string) error {
	_, err := database.Pool.Exec(ctx, `
		INSERT INTO ai_preview_drafts (user_id, conversation_id, dashboard_id, data, updated_at)
		VALUES ($1, NULL, $2, NULL, NOW())
		ON CONFLICT (user_id) DO UPDATE
		SET conversation_id = NULL,
		    dashboard_id    = EXCLUDED.dashboard_id,
		    data            = NULL,
		    updated_at      = NOW()
	`, userID, dashboardID)
	return err
}

func (r *Repository) GetDraft(ctx context.Context, userID string) (conversationID, dashboardID string, data json.RawMessage, found bool, err error) {
	var convID, dashID *string
	err = database.Pool.QueryRow(ctx, `
		SELECT conversation_id, dashboard_id, data FROM ai_preview_drafts WHERE user_id = $1
	`, userID).Scan(&convID, &dashID, &data)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", "", nil, false, nil
		}
		return "", "", nil, false, err
	}
	if convID != nil {
		conversationID = *convID
	}
	if dashID != nil {
		dashboardID = *dashID
	}
	return conversationID, dashboardID, data, true, nil
}

func (r *Repository) DeleteDraft(ctx context.Context, userID string) error {
	_, err := database.Pool.Exec(ctx, `DELETE FROM ai_preview_drafts WHERE user_id = $1`, userID)
	return err
}

// nullableJSON returns nil when the raw message is empty so pgx stores NULL instead of a zero-length JSONB.
func nullableJSON(r json.RawMessage) interface{} {
	if len(r) == 0 {
		return nil
	}
	return []byte(r)
}
