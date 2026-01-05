package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"team-assistant/internal/interfaces"
	"team-assistant/internal/model"
	"team-assistant/internal/repository"
	"team-assistant/pkg/dify"
	"team-assistant/pkg/llm"
	"team-assistant/pkg/memory"

	"github.com/redis/go-redis/v9"
)

// AIService AI 服务
type AIService struct {
	commitRepo    interfaces.CommitRepository
	messageRepo   interfaces.MessageRepository
	memberRepo    interfaces.MemberRepository
	convRepo      *repository.ConversationRepository
	llmClient     *llm.Client
	difyClient    *dify.Client
	useDify       bool
	datasetID     string
	memoryManager *memory.MemoryManager // 永久记忆管理器
}

// NewAIService 创建 AI 服务
func NewAIService(
	commitRepo interfaces.CommitRepository,
	messageRepo interfaces.MessageRepository,
	memberRepo interfaces.MemberRepository,
	convRepo *repository.ConversationRepository,
	llmClient *llm.Client,
	difyClient *dify.Client,
	useDify bool,
	datasetID string,
) *AIService {
	return &AIService{
		commitRepo:  commitRepo,
		messageRepo: messageRepo,
		memberRepo:  memberRepo,
		convRepo:    convRepo,
		llmClient:   llmClient,
		difyClient:  difyClient,
		useDify:     useDify,
		datasetID:   datasetID,
	}
}

// InitMemoryManager 初始化记忆管理器（需要在创建后调用）
func (s *AIService) InitMemoryManager(db *sql.DB, redis *redis.Client) {
	s.memoryManager = memory.NewMemoryManager(db, redis,
		memory.WithDefaultWindowSize(10),
		memory.WithCacheExpiry(24*time.Hour),
	)
	log.Println("Memory manager initialized with persistent storage")
}

// ProcessQuery 处理用户查询
func (s *AIService) ProcessQuery(ctx context.Context, userID, query string) (string, error) {
	// 获取或创建会话记忆
	var conversationHistory string
	if s.memoryManager != nil {
		sessionID, mem, err := s.memoryManager.GetOrCreateSession(ctx, userID)
		if err != nil {
			log.Printf("Failed to get memory session: %v", err)
		} else {
			// 获取历史对话上下文
			conversationHistory, _ = mem.GetFormattedHistory(ctx)
			log.Printf("Loaded conversation history for user %s, session %s", userID, sessionID)
		}
	}

	var response string
	var err error

	if s.useDify && s.difyClient != nil {
		response, err = s.processWithDify(ctx, userID, query, conversationHistory)
	} else {
		response, err = s.processWithNativeLLM(ctx, userID, query, conversationHistory)
	}

	// 保存对话到永久记忆
	if err == nil && s.memoryManager != nil {
		_, mem, memErr := s.memoryManager.GetOrCreateSession(ctx, userID)
		if memErr == nil {
			mem.SaveContext(ctx,
				map[string]any{"input": query},
				map[string]any{"output": response},
			)
		}
	}

	return response, err
}

// ProcessQueryWithMemory 处理查询并返回记忆上下文
func (s *AIService) ProcessQueryWithMemory(ctx context.Context, userID, query string) (response string, memoryContext string, err error) {
	if s.memoryManager == nil {
		response, err = s.ProcessQuery(ctx, userID, query)
		return response, "", err
	}

	_, mem, err := s.memoryManager.GetOrCreateSession(ctx, userID)
	if err != nil {
		response, err = s.ProcessQuery(ctx, userID, query)
		return response, "", err
	}

	// 获取记忆上下文
	memoryContext, _ = mem.GetFormattedHistory(ctx)

	// 处理查询
	response, err = s.ProcessQuery(ctx, userID, query)

	return response, memoryContext, err
}

