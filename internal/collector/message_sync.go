package collector

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"team-assistant/internal/model"
	"team-assistant/internal/service"
	"team-assistant/internal/svc"
	"team-assistant/pkg/lark"
)

// MessageSyncer 消息同步器
type MessageSyncer struct {
	svcCtx    *svc.ServiceContext
	batchSize int
	stopChan  chan struct{}
	wg        sync.WaitGroup
	running   bool
	mu        sync.Mutex

	// 用户名缓存 (open_id -> name)
	userCache   map[string]string
	userCacheMu sync.RWMutex
}

// NewMessageSyncer 创建消息同步器
func NewMessageSyncer(svcCtx *svc.ServiceContext) *MessageSyncer {
	return &MessageSyncer{
		svcCtx:    svcCtx,
		batchSize: 50, // 飞书API限制最大50条/次
		stopChan:  make(chan struct{}),
		userCache: make(map[string]string),
	}
}

// Start 启动同步器（后台运行）
func (s *MessageSyncer) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	s.wg.Add(1)
	go s.run()
	log.Println("Message syncer started")
}

// Stop 停止同步器
func (s *MessageSyncer) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.mu.Unlock()

	close(s.stopChan)
	s.wg.Wait()
	log.Println("Message syncer stopped")
}

// run 后台运行，检查待处理任务
func (s *MessageSyncer) run() {
	defer s.wg.Done()

	ticker := time.NewTicker(5 * time.Second) // 每5秒检查一次
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.processNextTask()
		}
	}
}

// processNextTask 处理下一个任务
func (s *MessageSyncer) processNextTask() {
	ctx := context.Background()

	// 获取待处理任务
	task, err := s.svcCtx.SyncTaskModel.GetPendingTask(ctx)
	if err != nil {
		if err != sql.ErrNoRows {
			log.Printf("Failed to get pending task: %v", err)
		}
		return
	}

	log.Printf("Processing sync task %d for chat %s", task.ID, task.ChatID)

	// 标记任务开始
	if task.Status == "pending" {
		if err := s.svcCtx.SyncTaskModel.MarkStarted(ctx, task.ID); err != nil {
			log.Printf("Failed to mark task started: %v", err)
			return
		}
	}

	// 执行同步
	if err := s.syncMessages(ctx, task); err != nil {
		log.Printf("Failed to sync messages: %v", err)
		s.svcCtx.SyncTaskModel.MarkFailed(ctx, task.ID, err.Error())
		return
	}
}

// SyncTask 同步单个任务（公开方法，供外部调用）
func (s *MessageSyncer) SyncTask(ctx context.Context, task *model.MessageSyncTask) error {
	return s.syncMessages(ctx, task)
}

