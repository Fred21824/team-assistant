package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"

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
func NewRAGService(qdrantEndpoint, ollamaEndpoint, embeddingModel, collectionName string, enabled bool) *RAGService {
	if !enabled {
		log.Println("RAG service disabled")
		return &RAGService{enabled: false}
	}

	embClient := embedding.NewOllamaClient(ollamaEndpoint, embeddingModel)
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

// Search 语义搜索
func (s *RAGService) Search(ctx context.Context, query string, limit int, chatID string) ([]SearchResult, error) {
	if !s.enabled {
		return nil, nil
	}

	// 生成查询的 embedding
	queryVector, err := s.embeddingClient.GetEmbedding(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get query embedding: %w", err)
	}

	// 构建过滤条件
	var filter map[string]interface{}
	if chatID != "" {
		filter = map[string]interface{}{
			"must": []map[string]interface{}{
				{
					"key":   "chat_id",
					"match": map[string]interface{}{"value": chatID},
				},
			},
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
