package ai

import (
	"context"
	"encoding/json"
	"iot-dashboard/internal/database"
	"time"
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

// GetMessageByID loads one message, but only if it belongs to a conversation the
// user owns — so a user can only confirm/cancel their own pending actions.
func (r *Repository) GetMessageByID(ctx context.Context, msgID, userID string) (*Message, error) {
	var m Message
	err := database.Pool.QueryRow(ctx, `
		SELECT m.id, m.conversation_id, m.role, m.content, m.tool_name, m.tool_input, m.tool_result, m.created_at
		FROM ai_messages m
		JOIN ai_conversations c ON c.id = m.conversation_id
		WHERE m.id = $1 AND c.user_id = $2
	`, msgID, userID).Scan(
		&m.ID, &m.ConversationID, &m.Role, &m.Content,
		&m.ToolName, &m.ToolInput, &m.ToolResult, &m.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// UpdateMessageResult overwrites a message's content + tool_result — used to flip
// a pending action to executed or cancelled.
func (r *Repository) UpdateMessageResult(ctx context.Context, msgID, content string, toolResult json.RawMessage) (*Message, error) {
	var m Message
	err := database.Pool.QueryRow(ctx, `
		UPDATE ai_messages SET content = $2, tool_result = $3
		WHERE id = $1
		RETURNING id, conversation_id, role, content, tool_name, tool_input, tool_result, created_at
	`, msgID, content, nullableJSON(toolResult)).Scan(
		&m.ID, &m.ConversationID, &m.Role, &m.Content,
		&m.ToolName, &m.ToolInput, &m.ToolResult, &m.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// nullableJSON returns nil when the raw message is empty so pgx stores NULL instead of a zero-length JSONB.
func nullableJSON(r json.RawMessage) interface{} {
	if len(r) == 0 {
		return nil
	}
	return []byte(r)
}
