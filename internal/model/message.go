package model

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

type ChatMessage struct {
	ID          int64          `db:"id"`
	MessageID   string         `db:"message_id"`
	ChatID      string         `db:"chat_id"`
	SenderID    sql.NullString `db:"sender_id"`
	SenderName  sql.NullString `db:"sender_name"`
	MemberID    sql.NullInt64  `db:"member_id"`
	MsgType     sql.NullString `db:"msg_type"`
	Content     sql.NullString `db:"content"`
	RawContent  sql.NullString `db:"raw_content"`
	Mentions    json.RawMessage`db:"mentions"`
	ReplyToID   sql.NullString `db:"reply_to_id"`
	IsAtBot     int            `db:"is_at_bot"`
	CreatedAt   time.Time      `db:"created_at"`
	IndexedAt   time.Time      `db:"indexed_at"`
}

type ChatGroup struct {
	ID          int64          `db:"id"`
	ChatID      string         `db:"chat_id"`
	ChatName    sql.NullString `db:"chat_name"`
	ChatType    sql.NullString `db:"chat_type"`
	OwnerID     sql.NullString `db:"owner_id"`
	MemberCount int            `db:"member_count"`
	Status      int            `db:"status"`
	CreatedAt   time.Time      `db:"created_at"`
	UpdatedAt   time.Time      `db:"updated_at"`
}

type ChatMessageModel struct {
	db *sql.DB
}

func NewChatMessageModel(db *sql.DB) *ChatMessageModel {
	return &ChatMessageModel{db: db}
}

func (m *ChatMessageModel) Insert(ctx context.Context, msg *ChatMessage) error {
	query := `INSERT INTO chat_messages (message_id, chat_id, sender_id, sender_name, member_id, msg_type,
              content, raw_content, mentions, reply_to_id, is_at_bot, created_at)
              VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
              ON DUPLICATE KEY UPDATE content = VALUES(content), sender_name = COALESCE(VALUES(sender_name), sender_name)`
	_, err := m.db.ExecContext(ctx, query, msg.MessageID, msg.ChatID, msg.SenderID, msg.SenderName,
		msg.MemberID, msg.MsgType, msg.Content, msg.RawContent, msg.Mentions, msg.ReplyToID, msg.IsAtBot, msg.CreatedAt)
	return err
}

