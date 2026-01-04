package service

import (
	"context"
	"fmt"
	"log"
	"strings"

	"team-assistant/internal/interfaces"
	"team-assistant/pkg/lark"
)

// ChatService 聊天服务（处理群聊相关业务）
type ChatService struct {
	groupRepo  interfaces.GroupRepository
	larkClient *lark.Client
}

// NewChatService 创建聊天服务
func NewChatService(
	groupRepo interfaces.GroupRepository,
	larkClient *lark.Client,
) *ChatService {
	return &ChatService{
		groupRepo:  groupRepo,
		larkClient: larkClient,
	}
}

// ListChats 列出机器人加入的群聊
func (s *ChatService) ListChats(ctx context.Context) ([]*lark.ChatInfo, error) {
	return s.larkClient.GetChats(ctx)
}

// FindChat 根据名称或 ID 查找群
func (s *ChatService) FindChat(ctx context.Context, target string) (chatID, chatName string, err error) {
	// 如果是 chat_id 格式
	if strings.HasPrefix(target, "oc_") {
		return target, target, nil
	}

	// 按名称查找
	chats, err := s.larkClient.GetChats(ctx)
	if err != nil {
		return "", "", err
	}

	for _, chat := range chats {
		if chat.Name == target || strings.Contains(chat.Name, target) {
			return chat.ChatID, chat.Name, nil
		}
	}

	return "", "", fmt.Errorf("chat not found: %s", target)
}

// FormatChatList 格式化群聊列表
func (s *ChatService) FormatChatList(chats []*lark.ChatInfo) string {
	if len(chats) == 0 {
		return "机器人还没有加入任何群聊"
	}

	var sb strings.Builder
	sb.WriteString("📋 **机器人加入的群聊**\n\n")
	for i, chat := range chats {
		sb.WriteString(fmt.Sprintf("%d. %s\n   ID: %s\n   成员数: %d\n\n",
			i+1, chat.Name, chat.ChatID, chat.MemberCount))
	}
	sb.WriteString("\n💡 发送 \"同步 群名\" 开始同步历史消息")

	return sb.String()
}

// ReplyMessage 回复消息
func (s *ChatService) ReplyMessage(ctx context.Context, messageID, msgType, content string) error {
	if err := s.larkClient.ReplyMessage(ctx, messageID, msgType, content); err != nil {
		log.Printf("Failed to reply message: %v", err)
		return err
	}
	return nil
}

// SendMessageToUser 发送消息给用户
func (s *ChatService) SendMessageToUser(ctx context.Context, openID, msgType, content string) error {
	if err := s.larkClient.SendMessageToUser(ctx, openID, msgType, content); err != nil {
		log.Printf("Failed to send message to user: %v", err)
		return err
	}
	return nil
}