// SearchMemory 搜索用户记忆
func (s *AIService) SearchMemory(ctx context.Context, userID, keyword string, limit int) ([]memory.ChatHistoryMessage, error) {
	if s.memoryManager == nil {
		return nil, fmt.Errorf("memory manager not initialized")
	}

	return s.memoryManager.SearchAcrossSessions(ctx, userID, keyword, limit)
}

// GetMemoryHistory 获取用户记忆历史
func (s *AIService) GetMemoryHistory(ctx context.Context, userID string, limit int) (string, error) {
	if s.memoryManager == nil {
		return "", fmt.Errorf("memory manager not initialized")
	}

	_, mem, err := s.memoryManager.GetOrCreateSession(ctx, userID)
	if err != nil {
		return "", err
	}

	messages, err := mem.GetRecentMessages(ctx, limit)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	for _, msg := range messages {
		sb.WriteString(fmt.Sprintf("[%s] %s\n", msg.GetType(), msg.GetContent()))
	}

	return sb.String(), nil
}

// StartNewConversation 开始新对话（清除当前会话的记忆）
func (s *AIService) StartNewConversation(ctx context.Context, userID string) (string, error) {
	if s.memoryManager == nil {
		return "", fmt.Errorf("memory manager not initialized")
	}

	sessionID, _, err := s.memoryManager.StartNewSession(ctx, userID)
	if err != nil {
		return "", err
	}

	// 同时清除旧的 Redis 对话 ID
	s.convRepo.DeleteConversation(ctx, userID)

	return sessionID, nil
}

// ClearConversation 清除用户对话历史
func (s *AIService) ClearConversation(ctx context.Context, userID string) error {
	// 清除 Redis 中的对话 ID
	if err := s.convRepo.DeleteConversation(ctx, userID); err != nil {
		log.Printf("Failed to clear Redis conversation: %v", err)
	}

	// 清除永久记忆
	if s.memoryManager != nil {
		if err := s.memoryManager.ClearAllUserSessions(ctx, userID); err != nil {
			log.Printf("Failed to clear memory sessions: %v", err)
			return err
		}
	}

	return nil
}

// ClearCurrentSession 只清除当前会话
func (s *AIService) ClearCurrentSession(ctx context.Context, userID string) error {
	if s.memoryManager == nil {
		return s.convRepo.DeleteConversation(ctx, userID)
	}

	sessionID, _, err := s.memoryManager.GetOrCreateSession(ctx, userID)
	if err != nil {
		return err
	}

	return s.memoryManager.ClearSession(ctx, userID, sessionID)
}

// GetMemoryStats 获取记忆统计信息
func (s *AIService) GetMemoryStats(ctx context.Context) map[string]interface{} {
	if s.memoryManager == nil {
		return map[string]interface{}{"status": "not initialized"}
	}

	return s.memoryManager.GetStats(ctx)
}

// SummarizeConversation 总结用户的对话历史
func (s *AIService) SummarizeConversation(ctx context.Context, userID string) (string, error) {
	if s.memoryManager == nil {
		return "", fmt.Errorf("memory manager not initialized")
	}

	// 获取用户的对话历史
	_, mem, err := s.memoryManager.GetOrCreateSession(ctx, userID)
	if err != nil {
		return "", err
	}

	history, err := mem.GetFormattedHistory(ctx)
	if err != nil {
		return "", err
	}

	if history == "" {
		return "目前没有可总结的对话记录。", nil
	}

	// 使用 LLM 总结对话
	if s.llmClient == nil {
		return "", fmt.Errorf("LLM client not initialized")
	}

	prompt := fmt.Sprintf(`请总结以下用户与 AI 助手的对话历史，提取关键信息和主要讨论内容：

%s

请用简洁的方式总结：
1. 主要讨论的话题
2. 关键信息点
3. 未解决的问题（如有）`, history)

	summary, err := s.llmClient.GenerateResponse(ctx, prompt, nil)
	if err != nil {
		return "", fmt.Errorf("failed to generate summary: %w", err)
	}

	return fmt.Sprintf("📝 **对话总结**\n\n%s", summary), nil
}

