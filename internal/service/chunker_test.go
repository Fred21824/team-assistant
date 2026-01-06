package service

import (
	"testing"
	"time"
)

func TestDefaultChunkerConfig(t *testing.T) {
	config := DefaultChunkerConfig()
	if config.ChunkSize != 400 {
		t.Errorf("Expected ChunkSize=400, got %d", config.ChunkSize)
	}
	if config.ChunkOverlap != 50 {
		t.Errorf("Expected ChunkOverlap=50, got %d", config.ChunkOverlap)
	}
	if config.MinChunkSize != 100 {
		t.Errorf("Expected MinChunkSize=100, got %d", config.MinChunkSize)
	}
}

func TestNewTextChunker(t *testing.T) {
	// 测试默认参数修正
	chunker := NewTextChunker(ChunkerConfig{
		ChunkSize:    0,  // 无效值
		ChunkOverlap: -1, // 无效值
		MinChunkSize: 0,  // 无效值
	})

	if chunker.config.ChunkSize != 400 {
		t.Errorf("Expected ChunkSize to be corrected to 400, got %d", chunker.config.ChunkSize)
	}
	if chunker.config.ChunkOverlap != 0 {
		t.Errorf("Expected ChunkOverlap to be corrected to 0, got %d", chunker.config.ChunkOverlap)
	}
	if chunker.config.MinChunkSize != 100 {
		t.Errorf("Expected MinChunkSize to be corrected to 100, got %d", chunker.config.MinChunkSize)
	}

	// 测试重叠大于分块大小
	chunker2 := NewTextChunker(ChunkerConfig{
		ChunkSize:    100,
		ChunkOverlap: 150, // 大于 ChunkSize
	})
	if chunker2.config.ChunkOverlap >= chunker2.config.ChunkSize {
		t.Errorf("ChunkOverlap should be less than ChunkSize")
	}
}

