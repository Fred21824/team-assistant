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

	log.Printf("Parsed query: intent=%s, time_range=%s, users=%v, group=%s",
		parsed.Intent, parsed.TimeRange, parsed.TargetUsers, parsed.TargetGroup)

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

// handleMessageSearch 处理消息搜索（支持语义搜索）
func (hp *HybridProcessor) handleMessageSearch(ctx context.Context, parsed *llm.ParsedQuery) (string, error) {
	// 优先使用 RAG 语义搜索
	if hp.svcCtx.Services.RAG != nil && hp.svcCtx.Services.RAG.IsEnabled() {
		return hp.handleSemanticSearch(ctx, parsed)
	}

	// 降级到传统关键词搜索
	return hp.handleKeywordSearch(ctx, parsed)
}

// handleSemanticSearch 语义搜索（RAG）
func (hp *HybridProcessor) handleSemanticSearch(ctx context.Context, parsed *llm.ParsedQuery) (string, error) {
	// 构建搜索查询
	query := parsed.RawQuery
	if len(parsed.Keywords) > 0 {
		query = strings.Join(parsed.Keywords, " ")
	}

	// 确定搜索范围
	var chatID string
	if parsed.TargetGroup != "" {
		chatID, _ = hp.findChatByName(ctx, parsed.TargetGroup)
	}

	// 执行语义搜索
	results, err := hp.svcCtx.Services.RAG.Search(ctx, query, 15, chatID)
	if err != nil {
		log.Printf("Semantic search failed: %v, falling back to keyword search", err)
		return hp.handleKeywordSearch(ctx, parsed)
	}

	if len(results) == 0 {
		return "没有找到相关的消息。", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🔍 语义搜索找到 %d 条相关消息:\n\n", len(results)))

	for i, r := range results {
		if i >= 10 {
			sb.WriteString(fmt.Sprintf("...(还有 %d 条消息)\n", len(results)-10))
			break
		}
		sb.WriteString(fmt.Sprintf("[%s] %s 在「%s」:\n%s\n(相关度: %.0f%%)\n\n",
			r.CreatedAt.Format("01-02 15:04"),
			r.SenderName,
			r.ChatName,
			truncateString(r.Content, 150),
			r.Score*100))
	}

	return sb.String(), nil
}

// handleKeywordSearch 传统关键词搜索
func (hp *HybridProcessor) handleKeywordSearch(ctx context.Context, parsed *llm.ParsedQuery) (string, error) {
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

	// 如果指定了群名，先查找对应的 chat_id
	var chatID string
	var groupName string
	if parsed.TargetGroup != "" {
		log.Printf("Looking for group: %s", parsed.TargetGroup)
		chatID, groupName = hp.findChatByName(ctx, parsed.TargetGroup)
		if chatID == "" {
			// 列出可用的群
			availableGroups := hp.listAvailableGroups(ctx)
			return fmt.Sprintf("❌ 未找到群「%s」\n\n可用的群：\n%s\n\n💡 请使用准确的群名，或发送「列出群聊」查看所有群。",
				parsed.TargetGroup, availableGroups), nil
		}
		log.Printf("Found group: %s (chat_id: %s)", groupName, chatID)
	}

	log.Printf("Summarizing messages from %s to %s, chatID: %s", startTime.Format("2006-01-02 15:04"), endTime.Format("2006-01-02 15:04"), chatID)

	messages, err := hp.svcCtx.MessageModel.GetMessagesByDateRange(ctx, chatID, startTime, endTime, 100)
	if err != nil {
		log.Printf("Failed to get messages: %v", err)
		return "获取消息失败，请稍后重试。", err
	}

	log.Printf("Found %d messages to summarize", len(messages))

	if len(messages) == 0 {
		if groupName != "" {
			return fmt.Sprintf("在「%s」群中没有找到 %s 至 %s 期间的消息。",
				groupName, startTime.Format("01-02"), endTime.Format("01-02")), nil
		}
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

	log.Printf("Calling LLM to summarize %d messages", len(msgTexts))
	summary, err := hp.llmClient.SummarizeMessages(ctx, msgTexts)
	if err != nil {
		log.Printf("LLM summarize error: %v", err)
		return "总结消息失败，请稍后重试。", err
	}
	log.Printf("LLM summary generated successfully")

	title := "消息总结"
	if groupName != "" {
		title = fmt.Sprintf("「%s」消息总结", groupName)
	}

	return fmt.Sprintf("📋 %s (%s ~ %s)\n\n%s",
		title,
		startTime.Format("01-02 15:04"),
		endTime.Format("01-02 15:04"),
		summary), nil
}

// findChatByName 根据群名查找 chat_id（使用 LLM 智能匹配）
func (hp *HybridProcessor) findChatByName(ctx context.Context, groupName string) (chatID, name string) {
	// 先从飞书 API 获取群列表
	chats, err := hp.svcCtx.LarkClient.GetChats(ctx)
	if err != nil {
		log.Printf("Failed to get chats from Lark: %v", err)
		// 尝试从数据库查找
		groups, dbErr := hp.svcCtx.GroupModel.ListAll(ctx)
		if dbErr != nil {
			log.Printf("Failed to get groups from DB: %v", dbErr)
			return "", ""
		}
		// 简单字符串匹配
		for _, g := range groups {
			if g.ChatName.Valid && strings.Contains(g.ChatName.String, groupName) {
				return g.ChatID, g.ChatName.String
			}
		}
		return "", ""
	}

	// 第一轮：精确包含匹配
	for _, chat := range chats {
		if strings.Contains(chat.Name, groupName) {
			return chat.ChatID, chat.Name
		}
	}

	// 第二轮：使用 LLM 智能匹配
	if hp.llmClient != nil && len(chats) > 0 {
		var chatNames []string
		chatMap := make(map[string]string) // name -> chatID
		for _, chat := range chats {
			chatNames = append(chatNames, chat.Name)
			chatMap[chat.Name] = chat.ChatID
		}

		matchedName := hp.matchGroupWithLLM(ctx, groupName, chatNames)
		if matchedName != "" {
			log.Printf("LLM matched group: '%s' -> '%s'", groupName, matchedName)
			return chatMap[matchedName], matchedName
		}
	}

	return "", ""
}

// matchGroupWithLLM 使用 LLM 智能匹配群名
func (hp *HybridProcessor) matchGroupWithLLM(ctx context.Context, userQuery string, availableGroups []string) string {
	if hp.llmClient == nil || len(availableGroups) == 0 {
		return ""
	}

	prompt := fmt.Sprintf(`用户想要查找的群: "%s"

可用的群列表:
%s

请判断用户想要的是哪个群？如果找到匹配的，只返回群的完整名称（必须与列表中完全一致）。如果没有匹配的，返回空字符串。

注意：
- "印尼群" 可能匹配 "印度尼西亚_研发沟通群"
- "研发群" 可能匹配 "研发沟通群" 或包含"研发"的群
- 进行语义理解，不只是简单的字符串匹配

只返回群名，不要其他内容:`, userQuery, strings.Join(availableGroups, "\n"))

	resp, err := hp.llmClient.GenerateResponse(ctx, prompt, nil)
	if err != nil {
		log.Printf("LLM group match failed: %v", err)
		return ""
	}

	// 清理响应
	resp = strings.TrimSpace(resp)
	resp = strings.Trim(resp, "\"'")

	// 验证返回的群名是否在列表中
	for _, g := range availableGroups {
		if resp == g {
			return resp
		}
	}

	return ""
}

// listAvailableGroups 列出可用的群
func (hp *HybridProcessor) listAvailableGroups(ctx context.Context) string {
	chats, err := hp.svcCtx.LarkClient.GetChats(ctx)
	if err != nil {
		return "（无法获取群列表）"
	}

	if len(chats) == 0 {
		return "（机器人未加入任何群）"
	}

	var names []string
	for _, chat := range chats {
		names = append(names, "• "+chat.Name)
	}
	return strings.Join(names, "\n")
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
