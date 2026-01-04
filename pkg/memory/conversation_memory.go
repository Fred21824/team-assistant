package memory

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/memory"
)

// ConversationMemory 对话记忆管理器
// 结合 langchaingo 的 Memory 接口和 MySQL 持久化存储
type ConversationMemory struct {
	buffer       *memory.ConversationBuffer
	history      *MySQLChatMessageHistory
	windowSize   int           // 窗口大小，保留最近 N 轮对话
	inputKey     string
	outputKey    string
	memoryKey    string
	humanPrefix  string
	aiPrefix     string
	mu           sync.RWMutex
}

// ConversationMemoryOption 配置选项
type ConversationMemoryOption func(*ConversationMemory)

// WithWindowSize 设置窗口大小
func WithMemoryWindowSize(size int) ConversationMemoryOption {
	return func(m *ConversationMemory) {
		m.windowSize = size
	}
}

// WithInputKey 设置输入键
func WithInputKey(key string) ConversationMemoryOption {
	return func(m *ConversationMemory) {
		m.inputKey = key
	}
}

// WithOutputKey 设置输出键
func WithOutputKey(key string) ConversationMemoryOption {
	return func(m *ConversationMemory) {
		m.outputKey = key
	}
}

// WithMemoryKey 设置记忆键
func WithMemoryKey(key string) ConversationMemoryOption {
	return func(m *ConversationMemory) {
		m.memoryKey = key
	}
}

// WithHumanPrefix 设置人类前缀
func WithHumanPrefix(prefix string) ConversationMemoryOption {
	return func(m *ConversationMemory) {
		m.humanPrefix = prefix
	}
}

// WithAIPrefix 设置 AI 前缀
func WithAIPrefix(prefix string) ConversationMemoryOption {
	return func(m *ConversationMemory) {
		m.aiPrefix = prefix
	}
}

// NewConversationMemory 创建对话记忆管理器
func NewConversationMemory(db *sql.DB, sessionID, userID string, opts ...ConversationMemoryOption) (*ConversationMemory, error) {
	// 创建 MySQL 历史存储
	history, err := NewMySQLChatMessageHistory(db,
		WithSessionID(sessionID),
		WithUserID(userID),
	)
	if err != nil {
		return nil, err
	}

	m := &ConversationMemory{
		history:     history,
		windowSize:  10, // 默认保留最近 10 轮对话
		inputKey:    "input",
		outputKey:   "output",
		memoryKey:   "history",
		humanPrefix: "用户",
		aiPrefix:    "助手",
	}

	for _, opt := range opts {
		opt(m)
	}

	// 创建底层的 ConversationBuffer
	m.buffer = memory.NewConversationBuffer(
		memory.WithInputKey(m.inputKey),
		memory.WithOutputKey(m.outputKey),
		memory.WithMemoryKey(m.memoryKey),
		memory.WithHumanPrefix(m.humanPrefix),
		memory.WithAIPrefix(m.aiPrefix),
	)

	// 加载历史记录到缓冲区
	if err := m.loadHistory(context.Background()); err != nil {
		return nil, err
	}

	return m, nil
}

// loadHistory 加载历史记录
func (m *ConversationMemory) loadHistory(ctx context.Context) error {
	messages, err := m.history.GetRecentMessages(ctx, m.windowSize*2) // 每轮 2 条消息
	if err != nil {
		return err
	}

	// 将消息加载到缓冲区
	for i := 0; i < len(messages); i += 2 {
		if i+1 < len(messages) {
			humanMsg := messages[i]
			aiMsg := messages[i+1]

			inputValues := map[string]any{m.inputKey: humanMsg.GetContent()}
			outputValues := map[string]any{m.outputKey: aiMsg.GetContent()}
			m.buffer.SaveContext(ctx, inputValues, outputValues)
		}
	}

	return nil
}