func TestChunkTextShortText(t *testing.T) {
	chunker := NewDefaultChunker()

	// 短文本不应该被分块
	shortText := "这是一条短消息"
	chunks := chunker.ChunkText(shortText, "msg_001", nil)

	if len(chunks) != 1 {
		t.Errorf("Short text should produce 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Content != shortText {
		t.Errorf("Chunk content should match original text")
	}
	if chunks[0].ID != "msg_001" {
		t.Errorf("Chunk ID should match parent ID for single chunk")
	}
}

func TestChunkTextLongText(t *testing.T) {
	chunker := NewTextChunker(ChunkerConfig{
		ChunkSize:    50,
		ChunkOverlap: 10,
		MinChunkSize: 30,
	})

	// 生成长文本（超过 50 字符，确保会被分块）
	longText := "这是第一段内容，包含一些重要信息。这是第二段内容，继续描述问题。" +
		"这是第三段内容，提供更多细节。这是第四段内容，总结要点。" +
		"这是第五段内容，补充说明。这是第六段内容，最后的内容。"

	chunks := chunker.ChunkText(longText, "msg_002", nil)

	if len(chunks) < 2 {
		t.Errorf("Long text should produce multiple chunks, got %d", len(chunks))
	}

	// 验证分块属性
	for i, chunk := range chunks {
		if chunk.ParentID != "msg_002" {
			t.Errorf("Chunk %d should have correct ParentID", i)
		}
		if chunk.ChunkIndex != i {
			t.Errorf("Chunk %d should have correct ChunkIndex, got %d", i, chunk.ChunkIndex)
		}
		if chunk.TotalChunks != len(chunks) {
			t.Errorf("Chunk %d should have correct TotalChunks", i)
		}
		if chunk.Content == "" {
			t.Errorf("Chunk %d should not be empty", i)
		}
	}

	// 验证分块 ID 格式
	if len(chunks) > 1 {
		if chunks[0].ID != "msg_002_chunk_0" {
			t.Errorf("First chunk ID should be msg_002_chunk_0, got %s", chunks[0].ID)
		}
	}

	t.Logf("Long text (%d chars) split into %d chunks", len([]rune(longText)), len(chunks))
	for i, chunk := range chunks {
		t.Logf("  Chunk %d: %d chars, offset %d-%d", i, len([]rune(chunk.Content)), chunk.StartOffset, chunk.EndOffset)
	}
}

func TestChunkTextWithSeparators(t *testing.T) {
	chunker := NewTextChunker(ChunkerConfig{
		ChunkSize:    20,
		ChunkOverlap: 5,
		MinChunkSize: 10,
	})

	// 文本包含明确的句子分隔符（确保超过 MinChunkSize）
	text := "第一句话内容较长。第二句话内容也长。第三句话继续写。第四句话更多。第五句话结束。"

	chunks := chunker.ChunkText(text, "msg_003", nil)

	// 应该在句号处断开
	for _, chunk := range chunks {
		// 除了最后一块，每块应该以句号结尾（或接近句号）
		t.Logf("Chunk: %q", chunk.Content)
	}

	if len(chunks) < 2 {
		t.Errorf("Text with separators should be split into multiple chunks, got %d", len(chunks))
	}
}

func TestChunkMessage(t *testing.T) {
	chunker := NewTextChunker(ChunkerConfig{
		ChunkSize:    100,
		ChunkOverlap: 20,
		MinChunkSize: 50,
	})

	msg := MessageVector{
		MessageID:  "om_test_123",
		ChatID:     "oc_chat_456",
		ChatName:   "测试群",
		SenderID:   "ou_user_789",
		SenderName: "张三",
		Content:    "这是一条很长的消息内容。包含多个段落和信息。需要被分块处理。这样可以更好地进行语义检索。每个分块都会保留原始消息的元数据。",
		CreatedAt:  time.Now(),
	}

	chunks := chunker.ChunkMessage(msg)

	if len(chunks) == 0 {
		t.Errorf("ChunkMessage should produce at least one chunk")
	}

	// 验证元数据继承
	for _, chunk := range chunks {
		metadata := chunk.Metadata
		if metadata["message_id"] != msg.MessageID {
			t.Errorf("Chunk should inherit message_id")
		}
		if metadata["chat_id"] != msg.ChatID {
			t.Errorf("Chunk should inherit chat_id")
		}
		if metadata["sender_name"] != msg.SenderName {
			t.Errorf("Chunk should inherit sender_name")
		}
	}
}

func TestShouldChunk(t *testing.T) {
	chunker := NewDefaultChunker() // MinChunkSize = 100

	tests := []struct {
		text     string
		expected bool
	}{
		{"短文本", false},
		{"这是一条中等长度的消息", false},
		{string(make([]rune, 50)), false},
		{string(make([]rune, 101)), true},
		{string(make([]rune, 500)), true},
	}

	for _, tt := range tests {
		result := chunker.ShouldChunk(tt.text)
		if result != tt.expected {
			t.Errorf("ShouldChunk(%d chars) = %v, want %v", len([]rune(tt.text)), result, tt.expected)
		}
	}
}

func TestMergeChunks(t *testing.T) {
	chunks := []Chunk{
		{Content: "第一部分"},
		{Content: "第二部分"},
		{Content: "第三部分"},
	}

	merged := MergeChunks(chunks)
	expected := "第一部分 第二部分 第三部分"
	if merged != expected {
		t.Errorf("MergeChunks = %q, want %q", merged, expected)
	}

	// 空切片
	empty := MergeChunks(nil)
	if empty != "" {
		t.Errorf("MergeChunks(nil) should return empty string")
	}

	// 单个分块
	single := MergeChunks([]Chunk{{Content: "单独内容"}})
	if single != "单独内容" {
		t.Errorf("MergeChunks single = %q, want %q", single, "单独内容")
	}
}

func TestItoa(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{10, "10"},
		{123, "123"},
		{9999, "9999"},
	}

	for _, tt := range tests {
		result := itoa(tt.input)
		if result != tt.expected {
			t.Errorf("itoa(%d) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// BenchmarkChunkText 性能测试
func BenchmarkChunkText(b *testing.B) {
	chunker := NewDefaultChunker()

	// 生成 1000 字符的测试文本
	text := ""
	for i := 0; i < 100; i++ {
		text += "这是测试文本。"
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		chunker.ChunkText(text, "test_msg", nil)
	}
}
