package collector

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"team-assistant/internal/svc"
	"team-assistant/pkg/dify"
)

// DifySyncer 将消息同步到 Dify 知识库
type DifySyncer struct {
	svcCtx     *svc.ServiceContext
	client     *dify.Client
	datasetID  string
	batchSize  int
	interval   time.Duration
	stopChan   chan struct{}
	wg         sync.WaitGroup
	running    bool
	mu         sync.Mutex
	lastSyncID int64 // 上次同步的最大消息 ID
}

// NewDifySyncer 创建 Dify 同步器
func NewDifySyncer(svcCtx *svc.ServiceContext) *DifySyncer {
	if !svcCtx.Config.Dify.Enabled || svcCtx.Config.Dify.APIKey == "" {
		return nil
	}

	return &DifySyncer{
		svcCtx:    svcCtx,
		client:    dify.NewClient(svcCtx.Config.Dify.BaseURL, svcCtx.Config.Dify.APIKey),
		datasetID: svcCtx.Config.Dify.DatasetID,
		batchSize: 100, // 每批处理 100 条消息
		interval:  5 * time.Minute, // 每 5 分钟同步一次
		stopChan:  make(chan struct{}),
	}
}

// Start 启动同步器
func (s *DifySyncer) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	s.wg.Add(1)
	go s.run()
	log.Println("Dify syncer started")
}

// Stop 停止同步器
func (s *DifySyncer) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.mu.Unlock()

	close(s.stopChan)
	s.wg.Wait()
	log.Println("Dify syncer stopped")
}

// run 后台运行
func (s *DifySyncer) run() {
	defer s.wg.Done()

	// 启动时先同步一次
	s.syncMessages()

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.syncMessages()
		}
	}
}

// syncMessages 同步消息到 Dify
func (s *DifySyncer) syncMessages() {
	if s.datasetID == "" {
		log.Println("Dify syncer: no dataset ID configured, skipping")
		return
	}

	ctx := context.Background()

	// 获取需要同步的消息（按日期分组）
	endTime := time.Now()
	startTime := endTime.AddDate(0, 0, -7) // 同步最近 7 天

	messages, err := s.svcCtx.MessageModel.GetMessagesByDateRange(ctx, "", startTime, endTime, 1000)
	if err != nil {
		log.Printf("Dify syncer: failed to get messages: %v", err)
		return
	}

	if len(messages) == 0 {
		log.Println("Dify syncer: no messages to sync")
		return
	}

	// 按日期分组消息
	messagesByDate := make(map[string][]string)
	for _, msg := range messages {
		dateKey := msg.CreatedAt.Format("2006-01-02")

		senderName := ""
		if msg.SenderName.Valid {
			senderName = msg.SenderName.String
		}
		content := ""
		if msg.Content.Valid {
			content = msg.Content.String
		}

		if content == "" {
			continue
		}

		msgText := fmt.Sprintf("[%s] %s: %s",
			msg.CreatedAt.Format("15:04:05"),
			senderName,
			content)
		messagesByDate[dateKey] = append(messagesByDate[dateKey], msgText)
	}

	// 为每天创建/更新文档
	for date, msgs := range messagesByDate {
		docName := fmt.Sprintf("群聊消息-%s", date)
		content := strings.Join(msgs, "\n")

		// 尝试创建文档（如果已存在会更新）
		_, err := s.client.CreateDocumentByText(ctx, s.datasetID, docName, content)
		if err != nil {
			log.Printf("Dify syncer: failed to create document %s: %v", docName, err)
		} else {
			log.Printf("Dify syncer: synced %d messages for %s", len(msgs), date)
		}
	}

	log.Printf("Dify syncer: completed syncing %d days of messages", len(messagesByDate))
}

// SyncNow 立即同步
func (s *DifySyncer) SyncNow() {
	go s.syncMessages()
}

// SyncChatHistory 同步指定群的历史消息
func (s *DifySyncer) SyncChatHistory(ctx context.Context, chatID string, days int) error {
	if s.datasetID == "" {
		return fmt.Errorf("no dataset ID configured")
	}

	endTime := time.Now()
	startTime := endTime.AddDate(0, 0, -days)

	messages, err := s.svcCtx.MessageModel.GetMessagesByDateRange(ctx, chatID, startTime, endTime, 5000)
	if err != nil {
		return fmt.Errorf("get messages: %w", err)
	}

	if len(messages) == 0 {
		return nil
	}

	// 按日期分组
	messagesByDate := make(map[string][]string)
	for _, msg := range messages {
		dateKey := msg.CreatedAt.Format("2006-01-02")

		senderName := ""
		if msg.SenderName.Valid {
			senderName = msg.SenderName.String
		}
		content := ""
		if msg.Content.Valid {
			content = msg.Content.String
		}

		if content == "" {
			continue
		}

		msgText := fmt.Sprintf("[%s] %s: %s",
			msg.CreatedAt.Format("15:04:05"),
			senderName,
			content)
		messagesByDate[dateKey] = append(messagesByDate[dateKey], msgText)
	}

	// 上传到 Dify
	for date, msgs := range messagesByDate {
		docName := fmt.Sprintf("群聊消息-%s-%s", chatID[:8], date)
		content := strings.Join(msgs, "\n")

		_, err := s.client.CreateDocumentByText(ctx, s.datasetID, docName, content)
		if err != nil {
			log.Printf("Dify syncer: failed to create document %s: %v", docName, err)
		}
	}

	return nil
}
