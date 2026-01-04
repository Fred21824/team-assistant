package interfaces

import (
	"context"

	"team-assistant/pkg/lark"
)

// LarkClient 飞书客户端接口
type LarkClient interface {
	// 消息相关
	ReplyMessage(ctx context.Context, messageID, msgType, content string) error
	SendMessage(ctx context.Context, chatID, msgType, content string) error
	SendMessageToUser(ctx context.Context, openID, msgType, content string) error

	// 群聊相关
	GetChats(ctx context.Context) ([]*lark.ChatInfo, error)
	GetChatHistory(ctx context.Context, chatID string, startTime, endTime string, pageSize int, pageToken string) (*lark.GetMessagesResponse, error)

	// 用户相关
	GetUserInfo(ctx context.Context, openID string) (*lark.UserInfo, error)

	// Token 相关
	GetTenantAccessToken(ctx context.Context) (string, error)
}

// LarkEvent 飞书事件相关接口
type LarkEventParser interface {
	ParseMessageContent(msgType, content string) string
	IsAtBot(event interface{}, botOpenID string) bool
	VerifyToken(token, expectedToken string) bool
	DecryptEvent(encrypted, key string) ([]byte, error)
}
