package service

import (
	"strings"
	"unicode/utf8"
)

// Chunk 文本分块
type Chunk struct {
	ID          string                 // 分块ID（原消息ID + 分块序号）
	ParentID    string                 // 原消息ID
	Content     string                 // 分块内容
	ChunkIndex  int                    // 分块索引（从0开始）
	TotalChunks int                    // 总分块数
	StartOffset int                    // 在原文中的起始位置（字符数）
	EndOffset   int                    // 在原文中的结束位置（字符数）
	Metadata    map[string]interface{} // 继承的元数据
}

// ChunkerConfig 分块器配置
type ChunkerConfig struct {
	ChunkSize    int    // 分块大小（字符数），默认 400
	ChunkOverlap int    // 重叠大小（字符数），默认 50
	MinChunkSize int    // 最小分块大小，小于此值不分块，默认 100
	Separators   []rune // 分隔符优先级列表
}

// DefaultChunkerConfig 默认配置
func DefaultChunkerConfig() ChunkerConfig {
	return ChunkerConfig{
		ChunkSize:    400,
		ChunkOverlap: 50,
		MinChunkSize: 100,
		Separators:   []rune{'\n', '。', '！', '？', '；', '.', '!', '?', ';', '，', ',', ' '},
	}
}

// TextChunker 文本分块器
type TextChunker struct {
	config ChunkerConfig
}

// NewTextChunker 创建文本分块器
func NewTextChunker(config ChunkerConfig) *TextChunker {
	if config.ChunkSize <= 0 {
		config.ChunkSize = 400
	}
	if config.ChunkOverlap < 0 {
		config.ChunkOverlap = 0
	}
	if config.ChunkOverlap >= config.ChunkSize {
		config.ChunkOverlap = config.ChunkSize / 4
	}
	if config.MinChunkSize <= 0 {
		config.MinChunkSize = 100
	}
	if len(config.Separators) == 0 {
		config.Separators = DefaultChunkerConfig().Separators
	}
	return &TextChunker{config: config}
}

// NewDefaultChunker 创建默认配置的分块器
func NewDefaultChunker() *TextChunker {
	return NewTextChunker(DefaultChunkerConfig())
}

// ChunkText 对文本进行分块
// parentID: 原消息ID，用于生成分块ID
// metadata: 要继承到每个分块的元数据
func (c *TextChunker) ChunkText(text string, parentID string, metadata map[string]interface{}) []Chunk {
	text = strings.TrimSpace(text)
	textLen := utf8.RuneCountInString(text)

	// 文本太短，不需要分块
	if textLen <= c.config.MinChunkSize {
		return []Chunk{{
			ID:          parentID,
			ParentID:    parentID,
			Content:     text,
			ChunkIndex:  0,
			TotalChunks: 1,
			StartOffset: 0,
			EndOffset:   textLen,
			Metadata:    metadata,
		}}
	}

	// 转换为 rune 切片以正确处理中文
	runes := []rune(text)
	var chunks []Chunk

	start := 0
	chunkIndex := 0

	for start < len(runes) {
		// 计算结束位置
		end := start + c.config.ChunkSize
		if end > len(runes) {
			end = len(runes)
		}

		// 如果不是最后一块，尝试在分隔符处断开
		if end < len(runes) {
			bestBreak := c.findBestBreak(runes, start, end)
			if bestBreak > start {
				end = bestBreak
			}
		}

		// 提取分块内容
		chunkContent := strings.TrimSpace(string(runes[start:end]))

		if len(chunkContent) > 0 {
			chunkID := parentID
			if chunkIndex > 0 || end < len(runes) {
				// 有多个分块时，添加序号后缀
				chunkID = parentID + "_chunk_" + itoa(chunkIndex)
			}

			chunks = append(chunks, Chunk{
				ID:          chunkID,
				ParentID:    parentID,
				Content:     chunkContent,
				ChunkIndex:  chunkIndex,
				StartOffset: start,
				EndOffset:   end,
				Metadata:    metadata,
			})
			chunkIndex++
		}

		// 计算下一个起始位置（考虑重叠）
		if end >= len(runes) {
			break
		}
		start = end - c.config.ChunkOverlap
		if start <= chunks[len(chunks)-1].StartOffset {
			// 避免死循环
			start = end
		}
	}

	// 更新 TotalChunks
	for i := range chunks {
		chunks[i].TotalChunks = len(chunks)
	}

	return chunks
}

// findBestBreak 在指定范围内找到最佳断点
// 优先在句子结束符处断开，其次是逗号、空格等
func (c *TextChunker) findBestBreak(runes []rune, start, end int) int {
	// 从 end 向前搜索，找到最近的分隔符
	searchStart := end - c.config.ChunkOverlap
	if searchStart < start {
		searchStart = start
	}

	// 按分隔符优先级搜索
	for _, sep := range c.config.Separators {
		for i := end - 1; i >= searchStart; i-- {
			if runes[i] == sep {
				return i + 1 // 包含分隔符
			}
		}
	}

	// 没找到分隔符，返回原位置
	return end
}

// ChunkMessage 对消息进行分块（便捷方法）
func (c *TextChunker) ChunkMessage(msg MessageVector) []Chunk {
	metadata := map[string]interface{}{
		"message_id":  msg.MessageID,
		"chat_id":     msg.ChatID,
		"chat_name":   msg.ChatName,
		"sender_id":   msg.SenderID,
		"sender_name": msg.SenderName,
		"created_at":  msg.CreatedAt,
	}
	return c.ChunkText(msg.Content, msg.MessageID, metadata)
}

// ShouldChunk 判断文本是否需要分块
func (c *TextChunker) ShouldChunk(text string) bool {
	return utf8.RuneCountInString(text) > c.config.MinChunkSize
}

// itoa 简单的整数转字符串
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// MergeChunks 合并分块内容（用于展示）
func MergeChunks(chunks []Chunk) string {
	if len(chunks) == 0 {
		return ""
	}
	if len(chunks) == 1 {
		return chunks[0].Content
	}

	var sb strings.Builder
	for i, chunk := range chunks {
		if i > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString(chunk.Content)
	}
	return sb.String()
}