// syncMessages 同步消息
func (s *MessageSyncer) syncMessages(ctx context.Context, task *model.MessageSyncTask) error {
	pageToken := ""
	if task.PageToken.Valid {
		pageToken = task.PageToken.String
	}

	startTime := ""
	if task.StartTime.Valid {
		startTime = task.StartTime.String
	}

	endTime := ""
	if task.EndTime.Valid {
		endTime = task.EndTime.String
	}

	totalSynced := task.SyncedMessages

	// 预加载群成员名称（使用群成员 API 而不是通讯录 API）
	s.preloadChatMembers(ctx, task.ChatID)

	// 拉取一批消息
	resp, err := s.svcCtx.LarkClient.GetChatHistory(ctx, task.ChatID, startTime, endTime, s.batchSize, pageToken)
	if err != nil {
		return err
	}

	// 存储消息并索引到向量数据库
	var vectorMsgs []service.MessageVector
	chatName := ""
	if task.ChatName.Valid {
		chatName = task.ChatName.String
	}

	for _, item := range resp.Data.Items {
		if item.Deleted {
			continue
		}

		msg := s.convertToMessage(ctx, item)
		if err := s.svcCtx.MessageModel.Insert(ctx, msg); err != nil {
			log.Printf("Failed to insert message %s: %v", item.MessageID, err)
		} else {
			totalSynced++

			// 收集向量数据
			if msg.Content.Valid && msg.Content.String != "" {
				vectorMsgs = append(vectorMsgs, service.MessageVector{
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
	}

	// 批量索引到向量数据库
	if len(vectorMsgs) > 0 && s.svcCtx.Services.RAG != nil && s.svcCtx.Services.RAG.IsEnabled() {
		if err := s.svcCtx.Services.RAG.IndexMessages(ctx, vectorMsgs); err != nil {
			log.Printf("Failed to index messages to vector DB: %v", err)
		}
	}

	log.Printf("Task %d: synced %d messages (total: %d), has_more: %v, page_token: %s",
		task.ID, len(resp.Data.Items), totalSynced, resp.Data.HasMore, truncateForLog(resp.Data.PageToken, 30))

	// 更新进度
	// 修复：如果返回空数据或没有更多数据，标记为完成
	if !resp.Data.HasMore || len(resp.Data.Items) == 0 {
		// 完成
		s.svcCtx.SyncTaskModel.MarkCompleted(ctx, task.ID, totalSynced)
		log.Printf("Task %d completed, total messages: %d", task.ID, totalSynced)

		// 发送完成通知
		if task.RequestedBy.Valid {
			s.notifyCompletion(ctx, task.RequestedBy.String, task, totalSynced)
		}
	} else {
		s.svcCtx.SyncTaskModel.UpdateProgress(ctx, task.ID, totalSynced, resp.Data.PageToken)
	}

	return nil
}

// preloadChatMembers 预加载群成员名称到缓存
func (s *MessageSyncer) preloadChatMembers(ctx context.Context, chatID string) {
	members, err := s.svcCtx.LarkClient.GetChatMembers(ctx, chatID)
	if err != nil {
		log.Printf("Failed to preload chat members for %s: %v", chatID, err)
		return
	}

	s.userCacheMu.Lock()
	for openID, name := range members {
		if name != "" {
			s.userCache[openID] = name
		}
	}
	s.userCacheMu.Unlock()

	log.Printf("Preloaded %d member names for chat %s", len(members), chatID)
}

// getUserName 获取用户名（带缓存）
func (s *MessageSyncer) getUserName(ctx context.Context, openID string) string {
	if openID == "" {
		return ""
	}

	// 跳过机器人ID（以 cli_ 开头的是机器人）
	if strings.HasPrefix(openID, "cli_") {
		return "机器人"
	}

	// 从缓存获取（已通过 preloadChatMembers 预加载）
	s.userCacheMu.RLock()
	name, ok := s.userCache[openID]
	s.userCacheMu.RUnlock()

	if ok {
		return name
	}

	// 缓存未命中，返回空（不再调用通讯录 API）
	log.Printf("User name not found in cache for %s", openID)
	return ""
}

// convertToMessage 转换消息格式
func (s *MessageSyncer) convertToMessage(ctx context.Context, item *lark.MessageItem) *model.ChatMessage {
	// 解析时间戳
	var createTime time.Time
	if ts, err := strconv.ParseInt(item.CreateTime, 10, 64); err == nil {
		createTime = time.UnixMilli(ts)
	} else {
		createTime = time.Now()
	}

	// 解析消息内容
	var content string
	if item.MsgType == "image" {
		// 处理图片消息：下载并分析（需要 messageID 来下载资源）
		content = s.analyzeImageMessage(ctx, item.MessageID, item.Body.Content)
	} else {
		content = lark.ParseMessageContent(item.MsgType, item.Body.Content)
	}

	// 替换 @_user_N 为真实用户名
	for _, mention := range item.Mentions {
		if mention.Key != "" && mention.Name != "" {
			content = strings.ReplaceAll(content, mention.Key, "@"+mention.Name)
		}
	}

	// 序列化 mentions
	mentionsJSON, _ := json.Marshal(item.Mentions)

	// 获取发送者名称
	senderName := s.getUserName(ctx, item.Sender.ID)
	if senderName != "" {
		log.Printf("Got sender name: %s -> %s", item.Sender.ID, senderName)
	}

	return &model.ChatMessage{
		MessageID:  item.MessageID,
		ChatID:     item.ChatID,
		SenderID:   sql.NullString{String: item.Sender.ID, Valid: item.Sender.ID != ""},
		SenderName: sql.NullString{String: senderName, Valid: senderName != ""},
		MsgType:    sql.NullString{String: item.MsgType, Valid: true},
		Content:    sql.NullString{String: content, Valid: content != ""},
		RawContent: sql.NullString{String: item.Body.Content, Valid: item.Body.Content != ""},
		Mentions:   mentionsJSON,
		ReplyToID:  sql.NullString{String: item.ParentID, Valid: item.ParentID != ""},
		IsAtBot:    0,
		CreatedAt:  createTime,
	}
}

// analyzeImageMessage 分析图片消息
func (s *MessageSyncer) analyzeImageMessage(ctx context.Context, messageID, rawContent string) string {
	// 解析 image_key
	var imageContent struct {
		ImageKey string `json:"image_key"`
	}
	if err := json.Unmarshal([]byte(rawContent), &imageContent); err != nil {
		log.Printf("Failed to parse image content: %v", err)
		return "[图片]"
	}

	if imageContent.ImageKey == "" {
		return "[图片]"
	}

	// 下载图片（使用消息资源 API，需要 messageID 和 imageKey）
	imageData, err := s.svcCtx.LarkClient.DownloadImage(ctx, messageID, imageContent.ImageKey)
	if err != nil {
		log.Printf("Failed to download image %s: %v", imageContent.ImageKey, err)
		return "[图片]"
	}

	// 检测图片类型
	mimeType := http.DetectContentType(imageData)
	if !strings.HasPrefix(mimeType, "image/") {
		mimeType = "image/png" // 默认
	}

	// 转换为 base64
	imageBase64 := base64.StdEncoding.EncodeToString(imageData)

	// 检查 LLM 客户端是否可用
	if s.svcCtx.LLMClient == nil {
		log.Printf("LLM client not available for image analysis")
		return "[图片]"
	}

	// 调用 Vision API 分析图片
	analysis, err := s.svcCtx.LLMClient.AnalyzeImage(ctx, imageBase64, mimeType)
	if err != nil {
		log.Printf("Failed to analyze image %s: %v", imageContent.ImageKey, err)
		return "[图片]"
	}

	log.Printf("Image %s analyzed: %s", imageContent.ImageKey, truncateForLog(analysis, 100))
	return "[图片] " + analysis
}

// truncateForLog 截断日志内容
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// notifyCompletion 通知用户同步完成
func (s *MessageSyncer) notifyCompletion(ctx context.Context, userID string, task *model.MessageSyncTask, total int) {
	chatName := task.ChatID
	if task.ChatName.Valid {
		chatName = task.ChatName.String
	}

	msg := "消息同步完成\n\n"
	msg += "群聊: " + chatName + "\n"
	msg += "同步消息数: " + strconv.Itoa(total) + "\n"
	msg += "状态: 已完成 ✅"

	// 发送私聊消息给请求者
	// 注意：这里需要使用 open_id 发送消息
	if err := s.svcCtx.LarkClient.SendMessageToUser(ctx, userID, "text", msg); err != nil {
		log.Printf("Failed to notify user %s: %v", userID, err)
	}
}

// CreateSyncTask 创建同步任务
func (s *MessageSyncer) CreateSyncTask(ctx context.Context, chatID, chatName, requestedBy string) (int64, error) {
	task := &model.MessageSyncTask{
		ChatID:      chatID,
		ChatName:    sql.NullString{String: chatName, Valid: chatName != ""},
		Status:      "pending",
		RequestedBy: sql.NullString{String: requestedBy, Valid: requestedBy != ""},
	}
	return s.svcCtx.SyncTaskModel.Create(ctx, task)
}
