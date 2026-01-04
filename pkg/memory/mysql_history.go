package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/tmc/langchaingo/llms"
)

// MySQLChatMessageHistory MySQL 聊天记忆存储
// 实现 langchaingo 的 ChatMessageHistory 接口
type MySQLChatMessageHistory struct {
	db        *sql.DB
	tableName string
	sessionID string
	userID    string
	limit     int
}

// ChatHistoryMessage 聊天记录消息
type ChatHistoryMessage struct {
	ID        int64     `db:"id"`
	SessionID string    `db:"session_id"`
	UserID    string    `db:"user_id"`
	Role      string    `db:"role"` // human, ai, system, function, tool
	Content   string    `db:"content"`
	Metadata  string    `db:"metadata"` // JSON 格式的元数据
	CreatedAt time.Time `db:"created_at"`
}

// MySQLHistoryOption 配置选项
type MySQLHistoryOption func(*MySQLChatMessageHistory)

// WithTableName 设置表名
func WithTableName(name string) MySQLHistoryOption {
	return func(h *MySQLChatMessageHistory) {
		h.tableName = name
	}
}

// WithSessionID 设置会话 ID
func WithSessionID(sessionID string) MySQLHistoryOption {
	return func(h *MySQLChatMessageHistory) {
		h.sessionID = sessionID
	}
}

// WithUserID 设置用户 ID
func WithUserID(userID string) MySQLHistoryOption {
	return func(h *MySQLChatMessageHistory) {
		h.userID = userID
	}
}

// WithLimit 设置查询限制
func WithLimit(limit int) MySQLHistoryOption {
	return func(h *MySQLChatMessageHistory) {
		h.limit = limit
	}
}

// NewMySQLChatMessageHistory 创建 MySQL 聊天记忆存储
func NewMySQLChatMessageHistory(db *sql.DB, opts ...MySQLHistoryOption) (*MySQLChatMessageHistory, error) {
	h := &MySQLChatMessageHistory{
		db:        db,
		tableName: "chat_memory",
		limit:     100,
	}

	for _, opt := range opts {
		opt(h)
	}

	// 确保表存在
	if err := h.ensureTable(); err != nil {
		return nil, fmt.Errorf("failed to ensure table: %w", err)
	}

	return h, nil
}

