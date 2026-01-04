package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"team-assistant/internal/model"
	"team-assistant/internal/svc"
	"team-assistant/pkg/dify"
	"team-assistant/pkg/llm"
)

// HybridProcessor 混合 AI 处理器
// 支持 Dify 和原生 LLM 两种模式
type HybridProcessor struct {
	svcCtx           *svc.ServiceContext
	difyClient       *dify.Client
	llmClient        *llm.Client
	useDify          bool
	datasetID        string            // Dify 知识库 ID
	conversationMap  map[string]string // 用户对话 ID 映射 (userID -> conversationID)
}

// NewHybridProcessor 创建混合处理器
func NewHybridProcessor(svcCtx *svc.ServiceContext) *HybridProcessor {
	hp := &HybridProcessor{
		svcCtx:          svcCtx,
		useDify:         svcCtx.Config.Dify.Enabled,
		datasetID:       svcCtx.Config.Dify.DatasetID,
		conversationMap: make(map[string]string),
	}

	if hp.useDify && svcCtx.Config.Dify.APIKey != "" {
		hp.difyClient = dify.NewClient(svcCtx.Config.Dify.BaseURL, svcCtx.Config.Dify.APIKey)
		log.Println("Using Dify for AI processing")
	}

	// 始终初始化原生 LLM 作为备用
	if svcCtx.Config.LLM.APIKey != "" {
		hp.llmClient = llm.NewClient(
			svcCtx.Config.LLM.APIKey,
			svcCtx.Config.LLM.Endpoint,
			svcCtx.Config.LLM.Model,
		)
		if !hp.useDify {
			log.Println("Using native LLM for AI processing")
		} else {
			log.Println("Native LLM initialized as fallback")
		}
	}

	return hp
}

// ProcessQuery 处理用户查询
func (hp *HybridProcessor) ProcessQuery(ctx context.Context, userID, query string) (string, error) {
	if hp.useDify && hp.difyClient != nil {
		return hp.processWithDify(ctx, userID, query)
	}
	return hp.processWithNativeLLM(ctx, query)
}

