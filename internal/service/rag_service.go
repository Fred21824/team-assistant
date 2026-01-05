package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"team-assistant/pkg/embedding"
	"team-assistant/pkg/vectordb"
)

// messageIDToUUID 将消息ID转换为UUID格式
func messageIDToUUID(messageID string) string {
	hash := md5.Sum([]byte(messageID))
	hexStr := hex.EncodeToString(hash[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexStr[0:8], hexStr[8:12], hexStr[12:16], hexStr[16:20], hexStr[20:32])
}

// RAGService RAG 服务（检索增强生成）
type RAGService struct {
	embeddingClient *embedding.OllamaClient
	vectorDB        *vectordb.QdrantClient
	collectionName  string
	enabled         bool
}

// MessageVector 消息向量数据
type MessageVector struct {
	MessageID  string    `json:"message_id"`
	ChatID     string    `json:"chat_id"`
	ChatName   string    `json:"chat_name"`
	SenderID   string    `json:"sender_id"`
	SenderName string    `json:"sender_name"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
}

// NewRAGService 创建 RAG 服务
func NewRAGService(qdrantEndpoint, ollamaEndpoint, embeddingModel, collectionName string, embeddingDimension int, enabled bool) *RAGService {
	if !enabled {
		log.Println("RAG service disabled")
		return &RAGService{enabled: false}
	}

	embClient := embedding.NewOllamaClientWithDimension(ollamaEndpoint, embeddingModel, embeddingDimension)
	vectorClient := vectordb.NewQdrantClient(qdrantEndpoint)

	svc := &RAGService{
		embeddingClient: embClient,
		vectorDB:        vectorClient,
		collectionName:  collectionName,
		enabled:         true,
	}

	// 初始化集合
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := svc.initCollection(ctx); err != nil {
		log.Printf("Failed to init RAG collection: %v", err)
	} else {
		log.Printf("RAG service initialized, collection: %s", collectionName)
	}

	return svc
}

// initCollection 初始化向量集合
func (s *RAGService) initCollection(ctx context.Context) error {
	if !s.enabled {
		return nil
	}

	exists, err := s.vectorDB.CollectionExists(ctx, s.collectionName)
	if err != nil {
		return fmt.Errorf("check collection exists: %w", err)
	}

	if !exists {
		dimension := s.embeddingClient.GetDimension()
		if err := s.vectorDB.CreateCollection(ctx, s.collectionName, dimension); err != nil {
			return fmt.Errorf("create collection: %w", err)
		}
		log.Printf("Created vector collection: %s (dimension: %d)", s.collectionName, dimension)
	}

	return nil
}

// IndexMessage 索引单条消息
func (s *RAGService) IndexMessage(ctx context.Context, msg MessageVector) error {
	if !s.enabled {
		return nil
	}

	// 生成 embedding
	vector, err := s.embeddingClient.GetEmbedding(ctx, msg.Content)
	if err != nil {
		return fmt.Errorf("get embedding: %w", err)
	}

	// 存入向量数据库
	point := vectordb.Point{
		ID:     messageIDToUUID(msg.MessageID),
		Vector: vector,
		Payload: map[string]interface{}{
			"message_id":  msg.MessageID,
			"chat_id":     msg.ChatID,
			"chat_name":   msg.ChatName,
			"sender_id":   msg.SenderID,
			"sender_name": msg.SenderName,
			"content":     msg.Content,
			"created_at":  msg.CreatedAt.Format(time.RFC3339),
		},
	}

	if err := s.vectorDB.Upsert(ctx, s.collectionName, []vectordb.Point{point}); err != nil {
		return fmt.Errorf("upsert point: %w", err)
	}

	return nil
}

// IndexMessages 批量索引消息
func (s *RAGService) IndexMessages(ctx context.Context, messages []MessageVector) error {
	if !s.enabled || len(messages) == 0 {
		return nil
	}

	log.Printf("Indexing %d messages to vector DB...", len(messages))

	points := make([]vectordb.Point, 0, len(messages))
	for _, msg := range messages {
		// 跳过空内容
		if strings.TrimSpace(msg.Content) == "" {
			continue
		}

		vector, err := s.embeddingClient.GetEmbedding(ctx, msg.Content)
		if err != nil {
			log.Printf("Failed to get embedding for message %s: %v", msg.MessageID, err)
			continue
		}

		points = append(points, vectordb.Point{
			ID:     messageIDToUUID(msg.MessageID),
			Vector: vector,
			Payload: map[string]interface{}{
				"message_id":  msg.MessageID,
				"chat_id":     msg.ChatID,
				"chat_name":   msg.ChatName,
				"sender_id":   msg.SenderID,
				"sender_name": msg.SenderName,
				"content":     msg.Content,
				"created_at":  msg.CreatedAt.Format(time.RFC3339),
			},
		})
	}

	if len(points) == 0 {
		return nil
	}

	if err := s.vectorDB.Upsert(ctx, s.collectionName, points); err != nil {
		return fmt.Errorf("batch upsert: %w", err)
	}

	log.Printf("Indexed %d messages to vector DB", len(points))
	return nil
}

// SearchResult 搜索结果
type SearchResult struct {
	MessageID  string    `json:"message_id"`
	ChatID     string    `json:"chat_id"`
	ChatName   string    `json:"chat_name"`
	SenderID   string    `json:"sender_id"`
	SenderName string    `json:"sender_name"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
	Score      float32   `json:"score"`
}

// SearchOptions 搜索选项
type SearchOptions struct {
	ChatID     string     // 群ID过滤
	SenderName string     // 发送者名称过滤
	StartTime  *time.Time // 开始时间
	EndTime    *time.Time // 结束时间
}

// Search 语义搜索（简单版本，向后兼容）
func (s *RAGService) Search(ctx context.Context, query string, limit int, chatID string) ([]SearchResult, error) {
	return s.SearchWithOptions(ctx, query, limit, SearchOptions{ChatID: chatID})
}

// SearchWithOptions 带选项的语义搜索
func (s *RAGService) SearchWithOptions(ctx context.Context, query string, limit int, opts SearchOptions) ([]SearchResult, error) {
	if !s.enabled {
		return nil, nil
	}

	// 生成查询的 embedding
	queryVector, err := s.embeddingClient.GetEmbedding(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get query embedding: %w", err)
	}

	// 构建过滤条件
	var mustFilters []map[string]interface{}

	// 群ID过滤
	if opts.ChatID != "" {
		mustFilters = append(mustFilters, map[string]interface{}{
			"key":   "chat_id",
			"match": map[string]interface{}{"value": opts.ChatID},
		})
	}

	// 发送者名称过滤（模糊匹配）
	if opts.SenderName != "" {
		mustFilters = append(mustFilters, map[string]interface{}{
			"key":   "sender_name",
			"match": map[string]interface{}{"text": opts.SenderName},
		})
	}

	// 时间范围过滤
	if opts.StartTime != nil {
		mustFilters = append(mustFilters, map[string]interface{}{
			"key": "created_at",
			"range": map[string]interface{}{
				"gte": opts.StartTime.Format(time.RFC3339),
			},
		})
	}
	if opts.EndTime != nil {
		mustFilters = append(mustFilters, map[string]interface{}{
			"key": "created_at",
			"range": map[string]interface{}{
				"lte": opts.EndTime.Format(time.RFC3339),
			},
		})
	}

	var filter map[string]interface{}
	if len(mustFilters) > 0 {
		filter = map[string]interface{}{
			"must": mustFilters,
		}
	}

	// 向量搜索
	results, err := s.vectorDB.Search(ctx, s.collectionName, queryVector, limit, filter)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}

	// 转换结果
	searchResults := make([]SearchResult, 0, len(results))
	for _, r := range results {
		createdAt, _ := time.Parse(time.RFC3339, getString(r.Payload, "created_at"))
		searchResults = append(searchResults, SearchResult{
			MessageID:  getString(r.Payload, "message_id"),
			ChatID:     getString(r.Payload, "chat_id"),
			ChatName:   getString(r.Payload, "chat_name"),
			SenderID:   getString(r.Payload, "sender_id"),
			SenderName: getString(r.Payload, "sender_name"),
			Content:    getString(r.Payload, "content"),
			CreatedAt:  createdAt,
			Score:      r.Score,
		})
	}

	return searchResults, nil
}