// LoadMemoryVariables 加载记忆变量
func (m *ConversationMemory) LoadMemoryVariables(ctx context.Context, inputs map[string]any) (map[string]any, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.buffer.LoadMemoryVariables(ctx, inputs)
}

// SaveContext 保存上下文
func (m *ConversationMemory) SaveContext(ctx context.Context, inputValues, outputValues map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 保存到内存缓冲区
	if err := m.buffer.SaveContext(ctx, inputValues, outputValues); err != nil {
		return err
	}

	// 保存到 MySQL
	input, ok := inputValues[m.inputKey].(string)
	if !ok {
		input = fmt.Sprintf("%v", inputValues[m.inputKey])
	}

	output, ok := outputValues[m.outputKey].(string)
	if !ok {
		output = fmt.Sprintf("%v", outputValues[m.outputKey])
	}

	if err := m.history.AddUserMessage(ctx, input); err != nil {
		return err
	}
	if err := m.history.AddAIMessage(ctx, output); err != nil {
		return err
	}

	return nil
}

// MemoryVariables 获取记忆变量名
func (m *ConversationMemory) MemoryVariables(ctx context.Context) []string {
	return m.buffer.MemoryVariables(ctx)
}

// Clear 清除记忆
func (m *ConversationMemory) Clear(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.buffer.Clear(ctx); err != nil {
		return err
	}
	return m.history.Clear(ctx)
}

// GetMemoryKey 获取记忆键
func (m *ConversationMemory) GetMemoryKey(ctx context.Context) string {
	return m.buffer.GetMemoryKey(ctx)
}

// GetHistory 获取聊天历史
func (m *ConversationMemory) GetHistory() *MySQLChatMessageHistory {
	return m.history
}

// GetMessages 获取所有消息
func (m *ConversationMemory) GetMessages(ctx context.Context) ([]llms.ChatMessage, error) {
	return m.history.Messages(ctx)
}

// GetRecentMessages 获取最近 N 条消息
func (m *ConversationMemory) GetRecentMessages(ctx context.Context, n int) ([]llms.ChatMessage, error) {
	return m.history.GetRecentMessages(ctx, n)
}

// SearchMemory 搜索记忆
func (m *ConversationMemory) SearchMemory(ctx context.Context, keyword string, limit int) ([]ChatHistoryMessage, error) {
	return m.history.SearchMessages(ctx, keyword, limit)
}

// GetFormattedHistory 获取格式化的历史记录
func (m *ConversationMemory) GetFormattedHistory(ctx context.Context) (string, error) {
	messages, err := m.history.GetRecentMessages(ctx, m.windowSize*2)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	for _, msg := range messages {
		switch msg.GetType() {
		case llms.ChatMessageTypeHuman:
			sb.WriteString(fmt.Sprintf("%s: %s\n", m.humanPrefix, msg.GetContent()))
		case llms.ChatMessageTypeAI:
			sb.WriteString(fmt.Sprintf("%s: %s\n", m.aiPrefix, msg.GetContent()))
		}
	}

	return sb.String(), nil
}

// CompressHistory 压缩历史记录（生成摘要并清理旧消息）
func (m *ConversationMemory) CompressHistory(ctx context.Context, summaryFunc func([]llms.ChatMessage) (string, error)) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 获取所有消息
	messages, err := m.history.Messages(ctx)
	if err != nil {
		return err
	}

	// 如果消息数量超过阈值，生成摘要
	if len(messages) > m.windowSize*4 {
		// 取旧消息生成摘要
		oldMessages := messages[:len(messages)-m.windowSize*2]
		summary, err := summaryFunc(oldMessages)
		if err != nil {
			return err
		}

		// 保存摘要
		if err := m.history.SaveSessionSummary(ctx, summary); err != nil {
			return err
		}

		// 只保留最近的消息
		recentMessages := messages[len(messages)-m.windowSize*2:]
		if err := m.history.SetMessages(ctx, recentMessages); err != nil {
			return err
		}
	}

	return nil
}