// GetUserSessionInfo 获取用户会话信息
func (s *AIService) GetUserSessionInfo(ctx context.Context, userID string) (map[string]interface{}, error) {
	if s.memoryManager == nil {
		return nil, fmt.Errorf("memory manager not initialized")
	}

	// 获取当前会话
	sessionID, mem, err := s.memoryManager.GetOrCreateSession(ctx, userID)
	if err != nil {
		return nil, err
	}

	// 获取所有会话列表
	sessions, _ := s.memoryManager.GetUserSessions(ctx, userID)

	// 获取当前会话消息数
	messages, _ := mem.GetRecentMessages(ctx, 100)

	return map[string]interface{}{
		"current_session_id":   sessionID,
		"total_sessions":       len(sessions),
		"current_message_count": len(messages),
		"sessions":             sessions,
	}, nil
}

// RecallConversation 回忆之前的对话内容
func (s *AIService) RecallConversation(ctx context.Context, userID, topic string) (string, error) {
	if s.memoryManager == nil {
		return "", fmt.Errorf("memory manager not initialized")
	}

	// 搜索相关对话
	results, err := s.memoryManager.SearchAcrossSessions(ctx, userID, topic, 20)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return fmt.Sprintf("没有找到关于「%s」的对话记录。", topic), nil
	}

	// 格式化结果
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🔍 找到 %d 条关于「%s」的对话记录：\n\n", len(results), topic))

	for i, msg := range results {
		if i >= 10 {
			sb.WriteString(fmt.Sprintf("\n...(还有 %d 条记录)", len(results)-10))
			break
		}

		role := "👤 用户"
		if msg.Role == "ai" || msg.Role == "assistant" {
			role = "🤖 助手"
		}

		content := msg.Content
		if len(content) > 150 {
			content = content[:147] + "..."
		}

		sb.WriteString(fmt.Sprintf("**[%s]** %s\n%s\n\n",
			msg.CreatedAt.Format("2006-01-02 15:04"),
			role,
			content))
	}

	return sb.String(), nil
}

// processWithDify 使用 Dify 处理
func (s *AIService) processWithDify(ctx context.Context, userID, query, conversationHistory string) (string, error) {
	// 收集上下文数据
	contextData, err := s.gatherContext(ctx, query)
	if err != nil {
		log.Printf("Failed to gather context: %v", err)
	}

	// 如果配置了知识库，先搜索相关内容
	var knowledgeContext string
	if s.datasetID != "" {
		searchResult, err := s.difyClient.SearchKnowledge(ctx, s.datasetID, &dify.KnowledgeSearchRequest{
			Query: query,
			TopK:  5,
		})
		if err != nil {
			log.Printf("Dify knowledge search error: %v", err)
		} else if len(searchResult.Records) > 0 {
			var contexts []string
			for _, r := range searchResult.Records {
				contexts = append(contexts, r.Segment.Content)
			}
			knowledgeContext = strings.Join(contexts, "\n---\n")
			log.Printf("Found %d relevant knowledge segments", len(searchResult.Records))
		}
	}

	// 获取对话 ID（从 Redis）
	conversationID, _ := s.convRepo.GetConversationID(ctx, userID)

	// 构建 Dify 请求
	req := &dify.ChatRequest{
		Query:          query,
		User:           userID,
		ConversationID: conversationID,
		ResponseMode:   "blocking",
		Inputs: map[string]interface{}{
			"git_stats":            contextData.GitStats,
			"recent_messages":      contextData.RecentMessages,
			"knowledge_context":    knowledgeContext,
			"conversation_history": conversationHistory, // 添加永久记忆上下文
			"current_time":         time.Now().Format("2006-01-02 15:04:05"),
		},
	}

	resp, err := s.difyClient.Chat(ctx, req)
	if err != nil {
		log.Printf("Dify chat error: %v, falling back to native LLM", err)
		if s.llmClient != nil {
			return s.processWithNativeLLM(ctx, userID, query, conversationHistory)
		}
		return "抱歉，AI 服务暂时不可用，请稍后重试。", nil
	}

	// 保存对话 ID 到 Redis（24小时过期）
	if resp.ConversationID != "" {
		s.convRepo.SaveConversationID(ctx, userID, resp.ConversationID, 24*time.Hour)
	}

	return resp.Answer, nil
}

