package service

import (
	"context"
	"testing"
	"time"
)

func TestDefaultRerankerConfig(t *testing.T) {
	config := DefaultRerankerConfig()

	// 验证权重总和为 1.0
	total := config.SemanticWeight + config.KeywordWeight + config.RecencyWeight +
		config.LengthWeight + config.PositionWeight

	if total < 0.99 || total > 1.01 {
		t.Errorf("Weights should sum to 1.0, got %f", total)
	}

	if config.RecencyDecayDays != 7 {
		t.Errorf("Expected RecencyDecayDays=7, got %d", config.RecencyDecayDays)
	}
}

func TestNewReranker(t *testing.T) {
	// 测试权重归一化
	config := RerankerConfig{
		SemanticWeight: 0.8,
		KeywordWeight:  0.6,
		RecencyWeight:  0.4,
		LengthWeight:   0.2,
	}

	reranker := NewReranker(config)
	normalized := reranker.GetConfig()

	total := normalized.SemanticWeight + normalized.KeywordWeight +
		normalized.RecencyWeight + normalized.LengthWeight + normalized.PositionWeight

	if total < 0.99 || total > 1.01 {
		t.Errorf("Normalized weights should sum to 1.0, got %f", total)
	}
}

func TestRerankerKeywordScore(t *testing.T) {
	reranker := NewDefaultReranker()

	tests := []struct {
		content  string
		keywords []string
		minScore float64
		maxScore float64
	}{
		{"登录系统失败，请重试", []string{"登录", "失败"}, 0.5, 1.0},       // 两个关键词都匹配
		{"登录成功，欢迎使用", []string{"登录", "失败"}, 0.2, 0.6},         // 只匹配一个
		{"今天天气很好", []string{"登录", "失败"}, 0.0, 0.1},              // 不匹配
		{"登录登录登录失败失败", []string{"登录", "失败"}, 0.6, 1.0},       // 多次出现
		{"任何内容", []string{}, 0.4, 0.6},                              // 无关键词
	}

	for _, tt := range tests {
		score := reranker.calculateKeywordScore(tt.content, tt.keywords)
		if score < tt.minScore || score > tt.maxScore {
			t.Errorf("KeywordScore(%q, %v) = %f, want [%f, %f]",
				tt.content, tt.keywords, score, tt.minScore, tt.maxScore)
		}
	}
}

func TestRerankerRecencyScore(t *testing.T) {
	reranker := NewDefaultReranker()

	now := time.Now()

	tests := []struct {
		createdAt time.Time
		minScore  float64
		maxScore  float64
	}{
		{now, 0.9, 1.0},                             // 刚才
		{now.Add(-24 * time.Hour), 0.7, 0.95},       // 1天前
		{now.Add(-7 * 24 * time.Hour), 0.3, 0.5},    // 7天前
		{now.Add(-30 * 24 * time.Hour), 0.0, 0.15},  // 30天前
		{time.Time{}, 0.4, 0.6},                     // 无时间信息
	}

	for _, tt := range tests {
		score := reranker.calculateRecencyScore(tt.createdAt)
		if score < tt.minScore || score > tt.maxScore {
			t.Errorf("RecencyScore(%v days ago) = %f, want [%f, %f]",
				time.Since(tt.createdAt).Hours()/24, score, tt.minScore, tt.maxScore)
		}
	}
}

func TestRerankerLengthScore(t *testing.T) {
	reranker := NewDefaultReranker()

	tests := []struct {
		contentLen int
		minScore   float64
		maxScore   float64
	}{
		{100, 0.9, 1.0},  // 理想范围内
		{200, 0.9, 1.0},  // 理想范围内
		{20, 0.3, 0.5},   // 太短
		{1000, 0.4, 0.8}, // 太长
		{5000, 0.2, 0.5}, // 非常长
	}

	for _, tt := range tests {
		content := string(make([]rune, tt.contentLen))
		score := reranker.calculateLengthScore(content, "测试查询")
		if score < tt.minScore || score > tt.maxScore {
			t.Errorf("LengthScore(len=%d) = %f, want [%f, %f]",
				tt.contentLen, score, tt.minScore, tt.maxScore)
		}
	}
}

func TestRerankerPositionScore(t *testing.T) {
	reranker := NewDefaultReranker()

	tests := []struct {
		content  string
		keywords []string
		minScore float64
		maxScore float64
	}{
		{"登录失败，请检查密码", []string{"登录"}, 0.9, 1.0},     // 开头
		{"用户登录失败的处理", []string{"登录"}, 0.7, 0.95},      // 中间偏前
		{"这是一条关于登录的消息", []string{"登录"}, 0.6, 0.85},  // 中间偏后
		{"这是内容不包含关键词", []string{"登录"}, 0.0, 0.1},     // 不包含
	}

	for _, tt := range tests {
		score := reranker.calculatePositionScore(tt.content, tt.keywords)
		if score < tt.minScore || score > tt.maxScore {
			t.Errorf("PositionScore(%q, %v) = %f, want [%f, %f]",
				tt.content, tt.keywords, score, tt.minScore, tt.maxScore)
		}
	}
}

