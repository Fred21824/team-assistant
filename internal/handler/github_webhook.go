package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"team-assistant/internal/model"
	"team-assistant/internal/svc"
	"team-assistant/pkg/github"
)

// GitHubWebhookHandler 处理GitHub Webhook
type GitHubWebhookHandler struct {
	svcCtx *svc.ServiceContext
}

// NewGitHubWebhookHandler 创建GitHub Webhook处理器
func NewGitHubWebhookHandler(svcCtx *svc.ServiceContext) *GitHubWebhookHandler {
	return &GitHubWebhookHandler{svcCtx: svcCtx}
}

// Handle 处理GitHub事件
func (h *GitHubWebhookHandler) Handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Failed to read request body: %v", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// 验证签名
	signature := r.Header.Get("X-Hub-Signature-256")
	if !h.verifySignature(body, signature) {
		log.Printf("GitHub webhook signature verification failed")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// 获取事件类型
	eventType := r.Header.Get("X-GitHub-Event")
	log.Printf("Received GitHub event: %s", eventType)

	switch eventType {
	case "push":
		h.handlePush(body)
	case "ping":
		log.Printf("GitHub webhook ping received")
	default:
		log.Printf("Unknown GitHub event type: %s", eventType)
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// verifySignature 验证GitHub签名
func (h *GitHubWebhookHandler) verifySignature(payload []byte, signature string) bool {
	if h.svcCtx.Config.GitHub.WebhookSecret == "" {
		// 没有配置secret，跳过验证
		return true
	}

	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}

	expectedSig := signature[7:]
	mac := hmac.New(sha256.New, []byte(h.svcCtx.Config.GitHub.WebhookSecret))
	mac.Write(payload)
	actualSig := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expectedSig), []byte(actualSig))
}

// handlePush 处理push事件
func (h *GitHubWebhookHandler) handlePush(payload []byte) {
	ctx := context.Background()

	var event github.WebhookPayload
	if err := json.Unmarshal(payload, &event); err != nil {
		log.Printf("Failed to parse push event: %v", err)
		return
	}

	log.Printf("Push to %s by %s, %d commits",
		event.Repository.FullName,
		event.Pusher.Name,
		len(event.Commits))

	// 提取仓库信息
	parts := strings.Split(event.Repository.FullName, "/")
	if len(parts) != 2 {
		log.Printf("Invalid repository name: %s", event.Repository.FullName)
		return
	}
	repoName := parts[1]

	// 保存提交记录
	for _, commit := range event.Commits {
		commitTime, _ := time.Parse(time.RFC3339, commit.Timestamp)

		gitCommit := &model.GitCommit{
			CommitSHA:     commit.ID,
			RepoName:      repoName,
			RepoFullName:  sql.NullString{String: event.Repository.FullName, Valid: true},
			AuthorName:    commit.Author.Name,
			AuthorEmail:   sql.NullString{String: commit.Author.Email, Valid: commit.Author.Email != ""},
			CommitMessage: sql.NullString{String: commit.Message, Valid: true},
			CommittedAt:   commitTime,
			Additions:     0, // Webhook不提供行数统计
			Deletions:     0,
			FilesChanged:  len(commit.Added) + len(commit.Modified) + len(commit.Removed),
		}

		if err := h.svcCtx.CommitModel.Insert(ctx, gitCommit); err != nil {
			log.Printf("Failed to save commit %s: %v", commit.ID, err)
		} else {
			log.Printf("Saved commit: %s - %s", commit.ID[:7], truncateString(commit.Message, 50))
		}
	}

	// 可选：发送通知到飞书
	go h.notifyLark(event)
}

// notifyLark 发送提交通知到飞书
func (h *GitHubWebhookHandler) notifyLark(event github.WebhookPayload) {
	if len(event.Commits) == 0 {
		return
	}

	// 构建通知消息
	var sb strings.Builder
	sb.WriteString("📦 代码提交通知\n\n")
	sb.WriteString("仓库: " + event.Repository.FullName + "\n")
	sb.WriteString("推送者: " + event.Pusher.Name + "\n")
	sb.WriteString(fmt.Sprintf("提交数: %d\n\n", len(event.Commits)))

	for i, commit := range event.Commits {
		if i >= 5 {
			sb.WriteString("...(更多提交省略)\n")
			break
		}
		sb.WriteString("• " + truncateString(commit.Message, 60) + "\n")
	}

	// 发送到配置的通知群（如果有配置）
	// h.svcCtx.LarkClient.SendMessage(chatID, "text", map[string]string{"text": sb.String()})
	log.Printf("Would send notification: %s", sb.String())
}

func truncateString(s string, maxLen int) string {
	// 取第一行
	if idx := strings.Index(s, "\n"); idx != -1 {
		s = s[:idx]
	}
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}