// processWithNativeLLM 使用原生 LLM 处理
func (s *AIService) processWithNativeLLM(ctx context.Context, userID, query, conversationHistory string) (string, error) {
	// 解析用户意图
	parsed, err := s.llmClient.ParseUserQuery(ctx, query)
	if err != nil {
		log.Printf("Failed to parse query: %v", err)
		return "抱歉，我无法理解您的问题，请换个方式提问。", nil
	}

	log.Printf("Parsed query: intent=%s, time_range=%s, users=%v",
		parsed.Intent, parsed.TimeRange, parsed.TargetUsers)

	// 根据意图处理
	switch parsed.Intent {
	case llm.IntentQueryWorkload, llm.IntentQueryCommits:
		return s.handleWorkloadQuery(ctx, parsed)
	case llm.IntentSearchMessage:
		// 搜索记忆中的相关内容
		if s.memoryManager != nil && len(parsed.Keywords) > 0 {
			keyword := strings.Join(parsed.Keywords, " ")
			memoryResults, err := s.memoryManager.SearchAcrossSessions(ctx, userID, keyword, 10)
			if err == nil && len(memoryResults) > 0 {
				return s.formatMemorySearchResults(memoryResults, keyword), nil
			}
		}
		return s.handleMessageSearch(ctx, parsed)
	case llm.IntentSummarize:
		return s.handleSummarize(ctx, parsed)
	case llm.IntentHelp:
		return s.getHelpMessage(), nil
	default:
		// 对于通用对话，使用记忆上下文增强
		if conversationHistory != "" {
			return s.handleGeneralChat(ctx, query, conversationHistory)
		}
		return "抱歉，我暂时无法处理这个请求。您可以问我：\n• 某人的工作量\n• 代码提交记录\n• 搜索聊天内容\n• 总结群消息", nil
	}
}

// handleGeneralChat 处理通用对话（带记忆上下文）
func (s *AIService) handleGeneralChat(ctx context.Context, query, conversationHistory string) (string, error) {
	prompt := fmt.Sprintf(`你是一个团队助手，正在与用户进行对话。

之前的对话历史：
%s

用户当前的问题：%s

请根据对话历史和当前问题，给出有帮助的回复。`, conversationHistory, query)

	response, err := s.llmClient.GenerateResponse(ctx, prompt, nil)
	if err != nil {
		return "抱歉，处理您的问题时出错了。", err
	}

	return response, nil
}

// formatMemorySearchResults 格式化记忆搜索结果
func (s *AIService) formatMemorySearchResults(results []memory.ChatHistoryMessage, keyword string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🧠 在对话记忆中找到 %d 条关于「%s」的记录:\n\n", len(results), keyword))

	for i, msg := range results {
		if i >= 10 {
			sb.WriteString(fmt.Sprintf("...(还有 %d 条记录)\n", len(results)-10))
			break
		}

		role := "用户"
		if msg.Role == "ai" || msg.Role == "assistant" {
			role = "助手"
		}

		content := msg.Content
		if len(content) > 100 {
			content = content[:97] + "..."
		}

		sb.WriteString(fmt.Sprintf("[%s] %s: %s\n",
			msg.CreatedAt.Format("01-02 15:04"),
			role,
			content))
	}

	return sb.String()
}

// ContextData 上下文数据
type ContextData struct {
	GitStats       string
	RecentMessages string
}

