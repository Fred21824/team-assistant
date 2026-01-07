package service

import (
	"context"
	"log"

	"team-assistant/internal/model"
)

// MessageIndexer 消息向量索引器
type MessageIndexer struct {
	rag *RAGService
}

// NewMessageIndexer 创建消息索引器
func NewMessageIndexer(rag *RAGService) *MessageIndexer {
	return &MessageIndexer{rag: rag}
}

// IsEnabled 检查是否启用
func (i *MessageIndexer) IsEnabled() bool {
	return i.rag != nil && i.rag.IsEnabled()
}

// IndexMessage 索引单条消息
func (i *MessageIndexer) IndexMessage(ctx context.Context, msg *model.ChatMessage, chatName string) error {
	if !i.IsEnabled() {
		log.Printf("RAG service not available, skipping vector indexing")
		return nil
	}

	if !msg.Content.Valid || msg.Content.String == "" {
		return nil
	}

	vectorMsg := MessageVector{
		MessageID:  msg.MessageID,
		ChatID:     msg.ChatID,
		ChatName:   chatName,
		SenderID:   msg.SenderID.String,
		SenderName: msg.SenderName.String,
		Content:    msg.Content.String,
		CreatedAt:  msg.CreatedAt,
	}

	if err := i.rag.IndexMessage(ctx, vectorMsg); err != nil {
		log.Printf("Failed to index message to vector DB: %v", err)
		return err
	}

	log.Printf("Indexed message to vector DB: %s", msg.MessageID)
	return nil
}

// IndexMessages 批量索引消息
func (i *MessageIndexer) IndexMessages(ctx context.Context, msgs []*model.ChatMessage, chatName string) error {
	if !i.IsEnabled() {
		return nil
	}

	var vectorMsgs []MessageVector
	for _, msg := range msgs {
		if msg.Content.Valid && msg.Content.String != "" {
			vectorMsgs = append(vectorMsgs, MessageVector{
				MessageID:  msg.MessageID,
				ChatID:     msg.ChatID,
				ChatName:   chatName,
				SenderID:   msg.SenderID.String,
				SenderName: msg.SenderName.String,
				Content:    msg.Content.String,
				CreatedAt:  msg.CreatedAt,
			})
		}
	}

	if len(vectorMsgs) == 0 {
		return nil
	}

	if err := i.rag.IndexMessages(ctx, vectorMsgs); err != nil {
		log.Printf("Failed to index messages to vector DB: %v", err)
		return err
	}

	log.Printf("Indexed %d messages to vector DB", len(vectorMsgs))
	return nil
}
