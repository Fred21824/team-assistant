package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"team-assistant/internal/logic/ai"
	"team-assistant/internal/model"
	"team-assistant/internal/service"
	"team-assistant/internal/svc"
	"team-assistant/pkg/lark"
)

// MessageSyncer 简化的消息同步器接口
type MessageSyncer interface {
	CreateSyncTask(ctx context.Context, chatID, chatName, requestedBy string) (int64, error)
}

// LarkWebhookHandler 处理飞书事件回调
type LarkWebhookHandler struct {
	svcCtx       *svc.ServiceContext
	processor    *ai.HybridProcessor
	msgSyncer    MessageSyncer
	// 用户名缓存 (chatID -> (openID -> name))
	userCache   map[string]map[string]string
	userCacheMu sync.RWMutex
}

// NewLarkWebhookHandler 创建飞书Webhook处理器
func NewLarkWebhookHandler(svcCtx *svc.ServiceContext) *LarkWebhookHandler {
	return &LarkWebhookHandler{
		svcCtx:    svcCtx,
		processor: ai.NewHybridProcessor(svcCtx),
		userCache: make(map[string]map[string]string),
	}
}

// SetMessageSyncer 设置消息同步器
func (h *LarkWebhookHandler) SetMessageSyncer(syncer MessageSyncer) {
	h.msgSyncer = syncer
}

// safeGo 安全地启动一个 goroutine，捕获 panic 防止程序崩溃
func safeGo(fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[PANIC] Goroutine panic recovered: %v", r)
			}
		}()
		fn()
	}()
}

