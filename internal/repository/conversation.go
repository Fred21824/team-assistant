package repository

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	conversationKeyPrefix = "conversation:"
	defaultConversationTTL = 24 * time.Hour // 对话默认保留24小时
)

// ConversationRepository 对话存储（Redis 实现）
type ConversationRepository struct {
	redis *redis.Client
}

// NewConversationRepository 创建对话存储
func NewConversationRepository(rdb *redis.Client) *ConversationRepository {
	return &ConversationRepository{redis: rdb}
}

// GetConversationID 获取对话 ID
func (r *ConversationRepository) GetConversationID(ctx context.Context, userID string) (string, error) {
	key := conversationKeyPrefix + userID
	val, err := r.redis.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", nil // 不存在返回空
	}
	return val, err
}

// SaveConversationID 保存对话 ID
func (r *ConversationRepository) SaveConversationID(ctx context.Context, userID, conversationID string, ttl time.Duration) error {
	if ttl == 0 {
		ttl = defaultConversationTTL
	}
	key := conversationKeyPrefix + userID
	return r.redis.Set(ctx, key, conversationID, ttl).Err()
}

// DeleteConversation 删除对话
func (r *ConversationRepository) DeleteConversation(ctx context.Context, userID string) error {
	key := conversationKeyPrefix + userID
	return r.redis.Del(ctx, key).Err()
}

// ExtendConversationTTL 延长对话过期时间
func (r *ConversationRepository) ExtendConversationTTL(ctx context.Context, userID string, ttl time.Duration) error {
	if ttl == 0 {
		ttl = defaultConversationTTL
	}
	key := conversationKeyPrefix + userID
	return r.redis.Expire(ctx, key, ttl).Err()
}