// processWithDify 使用 Dify 处理
func (hp *HybridProcessor) processWithDify(ctx context.Context, userID, query string) (string, error) {
	// 收集上下文数据
	contextData, err := hp.gatherContext(ctx, query)
	if err != nil {
		log.Printf("Failed to gather context: %v", err)
	}

	// 如果配置了知识库，先搜索相关内容
	var knowledgeContext string
	if hp.datasetID != "" {
		searchResult, err := hp.difyClient.SearchKnowledge(ctx, hp.datasetID, &dify.KnowledgeSearchRequest{
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

	// 获取对话 ID（支持多轮对话）
	conversationID := hp.conversationMap[userID]

	// 构建 Dify 请求
	req := &dify.ChatRequest{
		Query:          query,
		User:           userID,
		ConversationID: conversationID,
		ResponseMode:   "blocking",
		Inputs: map[string]interface{}{
			"git_stats":         contextData.GitStats,
			"recent_messages":   contextData.RecentMessages,
			"knowledge_context": knowledgeContext,
			"current_time":      time.Now().Format("2006-01-02 15:04:05"),
		},
	}

	resp, err := hp.difyClient.Chat(ctx, req)
	if err != nil {
		log.Printf("Dify chat error: %v, falling back to native LLM", err)
		// 回退到原生 LLM
		if hp.llmClient != nil {
			return hp.processWithNativeLLM(ctx, query)
		}
		return "抱歉，AI 服务暂时不可用，请稍后重试。", nil
	}

	// 保存对话 ID 用于多轮对话
	if resp.ConversationID != "" {
		hp.conversationMap[userID] = resp.ConversationID
	}

	return resp.Answer, nil
}

// ClearConversation 清除用户的对话历史
func (hp *HybridProcessor) ClearConversation(userID string) {
	delete(hp.conversationMap, userID)
}

// processWithNativeLLM 使用原生 LLM 处理
func (hp *HybridProcessor) processWithNativeLLM(ctx context.Context, query string) (string, error) {
	// 解析用户意图
	parsed, err := hp.llmClient.ParseUserQuery(ctx, query)
	if err != nil {
		log.Printf("Failed to parse query: %v", err)
		return "抱歉，我无法理解您的问题，请换个方式提问。", nil
	}

	log.Printf("Parsed query: intent=%s, time_range=%s, users=%v",
		parsed.Intent, parsed.TimeRange, parsed.TargetUsers)

	// 根据意图处理
	switch parsed.Intent {
	case llm.IntentQueryWorkload, llm.IntentQueryCommits:
		return hp.handleWorkloadQuery(ctx, parsed)
	case llm.IntentSearchMessage:
		return hp.handleMessageSearch(ctx, parsed)
	case llm.IntentSummarize:
		return hp.handleSummarize(ctx, parsed)
	case llm.IntentHelp:
		return hp.getHelpMessage(), nil
	default:
		return "抱歉，我暂时无法处理这个请求。您可以问我：\n• 某人的工作量\n• 代码提交记录\n• 搜索聊天内容\n• 总结群消息", nil
	}
}

// ContextData 上下文数据
type ContextData struct {
	GitStats       string
	RecentMessages string
}

// gatherContext 收集上下文数据
func (hp *HybridProcessor) gatherContext(ctx context.Context, query string) (*ContextData, error) {
	data := &ContextData{}

	// 获取最近的 Git 统计
	endTime := time.Now()
	startTime := endTime.AddDate(0, 0, -7) // 最近7天

	stats, err := hp.svcCtx.CommitModel.GetAllStats(ctx, startTime, endTime)
	if err == nil && len(stats) > 0 {
		statsJSON, _ := json.Marshal(stats)
		data.GitStats = string(statsJSON)
	}

	// 获取最近的消息
	messages, err := hp.svcCtx.MessageModel.GetMessagesByDateRange(ctx, "", startTime, endTime, 50)
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
func (hp *HybridProcessor) handleWorkloadQuery(ctx context.Context, parsed *llm.ParsedQuery) (string, error) {
	startTime, endTime := hp.getTimeRange(parsed.TimeRange)

	var stats []*model.CommitStats
	var err error

	if len(parsed.TargetUsers) > 0 {
		for _, user := range parsed.TargetUsers {
			members, findErr := hp.svcCtx.MemberModel.FindByName(ctx, user)
			if findErr == nil && len(members) > 0 && members[0].GitHubUsername.Valid {
				userStats, statErr := hp.svcCtx.CommitModel.GetStatsByMember(ctx, members[0].ID, startTime, endTime)
				if statErr == nil {
					stats = append(stats, userStats)
				}
			} else {
				userStats, statErr := hp.svcCtx.CommitModel.GetStatsByAuthorName(ctx, user, startTime, endTime)
				if statErr == nil {
					stats = append(stats, userStats)
				}
			}
		}
	} else {
		stats, err = hp.svcCtx.CommitModel.GetAllStats(ctx, startTime, endTime)
		if err != nil {
			return "查询工作量失败，请稍后重试。", err
		}
	}

	if len(stats) == 0 {
		return fmt.Sprintf("在 %s 到 %s 期间没有找到提交记录。",
			startTime.Format("2006-01-02"),
			endTime.Format("2006-01-02")), nil
	}

	// 使用LLM生成友好回复
	response, err := hp.llmClient.GenerateResponse(ctx, parsed.RawQuery, stats)
	if err != nil {
		return hp.formatWorkloadStats(stats, startTime, endTime), nil
	}

	return response, nil
}

// formatWorkloadStats 格式化工作量统计
func (hp *HybridProcessor) formatWorkloadStats(stats []*model.CommitStats, start, end time.Time) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📊 工作量统计 (%s ~ %s)\n\n",
		start.Format("01-02"), end.Format("01-02")))

	for _, s := range stats {
		sb.WriteString(fmt.Sprintf("👤 %s\n", s.AuthorName))
		sb.WriteString(fmt.Sprintf("   提交: %d 次\n", s.CommitCount))
		sb.WriteString(fmt.Sprintf("   新增: %d 行 | 删除: %d 行\n", s.Additions, s.Deletions))
		sb.WriteString(fmt.Sprintf("   涉及仓库: %d 个\n\n", s.RepoCount))
	}

	return sb.String()
}

// handleMessageSearch 处理消息搜索
func (hp *HybridProcessor) handleMessageSearch(ctx context.Context, parsed *llm.ParsedQuery) (string, error) {
	var messages []*model.ChatMessage
	var err error

	if len(parsed.Keywords) > 0 {
		keyword := strings.Join(parsed.Keywords, " ")
		messages, err = hp.svcCtx.MessageModel.SearchByContent(ctx, "", keyword, 20)
	} else if len(parsed.TargetUsers) > 0 {
		for _, user := range parsed.TargetUsers {
			userMsgs, searchErr := hp.svcCtx.MessageModel.SearchBySender(ctx, "", user, "", 20)
			if searchErr == nil {
				messages = append(messages, userMsgs...)
			}
		}
	} else {
		startTime, endTime := hp.getTimeRange(parsed.TimeRange)
		messages, err = hp.svcCtx.MessageModel.GetMessagesByDateRange(ctx, "", startTime, endTime, 50)
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
func (hp *HybridProcessor) handleSummarize(ctx context.Context, parsed *llm.ParsedQuery) (string, error) {
	startTime, endTime := hp.getTimeRange(parsed.TimeRange)

	messages, err := hp.svcCtx.MessageModel.GetMessagesByDateRange(ctx, "", startTime, endTime, 100)
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

	summary, err := hp.llmClient.SummarizeMessages(ctx, msgTexts)
	if err != nil {
		return "总结消息失败，请稍后重试。", err
	}

	return fmt.Sprintf("📋 消息总结 (%s ~ %s)\n\n%s",
		startTime.Format("01-02 15:04"),
		endTime.Format("01-02 15:04"),
		summary), nil
}

// getTimeRange 获取时间范围
func (hp *HybridProcessor) getTimeRange(tr llm.TimeRange) (time.Time, time.Time) {
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
		return today.AddDate(0, 0, -7), now
	}
}

// getHelpMessage 获取帮助信息
func (hp *HybridProcessor) getHelpMessage() string {
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

func truncateString(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}
