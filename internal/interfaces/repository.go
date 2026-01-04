package interfaces

import (
	"context"
	"time"

	"team-assistant/internal/model"
)

// MemberRepository 成员数据访问接口
type MemberRepository interface {
	FindByName(ctx context.Context, name string) ([]*model.TeamMember, error)
	FindByGitHubUsername(ctx context.Context, username string) (*model.TeamMember, error)
	FindByLarkUserID(ctx context.Context, userID string) (*model.TeamMember, error)
	ListAll(ctx context.Context) ([]*model.TeamMember, error)
	Upsert(ctx context.Context, member *model.TeamMember) error
}

// CommitRepository 提交记录数据访问接口
type CommitRepository interface {
	Insert(ctx context.Context, commit *model.GitCommit) error
	BatchInsert(ctx context.Context, commits []*model.GitCommit) error
	GetStatsByMember(ctx context.Context, memberID int64, start, end time.Time) (*model.CommitStats, error)
	GetStatsByAuthorName(ctx context.Context, authorName string, start, end time.Time) (*model.CommitStats, error)
	GetAllStats(ctx context.Context, start, end time.Time) ([]*model.CommitStats, error)
	GetRecentCommits(ctx context.Context, memberID int64, limit int) ([]*model.GitCommit, error)
	GetCommitsByDateRange(ctx context.Context, authorName string, start, end time.Time, limit int) ([]*model.GitCommit, error)
}

// MessageRepository 消息数据访问接口
type MessageRepository interface {
	Insert(ctx context.Context, msg *model.ChatMessage) error
	GetRecentMessages(ctx context.Context, chatID string, limit int) ([]*model.ChatMessage, error)
	GetMessagesByDateRange(ctx context.Context, chatID string, start, end time.Time, limit int) ([]*model.ChatMessage, error)
	SearchByContent(ctx context.Context, chatID, keyword string, limit int) ([]*model.ChatMessage, error)
	SearchBySender(ctx context.Context, chatID, senderName, keyword string, limit int) ([]*model.ChatMessage, error)
	GetAtBotMessages(ctx context.Context, limit int) ([]*model.ChatMessage, error)
}

// GroupRepository 群聊数据访问接口
type GroupRepository interface {
	Upsert(ctx context.Context, group *model.ChatGroup) error
	FindByChatID(ctx context.Context, chatID string) (*model.ChatGroup, error)
	ListAll(ctx context.Context) ([]*model.ChatGroup, error)
}

// SyncTaskRepository 同步任务数据访问接口
type SyncTaskRepository interface {
	Create(ctx context.Context, task *model.MessageSyncTask) (int64, error)
	GetByID(ctx context.Context, id int64) (*model.MessageSyncTask, error)
	GetPendingTask(ctx context.Context) (*model.MessageSyncTask, error)
	GetRecentTasks(ctx context.Context, limit int) ([]*model.MessageSyncTask, error)
	UpdateStatus(ctx context.Context, id int64, status string) error
	UpdateProgress(ctx context.Context, id int64, syncedMessages int, pageToken string) error
	MarkStarted(ctx context.Context, id int64) error
	MarkCompleted(ctx context.Context, id int64, totalMessages int) error
	MarkFailed(ctx context.Context, id int64, errMsg string) error
}

// ConversationRepository 对话存储接口（用于多轮对话）
type ConversationRepository interface {
	GetConversationID(ctx context.Context, userID string) (string, error)
	SaveConversationID(ctx context.Context, userID, conversationID string, ttl time.Duration) error
	DeleteConversation(ctx context.Context, userID string) error
}