func TestRerankResults(t *testing.T) {
	reranker := NewDefaultReranker()
	ctx := context.Background()

	now := time.Now()

	// 创建测试结果
	results := []SearchResult{
		{
			MessageID:  "msg_1",
			Content:    "今天天气很好，适合出门",
			Score:      0.9, // 高语义分数但不相关
			CreatedAt:  now.Add(-30 * 24 * time.Hour),
		},
		{
			MessageID:  "msg_2",
			Content:    "用户登录失败，请检查密码是否正确",
			Score:      0.7, // 中等语义分数但相关
			CreatedAt:  now.Add(-1 * time.Hour),
		},
		{
			MessageID:  "msg_3",
			Content:    "登录系统时遇到错误",
			Score:      0.6, // 较低语义分数但相关且新
			CreatedAt:  now,
		},
	}

	keywords := []string{"登录", "失败"}

	reranked := reranker.Rerank(ctx, "登录失败怎么办", keywords, results)

	if len(reranked) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(reranked))
	}

	// msg_2 或 msg_3 应该排在第一位（因为包含关键词且较新）
	if reranked[0].MessageID == "msg_1" {
		t.Errorf("msg_1 (unrelated) should not be first after reranking")
	}

	t.Logf("Rerank order: %s, %s, %s",
		reranked[0].MessageID,
		reranked[1].MessageID,
		reranked[2].MessageID)
}

func TestRerankWithDetail(t *testing.T) {
	reranker := NewDefaultReranker()
	ctx := context.Background()

	results := []SearchResult{
		{
			MessageID: "msg_1",
			Content:   "登录失败的处理方法",
			Score:     0.8,
			CreatedAt: time.Now(),
		},
	}

	keywords := []string{"登录", "失败"}

	outputs := reranker.RerankWithDetail(ctx, "登录失败", keywords, results)

	if len(outputs) != 1 {
		t.Fatalf("Expected 1 output, got %d", len(outputs))
	}

	detail := outputs[0].ScoreDetail

	// 验证各维度分数都在合理范围
	if detail.SemanticScore < 0 || detail.SemanticScore > 1 {
		t.Errorf("SemanticScore out of range: %f", detail.SemanticScore)
	}
	if detail.KeywordScore < 0 || detail.KeywordScore > 1 {
		t.Errorf("KeywordScore out of range: %f", detail.KeywordScore)
	}
	if detail.RecencyScore < 0 || detail.RecencyScore > 1 {
		t.Errorf("RecencyScore out of range: %f", detail.RecencyScore)
	}
	if detail.LengthScore < 0 || detail.LengthScore > 1 {
		t.Errorf("LengthScore out of range: %f", detail.LengthScore)
	}
	if detail.PositionScore < 0 || detail.PositionScore > 1 {
		t.Errorf("PositionScore out of range: %f", detail.PositionScore)
	}

	t.Logf("Score detail: semantic=%.2f, keyword=%.2f, recency=%.2f, length=%.2f, position=%.2f",
		detail.SemanticScore, detail.KeywordScore, detail.RecencyScore,
		detail.LengthScore, detail.PositionScore)
}

func TestRerankEmptyResults(t *testing.T) {
	reranker := NewDefaultReranker()
	ctx := context.Background()

	results := reranker.Rerank(ctx, "测试", []string{"测试"}, nil)
	if len(results) != 0 {
		t.Errorf("Expected empty results, got %d", len(results))
	}

	results = reranker.Rerank(ctx, "测试", []string{"测试"}, []SearchResult{})
	if len(results) != 0 {
		t.Errorf("Expected empty results, got %d", len(results))
	}
}

// BenchmarkRerank 性能测试
func BenchmarkRerank(b *testing.B) {
	reranker := NewDefaultReranker()
	ctx := context.Background()

	// 准备测试数据
	results := make([]SearchResult, 20)
	for i := range results {
		results[i] = SearchResult{
			MessageID: "msg_test",
			Content:   "这是一条测试消息，包含登录和失败等关键词，用于测试重排序性能",
			Score:     0.8,
			CreatedAt: time.Now().Add(-time.Duration(i) * time.Hour),
		}
	}

	keywords := []string{"登录", "失败", "测试"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reranker.Rerank(ctx, "登录失败测试", keywords, results)
	}
}