// gatherContext 收集上下文数据
func (s *AIService) gatherContext(ctx context.Context, query string) (*ContextData, error) {
	data := &ContextData{}

	// 获取最近的 Git 统计
	endTime := time.Now()
	startTime := endTime.AddDate(0, 0, -7)

	stats, err := s.commitRepo.GetAllStats(ctx, startTime, endTime)
	if err == nil && len(stats) > 0 {
		statsJSON, _ := json.Marshal(stats)
		data.GitStats = string(statsJSON)
	}

	// 获取最近的消息
	messages, err := s.messageRepo.GetMessagesByDateRange(ctx, "", startTime, endTime, 50)
	if err == nil && len(messages) > 0 {
		var msgTexts []string
		for _, msg := range messages {
			if msg.Content.Valid {
				msgTexts = append(msgTexts, msg.Content.String)
			}
		}
		data.RecentMessages = strings.Join(msgTexts, "\n")
	}

	return data, nil
}

// handleWorkloadQuery 处理工作量查询
func (s *AIService) handleWorkloadQuery(ctx context.Context, parsed *llm.ParsedQuery) (string, error) {
	startTime, endTime := s.getTimeRange(parsed.TimeRange)

	var stats []*model.CommitStats
	var err error

	if len(parsed.TargetUsers) > 0 {
		for _, user := range parsed.TargetUsers {
			members, findErr := s.memberRepo.FindByName(ctx, user)
			if findErr == nil && len(members) > 0 && members[0].GitHubUsername.Valid {
				userStats, statErr := s.commitRepo.GetStatsByMember(ctx, members[0].ID, startTime, endTime)
				if statErr == nil {
					stats = append(stats, userStats)
				}
			} else {
				userStats, statErr := s.commitRepo.GetStatsByAuthorName(ctx, user, startTime, endTime)
				if statErr == nil {
					stats = append(stats, userStats)
				}
			}
		}
	} else {
		stats, err = s.commitRepo.GetAllStats(ctx, startTime, endTime)
		if err != nil {
			return "查询工作量失败，请稍后重试。", err
		}
	}

	if len(stats) == 0 {
		return fmt.Sprintf("在 %s 到 %s 期间没有找到提交记录。",
			startTime.Format("2006-01-02"),
			endTime.Format("2006-01-02")), nil
	}

	// 使用 LLM 生成友好回复
	response, err := s.llmClient.GenerateResponse(ctx, parsed.RawQuery, stats)
	if err != nil {
		return s.formatWorkloadStats(stats, startTime, endTime), nil
	}

	return response, nil
}

// formatWorkloadStats 格式化工作量统计
func (s *AIService) formatWorkloadStats(stats []*model.CommitStats, start, end time.Time) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📊 工作量统计 (%s ~ %s)\n\n",
		start.Format("01-02"), end.Format("01-02")))

	for _, stat := range stats {
		sb.WriteString(fmt.Sprintf("👤 %s\n", stat.AuthorName))
		sb.WriteString(fmt.Sprintf("   提交: %d 次\n", stat.CommitCount))
		sb.WriteString(fmt.Sprintf("   新增: %d 行 | 删除: %d 行\n", stat.Additions, stat.Deletions))
		sb.WriteString(fmt.Sprintf("   涉及仓库: %d 个\n\n", stat.RepoCount))
	}

	return sb.String()
}

