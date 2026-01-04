package collector

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"strconv"
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
		batchSize: 50, // 每次拉取50条
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

	log.Printf("Task %d: synced %d messages (total: %d), has_more: %v",
		task.ID, len(resp.Data.Items), totalSynced, resp.Data.HasMore)

	// 更新进度
	if resp.Data.HasMore {
		s.svcCtx.SyncTaskModel.UpdateProgress(ctx, task.ID, totalSynced, resp.Data.PageToken)
	} else {
		// 完成
		s.svcCtx.SyncTaskModel.MarkCompleted(ctx, task.ID, totalSynced)
		log.Printf("Task %d completed, total messages: %d", task.ID, totalSynced)

		// 发送完成通知
		if task.RequestedBy.Valid {
			s.notifyCompletion(ctx, task.RequestedBy.String, task, totalSynced)
		}
	}

	return nil
}

// getUserName 获取用户名（带缓存）
func (s *MessageSyncer) getUserName(ctx context.Context, openID string) string {
	if openID == "" {
		return ""
	}

	// 先检查缓存
	s.userCacheMu.RLock()
	if name, ok := s.userCache[openID]; ok {
		s.userCacheMu.RUnlock()
		return name
	}
	s.userCacheMu.RUnlock()

	// 调用API获取用户信息
	userInfo, err := s.svcCtx.LarkClient.GetUserInfo(ctx, openID)
	if err != nil {
		log.Printf("Failed to get user info for %s: %v", openID, err)
		return ""
	}

	name := userInfo.Name
	if name == "" {
		name = userInfo.EnName
	}

	// 存入缓存
	s.userCacheMu.Lock()
	s.userCache[openID] = name
	s.userCacheMu.Unlock()

	return name
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
	content := lark.ParseMessageContent(item.MsgType, item.Body.Content)

	// 序列化 mentions
	mentionsJSON, _ := json.Marshal(item.Mentions)

	// 获取发送者名称
	senderName := s.getUserName(ctx, item.Sender.ID)

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
