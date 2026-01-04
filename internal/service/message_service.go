package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"strconv"
	"time"

	"team-assistant/internal/interfaces"
	"team-assistant/internal/model"
	"team-assistant/pkg/lark"
)

// MessageService 消息服务
type MessageService struct {
	messageRepo interfaces.MessageRepository
	groupRepo   interfaces.GroupRepository
	larkClient  *lark.Client
}

// NewMessageService 创建消息服务
func NewMessageService(
	messageRepo interfaces.MessageRepository,
	groupRepo interfaces.GroupRepository,
	larkClient *lark.Client,
) *MessageService {
	return &MessageService{
		messageRepo: messageRepo,
		groupRepo:   groupRepo,
		larkClient:  larkClient,
	}
}

// StoreMessage 存储消息
func (s *MessageService) StoreMessage(ctx context.Context, event *lark.MessageReceiveEvent, content string, botOpenID string) error {
	if content == "" {
		return nil
	}

	// 解析发送时间
	var sendTime time.Time
	if ts, err := strconv.ParseInt(event.Message.CreateTime, 10, 64); err == nil {
		sendTime = time.UnixMilli(ts)
	} else {
		sendTime = time.Now()
	}

	// 序列化 mentions
	mentionsJSON, _ := json.Marshal(event.Message.Mentions)

	// 检查是否@机器人
	isAtBot := 0
	if lark.IsAtBot(event, botOpenID) {
		isAtBot = 1
	}

	msg := &model.ChatMessage{
		MessageID:  event.Message.MessageID,
		ChatID:     event.Message.ChatID,
		SenderID:   sql.NullString{String: event.Sender.SenderID.OpenID, Valid: true},
		SenderName: sql.NullString{String: "", Valid: false},
		MsgType:    sql.NullString{String: event.Message.MessageType, Valid: true},
		Content:    sql.NullString{String: content, Valid: true},
		RawContent: sql.NullString{String: event.Message.Content, Valid: true},
		Mentions:   mentionsJSON,
		ReplyToID:  sql.NullString{String: event.Message.ParentID, Valid: event.Message.ParentID != ""},
		IsAtBot:    isAtBot,
		CreatedAt:  sendTime,
	}

	if err := s.messageRepo.Insert(ctx, msg); err != nil {
		log.Printf("Failed to store message: %v", err)
		return err
	}

	log.Printf("Stored message: %s from %s", event.Message.MessageID, event.Sender.SenderID.OpenID)
	return nil
}

// SearchMessages 搜索消息
func (s *MessageService) SearchMessages(ctx context.Context, chatID, keyword string, limit int) ([]*model.ChatMessage, error) {
	return s.messageRepo.SearchByContent(ctx, chatID, keyword, limit)
}

// GetRecentMessages 获取最近消息
func (s *MessageService) GetRecentMessages(ctx context.Context, chatID string, limit int) ([]*model.ChatMessage, error) {
	return s.messageRepo.GetRecentMessages(ctx, chatID, limit)
}

// GetMessagesByDateRange 按日期范围获取消息
func (s *MessageService) GetMessagesByDateRange(ctx context.Context, chatID string, start, end time.Time, limit int) ([]*model.ChatMessage, error) {
	return s.messageRepo.GetMessagesByDateRange(ctx, chatID, start, end, limit)
}