// Handle 处理飞书事件
func (h *LarkWebhookHandler) Handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Failed to read request body: %v", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	log.Printf("Received Lark event: %s", string(body))

	// 检查是否是加密消息
	var encryptedMsg struct {
		Encrypt string `json:"encrypt"`
	}
	if err := json.Unmarshal(body, &encryptedMsg); err == nil && encryptedMsg.Encrypt != "" {
		// 解密消息
		decrypted, err := lark.DecryptEvent(encryptedMsg.Encrypt, h.svcCtx.Config.Lark.EncryptKey)
		if err != nil {
			log.Printf("Failed to decrypt event: %v", err)
			http.Error(w, "Decrypt Failed", http.StatusBadRequest)
			return
		}
		body = decrypted
		log.Printf("Decrypted event: %s", string(body))
	}

	// 解析事件
	var callback lark.EventCallback
	if err := json.Unmarshal(body, &callback); err != nil {
		log.Printf("Failed to parse event: %v", err)
		http.Error(w, "Parse Failed", http.StatusBadRequest)
		return
	}

	// 处理URL验证（首次配置回调时）
	if callback.Type == "url_verification" || callback.Challenge != "" {
		log.Printf("URL verification challenge: %s", callback.Challenge)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"challenge": callback.Challenge,
		})
		return
	}

	// 验证Token（如果配置了）
	if h.svcCtx.Config.Lark.VerificationToken != "" {
		token := callback.Token
		if callback.Header != nil {
			token = callback.Header.Token
		}
		if !lark.VerifyToken(token, h.svcCtx.Config.Lark.VerificationToken) {
			log.Printf("Token verification failed")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	// 处理事件
	eventType := callback.Type
	if callback.Header != nil {
		eventType = callback.Header.EventType
	}

	switch eventType {
	case "im.message.receive_v1":
		h.handleMessageReceive(callback.Event)
	default:
		log.Printf("Unknown event type: %s", eventType)
	}

	// 立即返回成功（异步处理）
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// handleMessageReceive 处理消息接收事件
func (h *LarkWebhookHandler) handleMessageReceive(eventData json.RawMessage) {
	var event lark.MessageReceiveEvent
	if err := json.Unmarshal(eventData, &event); err != nil {
		log.Printf("Failed to parse message event: %v", err)
		return
	}

	// 解析消息内容
	content := lark.ParseMessageContent(event.Message.MessageType, event.Message.Content)

	// 存储所有消息用于后续搜索（仅群聊消息）
	if event.Message.ChatType == "group" {
		safeGo(func() { h.storeMessage(&event, content) })
	}

	// 私聊消息直接处理命令
	if event.Message.ChatType == "p2p" {
		content = strings.TrimSpace(content)
		if content != "" {
			// 检查私聊权限
			if !h.checkPrivateChatPermission(&event) {
				safeGo(func() { h.replyNoPrivateChatPermission(&event) })
				return
			}
			// 检查是否包含图片（纯图片或富文本图片）
			if lark.HasImage(content) {
				log.Printf("Received private message with image: %s", content)
				safeGo(func() { h.handlePrivateImageMessage(&event, content) })
				return
			}
			log.Printf("Received private message: %s", content)
			safeGo(func() { h.handlePrivateCommand(&event, content) })
		}
		return
	}

	// 群聊消息需要检查是否@机器人
	isAtBot := lark.IsAtBot(&event, h.svcCtx.Config.Lark.BotOpenID)
	if !isAtBot {
		return
	}

	if content == "" {
		return
	}

	// 检查群聊成员数权限
	if !h.checkGroupPermission(&event) {
		safeGo(func() { h.replyNoGroupPermission(&event) })
		return
	}

	// 移除@信息
	content = lark.ExtractTextFromMentions(content)
	content = strings.TrimSpace(content)

	// 移除@用户名标记
	for _, mention := range event.Message.Mentions {
		content = strings.ReplaceAll(content, mention.Key, "")
	}
	content = strings.TrimSpace(content)

	if content == "" {
		return
	}

	log.Printf("Received bot message: %s", content)

	// 处理用户查询
	safeGo(func() { h.processQuery(event.Message.ChatID, event.Message.MessageID, content) })
}

// getUserName 获取用户名（带缓存）
func (h *LarkWebhookHandler) getUserName(ctx context.Context, chatID, openID string) string {
	if openID == "" {
		return ""
	}

	// 跳过机器人ID
	if strings.HasPrefix(openID, "cli_") {
		return "机器人"
	}

	// 先检查缓存
	h.userCacheMu.RLock()
	if chatCache, ok := h.userCache[chatID]; ok {
		if name, ok := chatCache[openID]; ok {
			h.userCacheMu.RUnlock()
			return name
		}
	}
	h.userCacheMu.RUnlock()

	// 缓存未命中，加载群成员
	members, err := h.svcCtx.LarkClient.GetChatMembers(ctx, chatID)
	if err != nil {
		log.Printf("Failed to get chat members for %s: %v", chatID, err)
		return ""
	}

	// 更新缓存
	h.userCacheMu.Lock()
	h.userCache[chatID] = members
	h.userCacheMu.Unlock()

	if name, ok := members[openID]; ok {
		return name
	}
	return ""
}

// storeMessage 存储消息到数据库
func (h *LarkWebhookHandler) storeMessage(event *lark.MessageReceiveEvent, content string) {
	if content == "" {
		return
	}

	ctx := context.Background()

	// 解析发送时间（飞书返回的是毫秒时间戳）
	var sendTime time.Time
	var sendTimeTs int64
	if ts, err := strconv.ParseInt(event.Message.CreateTime, 10, 64); err == nil {
		sendTimeTs = ts
		sendTime = time.UnixMilli(ts)
	} else {
		sendTimeTs = time.Now().UnixMilli()
		sendTime = time.Now()
	}

	// 替换 @_user_N 为真实用户名
	for _, mention := range event.Message.Mentions {
		if mention.Key != "" && mention.Name != "" {
			content = strings.ReplaceAll(content, mention.Key, "@"+mention.Name)
		}
	}

	// 序列化 mentions
	mentionsJSON, _ := json.Marshal(event.Message.Mentions)

	// 检查是否@机器人
	isAtBot := 0
	if lark.IsAtBot(event, h.svcCtx.Config.Lark.BotOpenID) {
		isAtBot = 1
	}

	// 获取发送者名称
	senderName := h.getUserName(ctx, event.Message.ChatID, event.Sender.SenderID.OpenID)

	msg := &model.ChatMessage{
		MessageID:   event.Message.MessageID,
		ChatID:      event.Message.ChatID,
		SenderID:    sql.NullString{String: event.Sender.SenderID.OpenID, Valid: true},
		SenderName:  sql.NullString{String: senderName, Valid: senderName != ""},
		MsgType:     sql.NullString{String: event.Message.MessageType, Valid: true},
		Content:     sql.NullString{String: content, Valid: true},
		RawContent:  sql.NullString{String: event.Message.Content, Valid: true},
		Mentions:    mentionsJSON,
		ReplyToID:   sql.NullString{String: event.Message.ParentID, Valid: event.Message.ParentID != ""},
		RootID:      sql.NullString{String: event.Message.RootID, Valid: event.Message.RootID != ""},
		IsAtBot:     isAtBot,
		CreatedAt:   sendTime,
		CreatedAtTs: sql.NullInt64{Int64: sendTimeTs, Valid: true},
	}

	if err := h.svcCtx.MessageModel.Insert(ctx, msg); err != nil {
		log.Printf("Failed to store message: %v", err)
	} else {
		log.Printf("Stored message: %s from %s (%s)", event.Message.MessageID, senderName, event.Sender.SenderID.OpenID)

		// 同时索引到向量数据库（用于 RAG 语义搜索）
		if h.svcCtx.Services != nil && h.svcCtx.Services.RAG != nil && h.svcCtx.Services.RAG.IsEnabled() {
			// 获取群名称
			chatName := ""
			if group, err := h.svcCtx.GroupModel.FindByChatID(ctx, event.Message.ChatID); err == nil && group != nil {
				if group.ChatName.Valid {
					chatName = group.ChatName.String
				}
			}

			vectorMsg := service.MessageVector{
				MessageID:  event.Message.MessageID,
				ChatID:     event.Message.ChatID,
				ChatName:   chatName,
				SenderID:   event.Sender.SenderID.OpenID,
				SenderName: senderName,
				Content:    content,
				CreatedAt:  sendTime,
			}
			if err := h.svcCtx.Services.RAG.IndexMessage(ctx, vectorMsg); err != nil {
				log.Printf("Failed to index message to vector DB: %v", err)
			} else {
				log.Printf("Indexed message to vector DB: %s", event.Message.MessageID)
			}
		} else {
			log.Printf("RAG service not available, skipping vector indexing")
		}
	}
}

// processQuery 处理用户查询
func (h *LarkWebhookHandler) processQuery(chatID, messageID, query string) {
	ctx := context.Background()

	log.Printf("Processing query: %s", query)

	// 检查是否配置了 AI 功能（Dify 或 LLM）
	hasAI := h.svcCtx.Config.Dify.Enabled && h.svcCtx.Config.Dify.APIKey != "" ||
		h.svcCtx.Config.LLM.APIKey != ""

	if !hasAI {
		reply := "⚠️ AI 功能未配置，请联系管理员设置 Dify 或 LLM API Key。\n\n当前支持的命令：\n• 输入 \"帮助\" 查看使用指南"
		if query == "帮助" || query == "help" {
			reply = h.getHelpMessage()
		}
		if err := h.svcCtx.LarkClient.ReplyMessage(ctx, messageID, "text", reply); err != nil {
			log.Printf("Failed to reply message: %v", err)
		}
		return
	}

	// 使用混合处理器处理查询
	reply, err := h.processor.ProcessQuery(ctx, chatID, query)
	if err != nil {
		log.Printf("Query processing error: %v", err)
		reply = "处理请求时出错，请稍后重试。"
	}

	// 添加模型来源标识
	modelName := h.svcCtx.Config.LLM.Model
	if modelName != "" {
		reply = reply + "\n\n---\n_🤖 Powered by " + modelName + "_"
	}

	if err := h.svcCtx.LarkClient.ReplyMessage(ctx, messageID, "text", reply); err != nil {
		log.Printf("Failed to reply message: %v", err)
	}
}

func (h *LarkWebhookHandler) getHelpMessage() string {
	return `🤖 团队助手使用指南

📊 **工作量查询**
• "小明这周干了多少活？"
• "今天谁提交了代码？"
• "上周团队的工作量统计"

🔍 **消息搜索**
• "张三说过什么关于登录的？"
• "搜索关于支付的讨论"

📋 **消息总结**
• "总结一下今天的讨论"
• "本周群消息摘要"

💡 **提示**
• 支持自然语言提问
• 可以指定时间范围（今天、本周、上周、本月等）
• @我即可开始对话`
}

// handlePrivateCommand 处理私聊命令
func (h *LarkWebhookHandler) handlePrivateCommand(event *lark.MessageReceiveEvent, content string) {
	ctx := context.Background()
	senderOpenID := event.Sender.SenderID.OpenID
	messageID := event.Message.MessageID

	log.Printf("Processing private command from %s: %s", senderOpenID, content)

	// 命令匹配
	switch {
	case content == "帮助" || content == "help" || content == "菜单":
		h.replyPrivateHelp(ctx, messageID)

	case content == "列出群聊" || content == "群列表" || content == "我的群":
		h.listChats(ctx, messageID)

	case strings.HasPrefix(content, "同步") || strings.HasPrefix(content, "下载"):
		h.handleSyncCommand(ctx, messageID, senderOpenID, content)

	case content == "同步状态" || content == "任务状态":
		h.showSyncStatus(ctx, messageID, senderOpenID)

	default:
		// 尝试作为群名匹配
		if h.trySyncByName(ctx, messageID, senderOpenID, content) {
			return
		}
		// 尝试作为 AI 查询处理
		h.handleAIQuery(ctx, messageID, senderOpenID, content)
	}
}

// handleAIQuery 处理 AI 查询
func (h *LarkWebhookHandler) handleAIQuery(ctx context.Context, messageID, userID, query string) {
	log.Printf("Processing AI query from %s: %s", userID, query)

	response, err := h.processor.ProcessQuery(ctx, userID, query)
	if err != nil {
		log.Printf("AI query error: %v", err)
		h.svcCtx.LarkClient.ReplyMessage(ctx, messageID, "text", "处理请求时出错，请稍后重试")
		return
	}

	log.Printf("AI response generated, length: %d chars", len(response))

	// 添加模型来源标识
	modelName := h.svcCtx.Config.LLM.Model
	if modelName != "" {
		response = response + "\n\n---\n_🤖 Powered by " + modelName + "_"
	}

	if err := h.svcCtx.LarkClient.ReplyMessage(ctx, messageID, "text", response); err != nil {
		log.Printf("Failed to reply AI response: %v", err)
	} else {
		log.Printf("Reply sent successfully to message: %s", messageID)
	}
}

// handlePrivateImageMessage 处理私聊中的图片消息（支持纯图片和富文本图片）
func (h *LarkWebhookHandler) handlePrivateImageMessage(event *lark.MessageReceiveEvent, content string) {
	ctx := context.Background()
	messageID := event.Message.MessageID
	senderOpenID := event.Sender.SenderID.OpenID

	// 提取 image_key 和用户问题
	var imageKey string
	var query string
	if lark.IsImageMessage(content) {
		// 纯图片消息
		imageKey = lark.ExtractImageKey(content)
		query = "请描述这张图片的内容"
	} else if lark.IsPostWithImage(content) {
		// 富文本图片消息
		imageKey = lark.ExtractPostImageKey(content)
		query = lark.ExtractPostText(content)
		if query == "" {
			query = "请描述这张图片的内容"
		}
	}

	if imageKey == "" {
		log.Printf("Failed to extract image_key from content: %s", content)
		h.svcCtx.LarkClient.ReplyMessage(ctx, messageID, "text", "无法识别图片，请重试")
		return
	}

	log.Printf("Processing private image from %s, image_key: %s, query: %s", senderOpenID, imageKey, query)

	// 下载图片
	imageData, err := h.svcCtx.LarkClient.DownloadImage(ctx, messageID, imageKey)
	if err != nil {
		log.Printf("Failed to download image: %v", err)
		h.svcCtx.LarkClient.ReplyMessage(ctx, messageID, "text", "无法下载图片，请重试")
		return
	}

	log.Printf("Image downloaded, size: %d bytes", len(imageData))

	// 使用视觉模型分析图片
	response, err := h.processor.ProcessImageQuery(ctx, senderOpenID, query, imageData)
	if err != nil {
		log.Printf("Vision model error: %v", err)
		h.svcCtx.LarkClient.ReplyMessage(ctx, messageID, "text", "图片分析失败："+err.Error())
		return
	}

	log.Printf("Vision response generated, length: %d chars", len(response))

	// 添加模型来源标识
	visionModel := h.svcCtx.Config.LLM.VisionModel
	if visionModel != "" {
		response = response + "\n\n---\n_🖼️ Powered by " + visionModel + "_"
	}

	if err := h.svcCtx.LarkClient.ReplyMessage(ctx, messageID, "text", response); err != nil {
		log.Printf("Failed to reply vision response: %v", err)
	} else {
		log.Printf("Reply sent successfully to message: %s", messageID)
	}
}

// replyPrivateHelp 回复私聊帮助信息
func (h *LarkWebhookHandler) replyPrivateHelp(ctx context.Context, messageID string) {
	help := `🤖 **团队助手**

**消息同步：**
• "列出群聊" - 查看机器人加入的所有群
• "同步 [群名/群ID]" - 同步指定群的历史消息
• "同步状态" - 查看当前同步任务进度

**AI 查询（自然语言）：**
• "搜索关于登录的讨论"
• "总结今天的消息"
• "本周群消息摘要"
• "谁提到过支付？"

**示例：**
• 同步 研发群
• 今天大家讨论了什么？

**说明：**
消息同步后可使用自然语言查询历史记录。`

	if err := h.svcCtx.LarkClient.ReplyMessage(ctx, messageID, "text", help); err != nil {
		log.Printf("Failed to reply help: %v", err)
	}
}

// listChats 列出机器人加入的群聊
func (h *LarkWebhookHandler) listChats(ctx context.Context, messageID string) {
	chats, err := h.svcCtx.LarkClient.GetChats(ctx)
	if err != nil {
		log.Printf("Failed to get chats: %v", err)
		h.svcCtx.LarkClient.ReplyMessage(ctx, messageID, "text", "获取群列表失败: "+err.Error())
		return
	}

	if len(chats) == 0 {
		h.svcCtx.LarkClient.ReplyMessage(ctx, messageID, "text", "机器人还没有加入任何群聊")
		return
	}

	var sb strings.Builder
	sb.WriteString("📋 **机器人加入的群聊**\n\n")
	for i, chat := range chats {
		sb.WriteString(fmt.Sprintf("%d. %s\n   ID: %s\n   成员数: %d\n\n",
			i+1, chat.Name, chat.ChatID, chat.MemberCount))
	}
	sb.WriteString("\n💡 发送 \"同步 群名\" 开始同步历史消息")

	if err := h.svcCtx.LarkClient.ReplyMessage(ctx, messageID, "text", sb.String()); err != nil {
		log.Printf("Failed to reply chat list: %v", err)
	}
}

// handleSyncCommand 处理同步命令
func (h *LarkWebhookHandler) handleSyncCommand(ctx context.Context, messageID, senderOpenID, content string) {
	if h.msgSyncer == nil {
		h.svcCtx.LarkClient.ReplyMessage(ctx, messageID, "text", "消息同步服务未启动")
		return
	}

	// 解析目标群
	target := ""
	if strings.HasPrefix(content, "同步") {
		target = strings.TrimSpace(strings.TrimPrefix(content, "同步"))
	} else if strings.HasPrefix(content, "下载") {
		target = strings.TrimSpace(strings.TrimPrefix(content, "下载"))
	}

	if target == "" {
		h.svcCtx.LarkClient.ReplyMessage(ctx, messageID, "text", "请指定要同步的群，例如：同步 研发群")
		return
	}

	// 查找群
	chatID, chatName, err := h.findChat(ctx, target)
	if err != nil {
		h.svcCtx.LarkClient.ReplyMessage(ctx, messageID, "text", "未找到群聊: "+target+"\n\n发送 \"列出群聊\" 查看可用的群")
		return
	}

	// 创建同步任务
	taskID, err := h.msgSyncer.CreateSyncTask(ctx, chatID, chatName, senderOpenID)
	if err != nil {
		log.Printf("Failed to create sync task: %v", err)
		h.svcCtx.LarkClient.ReplyMessage(ctx, messageID, "text", "创建同步任务失败: "+err.Error())
		return
	}

	reply := fmt.Sprintf("✅ **同步任务已创建**\n\n任务ID: %d\n群聊: %s\n状态: 等待处理\n\n消息同步将在后台进行，完成后会通知您。", taskID, chatName)
	if err := h.svcCtx.LarkClient.ReplyMessage(ctx, messageID, "text", reply); err != nil {
		log.Printf("Failed to reply sync task created: %v", err)
	}
}

// findChat 根据名称或ID查找群
func (h *LarkWebhookHandler) findChat(ctx context.Context, target string) (chatID, chatName string, err error) {
	// 如果是 chat_id 格式
	if strings.HasPrefix(target, "oc_") {
		return target, target, nil
	}

	// 按名称查找
	chats, err := h.svcCtx.LarkClient.GetChats(ctx)
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

// trySyncByName 尝试根据群名直接同步
func (h *LarkWebhookHandler) trySyncByName(ctx context.Context, messageID, senderOpenID, content string) bool {
	if h.msgSyncer == nil {
		return false
	}

	// 尝试查找群
	chatID, chatName, err := h.findChat(ctx, content)
	if err != nil {
		return false
	}

	// 找到了，创建同步任务
	taskID, err := h.msgSyncer.CreateSyncTask(ctx, chatID, chatName, senderOpenID)
	if err != nil {
		log.Printf("Failed to create sync task: %v", err)
		h.svcCtx.LarkClient.ReplyMessage(ctx, messageID, "text", "创建同步任务失败: "+err.Error())
		return true
	}

	reply := fmt.Sprintf("✅ **同步任务已创建**\n\n任务ID: %d\n群聊: %s\n状态: 等待处理\n\n消息同步将在后台进行，完成后会通知您。", taskID, chatName)
	h.svcCtx.LarkClient.ReplyMessage(ctx, messageID, "text", reply)
	return true
}

// checkPrivateChatPermission 检查用户是否有私聊权限
func (h *LarkWebhookHandler) checkPrivateChatPermission(event *lark.MessageReceiveEvent) bool {
	// 如果没有配置白名单，默认允许所有用户
	allowedUsers := h.svcCtx.Config.Permissions.PrivateChatAllowedUsers
	if len(allowedUsers) == 0 {
		return true
	}

	// 获取用户名
	ctx := context.Background()
	userInfo, err := h.svcCtx.LarkClient.GetUserInfo(ctx, event.Sender.SenderID.OpenID)
	if err != nil {
		log.Printf("Failed to get user info for permission check: %v", err)
		return false
	}

	// 检查用户名是否在白名单中（不区分大小写）
	userName := strings.ToLower(userInfo.Name)
	enName := strings.ToLower(userInfo.EnName)
	for _, allowed := range allowedUsers {
		allowedLower := strings.ToLower(allowed)
		if userName == allowedLower || enName == allowedLower {
			log.Printf("User %s (%s) has private chat permission", userInfo.Name, event.Sender.SenderID.OpenID)
			return true
		}
	}

	log.Printf("User %s (%s) does not have private chat permission", userInfo.Name, event.Sender.SenderID.OpenID)
	return false
}

// replyNoPrivateChatPermission 回复无私聊权限
func (h *LarkWebhookHandler) replyNoPrivateChatPermission(event *lark.MessageReceiveEvent) {
	ctx := context.Background()
	reply := "抱歉，您没有私聊机器人的权限。"
	if err := h.svcCtx.LarkClient.ReplyMessage(ctx, event.Message.MessageID, "text", reply); err != nil {
		log.Printf("Failed to reply no permission: %v", err)
	}
}

// isAllowedUser 检查用户是否在白名单中
func (h *LarkWebhookHandler) isAllowedUser(openID string) bool {
	allowedUsers := h.svcCtx.Config.Permissions.PrivateChatAllowedUsers
	if len(allowedUsers) == 0 {
		return false
	}

	ctx := context.Background()
	userInfo, err := h.svcCtx.LarkClient.GetUserInfo(ctx, openID)
	if err != nil {
		log.Printf("Failed to get user info for whitelist check: %v", err)
		return false
	}

	userName := strings.ToLower(userInfo.Name)
	enName := strings.ToLower(userInfo.EnName)
	for _, allowed := range allowedUsers {
		allowedLower := strings.ToLower(allowed)
		if userName == allowedLower || enName == allowedLower {
			return true
		}
	}
	return false
}

// checkGroupPermission 检查群聊是否满足成员数要求
func (h *LarkWebhookHandler) checkGroupPermission(event *lark.MessageReceiveEvent) bool {
	// 如果没有配置最小成员数，默认允许
	minMembers := h.svcCtx.Config.Permissions.GroupMinMembers
	if minMembers <= 0 {
		return true
	}

	// 白名单用户不受群成员数限制
	if h.isAllowedUser(event.Sender.SenderID.OpenID) {
		log.Printf("User %s is in whitelist, bypassing group member check", event.Sender.SenderID.OpenID)
		return true
	}

	// 获取群信息
	ctx := context.Background()
	chatInfo, err := h.svcCtx.LarkClient.GetChatInfo(ctx, event.Message.ChatID)
	if err != nil {
		log.Printf("Failed to get chat info for permission check: %v", err)
		// 如果获取失败，默认允许（避免因 API 问题阻断服务）
		return true
	}

	if chatInfo.MemberCount >= minMembers {
		log.Printf("Chat %s has %d members, permission granted", event.Message.ChatID, chatInfo.MemberCount)
		return true
	}

	log.Printf("Chat %s has only %d members (min: %d), permission denied", event.Message.ChatID, chatInfo.MemberCount, minMembers)
	return false
}

// replyNoGroupPermission 回复群成员数不足
func (h *LarkWebhookHandler) replyNoGroupPermission(event *lark.MessageReceiveEvent) {
	ctx := context.Background()
	minMembers := h.svcCtx.Config.Permissions.GroupMinMembers
	reply := fmt.Sprintf("抱歉，机器人仅在成员数 >= %d 人的群聊中提供服务。", minMembers)
	if err := h.svcCtx.LarkClient.ReplyMessage(ctx, event.Message.MessageID, "text", reply); err != nil {
		log.Printf("Failed to reply no permission: %v", err)
	}
}

// showSyncStatus 显示同步状态
func (h *LarkWebhookHandler) showSyncStatus(ctx context.Context, messageID, senderOpenID string) {
	tasks, err := h.svcCtx.SyncTaskModel.GetRecentTasks(ctx, 5)
	if err != nil {
		log.Printf("Failed to get sync tasks: %v", err)
		h.svcCtx.LarkClient.ReplyMessage(ctx, messageID, "text", "获取任务状态失败")
		return
	}

	if len(tasks) == 0 {
		h.svcCtx.LarkClient.ReplyMessage(ctx, messageID, "text", "暂无同步任务记录")
		return
	}

	var sb strings.Builder
	sb.WriteString("📊 **最近同步任务**\n\n")
	for _, task := range tasks {
		chatName := task.ChatID
		if task.ChatName.Valid {
			chatName = task.ChatName.String
		}
		status := task.Status
		switch status {
		case "pending":
			status = "⏳ 等待中"
		case "running":
			status = "🔄 同步中"
		case "completed":
			status = "✅ 已完成"
		case "failed":
			status = "❌ 失败"
		}
		sb.WriteString(fmt.Sprintf("• %s\n  状态: %s | 已同步: %d 条\n\n",
			chatName, status, task.SyncedMessages))
	}

	if err := h.svcCtx.LarkClient.ReplyMessage(ctx, messageID, "text", sb.String()); err != nil {
		log.Printf("Failed to reply sync status: %v", err)
	}
}