// GetRecentMessages 获取群最近的消息
func (m *ChatMessageModel) GetRecentMessages(ctx context.Context, chatID string, limit int) ([]*ChatMessage, error) {
	query := `SELECT id, message_id, chat_id, sender_id, sender_name, member_id, msg_type,
              content, raw_content, mentions, reply_to_id, is_at_bot, created_at, indexed_at
              FROM chat_messages
              WHERE chat_id = ?
              ORDER BY created_at DESC LIMIT ?`
	rows, err := m.db.QueryContext(ctx, query, chatID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*ChatMessage
	for rows.Next() {
		var msg ChatMessage
		err := rows.Scan(&msg.ID, &msg.MessageID, &msg.ChatID, &msg.SenderID, &msg.SenderName,
			&msg.MemberID, &msg.MsgType, &msg.Content, &msg.RawContent, &msg.Mentions,
			&msg.ReplyToID, &msg.IsAtBot, &msg.CreatedAt, &msg.IndexedAt)
		if err != nil {
			return nil, err
		}
		messages = append(messages, &msg)
	}
	return messages, nil
}

// GetMessagesByDateRange 按日期范围获取消息
func (m *ChatMessageModel) GetMessagesByDateRange(ctx context.Context, chatID string, start, end time.Time, limit int) ([]*ChatMessage, error) {
	var query string
	var rows *sql.Rows
	var err error

	if chatID != "" {
		query = `SELECT id, message_id, chat_id, sender_id, sender_name, member_id, msg_type,
              content, raw_content, mentions, reply_to_id, is_at_bot, created_at, indexed_at
              FROM chat_messages
              WHERE chat_id = ? AND created_at BETWEEN ? AND ?
              ORDER BY created_at DESC LIMIT ?`
		rows, err = m.db.QueryContext(ctx, query, chatID, start, end, limit)
	} else {
		query = `SELECT id, message_id, chat_id, sender_id, sender_name, member_id, msg_type,
              content, raw_content, mentions, reply_to_id, is_at_bot, created_at, indexed_at
              FROM chat_messages
              WHERE created_at BETWEEN ? AND ?
              ORDER BY created_at DESC LIMIT ?`
		rows, err = m.db.QueryContext(ctx, query, start, end, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*ChatMessage
	for rows.Next() {
		var msg ChatMessage
		err := rows.Scan(&msg.ID, &msg.MessageID, &msg.ChatID, &msg.SenderID, &msg.SenderName,
			&msg.MemberID, &msg.MsgType, &msg.Content, &msg.RawContent, &msg.Mentions,
			&msg.ReplyToID, &msg.IsAtBot, &msg.CreatedAt, &msg.IndexedAt)
		if err != nil {
			return nil, err
		}
		messages = append(messages, &msg)
	}
	return messages, nil
}

// SearchByContent 按内容搜索消息
func (m *ChatMessageModel) SearchByContent(ctx context.Context, chatID, keyword string, limit int) ([]*ChatMessage, error) {
	// 直接使用 LIKE 搜索，因为全文索引可能未配置
	return m.searchByLike(ctx, chatID, keyword, limit)
}

func (m *ChatMessageModel) searchByLike(ctx context.Context, chatID, keyword string, limit int) ([]*ChatMessage, error) {
	var query string
	var rows *sql.Rows
	var err error

	if chatID != "" {
		query = `SELECT id, message_id, chat_id, sender_id, sender_name, member_id, msg_type,
              content, raw_content, mentions, reply_to_id, is_at_bot, created_at, indexed_at
              FROM chat_messages
              WHERE chat_id = ? AND content LIKE ?
              ORDER BY created_at DESC LIMIT ?`
		rows, err = m.db.QueryContext(ctx, query, chatID, "%"+keyword+"%", limit)
	} else {
		query = `SELECT id, message_id, chat_id, sender_id, sender_name, member_id, msg_type,
              content, raw_content, mentions, reply_to_id, is_at_bot, created_at, indexed_at
              FROM chat_messages
              WHERE content LIKE ?
              ORDER BY created_at DESC LIMIT ?`
		rows, err = m.db.QueryContext(ctx, query, "%"+keyword+"%", limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*ChatMessage
	for rows.Next() {
		var msg ChatMessage
		err := rows.Scan(&msg.ID, &msg.MessageID, &msg.ChatID, &msg.SenderID, &msg.SenderName,
			&msg.MemberID, &msg.MsgType, &msg.Content, &msg.RawContent, &msg.Mentions,
			&msg.ReplyToID, &msg.IsAtBot, &msg.CreatedAt, &msg.IndexedAt)
		if err != nil {
			return nil, err
		}
		messages = append(messages, &msg)
	}
	return messages, nil
}

// SearchBySender 按发送者搜索消息
func (m *ChatMessageModel) SearchBySender(ctx context.Context, chatID, senderName, keyword string, limit int) ([]*ChatMessage, error) {
	var query string
	var rows *sql.Rows
	var err error

	if chatID != "" {
		query = `SELECT id, message_id, chat_id, sender_id, sender_name, member_id, msg_type,
              content, raw_content, mentions, reply_to_id, is_at_bot, created_at, indexed_at
              FROM chat_messages
              WHERE chat_id = ? AND sender_name LIKE ? AND content LIKE ?
              ORDER BY created_at DESC LIMIT ?`
		rows, err = m.db.QueryContext(ctx, query, chatID, "%"+senderName+"%", "%"+keyword+"%", limit)
	} else {
		query = `SELECT id, message_id, chat_id, sender_id, sender_name, member_id, msg_type,
              content, raw_content, mentions, reply_to_id, is_at_bot, created_at, indexed_at
              FROM chat_messages
              WHERE sender_name LIKE ? AND content LIKE ?
              ORDER BY created_at DESC LIMIT ?`
		rows, err = m.db.QueryContext(ctx, query, "%"+senderName+"%", "%"+keyword+"%", limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*ChatMessage
	for rows.Next() {
		var msg ChatMessage
		err := rows.Scan(&msg.ID, &msg.MessageID, &msg.ChatID, &msg.SenderID, &msg.SenderName,
			&msg.MemberID, &msg.MsgType, &msg.Content, &msg.RawContent, &msg.Mentions,
			&msg.ReplyToID, &msg.IsAtBot, &msg.CreatedAt, &msg.IndexedAt)
		if err != nil {
			return nil, err
		}
		messages = append(messages, &msg)
	}
	return messages, nil
}

// GetAtBotMessages 获取@机器人的消息
func (m *ChatMessageModel) GetAtBotMessages(ctx context.Context, limit int) ([]*ChatMessage, error) {
	query := `SELECT id, message_id, chat_id, sender_id, sender_name, member_id, msg_type,
              content, raw_content, mentions, reply_to_id, is_at_bot, created_at, indexed_at
              FROM chat_messages
              WHERE is_at_bot = 1
              ORDER BY created_at DESC LIMIT ?`
	rows, err := m.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*ChatMessage
	for rows.Next() {
		var msg ChatMessage
		err := rows.Scan(&msg.ID, &msg.MessageID, &msg.ChatID, &msg.SenderID, &msg.SenderName,
			&msg.MemberID, &msg.MsgType, &msg.Content, &msg.RawContent, &msg.Mentions,
			&msg.ReplyToID, &msg.IsAtBot, &msg.CreatedAt, &msg.IndexedAt)
		if err != nil {
			return nil, err
		}
		messages = append(messages, &msg)
	}
	return messages, nil
}

// GetDistinctSenders 获取不重复的发送者名称列表
func (m *ChatMessageModel) GetDistinctSenders(ctx context.Context, chatID string) ([]string, error) {
	var query string
	var rows *sql.Rows
	var err error

	if chatID != "" {
		query = `SELECT DISTINCT sender_name FROM chat_messages
                 WHERE chat_id = ? AND sender_name IS NOT NULL AND sender_name != ''
                 ORDER BY sender_name`
		rows, err = m.db.QueryContext(ctx, query, chatID)
	} else {
		query = `SELECT DISTINCT sender_name FROM chat_messages
                 WHERE sender_name IS NOT NULL AND sender_name != ''
                 ORDER BY sender_name`
		rows, err = m.db.QueryContext(ctx, query)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var senders []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		senders = append(senders, name)
	}
	return senders, nil
}

// ChatGroupModel 群聊模型
type ChatGroupModel struct {
	db *sql.DB
}

// NewChatGroupModel 创建群聊模型
func NewChatGroupModel(db *sql.DB) *ChatGroupModel {
	return &ChatGroupModel{db: db}
}

// Upsert 插入或更新群聊
func (m *ChatGroupModel) Upsert(ctx context.Context, group *ChatGroup) error {
	query := `INSERT INTO chat_groups (chat_id, chat_name, chat_type, owner_id, member_count, status)
              VALUES (?, ?, ?, ?, ?, ?)
              ON DUPLICATE KEY UPDATE chat_name = VALUES(chat_name), member_count = VALUES(member_count)`
	_, err := m.db.ExecContext(ctx, query, group.ChatID, group.ChatName, group.ChatType,
		group.OwnerID, group.MemberCount, group.Status)
	return err
}

// FindByChatID 根据chat_id查找群聊
func (m *ChatGroupModel) FindByChatID(ctx context.Context, chatID string) (*ChatGroup, error) {
	query := `SELECT id, chat_id, chat_name, chat_type, owner_id, member_count, status, created_at, updated_at
              FROM chat_groups WHERE chat_id = ?`
	var group ChatGroup
	err := m.db.QueryRowContext(ctx, query, chatID).Scan(&group.ID, &group.ChatID, &group.ChatName,
		&group.ChatType, &group.OwnerID, &group.MemberCount, &group.Status, &group.CreatedAt, &group.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &group, nil
}

// ListAll 列出所有群聊
func (m *ChatGroupModel) ListAll(ctx context.Context) ([]*ChatGroup, error) {
	query := `SELECT id, chat_id, chat_name, chat_type, owner_id, member_count, status, created_at, updated_at
              FROM chat_groups WHERE status = 1 ORDER BY updated_at DESC`
	rows, err := m.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []*ChatGroup
	for rows.Next() {
		var group ChatGroup
		err := rows.Scan(&group.ID, &group.ChatID, &group.ChatName, &group.ChatType,
			&group.OwnerID, &group.MemberCount, &group.Status, &group.CreatedAt, &group.UpdatedAt)
		if err != nil {
			return nil, err
		}
		groups = append(groups, &group)
	}
	return groups, nil
}

// MessageSyncTask 消息同步任务
type MessageSyncTask struct {
	ID             int64          `db:"id"`
	ChatID         string         `db:"chat_id"`
	ChatName       sql.NullString `db:"chat_name"`
	Status         string         `db:"status"`
	TotalMessages  int            `db:"total_messages"`
	SyncedMessages int            `db:"synced_messages"`
	PageToken      sql.NullString `db:"page_token"`
	StartTime      sql.NullString `db:"start_time"`
	EndTime        sql.NullString `db:"end_time"`
	ErrorMsg       sql.NullString `db:"error_msg"`
	RequestedBy    sql.NullString `db:"requested_by"`
	StartedAt      sql.NullTime   `db:"started_at"`
	FinishedAt     sql.NullTime   `db:"finished_at"`
	CreatedAt      time.Time      `db:"created_at"`
	UpdatedAt      time.Time      `db:"updated_at"`
}

// MessageSyncTaskModel 消息同步任务模型
type MessageSyncTaskModel struct {
	db *sql.DB
}

// NewMessageSyncTaskModel 创建消息同步任务模型
func NewMessageSyncTaskModel(db *sql.DB) *MessageSyncTaskModel {
	return &MessageSyncTaskModel{db: db}
}

// Create 创建同步任务
func (m *MessageSyncTaskModel) Create(ctx context.Context, task *MessageSyncTask) (int64, error) {
	query := `INSERT INTO message_sync_tasks (chat_id, chat_name, status, requested_by, start_time, end_time)
              VALUES (?, ?, ?, ?, ?, ?)`
	result, err := m.db.ExecContext(ctx, query, task.ChatID, task.ChatName, task.Status,
		task.RequestedBy, task.StartTime, task.EndTime)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// GetByID 根据ID获取任务
func (m *MessageSyncTaskModel) GetByID(ctx context.Context, id int64) (*MessageSyncTask, error) {
	query := `SELECT id, chat_id, chat_name, status, total_messages, synced_messages,
              page_token, start_time, end_time, error_msg, requested_by,
              started_at, finished_at, created_at, updated_at
              FROM message_sync_tasks WHERE id = ?`
	var task MessageSyncTask
	err := m.db.QueryRowContext(ctx, query, id).Scan(
		&task.ID, &task.ChatID, &task.ChatName, &task.Status, &task.TotalMessages, &task.SyncedMessages,
		&task.PageToken, &task.StartTime, &task.EndTime, &task.ErrorMsg, &task.RequestedBy,
		&task.StartedAt, &task.FinishedAt, &task.CreatedAt, &task.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// GetPendingTask 获取待处理的任务
func (m *MessageSyncTaskModel) GetPendingTask(ctx context.Context) (*MessageSyncTask, error) {
	query := `SELECT id, chat_id, chat_name, status, total_messages, synced_messages,
              page_token, start_time, end_time, error_msg, requested_by,
              started_at, finished_at, created_at, updated_at
              FROM message_sync_tasks WHERE status IN ('pending', 'running')
              ORDER BY created_at ASC LIMIT 1`
	var task MessageSyncTask
	err := m.db.QueryRowContext(ctx, query).Scan(
		&task.ID, &task.ChatID, &task.ChatName, &task.Status, &task.TotalMessages, &task.SyncedMessages,
		&task.PageToken, &task.StartTime, &task.EndTime, &task.ErrorMsg, &task.RequestedBy,
		&task.StartedAt, &task.FinishedAt, &task.CreatedAt, &task.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// UpdateStatus 更新任务状态
func (m *MessageSyncTaskModel) UpdateStatus(ctx context.Context, id int64, status string) error {
	query := `UPDATE message_sync_tasks SET status = ? WHERE id = ?`
	_, err := m.db.ExecContext(ctx, query, status, id)
	return err
}

// UpdateProgress 更新同步进度
func (m *MessageSyncTaskModel) UpdateProgress(ctx context.Context, id int64, syncedMessages int, pageToken string) error {
	query := `UPDATE message_sync_tasks SET synced_messages = ?, page_token = ? WHERE id = ?`
	_, err := m.db.ExecContext(ctx, query, syncedMessages, pageToken, id)
	return err
}

// MarkStarted 标记任务开始
func (m *MessageSyncTaskModel) MarkStarted(ctx context.Context, id int64) error {
	query := `UPDATE message_sync_tasks SET status = 'running', started_at = NOW() WHERE id = ?`
	_, err := m.db.ExecContext(ctx, query, id)
	return err
}

// MarkCompleted 标记任务完成
func (m *MessageSyncTaskModel) MarkCompleted(ctx context.Context, id int64, totalMessages int) error {
	query := `UPDATE message_sync_tasks SET status = 'completed', total_messages = ?, synced_messages = ?, finished_at = NOW() WHERE id = ?`
	_, err := m.db.ExecContext(ctx, query, totalMessages, totalMessages, id)
	return err
}

// MarkFailed 标记任务失败
func (m *MessageSyncTaskModel) MarkFailed(ctx context.Context, id int64, errMsg string) error {
	query := `UPDATE message_sync_tasks SET status = 'failed', error_msg = ?, finished_at = NOW() WHERE id = ?`
	_, err := m.db.ExecContext(ctx, query, errMsg, id)
	return err
}

// GetRecentTasks 获取最近的任务列表
func (m *MessageSyncTaskModel) GetRecentTasks(ctx context.Context, limit int) ([]*MessageSyncTask, error) {
	query := `SELECT id, chat_id, chat_name, status, total_messages, synced_messages,
              page_token, start_time, end_time, error_msg, requested_by,
              started_at, finished_at, created_at, updated_at
              FROM message_sync_tasks ORDER BY created_at DESC LIMIT ?`
	rows, err := m.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*MessageSyncTask
	for rows.Next() {
		var task MessageSyncTask
		err := rows.Scan(&task.ID, &task.ChatID, &task.ChatName, &task.Status, &task.TotalMessages, &task.SyncedMessages,
			&task.PageToken, &task.StartTime, &task.EndTime, &task.ErrorMsg, &task.RequestedBy,
			&task.StartedAt, &task.FinishedAt, &task.CreatedAt, &task.UpdatedAt)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, &task)
	}
	return tasks, nil
}

// GetGroupFirstMessage 获取群的第一条消息（用于确定群的起始时间）
func (m *ChatMessageModel) GetGroupFirstMessage(ctx context.Context, chatID string) (*ChatMessage, error) {
	query := `SELECT id, message_id, chat_id, sender_id, sender_name, member_id, msg_type,
              content, raw_content, mentions, reply_to_id, is_at_bot, created_at, indexed_at
              FROM chat_messages
              WHERE chat_id = ?
              ORDER BY created_at ASC LIMIT 1`
	var msg ChatMessage
	err := m.db.QueryRowContext(ctx, query, chatID).Scan(
		&msg.ID, &msg.MessageID, &msg.ChatID, &msg.SenderID, &msg.SenderName,
		&msg.MemberID, &msg.MsgType, &msg.Content, &msg.RawContent, &msg.Mentions,
		&msg.ReplyToID, &msg.IsAtBot, &msg.CreatedAt, &msg.IndexedAt)
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

// GetDistinctSendersByDateRange 获取指定时间段内的不重复发送者
func (m *ChatMessageModel) GetDistinctSendersByDateRange(ctx context.Context, chatID string, start, end time.Time) ([]string, error) {
	query := `SELECT DISTINCT sender_name FROM chat_messages
              WHERE chat_id = ? AND created_at BETWEEN ? AND ?
              AND sender_name IS NOT NULL AND sender_name != ''
              ORDER BY sender_name`
	rows, err := m.db.QueryContext(ctx, query, chatID, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var senders []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		senders = append(senders, name)
	}
	return senders, nil
}
