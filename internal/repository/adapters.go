package repository

import (
	"context"
	"time"

	"team-assistant/internal/model"
)

// MemberRepositoryAdapter 成员仓库适配器
type MemberRepositoryAdapter struct {
	model *model.TeamMemberModel
}

func NewMemberRepositoryAdapter(m *model.TeamMemberModel) *MemberRepositoryAdapter {
	return &MemberRepositoryAdapter{model: m}
}

func (a *MemberRepositoryAdapter) FindByName(ctx context.Context, name string) ([]*model.TeamMember, error) {
	return a.model.FindByName(ctx, name)
}

func (a *MemberRepositoryAdapter) FindByGitHubUsername(ctx context.Context, username string) (*model.TeamMember, error) {
	return a.model.FindByGitHubUsername(ctx, username)
}

func (a *MemberRepositoryAdapter) FindByLarkUserID(ctx context.Context, userID string) (*model.TeamMember, error) {
	return a.model.FindByLarkUserID(ctx, userID)
}

func (a *MemberRepositoryAdapter) ListAll(ctx context.Context) ([]*model.TeamMember, error) {
	return a.model.ListAll(ctx)
}

func (a *MemberRepositoryAdapter) Upsert(ctx context.Context, member *model.TeamMember) error {
	return a.model.Upsert(ctx, member)
}

// CommitRepositoryAdapter 提交仓库适配器
type CommitRepositoryAdapter struct {
	model *model.GitCommitModel
}

func NewCommitRepositoryAdapter(m *model.GitCommitModel) *CommitRepositoryAdapter {
	return &CommitRepositoryAdapter{model: m}
}

func (a *CommitRepositoryAdapter) Insert(ctx context.Context, commit *model.GitCommit) error {
	return a.model.Insert(ctx, commit)
}

func (a *CommitRepositoryAdapter) BatchInsert(ctx context.Context, commits []*model.GitCommit) error {
	return a.model.BatchInsert(ctx, commits)
}

func (a *CommitRepositoryAdapter) GetStatsByMember(ctx context.Context, memberID int64, start, end time.Time) (*model.CommitStats, error) {
	return a.model.GetStatsByMember(ctx, memberID, start, end)
}

func (a *CommitRepositoryAdapter) GetStatsByAuthorName(ctx context.Context, authorName string, start, end time.Time) (*model.CommitStats, error) {
	return a.model.GetStatsByAuthorName(ctx, authorName, start, end)
}

func (a *CommitRepositoryAdapter) GetAllStats(ctx context.Context, start, end time.Time) ([]*model.CommitStats, error) {
	return a.model.GetAllStats(ctx, start, end)
}

func (a *CommitRepositoryAdapter) GetRecentCommits(ctx context.Context, memberID int64, limit int) ([]*model.GitCommit, error) {
	return a.model.GetRecentCommits(ctx, memberID, limit)
}

func (a *CommitRepositoryAdapter) GetCommitsByDateRange(ctx context.Context, authorName string, start, end time.Time, limit int) ([]*model.GitCommit, error) {
	return a.model.GetCommitsByDateRange(ctx, authorName, start, end, limit)
}

// MessageRepositoryAdapter 消息仓库适配器
type MessageRepositoryAdapter struct {
	model *model.ChatMessageModel
}

func NewMessageRepositoryAdapter(m *model.ChatMessageModel) *MessageRepositoryAdapter {
	return &MessageRepositoryAdapter{model: m}
}

func (a *MessageRepositoryAdapter) Insert(ctx context.Context, msg *model.ChatMessage) error {
	return a.model.Insert(ctx, msg)
}

func (a *MessageRepositoryAdapter) GetRecentMessages(ctx context.Context, chatID string, limit int) ([]*model.ChatMessage, error) {
	return a.model.GetRecentMessages(ctx, chatID, limit)
}

func (a *MessageRepositoryAdapter) GetMessagesByDateRange(ctx context.Context, chatID string, start, end time.Time, limit int) ([]*model.ChatMessage, error) {
	return a.model.GetMessagesByDateRange(ctx, chatID, start, end, limit)
}

func (a *MessageRepositoryAdapter) SearchByContent(ctx context.Context, chatID, keyword string, limit int) ([]*model.ChatMessage, error) {
	return a.model.SearchByContent(ctx, chatID, keyword, limit)
}

func (a *MessageRepositoryAdapter) SearchBySender(ctx context.Context, chatID, senderName, keyword string, limit int) ([]*model.ChatMessage, error) {
	return a.model.SearchBySender(ctx, chatID, senderName, keyword, limit)
}

func (a *MessageRepositoryAdapter) GetAtBotMessages(ctx context.Context, limit int) ([]*model.ChatMessage, error) {
	return a.model.GetAtBotMessages(ctx, limit)
}

// GroupRepositoryAdapter 群聊仓库适配器
type GroupRepositoryAdapter struct {
	model *model.ChatGroupModel
}

func NewGroupRepositoryAdapter(m *model.ChatGroupModel) *GroupRepositoryAdapter {
	return &GroupRepositoryAdapter{model: m}
}

func (a *GroupRepositoryAdapter) Upsert(ctx context.Context, group *model.ChatGroup) error {
	return a.model.Upsert(ctx, group)
}

func (a *GroupRepositoryAdapter) FindByChatID(ctx context.Context, chatID string) (*model.ChatGroup, error) {
	return a.model.FindByChatID(ctx, chatID)
}

func (a *GroupRepositoryAdapter) ListAll(ctx context.Context) ([]*model.ChatGroup, error) {
	return a.model.ListAll(ctx)
}

// SyncTaskRepositoryAdapter 同步任务仓库适配器
type SyncTaskRepositoryAdapter struct {
	model *model.MessageSyncTaskModel
}

func NewSyncTaskRepositoryAdapter(m *model.MessageSyncTaskModel) *SyncTaskRepositoryAdapter {
	return &SyncTaskRepositoryAdapter{model: m}
}

func (a *SyncTaskRepositoryAdapter) Create(ctx context.Context, task *model.MessageSyncTask) (int64, error) {
	return a.model.Create(ctx, task)
}

func (a *SyncTaskRepositoryAdapter) GetByID(ctx context.Context, id int64) (*model.MessageSyncTask, error) {
	return a.model.GetByID(ctx, id)
}

func (a *SyncTaskRepositoryAdapter) GetPendingTask(ctx context.Context) (*model.MessageSyncTask, error) {
	return a.model.GetPendingTask(ctx)
}

func (a *SyncTaskRepositoryAdapter) GetRecentTasks(ctx context.Context, limit int) ([]*model.MessageSyncTask, error) {
	return a.model.GetRecentTasks(ctx, limit)
}

func (a *SyncTaskRepositoryAdapter) UpdateStatus(ctx context.Context, id int64, status string) error {
	return a.model.UpdateStatus(ctx, id, status)
}

func (a *SyncTaskRepositoryAdapter) UpdateProgress(ctx context.Context, id int64, syncedMessages int, pageToken string) error {
	return a.model.UpdateProgress(ctx, id, syncedMessages, pageToken)
}

func (a *SyncTaskRepositoryAdapter) MarkStarted(ctx context.Context, id int64) error {
	return a.model.MarkStarted(ctx, id)
}

func (a *SyncTaskRepositoryAdapter) MarkCompleted(ctx context.Context, id int64, totalMessages int) error {
	return a.model.MarkCompleted(ctx, id, totalMessages)
}

func (a *SyncTaskRepositoryAdapter) MarkFailed(ctx context.Context, id int64, errMsg string) error {
	return a.model.MarkFailed(ctx, id, errMsg)
}