// handleMessageSearch 处理消息搜索
func (s *AIService) handleMessageSearch(ctx context.Context, parsed *llm.ParsedQuery) (string, error) {
	var messages []*model.ChatMessage
	var err error

	if len(parsed.Keywords) > 0 {
		keyword := strings.Join(parsed.Keywords, " ")
		messages, err = s.messageRepo.SearchByContent(ctx, "", keyword, 20)
	} else {
		startTime, endTime := s.getTimeRange(parsed.TimeRange)
		messages, err = s.messageRepo.GetMessagesByDateRange(ctx, "", startTime, endTime, 50)
	}

	if err != nil {
		return "搜索消息失败，请稍后重试。", err
	}

	if len(messages) == 0 {
		return "没有找到匹配的消息。", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🔍 找到 %d 条相关消息:\n\n", len(messages)))

	for i, msg := range messages {
		if i >= 10 {
			sb.WriteString(fmt.Sprintf("...(还有 %d 条消息)\n", len(messages)-10))
			break
		}
		senderName := ""
		if msg.SenderName.Valid {
			senderName = msg.SenderName.String
		}
		content := ""
		if msg.Content.Valid {
			content = msg.Content.String
		}
		sb.WriteString(fmt.Sprintf("[%s] %s: %s\n",
			msg.CreatedAt.Format("01-02 15:04"),
			senderName,
			truncateString(content, 100)))
	}

	return sb.String(), nil
}

// handleSummarize 处理总结请求
func (s *AIService) handleSummarize(ctx context.Context, parsed *llm.ParsedQuery) (string, error) {
	startTime, endTime := s.getTimeRange(parsed.TimeRange)

	messages, err := s.messageRepo.GetMessagesByDateRange(ctx, "", startTime, endTime, 100)
	if err != nil {
		return "获取消息失败，请稍后重试。", err
	}

	if len(messages) == 0 {
		return "没有找到需要总结的消息。", nil
	}

	var msgTexts []string
	for _, msg := range messages {
		senderName := ""
		if msg.SenderName.Valid {
			senderName = msg.SenderName.String
		}
		content := ""
		if msg.Content.Valid {
			content = msg.Content.String
		}
		msgTexts = append(msgTexts, fmt.Sprintf("[%s] %s: %s",
			msg.CreatedAt.Format("15:04"),
			senderName,
			content))
	}

	summary, err := s.llmClient.SummarizeMessages(ctx, msgTexts)
	if err != nil {
		return "总结消息失败，请稍后重试。", err
	}

	return fmt.Sprintf("📋 消息总结 (%s ~ %s)\n\n%s",
		startTime.Format("01-02 15:04"),
		endTime.Format("01-02 15:04"),
		summary), nil
}

// getTimeRange 获取时间范围
func (s *AIService) getTimeRange(tr llm.TimeRange) (time.Time, time.Time) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	switch tr {
	case llm.TimeRangeToday:
		return today, now
	case llm.TimeRangeYesterday:
		return today.AddDate(0, 0, -1), today
	case llm.TimeRangeThisWeek:
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		weekStart := today.AddDate(0, 0, -(weekday - 1))
		return weekStart, now
	case llm.TimeRangeLastWeek:
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		thisWeekStart := today.AddDate(0, 0, -(weekday - 1))
		lastWeekStart := thisWeekStart.AddDate(0, 0, -7)
		return lastWeekStart, thisWeekStart
	case llm.TimeRangeThisMonth:
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		return monthStart, now
	case llm.TimeRangeLastMonth:
		thisMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		lastMonthStart := thisMonthStart.AddDate(0, -1, 0)
		return lastMonthStart, thisMonthStart
	default:
		// 默认查询最近3年的消息
		return today.AddDate(-3, 0, 0), now
	}
}

// getHelpMessage 获取帮助信息
func (s *AIService) getHelpMessage() string {
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

🧠 **对话记忆** (永久存储)
• 我会记住我们所有的对话历史
• "我们之前聊过什么？" - 查看对话历史
• "总结我们的对话" - 总结对话记录
• "回忆一下关于XX的对话" - 搜索特定话题
• "新对话" - 开始新的会话
• "清除对话" - 清除所有对话记录

💡 **提示**
• 支持自然语言提问
• 可以指定时间范围（今天、本周、上周、本月等）
• 对话记录会永久保存，跨会话可搜索
• @我即可开始对话`
}

func truncateString(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}
