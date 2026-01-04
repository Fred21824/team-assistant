package service

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"

	"team-assistant/internal/interfaces"
	"team-assistant/internal/model"
)

// SyncService 同步服务
type SyncService struct {
	syncTaskRepo interfaces.SyncTaskRepository
}

// NewSyncService 创建同步服务
func NewSyncService(syncTaskRepo interfaces.SyncTaskRepository) *SyncService {
	return &SyncService{
		syncTaskRepo: syncTaskRepo,
	}
}

// CreateSyncTask 创建同步任务
func (s *SyncService) CreateSyncTask(ctx context.Context, chatID, chatName, requestedBy string) (int64, error) {
	task := &model.MessageSyncTask{
		ChatID:      chatID,
		ChatName:    sql.NullString{String: chatName, Valid: chatName != ""},
		Status:      "pending",
		RequestedBy: sql.NullString{String: requestedBy, Valid: requestedBy != ""},
	}

	taskID, err := s.syncTaskRepo.Create(ctx, task)
	if err != nil {
		log.Printf("Failed to create sync task: %v", err)
		return 0, err
	}

	log.Printf("Created sync task %d for chat %s", taskID, chatName)
	return taskID, nil
}

// GetPendingTask 获取待处理的任务
func (s *SyncService) GetPendingTask(ctx context.Context) (*model.MessageSyncTask, error) {
	return s.syncTaskRepo.GetPendingTask(ctx)
}

// GetRecentTasks 获取最近的任务列表
func (s *SyncService) GetRecentTasks(ctx context.Context, limit int) ([]*model.MessageSyncTask, error) {
	return s.syncTaskRepo.GetRecentTasks(ctx, limit)
}

// MarkTaskStarted 标记任务开始
func (s *SyncService) MarkTaskStarted(ctx context.Context, taskID int64) error {
	return s.syncTaskRepo.MarkStarted(ctx, taskID)
}

// MarkTaskCompleted 标记任务完成
func (s *SyncService) MarkTaskCompleted(ctx context.Context, taskID int64, totalMessages int) error {
	return s.syncTaskRepo.MarkCompleted(ctx, taskID, totalMessages)
}

// MarkTaskFailed 标记任务失败
func (s *SyncService) MarkTaskFailed(ctx context.Context, taskID int64, errMsg string) error {
	return s.syncTaskRepo.MarkFailed(ctx, taskID, errMsg)
}

// UpdateProgress 更新同步进度
func (s *SyncService) UpdateProgress(ctx context.Context, taskID int64, syncedMessages int, pageToken string) error {
	return s.syncTaskRepo.UpdateProgress(ctx, taskID, syncedMessages, pageToken)
}

// FormatTaskStatus 格式化任务状态
func (s *SyncService) FormatTaskStatus(tasks []*model.MessageSyncTask) string {
	if len(tasks) == 0 {
		return "暂无同步任务记录"
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

	return sb.String()
}

// FormatTaskCreated 格式化任务创建成功消息
func (s *SyncService) FormatTaskCreated(taskID int64, chatName string) string {
	return fmt.Sprintf("✅ **同步任务已创建**\n\n任务ID: %d\n群聊: %s\n状态: 等待处理\n\n消息同步将在后台进行，完成后会通知您。", taskID, chatName)
}
