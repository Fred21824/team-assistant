package memory

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// MemoryManager 记忆管理器
// 管理多用户的对话记忆，支持 Redis 缓存 + MySQL 持久化
type MemoryManager struct {
	db           *sql.DB
	redis        *redis.Client
	memories     map[string]*ConversationMemory // key: userID:sessionID
	mu           sync.RWMutex
	windowSize   int
	cacheExpiry  time.Duration
}

// MemoryManagerOption 配置选项
type MemoryManagerOption func(*MemoryManager)

// WithDefaultWindowSize 设置默认窗口大小
func WithDefaultWindowSize(size int) MemoryManagerOption {
	return func(m *MemoryManager) {
		m.windowSize = size
	}
}

// WithCacheExpiry 设置缓存过期时间
func WithCacheExpiry(expiry time.Duration) MemoryManagerOption {
	return func(m *MemoryManager) {
		m.cacheExpiry = expiry
	}
}

// NewMemoryManager 创建记忆管理器
func NewMemoryManager(db *sql.DB, redis *redis.Client, opts ...MemoryManagerOption) *MemoryManager {
	m := &MemoryManager{
		db:          db,
		redis:       redis,
		memories:    make(map[string]*ConversationMemory),
		windowSize:  10,
		cacheExpiry: 24 * time.Hour,
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// GetMemory 获取用户的对话记忆
func (m *MemoryManager) GetMemory(ctx context.Context, userID, sessionID string) (*ConversationMemory, error) {
	key := fmt.Sprintf("%s:%s", userID, sessionID)

	m.mu.RLock()
	mem, exists := m.memories[key]
	m.mu.RUnlock()

	if exists {
		return mem, nil
	}

	// 创建新的记忆
	m.mu.Lock()
	defer m.mu.Unlock()

	// 双重检查
	if mem, exists := m.memories[key]; exists {
		return mem, nil
	}

	mem, err := NewConversationMemory(m.db, sessionID, userID,
		WithMemoryWindowSize(m.windowSize),
		WithHumanPrefix("用户"),
		WithAIPrefix("助手"),
	)
	if err != nil {
		return nil, err
	}

	m.memories[key] = mem
	return mem, nil
}

// GetOrCreateSession 获取或创建会话
func (m *MemoryManager) GetOrCreateSession(ctx context.Context, userID string) (string, *ConversationMemory, error) {
	// 尝试从 Redis 获取当前会话 ID
	sessionKey := fmt.Sprintf("session:%s:current", userID)
	sessionID, err := m.redis.Get(ctx, sessionKey).Result()

	if err == redis.Nil || sessionID == "" {
		// 创建新会话
		sessionID = fmt.Sprintf("%s_%d", userID, time.Now().UnixNano())
		m.redis.Set(ctx, sessionKey, sessionID, m.cacheExpiry)
	} else if err != nil {
		return "", nil, err
	}

	// 获取记忆
	mem, err := m.GetMemory(ctx, userID, sessionID)
	if err != nil {
		return "", nil, err
	}

	// 刷新 Redis 过期时间
	m.redis.Expire(ctx, sessionKey, m.cacheExpiry)

	return sessionID, mem, nil
}

// StartNewSession 开始新会话
func (m *MemoryManager) StartNewSession(ctx context.Context, userID string) (string, *ConversationMemory, error) {
	sessionID := fmt.Sprintf("%s_%d", userID, time.Now().UnixNano())

	// 更新 Redis 中的当前会话
	sessionKey := fmt.Sprintf("session:%s:current", userID)
	m.redis.Set(ctx, sessionKey, sessionID, m.cacheExpiry)

	// 创建新记忆
	mem, err := m.GetMemory(ctx, userID, sessionID)
	if err != nil {
		return "", nil, err
	}

	return sessionID, mem, nil
}

// SaveMessage 保存消息到记忆
func (m *MemoryManager) SaveMessage(ctx context.Context, userID, sessionID, input, output string) error {
	mem, err := m.GetMemory(ctx, userID, sessionID)
	if err != nil {
		return err
	}

	return mem.SaveContext(ctx,
		map[string]any{"input": input},
		map[string]any{"output": output},
	)
}

// GetContextForPrompt 获取用于提示词的上下文
func (m *MemoryManager) GetContextForPrompt(ctx context.Context, userID, sessionID string) (string, error) {
	mem, err := m.GetMemory(ctx, userID, sessionID)
	if err != nil {
		return "", err
	}

	return mem.GetFormattedHistory(ctx)
}

// SearchAcrossSessions 跨会话搜索
func (m *MemoryManager) SearchAcrossSessions(ctx context.Context, userID, keyword string, limit int) ([]ChatHistoryMessage, error) {
	// 创建一个临时的历史存储来搜索
	history, err := NewMySQLChatMessageHistory(m.db,
		WithUserID(userID),
		WithSessionID(""), // 空会话 ID 表示搜索所有会话
	)
	if err != nil {
		return nil, err
	}

	return history.SearchMessages(ctx, keyword, limit)
}

// GetUserSessions 获取用户所有会话
func (m *MemoryManager) GetUserSessions(ctx context.Context, userID string) ([]string, error) {
	history, err := NewMySQLChatMessageHistory(m.db, WithUserID(userID))
	if err != nil {
		return nil, err
	}

	return history.GetAllUserSessions(ctx, userID)
}

// ClearSession 清除指定会话
func (m *MemoryManager) ClearSession(ctx context.Context, userID, sessionID string) error {
	mem, err := m.GetMemory(ctx, userID, sessionID)
	if err != nil {
		return err
	}

	// 从内存缓存中删除
	key := fmt.Sprintf("%s:%s", userID, sessionID)
	m.mu.Lock()
	delete(m.memories, key)
	m.mu.Unlock()

	// 清除持久化数据
	return mem.Clear(ctx)
}

// ClearAllUserSessions 清除用户所有会话
func (m *MemoryManager) ClearAllUserSessions(ctx context.Context, userID string) error {
	sessions, err := m.GetUserSessions(ctx, userID)
	if err != nil {
		return err
	}

	for _, sessionID := range sessions {
		if err := m.ClearSession(ctx, userID, sessionID); err != nil {
			return err
		}
	}

	// 清除 Redis 中的当前会话
	sessionKey := fmt.Sprintf("session:%s:current", userID)
	m.redis.Del(ctx, sessionKey)

	return nil
}

// GetStats 获取统计信息
func (m *MemoryManager) GetStats(ctx context.Context) map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return map[string]interface{}{
		"active_memories": len(m.memories),
		"window_size":     m.windowSize,
		"cache_expiry":    m.cacheExpiry.String(),
	}
}

// Cleanup 清理过期的内存缓存
func (m *MemoryManager) Cleanup(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 简单清理：删除所有缓存（可以根据需要实现更复杂的 LRU 策略）
	m.memories = make(map[string]*ConversationMemory)
}