// SearchWithContext 搜索并返回上下文（用于 RAG）
func (s *RAGService) SearchWithContext(ctx context.Context, query string, limit int, chatID string) (string, error) {
	results, err := s.Search(ctx, query, limit, chatID)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "", nil
	}

	// 格式化为上下文字符串
	var sb strings.Builder
	sb.WriteString("以下是相关的历史消息记录：\n\n")
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("[%d] [%s] %s 在「%s」群说：\n%s\n\n",
			i+1,
			r.CreatedAt.Format("01-02 15:04"),
			r.SenderName,
			r.ChatName,
			r.Content,
		))
	}

	return sb.String(), nil
}

// IsEnabled 是否启用
func (s *RAGService) IsEnabled() bool {
	return s.enabled
}

// GetStats 获取统计信息
func (s *RAGService) GetStats(ctx context.Context) map[string]interface{} {
	if !s.enabled {
		return map[string]interface{}{"enabled": false}
	}

	info, err := s.vectorDB.GetCollectionInfo(ctx, s.collectionName)
	if err != nil {
		return map[string]interface{}{
			"enabled": true,
			"error":   err.Error(),
		}
	}

	return map[string]interface{}{
		"enabled":    true,
		"collection": s.collectionName,
		"info":       info,
	}
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// HybridSearchOptions 混合搜索选项
type HybridSearchOptions struct {
	SearchOptions
	Keywords       []string // 关键词列表（用于关键词加权）
	ExpandSynonyms bool     // 是否扩展同义词
	SemanticWeight float32  // 语义搜索权重（0-1），默认 0.6
	KeywordWeight  float32  // 关键词匹配权重（0-1），默认 0.4
	DynamicLimit   bool     // 是否启用动态 limit 调整
}

// DefaultHybridSearchOptions 默认混合搜索选项
func DefaultHybridSearchOptions() HybridSearchOptions {
	return HybridSearchOptions{
		ExpandSynonyms: true,
		SemanticWeight: 0.6,
		KeywordWeight:  0.4,
		DynamicLimit:   true,
	}
}

// HybridSearch 混合搜索（语义 + 关键词融合）
func (s *RAGService) HybridSearch(ctx context.Context, query string, keywords []string, limit int, opts HybridSearchOptions) ([]SearchResult, error) {
	if !s.enabled {
		return nil, nil
	}

	// 1. 同义词扩展
	expandedKeywords := keywords
	if opts.ExpandSynonyms && len(keywords) > 0 {
		expander := NewSynonymExpander()
		expandedKeywords = expander.Expand(keywords)
		log.Printf("[RAG] Keywords expanded: %v -> %v", keywords, expandedKeywords)
	}

	// 2. 动态调整 limit
	actualLimit := limit
	if opts.DynamicLimit {
		actualLimit = s.calculateDynamicLimit(query, expandedKeywords, limit, opts.SearchOptions)
		if actualLimit != limit {
			log.Printf("[RAG] Dynamic limit adjusted: %d -> %d", limit, actualLimit)
		}
	}

	// 3. 执行语义搜索（多取一些用于后续融合）
	semanticLimit := int(float64(actualLimit) * 1.5)
	if semanticLimit < actualLimit+5 {
		semanticLimit = actualLimit + 5
	}

	semanticResults, err := s.SearchWithOptions(ctx, query, semanticLimit, opts.SearchOptions)
	if err != nil {
		return nil, fmt.Errorf("semantic search: %w", err)
	}

	// 4. 如果没有关键词，直接返回语义搜索结果
	if len(expandedKeywords) == 0 {
		if len(semanticResults) > actualLimit {
			return semanticResults[:actualLimit], nil
		}
		return semanticResults, nil
	}

	// 5. 对结果进行关键词加权融合
	fusedResults := s.fuseResults(semanticResults, expandedKeywords, opts.SemanticWeight, opts.KeywordWeight)

	// 6. 截取 top-N
	if len(fusedResults) > actualLimit {
		fusedResults = fusedResults[:actualLimit]
	}

	return fusedResults, nil
}

// calculateDynamicLimit 根据查询特征计算合适的 limit
func (s *RAGService) calculateDynamicLimit(query string, keywords []string, baseLimit int, opts SearchOptions) int {
	multiplier := 1.0

	// 查询长度调整
	queryLen := utf8.RuneCountInString(query)
	if queryLen < 5 {
		// 短查询：需要更多候选结果
		multiplier *= 1.5
	} else if queryLen > 50 {
		// 长查询：更精准，减少结果
		multiplier *= 0.7
	} else if queryLen > 20 {
		multiplier *= 0.85
	}

	// 关键词数量调整
	if len(keywords) > 5 {
		// 多关键词：需要更多候选来匹配
		multiplier *= 1.2
	} else if len(keywords) > 3 {
		multiplier *= 1.1
	}

	// 有时间过滤时：范围缩小，可以多取一些
	if opts.StartTime != nil || opts.EndTime != nil {
		multiplier *= 1.2
	}

	// 有用户过滤时：范围缩小
	if opts.SenderName != "" {
		multiplier *= 0.9
	}

	// 计算最终 limit
	result := int(float64(baseLimit) * multiplier)

	// 限制范围
	if result < 5 {
		result = 5
	}
	if result > 50 {
		result = 50
	}

	return result
}

// fuseResults 融合语义搜索结果和关键词匹配
func (s *RAGService) fuseResults(semanticResults []SearchResult, keywords []string, semanticWeight, keywordWeight float32) []SearchResult {
	if len(semanticResults) == 0 {
		return semanticResults
	}

	// 归一化权重
	totalWeight := semanticWeight + keywordWeight
	if totalWeight == 0 {
		totalWeight = 1
	}
	semWeight := semanticWeight / totalWeight
	kwWeight := keywordWeight / totalWeight

	// 预处理关键词为小写
	lowerKeywords := make([]string, len(keywords))
	for i, kw := range keywords {
		lowerKeywords[i] = strings.ToLower(kw)
	}

	// 计算融合分数
	type scoredResult struct {
		result     SearchResult
		fusedScore float32
	}

	scored := make([]scoredResult, len(semanticResults))
	for i, r := range semanticResults {
		// 语义分数（已归一化到 0-1）
		semanticScore := r.Score

		// 关键词匹配分数
		keywordScore := s.calculateKeywordScore(r.Content, lowerKeywords)

		// 融合分数
		fusedScore := semanticScore*semWeight + keywordScore*kwWeight

		scored[i] = scoredResult{
			result:     r,
			fusedScore: fusedScore,
		}
		scored[i].result.Score = fusedScore // 更新分数为融合分数
	}

	// 按融合分数排序
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].fusedScore > scored[j].fusedScore
	})

	// 提取结果
	results := make([]SearchResult, len(scored))
	for i, s := range scored {
		results[i] = s.result
	}

	return results
}

// calculateKeywordScore 计算关键词匹配分数
func (s *RAGService) calculateKeywordScore(content string, keywords []string) float32 {
	if len(keywords) == 0 {
		return 0
	}

	lowerContent := strings.ToLower(content)
	matchCount := 0

	for _, kw := range keywords {
		if strings.Contains(lowerContent, kw) {
			matchCount++
		}
	}

	// 归一化到 0-1
	return float32(matchCount) / float32(len(keywords))
}

// HybridSearchWithContext 混合搜索并返回格式化上下文
func (s *RAGService) HybridSearchWithContext(ctx context.Context, query string, keywords []string, limit int, opts HybridSearchOptions) (string, error) {
	results, err := s.HybridSearch(ctx, query, keywords, limit, opts)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "", nil
	}

	// 格式化为上下文字符串
	var sb strings.Builder
	sb.WriteString("以下是相关的历史消息记录：\n\n")
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("[%d] [%s] %s 在「%s」群说：\n%s\n\n",
			i+1,
			r.CreatedAt.Format("01-02 15:04"),
			r.SenderName,
			r.ChatName,
			r.Content,
		))
	}

	return sb.String(), nil
}
