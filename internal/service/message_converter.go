package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"team-assistant/internal/model"
	"team-assistant/pkg/lark"
)

// RawMessage 统一的原始消息格式（适配两种数据来源）
type RawMessage struct {
	// 基础字段（两种来源共有）
	MessageID  string
	ChatID     string
	MsgType    string
	RawContent string // 原始 JSON 内容
	CreateTime string // 时间戳字符串
	ParentID   string // 回复的消息ID
	RootID     string
	SenderID   string // 发送者 OpenID

	// 可选字段（仅 API 拉取有）
	ThreadID string // 话题ID

	// Mentions 统一格式
	Mentions []MentionInfo
}

// MentionInfo 统一的 mention 格式
type MentionInfo struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	OpenID string `json:"open_id"`
}

// ConvertOptions 消息转换选项
type ConvertOptions struct {
	// 是否检测 @机器人（Webhook 场景需要）
	DetectAtBot bool
	BotOpenID   string

	// 是否分析图片（手动同步场景需要）
	AnalyzeImage  bool
	ImageAnalyzer ImageAnalyzer

	// 用户名获取器（可选，用于填充 SenderName）
	UserNameFetcher UserNameFetcher
}

// ConvertOption 函数式选项
type ConvertOption func(*ConvertOptions)

// ImageAnalyzer 图片分析器接口
type ImageAnalyzer interface {
	AnalyzeImage(ctx context.Context, messageID, rawContent string) string
}

// UserNameFetcher 用户名获取器接口
type UserNameFetcher interface {
	GetUserName(ctx context.Context, chatID, openID string) string
}

// WithAtBotDetection 启用 @机器人 检测
func WithAtBotDetection(botOpenID string) ConvertOption {
	return func(o *ConvertOptions) {
		o.DetectAtBot = true
		o.BotOpenID = botOpenID
	}
}

// WithImageAnalysis 启用图片分析
func WithImageAnalysis(analyzer ImageAnalyzer) ConvertOption {
	return func(o *ConvertOptions) {
		o.AnalyzeImage = true
		o.ImageAnalyzer = analyzer
	}
}

// WithUserNameFetcher 设置用户名获取器
func WithUserNameFetcher(fetcher UserNameFetcher) ConvertOption {
	return func(o *ConvertOptions) {
		o.UserNameFetcher = fetcher
	}
}

// MessageConverter 消息转换器
type MessageConverter struct{}

// NewMessageConverter 创建消息转换器
func NewMessageConverter() *MessageConverter {
	return &MessageConverter{}
}

// Convert 将 RawMessage 转换为 ChatMessage
func (c *MessageConverter) Convert(ctx context.Context, raw RawMessage, opts ...ConvertOption) *model.ChatMessage {
	// 应用选项
	options := &ConvertOptions{}
	for _, opt := range opts {
		opt(options)
	}

	// 1. 解析时间戳
	createTime, createTimeTs := c.ParseTimestamp(raw.CreateTime)

	// 2. 解析消息内容
	content := c.parseContent(ctx, raw, options)

	// 3. 替换 @提及
	content = c.ReplaceMentions(content, raw.Mentions)

	// 4. 序列化 mentions
	mentionsJSON, _ := json.Marshal(raw.Mentions)

	// 5. 检测是否 @机器人
	isAtBot := 0
	if options.DetectAtBot && c.isAtBot(raw.Mentions, options.BotOpenID) {
		isAtBot = 1
	}

	// 6. 获取发送者名称
	senderName := ""
	if options.UserNameFetcher != nil {
		senderName = options.UserNameFetcher.GetUserName(ctx, raw.ChatID, raw.SenderID)
	}

	return &model.ChatMessage{
		MessageID:   raw.MessageID,
		ChatID:      raw.ChatID,
		SenderID:    sql.NullString{String: raw.SenderID, Valid: raw.SenderID != ""},
		SenderName:  sql.NullString{String: senderName, Valid: senderName != ""},
		MsgType:     sql.NullString{String: raw.MsgType, Valid: true},
		Content:     sql.NullString{String: content, Valid: content != ""},
		RawContent:  sql.NullString{String: raw.RawContent, Valid: raw.RawContent != ""},
		Mentions:    mentionsJSON,
		ReplyToID:   sql.NullString{String: raw.ParentID, Valid: raw.ParentID != ""},
		ThreadID:    sql.NullString{String: raw.ThreadID, Valid: raw.ThreadID != ""},
		RootID:      sql.NullString{String: raw.RootID, Valid: raw.RootID != ""},
		IsAtBot:     isAtBot,
		CreatedAt:   createTime,
		CreatedAtTs: sql.NullInt64{Int64: createTimeTs, Valid: true},
	}
}

// ParseTimestamp 解析时间戳（统一逻辑）
func (c *MessageConverter) ParseTimestamp(createTimeStr string) (time.Time, int64) {
	if ts, err := strconv.ParseInt(createTimeStr, 10, 64); err == nil {
		return time.UnixMilli(ts), ts
	}
	now := time.Now()
	return now, now.UnixMilli()
}

// parseContent 解析消息内容
func (c *MessageConverter) parseContent(ctx context.Context, raw RawMessage, opts *ConvertOptions) string {
	// 图片消息特殊处理
	if raw.MsgType == "image" && opts.AnalyzeImage && opts.ImageAnalyzer != nil {
		return opts.ImageAnalyzer.AnalyzeImage(ctx, raw.MessageID, raw.RawContent)
	}
	return lark.ParseMessageContent(raw.MsgType, raw.RawContent)
}

// ReplaceMentions 替换 @提及（统一逻辑）
func (c *MessageConverter) ReplaceMentions(content string, mentions []MentionInfo) string {
	for _, m := range mentions {
		if m.Key != "" && m.Name != "" {
			content = strings.ReplaceAll(content, m.Key, "@"+m.Name)
		}
	}
	return content
}

// isAtBot 检查是否 @机器人
func (c *MessageConverter) isAtBot(mentions []MentionInfo, botOpenID string) bool {
	for _, m := range mentions {
		if m.OpenID == botOpenID {
			return true
		}
	}
	return false
}