// ensureTable 确保表存在
func (h *MySQLChatMessageHistory) ensureTable() error {
	schema := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			session_id VARCHAR(255) NOT NULL,
			user_id VARCHAR(255) NOT NULL DEFAULT '',
			role VARCHAR(50) NOT NULL,
			content TEXT NOT NULL,
			metadata JSON,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_session_user (session_id, user_id),
			INDEX idx_created_at (created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`, h.tableName)

	_, err := h.db.Exec(schema)
	return err
}

// Messages 获取所有消息
func (h *MySQLChatMessageHistory) Messages(ctx context.Context) ([]llms.ChatMessage, error) {
	query := fmt.Sprintf(`
		SELECT role, content, metadata FROM %s
		WHERE session_id = ? AND user_id = ?
		ORDER BY created_at ASC
		LIMIT ?
	`, h.tableName)

	rows, err := h.db.QueryContext(ctx, query, h.sessionID, h.userID, h.limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []llms.ChatMessage
	for rows.Next() {
		var role, content string
		var metadata sql.NullString
		if err := rows.Scan(&role, &content, &metadata); err != nil {
			return nil, err
		}

		msg := h.roleToMessage(role, content)
		messages = append(messages, msg)
	}

	return messages, nil
}

// AddMessage 添加消息
func (h *MySQLChatMessageHistory) AddMessage(ctx context.Context, msg llms.ChatMessage) error {
	role := h.messageToRole(msg)
	content := msg.GetContent()

	query := fmt.Sprintf(`
		INSERT INTO %s (session_id, user_id, role, content) VALUES (?, ?, ?, ?)
	`, h.tableName)

	_, err := h.db.ExecContext(ctx, query, h.sessionID, h.userID, role, content)
	return err
}

// AddUserMessage 添加用户消息
func (h *MySQLChatMessageHistory) AddUserMessage(ctx context.Context, text string) error {
	return h.AddMessage(ctx, llms.HumanChatMessage{Content: text})
}

// AddAIMessage 添加 AI 消息
func (h *MySQLChatMessageHistory) AddAIMessage(ctx context.Context, text string) error {
	return h.AddMessage(ctx, llms.AIChatMessage{Content: text})
}

// SetMessages 替换所有消息
func (h *MySQLChatMessageHistory) SetMessages(ctx context.Context, messages []llms.ChatMessage) error {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 删除旧消息
	deleteQuery := fmt.Sprintf(`DELETE FROM %s WHERE session_id = ? AND user_id = ?`, h.tableName)
	if _, err := tx.ExecContext(ctx, deleteQuery, h.sessionID, h.userID); err != nil {
		return err
	}

	// 插入新消息
	insertQuery := fmt.Sprintf(`INSERT INTO %s (session_id, user_id, role, content) VALUES (?, ?, ?, ?)`, h.tableName)
	for _, msg := range messages {
		role := h.messageToRole(msg)
		content := msg.GetContent()
		if _, err := tx.ExecContext(ctx, insertQuery, h.sessionID, h.userID, role, content); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// Clear 清除所有消息
func (h *MySQLChatMessageHistory) Clear(ctx context.Context) error {
	query := fmt.Sprintf(`DELETE FROM %s WHERE session_id = ? AND user_id = ?`, h.tableName)
	_, err := h.db.ExecContext(ctx, query, h.sessionID, h.userID)
	return err
}

// roleToMessage 将角色转换为消息
func (h *MySQLChatMessageHistory) roleToMessage(role, content string) llms.ChatMessage {
	switch role {
	case "human", "user":
		return llms.HumanChatMessage{Content: content}
	case "ai", "assistant":
		return llms.AIChatMessage{Content: content}
	case "system":
		return llms.SystemChatMessage{Content: content}
	default:
		return llms.HumanChatMessage{Content: content}
	}
}

// messageToRole 将消息转换为角色
func (h *MySQLChatMessageHistory) messageToRole(msg llms.ChatMessage) string {
	switch msg.GetType() {
	case llms.ChatMessageTypeHuman:
		return "human"
	case llms.ChatMessageTypeAI:
		return "ai"
	case llms.ChatMessageTypeSystem:
		return "system"
	case llms.ChatMessageTypeFunction:
		return "function"
	case llms.ChatMessageTypeTool:
		return "tool"
	default:
		return "human"
	}
}

// GetRecentMessages 获取最近 N 条消息
func (h *MySQLChatMessageHistory) GetRecentMessages(ctx context.Context, n int) ([]llms.ChatMessage, error) {
	query := fmt.Sprintf(`
		SELECT role, content FROM (
			SELECT role, content, created_at FROM %s
			WHERE session_id = ? AND user_id = ?
			ORDER BY created_at DESC
			LIMIT ?
		) sub ORDER BY created_at ASC
	`, h.tableName)

	rows, err := h.db.QueryContext(ctx, query, h.sessionID, h.userID, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []llms.ChatMessage
	for rows.Next() {
		var role, content string
		if err := rows.Scan(&role, &content); err != nil {
			return nil, err
		}
		messages = append(messages, h.roleToMessage(role, content))
	}

	return messages, nil
}

// SearchMessages 搜索消息
func (h *MySQLChatMessageHistory) SearchMessages(ctx context.Context, keyword string, limit int) ([]ChatHistoryMessage, error) {
	query := fmt.Sprintf(`
		SELECT id, session_id, user_id, role, content, metadata, created_at
		FROM %s
		WHERE (session_id = ? OR user_id = ?) AND content LIKE ?
		ORDER BY created_at DESC
		LIMIT ?
	`, h.tableName)

	rows, err := h.db.QueryContext(ctx, query, h.sessionID, h.userID, "%"+keyword+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []ChatHistoryMessage
	for rows.Next() {
		var msg ChatHistoryMessage
		var metadata sql.NullString
		if err := rows.Scan(&msg.ID, &msg.SessionID, &msg.UserID, &msg.Role, &msg.Content, &metadata, &msg.CreatedAt); err != nil {
			return nil, err
		}
		if metadata.Valid {
			msg.Metadata = metadata.String
		}
		messages = append(messages, msg)
	}

	return messages, nil
}

// GetSessionSummary 获取会话摘要（用于长对话压缩）
func (h *MySQLChatMessageHistory) GetSessionSummary(ctx context.Context) (string, error) {
	query := fmt.Sprintf(`
		SELECT content FROM %s
		WHERE session_id = ? AND user_id = ? AND role = 'summary'
		ORDER BY created_at DESC
		LIMIT 1
	`, h.tableName)

	var summary string
	err := h.db.QueryRowContext(ctx, query, h.sessionID, h.userID).Scan(&summary)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return summary, err
}

// SaveSessionSummary 保存会话摘要
func (h *MySQLChatMessageHistory) SaveSessionSummary(ctx context.Context, summary string) error {
	query := fmt.Sprintf(`
		INSERT INTO %s (session_id, user_id, role, content) VALUES (?, ?, 'summary', ?)
	`, h.tableName)

	_, err := h.db.ExecContext(ctx, query, h.sessionID, h.userID, summary)
	return err
}

// GetAllUserSessions 获取用户所有会话
func (h *MySQLChatMessageHistory) GetAllUserSessions(ctx context.Context, userID string) ([]string, error) {
	query := fmt.Sprintf(`
		SELECT DISTINCT session_id FROM %s WHERE user_id = ? ORDER BY MAX(created_at) DESC
	`, h.tableName)

	rows, err := h.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []string
	for rows.Next() {
		var sessionID string
		if err := rows.Scan(&sessionID); err != nil {
			return nil, err
		}
		sessions = append(sessions, sessionID)
	}

	return sessions, nil
}

// AddMessageWithMetadata 添加带元数据的消息
func (h *MySQLChatMessageHistory) AddMessageWithMetadata(ctx context.Context, msg llms.ChatMessage, metadata map[string]interface{}) error {
	role := h.messageToRole(msg)
	content := msg.GetContent()

	metadataJSON, _ := json.Marshal(metadata)

	query := fmt.Sprintf(`
		INSERT INTO %s (session_id, user_id, role, content, metadata) VALUES (?, ?, ?, ?, ?)
	`, h.tableName)

	_, err := h.db.ExecContext(ctx, query, h.sessionID, h.userID, role, content, string(metadataJSON))
	return err
}
