package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"team-assistant/internal/model"
	"team-assistant/internal/service"
	"team-assistant/internal/svc"
	"team-assistant/pkg/dify"
	"team-assistant/pkg/lark"
	"team-assistant/pkg/llm"
)

// ConversationContext 对话上下文
type ConversationContext struct {
	LastQuery     string           // 上一个问题
	LastAnswer    string           // 上一次的回答（用于追问时参考）
	LastParsed    *llm.ParsedQuery // 上一次解析结果
	LastChatID    string           // 上一次使用的 chatID
	LastTimestamp time.Time        // 上一次交互时间
}

// HybridProcessor 混合 AI 处理器
// 支持 Dify 和原生 LLM 两种模式
type HybridProcessor struct {
	svcCtx          *svc.ServiceContext
	difyClient      *dify.Client
	llmClient       *llm.Client
	useDify         bool
	datasetID       string                          // Dify 知识库 ID
	conversationMap map[string]string               // 用户对话 ID 映射 (userID -> conversationID)
	contextMap      map[string]*ConversationContext // 用户对话上下文 (userID -> context)
	mu              sync.RWMutex                    // 保护 conversationMap 和 contextMap 的并发访问
}

// NewHybridProcessor 创建混合处理器
func NewHybridProcessor(svcCtx *svc.ServiceContext) *HybridProcessor {
	hp := &HybridProcessor{
		svcCtx:          svcCtx,
		useDify:         svcCtx.Config.Dify.Enabled,
		datasetID:       svcCtx.Config.Dify.DatasetID,
		conversationMap: make(map[string]string),
		contextMap:      make(map[string]*ConversationContext),
	}

	if hp.useDify && svcCtx.Config.Dify.APIKey != "" {
		hp.difyClient = dify.NewClient(svcCtx.Config.Dify.BaseURL, svcCtx.Config.Dify.APIKey)
		log.Println("Using Dify for AI processing")
	}

	// 始终初始化原生 LLM 作为备用
	if svcCtx.Config.LLM.APIKey != "" {
		// 创建代理配置（如果有）
		var proxyConfig *llm.ProxyConfig
		if svcCtx.Config.LLM.ProxyHost != "" && svcCtx.Config.LLM.ProxyPort > 0 {
			proxyConfig = &llm.ProxyConfig{
				Host:     svcCtx.Config.LLM.ProxyHost,
				Port:     svcCtx.Config.LLM.ProxyPort,
				User:     svcCtx.Config.LLM.ProxyUser,
				Password: svcCtx.Config.LLM.ProxyPassword,
			}
			log.Printf("LLM proxy configured: %s:%d", proxyConfig.Host, proxyConfig.Port)
		}
		hp.llmClient = llm.NewClientWithProxy(
			svcCtx.Config.LLM.APIKey,
			svcCtx.Config.LLM.Endpoint,
			svcCtx.Config.LLM.Model,
			proxyConfig,
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
// chatID 是当前会话所在的群ID（群聊时）或用户ID（私聊时）
func (hp *HybridProcessor) ProcessQuery(ctx context.Context, chatID, query string) (string, error) {
	if hp.useDify && hp.difyClient != nil {
		return hp.processWithDify(ctx, chatID, query)
	}
	// 传递 chatID 以便搜索时限定范围
	return hp.processWithNativeLLM(ctx, chatID, query)
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
	hp.mu.RLock()
	conversationID := hp.conversationMap[userID]
	hp.mu.RUnlock()

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
			return hp.processWithNativeLLM(ctx, userID, query)
		}
		return "抱歉，AI 服务暂时不可用，请稍后重试。", nil
	}

	// 保存对话 ID 用于多轮对话
	if resp.ConversationID != "" {
		hp.mu.Lock()
		hp.conversationMap[userID] = resp.ConversationID
		hp.mu.Unlock()
	}

	return resp.Answer, nil
}

// ClearConversation 清除用户的对话历史
func (hp *HybridProcessor) ClearConversation(userID string) {
	hp.mu.Lock()
	delete(hp.conversationMap, userID)
	delete(hp.contextMap, userID)
	hp.mu.Unlock()
}

// isFollowUpQuestion 判断是否是追问（如"再看看"、"你再想想"）
func (hp *HybridProcessor) isFollowUpQuestion(query string) bool {
	followUpPatterns := []string{
		"再看看", "再想想", "再查查", "再找找", "再搜搜",
		"好好看看", "仔细看看", "认真看看", "再仔细",
		"不对", "不是", "错了", "重新", "再来",
		"没有回答", "没回答", "答非所问",
		"详细点", "具体点", "说清楚", "再说一遍",
		"我意思是", "我的意思", "我问的是",
		// 简短请求类追问
		"给我个", "给一个", "来一个", "来个", "发一个", "发个",
		"那就给", "直接给", "就给我",
		// 补充追问模式（"xxx的没有吗"、"那xxx呢"、"xxx呢"）
		"没有吗", "有没有", "有吗", "呢?", "呢？",
		"那个呢", "这个呢", "其他的呢", "别的呢",
		"还有吗", "还有没有", "就这些吗", "只有这些",
	}

	queryLower := strings.ToLower(query)
	for _, pattern := range followUpPatterns {
		if strings.Contains(queryLower, pattern) {
			return true
		}
	}
	return false
}

// isLikelyFollowUp 判断是否可能是追问（结合上下文判断）
// 短查询 + 有上下文 + 查询内容与上次回答相关 → 很可能是追问
func (hp *HybridProcessor) isLikelyFollowUp(query string, prevContext *ConversationContext) bool {
	if prevContext == nil || prevContext.LastAnswer == "" {
		return false
	}

	// 短查询（小于15个字符）很可能是追问
	queryLen := len([]rune(query))
	if queryLen <= 15 {
		return true
	}

	// 查询中包含"的"且很短，很可能是追问（如"bx7的没有吗"）
	if queryLen <= 20 && strings.Contains(query, "的") {
		return true
	}

	return false
}

// getOrRestoreContext 获取或恢复对话上下文
// 如果是追问且有上下文，返回合并后的问题
func (hp *HybridProcessor) getOrRestoreContext(userID, query string) (string, *ConversationContext) {
	hp.mu.RLock()
	ctx, exists := hp.contextMap[userID]
	hp.mu.RUnlock()

	if !exists || ctx == nil {
		return query, nil
	}

	// 检查上下文是否过期（5分钟内有效）
	if time.Since(ctx.LastTimestamp) > 5*time.Minute {
		hp.mu.Lock()
		delete(hp.contextMap, userID)
		hp.mu.Unlock()
		return query, nil
	}

	// 如果当前问题是追问，合并上下文
	if hp.isFollowUpQuestion(query) {
		log.Printf("Detected follow-up question, restoring context from: %s", ctx.LastQuery)
		// 如果当前问题包含关键词（非纯追问语），提取并合并
		// 例如："我意思是你再好好看看场馆都是谁在对接" -> 提取 "场馆都是谁在对接"
		mergedQuery := hp.mergeWithContext(query, ctx)
		return mergedQuery, ctx
	}

	return query, ctx
}

// mergeWithContext 合并追问与上下文
func (hp *HybridProcessor) mergeWithContext(followUp string, ctx *ConversationContext) string {
	// 如果追问中包含实质性内容，提取出来
	// 例如："我意思是你再好好看看场馆都是谁在对接" -> "场馆都是谁在对接"

	// 移除常见的追问前缀
	cleanPatterns := []string{
		"我意思是", "我的意思是", "我问的是",
		"你再好好看看", "再好好看看", "好好看看",
		"再看看", "再想想", "再查查",
		// 简短请求类
		"给我个", "给一个", "来一个", "来个", "发一个", "发个",
		"那就给", "直接给", "就给我",
	}

	cleaned := followUp
	for _, p := range cleanPatterns {
		cleaned = strings.ReplaceAll(cleaned, p, "")
	}
	cleaned = strings.TrimSpace(cleaned)
	// 移除结尾的语气词
	cleaned = strings.TrimSuffix(cleaned, "吧")
	cleaned = strings.TrimSuffix(cleaned, "呗")
	cleaned = strings.TrimSuffix(cleaned, "啊")
	cleaned = strings.TrimSpace(cleaned)

	// 如果清理后还有实质内容，使用清理后的内容
	if len(cleaned) > 2 {
		return cleaned
	}

	// 否则使用上一次的问题
	return ctx.LastQuery
}

// saveContext 保存对话上下文
func (hp *HybridProcessor) saveContext(userID string, query string, parsed *llm.ParsedQuery, chatID string) {
	hp.mu.Lock()
	hp.contextMap[userID] = &ConversationContext{
		LastQuery:     query,
		LastParsed:    parsed,
		LastChatID:    chatID,
		LastTimestamp: time.Now(),
	}
	hp.mu.Unlock()
}

// saveContextWithAnswer 保存对话上下文（包含回答）
func (hp *HybridProcessor) saveContextWithAnswer(userID string, query string, answer string, parsed *llm.ParsedQuery, chatID string) {
	hp.mu.Lock()
	hp.contextMap[userID] = &ConversationContext{
		LastQuery:     query,
		LastAnswer:    answer,
		LastParsed:    parsed,
		LastChatID:    chatID,
		LastTimestamp: time.Now(),
	}
	hp.mu.Unlock()
}

// processWithNativeLLM 使用原生 LLM 处理
// currentChatID 是当前会话所在的群ID，用于限定搜索范围
func (hp *HybridProcessor) processWithNativeLLM(ctx context.Context, currentChatID, query string) (string, error) {
	// 获取用户ID（私聊时用currentChatID，群聊时也用）
	userID := currentChatID

	// 检查是否是追问，尝试恢复上下文
	originalQuery := query
	restoredQuery, prevContext := hp.getOrRestoreContext(userID, query)

	// 判断是否是追问：明确的追问模式 或 可能的追问（短查询+有上下文）
	isFollowUp := hp.isFollowUpQuestion(originalQuery) || hp.isLikelyFollowUp(originalQuery, prevContext)

	// 如果是追问且有上一轮回答，直接让 LLM 从上一轮回答中提取信息
	if isFollowUp && prevContext != nil && prevContext.LastAnswer != "" {
		log.Printf("Follow-up question detected (query: %s), answering from previous context", originalQuery)
		answer, err := hp.answerFollowUpFromContext(ctx, originalQuery, prevContext)
		if err == nil && answer != "" {
			// 保存本次回答到上下文
			hp.saveContextWithAnswer(userID, originalQuery, answer, prevContext.LastParsed, prevContext.LastChatID)
			return answer, nil
		}
		log.Printf("Failed to answer from context: %v, falling back to normal processing", err)
	}

	if restoredQuery != query {
		log.Printf("Restored query from '%s' to '%s'", query, restoredQuery)
		query = restoredQuery
	}

	// 解析用户意图
	parsed, err := hp.llmClient.ParseUserQuery(ctx, query)
	if err != nil {
		log.Printf("Failed to parse query: %v", err)
		// 如果有上下文，尝试使用上一次的解析结果
		if prevContext != nil && prevContext.LastParsed != nil {
			log.Printf("Using previous parsed result due to LLM error")
			parsed = prevContext.LastParsed
			parsed.RawQuery = query // 更新原始查询
		} else {
			return "抱歉，我无法理解您的问题，请换个方式提问。", nil
		}
	}

	log.Printf("Parsed query: intent=%s, time_range=%s, users=%v, group=%s, currentChat=%s",
		parsed.Intent, parsed.TimeRange, parsed.TargetUsers, parsed.TargetGroup, currentChatID)

	// 根据意图处理，传递当前群ID
	var answer string
	switch parsed.Intent {
	case llm.IntentSiteQuery:
		answer, err = hp.handleSiteQueryByLLM(ctx, parsed)
	case llm.IntentGroupTimeline:
		answer, err = hp.handleGroupTimeline(ctx, parsed, currentChatID)
	case llm.IntentQueryWorkload, llm.IntentQueryCommits:
		answer, err = hp.handleWorkloadQuery(ctx, parsed)
	case llm.IntentSearchMessage:
		answer, err = hp.handleMessageSearch(ctx, parsed, currentChatID)
	case llm.IntentSummarize:
		answer, err = hp.handleSummarize(ctx, parsed, currentChatID)
	case llm.IntentQA:
		answer, err = hp.handleQA(ctx, parsed, currentChatID)
	case llm.IntentHelp:
		return hp.getHelpMessage(), nil
	default:
		// 对于未知意图，尝试作为问答处理
		answer, err = hp.handleQA(ctx, parsed, currentChatID)
	}

	// 保存对话上下文（包含回答，用于追问）
	if err == nil && answer != "" {
		chatID := hp.getSearchChatID(currentChatID, parsed.TargetGroup, ctx)
		hp.saveContextWithAnswer(userID, query, answer, parsed, chatID)
	}

	return answer, err
}

// answerFollowUpFromContext 从上一轮回答中提取信息回答追问
func (hp *HybridProcessor) answerFollowUpFromContext(ctx context.Context, followUpQuery string, prevContext *ConversationContext) (string, error) {
	if hp.llmClient == nil {
		return "", fmt.Errorf("LLM client not available")
	}

	// 限制上一轮回答的长度
	lastAnswer := prevContext.LastAnswer
	if len(lastAnswer) > 4000 {
		lastAnswer = lastAnswer[:4000] + "...(已截断)"
	}

	prompt := fmt.Sprintf(`用户之前问了："%s"

我的回答是：
%s

现在用户追问："%s"

请根据上面的回答内容，直接提取相关信息回答用户的追问。
如果上面的回答中没有相关信息，请明确说"上述回答中没有找到相关信息"。
回答要简洁直接。`, prevContext.LastQuery, lastAnswer, followUpQuery)

	return hp.llmClient.GenerateResponse(ctx, prompt, nil)
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
func (hp *HybridProcessor) handleMessageSearch(ctx context.Context, parsed *llm.ParsedQuery, currentChatID string) (string, error) {
	// 优先使用 RAG 语义搜索
	if hp.svcCtx.Services.RAG != nil && hp.svcCtx.Services.RAG.IsEnabled() {
		return hp.handleSemanticSearch(ctx, parsed, currentChatID)
	}

	// 降级到传统关键词搜索
	return hp.handleKeywordSearch(ctx, parsed, currentChatID)
}

// handleSemanticSearch 语义搜索（RAG）- 使用混合搜索
func (hp *HybridProcessor) handleSemanticSearch(ctx context.Context, parsed *llm.ParsedQuery, currentChatID string) (string, error) {
	// 构建搜索查询
	query := parsed.RawQuery
	if len(parsed.Keywords) > 0 {
		query = strings.Join(parsed.Keywords, " ")
	}

	// 确定搜索范围：私聊时搜索所有群，群聊时限定当前群
	chatID := hp.getSearchChatID(currentChatID, parsed.TargetGroup, ctx)

	// 构建混合搜索选项
	hybridOpts := service.DefaultHybridSearchOptions()
	hybridOpts.ChatID = chatID
	hybridOpts.Keywords = parsed.Keywords

	// 添加用户过滤
	if len(parsed.TargetUsers) > 0 {
		hybridOpts.SenderName = parsed.TargetUsers[0] // 使用第一个目标用户
		log.Printf("Hybrid search with user filter: %s", hybridOpts.SenderName)
	}

	// 添加时间范围过滤
	startTime, endTime := hp.getTimeRange(parsed.TimeRange)
	hybridOpts.StartTime = &startTime
	hybridOpts.EndTime = &endTime
	log.Printf("Hybrid search time range: %s ~ %s", startTime.Format("2006-01-02 15:04"), endTime.Format("2006-01-02 15:04"))

	// 执行混合搜索（语义 + 关键词融合 + 同义词扩展 + 动态 top-k）
	results, err := hp.svcCtx.Services.RAG.HybridSearch(ctx, query, parsed.Keywords, 15, hybridOpts)
	if err != nil {
		log.Printf("Hybrid search failed: %v, falling back to keyword search", err)
		return hp.handleKeywordSearch(ctx, parsed, currentChatID)
	}

	if len(results) == 0 {
		// 如果带过滤条件没找到，尝试放宽条件重新搜索
		if hybridOpts.SenderName != "" || hybridOpts.StartTime != nil {
			log.Printf("No results with filters, trying without time filter")
			hybridOpts.StartTime = nil
			hybridOpts.EndTime = nil
			results, err = hp.svcCtx.Services.RAG.HybridSearch(ctx, query, parsed.Keywords, 15, hybridOpts)
			if err != nil || len(results) == 0 {
				return "没有找到相关的消息。", nil
			}
		} else {
			return "没有找到相关的消息。", nil
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🔍 混合搜索找到 %d 条相关消息:\n\n", len(results)))

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
func (hp *HybridProcessor) handleKeywordSearch(ctx context.Context, parsed *llm.ParsedQuery, currentChatID string) (string, error) {
	var messages []*model.ChatMessage
	var err error

	// 确定搜索范围：私聊时搜索所有群，群聊时限定当前群
	chatID := hp.getSearchChatID(currentChatID, parsed.TargetGroup, ctx)

	if len(parsed.Keywords) > 0 {
		keyword := strings.Join(parsed.Keywords, " ")
		messages, err = hp.svcCtx.MessageModel.SearchByContent(ctx, chatID, keyword, 20)
	} else if len(parsed.TargetUsers) > 0 {
		for _, user := range parsed.TargetUsers {
			userMsgs, searchErr := hp.svcCtx.MessageModel.SearchBySender(ctx, chatID, user, "", 20)
			if searchErr == nil {
				messages = append(messages, userMsgs...)
			}
		}
	} else {
		startTime, endTime := hp.getTimeRange(parsed.TimeRange)
		messages, err = hp.svcCtx.MessageModel.GetMessagesByDateRange(ctx, chatID, startTime, endTime, 50)
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
func (hp *HybridProcessor) handleSummarize(ctx context.Context, parsed *llm.ParsedQuery, currentChatID string) (string, error) {
	startTime, endTime := hp.getTimeRange(parsed.TimeRange)

	// 确定搜索范围
	var chatID string
	var groupName string

	if parsed.TargetGroup != "" {
		// 用户指定了目标群
		log.Printf("Looking for group: %s", parsed.TargetGroup)
		foundID, foundName := hp.findChatByName(ctx, parsed.TargetGroup)
		if foundID == "" {
			availableGroups := hp.listAvailableGroups(ctx)
			return fmt.Sprintf("❌ 未找到群「%s」\n\n可用的群：\n%s\n\n💡 请使用准确的群名，或发送「列出群聊」查看所有群。",
				parsed.TargetGroup, availableGroups), nil
		}
		chatID = foundID
		groupName = foundName
		log.Printf("Found group: %s (chat_id: %s)", groupName, chatID)
	} else if isPrivateChat(currentChatID) {
		// 私聊时，总结所有群（chatID 为空）
		chatID = ""
		groupName = "所有群"
	} else {
		// 群聊时，总结当前群
		chatID = currentChatID
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

// handleQA 处理基于聊天记录的问答
func (hp *HybridProcessor) handleQA(ctx context.Context, parsed *llm.ParsedQuery, currentChatID string) (string, error) {
	query := parsed.RawQuery

	// 确定搜索范围：私聊时搜索所有群，群聊时限定当前群
	chatID := hp.getSearchChatID(currentChatID, parsed.TargetGroup, ctx)
	log.Printf("handleQA: using chatID=%s (current=%s, target=%s, isPrivate=%v)",
		chatID, currentChatID, parsed.TargetGroup, isPrivateChat(currentChatID))

	// 获取时间范围（如果用户指定了时间）
	startTime, endTime := hp.getTimeRange(parsed.TimeRange)
	hasTimeFilter := parsed.TimeRange != "" && parsed.TimeRange != llm.TimeRangeCustom
	if hasTimeFilter {
		log.Printf("handleQA: time filter %s ~ %s", startTime.Format("2006-01-02"), endTime.Format("2006-01-02"))
	}

	// 检测是否是询问人员角色的问题（如"后端是谁"、"产品是谁"）
	if hp.isRoleQuery(query) {
		return hp.handleRoleQuery(ctx, query, chatID)
	}

	// 检测是否是询问某人做了什么的问题
	if hp.isPersonActivityQuery(query) {
		return hp.handlePersonActivityQuery(ctx, parsed, chatID)
	}

	// 提取搜索关键词
	keywords := hp.extractSearchKeywords(query, parsed.Keywords)
	log.Printf("QA search keywords: %v", keywords)

	// 使用混合搜索：同时使用 RAG 语义搜索和关键词搜索
	messageMap := make(map[string]string) // 用于去重 (content -> formatted message)

	// 1. 先尝试关键词搜索（更精确）
	for _, kw := range keywords {
		if len(kw) < 2 {
			continue // 跳过太短的关键词
		}
		messages, err := hp.svcCtx.MessageModel.SearchByContent(ctx, chatID, kw, 30)
		if err == nil {
			for _, msg := range messages {
				if msg.Content.Valid {
					// 如果有时间过滤，检查消息是否在时间范围内
					if hasTimeFilter && (msg.CreatedAt.Before(startTime) || msg.CreatedAt.After(endTime)) {
						continue
					}
					// 获取发送者名称，如果为空则显示"系统/机器人"
					senderName := "系统/机器人"
					if msg.SenderName.Valid && msg.SenderName.String != "" {
						senderName = msg.SenderName.String
					}
					formatted := fmt.Sprintf("[%s] %s: %s",
						msg.CreatedAt.Format("01-02 15:04"),
						senderName,
						msg.Content.String)
					messageMap[msg.Content.String] = formatted
				}
			}
		}
		log.Printf("Keyword search '%s': found %d messages", kw, len(messages))
	}

	// 2. 使用混合搜索补充（语义 + 关键词融合 + 同义词扩展）
	if hp.svcCtx.Services.RAG != nil && hp.svcCtx.Services.RAG.IsEnabled() {
		searchQuery := query
		if len(keywords) > 0 {
			searchQuery = strings.Join(keywords, " ")
		}

		// 构建混合搜索选项
		hybridOpts := service.DefaultHybridSearchOptions()
		hybridOpts.ChatID = chatID
		hybridOpts.Keywords = keywords
		if hasTimeFilter {
			hybridOpts.StartTime = &startTime
			hybridOpts.EndTime = &endTime
		}

		results, err := hp.svcCtx.Services.RAG.HybridSearch(ctx, searchQuery, keywords, 30, hybridOpts)
		if err != nil {
			log.Printf("Hybrid search failed: %v", err)
		} else {
			log.Printf("Hybrid search found %d results", len(results))
			for _, r := range results {
				if _, exists := messageMap[r.Content]; !exists {
					formatted := fmt.Sprintf("[%s] %s: %s",
						r.CreatedAt.Format("01-02 15:04"),
						r.SenderName,
						r.Content)
					messageMap[r.Content] = formatted
				}
			}
		}
	}

	// 转换为列表
	var relevantMessages []string
	for _, msg := range messageMap {
		relevantMessages = append(relevantMessages, msg)
	}

	log.Printf("Total unique messages found: %d", len(relevantMessages))

	if len(relevantMessages) == 0 {
		return "抱歉，我在聊天记录中没有找到与您问题相关的信息。您可以尝试：\n• 换个关键词提问\n• 指定具体的群名\n• 使用「搜索 XXX」查找相关消息", nil
	}

	// 使用 LLM 根据找到的消息回答问题
	context := strings.Join(relevantMessages, "\n")
	if len(context) > 8000 {
		context = context[:8000] + "...(内容已截断)"
	}

	answer, err := hp.answerWithContext(ctx, parsed.RawQuery, context)
	if err != nil {
		log.Printf("Failed to generate answer: %v", err)
		// LLM 失败时，尝试提供一个简单的本地分析
		return hp.generateLocalAnswer(parsed.RawQuery, relevantMessages), nil
	}

	return answer, nil
}

// generateLocalAnswer 当 LLM 不可用时，生成本地回答
func (hp *HybridProcessor) generateLocalAnswer(query string, messages []string) string {
	if len(messages) == 0 {
		return "抱歉，没有找到相关信息。"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📝 找到 %d 条相关消息，以下是关键内容：\n\n", len(messages)))

	// 显示最多10条消息
	displayCount := len(messages)
	if displayCount > 10 {
		displayCount = 10
	}

	for i := 0; i < displayCount; i++ {
		msg := messages[i]
		// 截断过长的消息
		if len(msg) > 150 {
			msg = msg[:147] + "..."
		}
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, msg))
	}

	if len(messages) > 10 {
		sb.WriteString(fmt.Sprintf("\n...(还有 %d 条消息未显示)\n", len(messages)-10))
	}

	sb.WriteString("\n💡 提示：AI 服务暂时繁忙，以上是原始消息记录。请稍后重试获取智能分析。")
	return sb.String()
}

// extractSearchKeywords 从问题中提取搜索关键词
func (hp *HybridProcessor) extractSearchKeywords(query string, parsedKeywords []string) []string {
	keywords := make([]string, 0)
	seen := make(map[string]bool)

	// 使用 LLM 解析的关键词（过滤掉无意义的追问词）
	for _, kw := range parsedKeywords {
		kwLower := strings.ToLower(kw)
		if !seen[kwLower] && len(kw) >= 2 && !hp.isStopWord(kw) {
			keywords = append(keywords, kw)
			seen[kwLower] = true
		}
	}

	// 从问题中提取名词/专业术语（简单的规则）
	// 移除常见疑问词和助词
	stopWords := []string{
		"是谁", "是什么", "怎么", "如何", "为什么", "哪个", "哪些",
		"做的", "做了", "开发的", "负责", "在做", "什么",
		"请问", "问一下", "想知道", "告诉我",
		"这个", "那个", "的", "了", "吗", "呢", "啊",
		// 追问相关停用词
		"再看看", "好好看看", "再想想", "再查查",
		"我意思是", "我的意思", "我问的是",
		"你再", "再好好", "仔细", "认真",
	}

	cleanQuery := query
	for _, sw := range stopWords {
		cleanQuery = strings.ReplaceAll(cleanQuery, sw, " ")
	}

	// 分词（简单按空格和标点分割）
	words := strings.FieldsFunc(cleanQuery, func(r rune) bool {
		return r == ' ' || r == '，' || r == '。' || r == '？' || r == '！' || r == '、'
	})

	for _, w := range words {
		w = strings.TrimSpace(w)
		wLower := strings.ToLower(w)
		if len(w) >= 2 && !seen[wLower] && !hp.isStopWord(w) {
			keywords = append(keywords, w)
			seen[wLower] = true
		}
	}

	return keywords
}

// isStopWord 判断是否是停用词（不应该作为搜索关键词）
func (hp *HybridProcessor) isStopWord(word string) bool {
	stopWords := map[string]bool{
		// 追问相关
		"再看看": true, "好好看看": true, "再想想": true, "再查查": true,
		"我意思是": true, "我的意思": true, "我问的是": true,
		"你再": true, "再好好": true, "仔细": true, "认真": true,
		"再": true, "好好": true, "看看": true, "想想": true,
		// 常见无意义词
		"你": true, "我": true, "他": true, "她": true, "它": true,
		"是": true, "的": true, "了": true, "吗": true, "呢": true,
		"啊": true, "哦": true, "嗯": true,
		"什么": true, "哪个": true, "哪些": true, "怎么": true,
		"请问": true, "告诉": true, "说说": true,
		"都": true, "在": true, "有": true, "没有": true,
		// 英文停用词
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"what": true, "who": true, "how": true, "why": true,
	}
	return stopWords[strings.ToLower(word)]
}

// isRoleQuery 检测是否是询问人员角色的问题
// 例如："后端是谁"、"产品经理有哪些人"
// 但不匹配："归集是谁做的"、"XX功能是谁做的"
func (hp *HybridProcessor) isRoleQuery(query string) bool {
	// 排除模式：如果问的是"XXX是谁做的"，这不是角色查询
	excludePatterns := []string{"是谁做的", "谁做的", "谁开发的", "谁写的", "谁负责的", "谁实现的"}
	for _, p := range excludePatterns {
		if strings.Contains(query, p) {
			return false
		}
	}

	// 角色关键词（必须出现）
	roleKeywords := []string{"后端", "前端", "产品", "测试", "运维", "设计", "客户端", "ios", "android"}
	// 疑问词
	questionWords := []string{"是谁", "有谁", "哪些人", "谁是", "有哪些"}

	queryLower := strings.ToLower(query)
	hasRole := false
	hasQuestion := false

	for _, r := range roleKeywords {
		if strings.Contains(queryLower, r) {
			hasRole = true
			break
		}
	}

	for _, q := range questionWords {
		if strings.Contains(query, q) {
			hasQuestion = true
			break
		}
	}

	// 必须同时包含角色关键词和疑问词
	return hasRole && hasQuestion
}

// handleRoleQuery 处理人员角色查询
func (hp *HybridProcessor) handleRoleQuery(ctx context.Context, query, chatID string) (string, error) {
	// 从 sender_name 中提取角色信息
	senderNames, err := hp.svcCtx.MessageModel.GetDistinctSenders(ctx, chatID)
	if err != nil {
		log.Printf("Failed to get distinct senders: %v", err)
		return "获取人员信息失败，请稍后重试。", nil
	}

	if len(senderNames) == 0 {
		return "暂无人员信息。", nil
	}

	// 按角色分类
	roleMap := make(map[string][]string)
	for _, name := range senderNames {
		role := hp.extractRole(name)
		if role != "" {
			roleMap[role] = append(roleMap[role], name)
		}
	}

	// 检测用户问的是哪个角色
	queryLower := strings.ToLower(query)
	var targetRole string
	roleKeywords := map[string][]string{
		"后端":  {"后端", "backend", "服务端"},
		"前端":  {"前端", "frontend", "web"},
		"产品":  {"产品", "pm", "product"},
		"测试":  {"测试", "qa", "test"},
		"运维":  {"运维", "ops", "devops"},
		"设计":  {"设计", "ui", "ux"},
		"客户端": {"客户端", "ios", "android", "mobile"},
	}

	for role, keywords := range roleKeywords {
		for _, kw := range keywords {
			if strings.Contains(queryLower, kw) {
				targetRole = role
				break
			}
		}
		if targetRole != "" {
			break
		}
	}

	var sb strings.Builder
	if targetRole != "" {
		// 回答特定角色
		members := roleMap[targetRole]
		if len(members) == 0 {
			return fmt.Sprintf("聊天记录中没有找到%s相关人员。", targetRole), nil
		}
		sb.WriteString(fmt.Sprintf("👥 %s人员（%d人）：\n", targetRole, len(members)))
		for _, m := range members {
			sb.WriteString(fmt.Sprintf("• %s\n", m))
		}
	} else {
		// 列出所有角色
		sb.WriteString("👥 群成员角色分布：\n\n")
		for role, members := range roleMap {
			sb.WriteString(fmt.Sprintf("**%s**（%d人）：%s\n", role, len(members), strings.Join(members, "、")))
		}
		if len(roleMap) == 0 {
			sb.WriteString("群成员：" + strings.Join(senderNames, "、"))
		}
	}

	return sb.String(), nil
}

// extractRole 从名字中提取角色
func (hp *HybridProcessor) extractRole(name string) string {
	nameLower := strings.ToLower(name)
	rolePatterns := map[string][]string{
		"后端":  {"后端", "backend", "服务端", "server"},
		"前端":  {"前端", "frontend", "web"},
		"产品":  {"产品", "pm", "product"},
		"测试":  {"测试", "qa", "test"},
		"运维":  {"运维", "ops", "devops", "sre"},
		"设计":  {"设计", "ui", "ux", "design"},
		"客户端": {"客户端", "ios", "android", "mobile"},
	}

	for role, patterns := range rolePatterns {
		for _, p := range patterns {
			if strings.Contains(nameLower, p) {
				return role
			}
		}
	}
	return "其他"
}

// isPersonActivityQuery 检测是否是询问某人做了什么
func (hp *HybridProcessor) isPersonActivityQuery(query string) bool {
	activityPatterns := []string{"做了什么", "干了什么", "做什么", "在做什么", "负责什么", "做了啥"}
	for _, p := range activityPatterns {
		if strings.Contains(query, p) {
			return true
		}
	}
	return false
}

// handlePersonActivityQuery 处理某人做了什么的查询
func (hp *HybridProcessor) handlePersonActivityQuery(ctx context.Context, parsed *llm.ParsedQuery, chatID string) (string, error) {
	// 提取人名
	personName := ""
	for _, user := range parsed.TargetUsers {
		personName = user
		break
	}

	// 从问题中提取人名
	if personName == "" {
		// 尝试从关键词中找
		for _, kw := range parsed.Keywords {
			if !strings.Contains(kw, "做") && !strings.Contains(kw, "什么") {
				personName = kw
				break
			}
		}
	}

	if personName == "" {
		return "请指定您想查询的人员名称。", nil
	}

	// 搜索这个人发的消息
	messages, err := hp.svcCtx.MessageModel.SearchBySender(ctx, chatID, personName, "", 50)
	if err != nil {
		log.Printf("Failed to search messages by sender: %v", err)
		return "查询失败，请稍后重试。", nil
	}

	if len(messages) == 0 {
		return fmt.Sprintf("没有找到 %s 的消息记录。", personName), nil
	}

	// 构建消息上下文
	var msgTexts []string
	for _, msg := range messages {
		if msg.Content.Valid && msg.SenderName.Valid {
			msgTexts = append(msgTexts,
				fmt.Sprintf("[%s] %s: %s",
					msg.CreatedAt.Format("01-02 15:04"),
					msg.SenderName.String,
					msg.Content.String))
		}
	}

	context := strings.Join(msgTexts, "\n")
	if len(context) > 8000 {
		context = context[:8000] + "..."
	}

	// 让 LLM 总结这个人做了什么
	prompt := fmt.Sprintf(`请根据以下聊天记录，总结 %s 做了什么工作。

聊天记录：
%s

要求：
1. 列出这个人参与的主要工作/任务
2. 如果有具体的功能、Bug修复、讨论等，请具体说明
3. 按重要性或时间排序
4. 简洁明了，重点突出`, personName, context)

	return hp.llmClient.GenerateResponse(ctx, prompt, nil)
}

// answerWithContext 根据上下文回答问题
func (hp *HybridProcessor) answerWithContext(ctx context.Context, question, context string) (string, error) {
	if hp.llmClient == nil {
		return "", fmt.Errorf("LLM client not available")
	}

	prompt := fmt.Sprintf(`根据聊天记录回答问题，要求精简。

问题：%s

记录：
%s

约束：
1. 只从记录提取，不编造
2. 按频次/重要性列出要点（如问题统计：列出问题类型+次数）
3. 无相关信息则说明"未找到"
4. 格式：标题+要点列表，不要长段落`, question, context)

	return hp.llmClient.GenerateResponse(ctx, prompt, nil)
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

// isPrivateChat 判断是否是私聊
// 群聊 chat_id 以 "oc_" 开头，私聊时传入的是用户 open_id (以 "ou_" 开头)
func isPrivateChat(chatID string) bool {
	return !strings.HasPrefix(chatID, "oc_")
}

// getSearchChatID 获取搜索时使用的 chatID
// 私聊时返回空字符串（搜索所有群），群聊时返回当前群ID
func (hp *HybridProcessor) getSearchChatID(currentChatID string, targetGroup string, ctx context.Context) string {
	// 如果用户指定了目标群，优先使用
	if targetGroup != "" {
		if foundID, _ := hp.findChatByName(ctx, targetGroup); foundID != "" {
			return foundID
		}
	}

	// 私聊时，搜索所有群（返回空字符串）
	if isPrivateChat(currentChatID) {
		return ""
	}

	// 群聊时，限定在当前群
	return currentChatID
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
		// 默认查询本周的消息（更合理的默认值）
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		weekStart := today.AddDate(0, 0, -(weekday - 1))
		return weekStart, now
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

// ======================== Bitable 站点查询 ========================

// handleSiteQueryByLLM 处理 LLM 识别的站点查询
func (hp *HybridProcessor) handleSiteQueryByLLM(ctx context.Context, parsed *llm.ParsedQuery) (string, error) {
	sitePrefix := strings.ToLower(parsed.SitePrefix)
	siteID := strings.TrimSpace(parsed.SiteID)

	// 优先使用站点前缀查询
	if sitePrefix != "" {
		log.Printf("Handling site query by prefix: %s", sitePrefix)
		if !hp.svcCtx.Config.Bitable.Enabled {
			return "站点信息查询功能未启用。", nil
		}
		return hp.handleSiteQuery(ctx, parsed.RawQuery, sitePrefix)
	}

	// 如果有站点ID，通过ID反查
	if siteID != "" {
		log.Printf("Handling site query by ID: %s", siteID)
		if !hp.svcCtx.Config.Bitable.Enabled {
			return "站点信息查询功能未启用。", nil
		}
		return hp.handleSiteQueryByID(ctx, parsed.RawQuery, siteID)
	}

	// 如果都没有，回退到聊天记录搜索
	log.Printf("Site query detected but no prefix/ID extracted, falling back to QA")
	return hp.handleQA(ctx, parsed, "")
}

// handleSiteQueryByID 通过站点ID查询站点信息
func (hp *HybridProcessor) handleSiteQueryByID(ctx context.Context, query, siteID string) (string, error) {
	appToken := hp.svcCtx.Config.Bitable.AppToken
	tableID := hp.svcCtx.Config.Bitable.TableID

	if appToken == "" || tableID == "" {
		log.Printf("Bitable config missing: appToken=%s, tableID=%s", appToken, tableID)
		return "", nil
	}

	// 通过站点ID查询
	record, err := hp.svcCtx.LarkClient.GetSiteInfoBySiteID(ctx, appToken, tableID, siteID)
	if err != nil {
		log.Printf("Failed to query site info by ID %s: %v", siteID, err)
		return "", err
	}

	if record == nil {
		return fmt.Sprintf("未找到站点ID为「%s」的站点信息。", siteID), nil
	}

	// 获取站点前缀用于显示
	prefix := getFieldString(record.Fields, "站点前缀")
	if prefix == "" {
		prefix = siteID
	}

	// 格式化站点信息
	return hp.formatSiteInfo(record, prefix), nil
}

// handleSiteQuery 处理站点信息查询
func (hp *HybridProcessor) handleSiteQuery(ctx context.Context, query, sitePrefix string) (string, error) {
	appToken := hp.svcCtx.Config.Bitable.AppToken
	tableID := hp.svcCtx.Config.Bitable.TableID

	if appToken == "" || tableID == "" {
		log.Printf("Bitable config missing: appToken=%s, tableID=%s", appToken, tableID)
		return "", nil
	}

	// 查询站点信息
	record, err := hp.svcCtx.LarkClient.GetSiteInfoByPrefix(ctx, appToken, tableID, sitePrefix)
	if err != nil {
		log.Printf("Failed to query site info for %s: %v", sitePrefix, err)
		return "", err
	}

	if record == nil {
		return fmt.Sprintf("未找到站点前缀为「%s」的站点信息。", sitePrefix), nil
	}

	// 格式化站点信息
	return hp.formatSiteInfo(record, sitePrefix), nil
}

// formatSiteInfo 格式化站点信息
func (hp *HybridProcessor) formatSiteInfo(record *lark.BitableRecord, prefix string) string {
	fields := record.Fields

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📍 站点「%s」信息：\n\n", strings.ToUpper(prefix)))

	// 站点ID
	if siteID := getFieldString(fields, "站点ID"); siteID != "" {
		sb.WriteString(fmt.Sprintf("• 站点ID: %s\n", siteID))
	}

	// 站点前缀
	if sitePrefix := getFieldString(fields, "站点前缀"); sitePrefix != "" {
		sb.WriteString(fmt.Sprintf("• 站点前缀: %s\n", sitePrefix))
	}

	// 国家
	if country := getFieldString(fields, "国家"); country != "" {
		sb.WriteString(fmt.Sprintf("• 国家: %s\n", country))
	}

	// 状态
	if status := getFieldString(fields, "状态"); status != "" {
		sb.WriteString(fmt.Sprintf("• 状态: %s\n", status))
	}

	// 前台域名（可能有多个，表格中有前台域名1-6）
	frontDomains := []string{}
	for i := 1; i <= 6; i++ {
		fieldName := fmt.Sprintf("前台域名%d", i)
		if domain := getFieldString(fields, fieldName); domain != "" {
			frontDomains = append(frontDomains, domain)
		}
	}
	// 还有多域名字段
	if multiDomain := getFieldString(fields, "多域名"); multiDomain != "" {
		frontDomains = append(frontDomains, multiDomain)
	}
	if len(frontDomains) > 0 {
		sb.WriteString(fmt.Sprintf("• 前台域名: %s\n", strings.Join(frontDomains, ", ")))
	}

	// 注意事项
	if note := getFieldString(fields, "注意"); note != "" {
		sb.WriteString(fmt.Sprintf("• 注意: %s\n", note))
	}

	return sb.String()
}

// getFieldString 从字段中获取字符串值
func getFieldString(fields map[string]interface{}, key string) string {
	if v, ok := fields[key]; ok {
		switch val := v.(type) {
		case string:
			return val
		case []interface{}:
			// 多选字段返回第一个值
			if len(val) > 0 {
				if s, ok := val[0].(string); ok {
					return s
				}
				// 可能是 map 结构（如链接字段）
				if m, ok := val[0].(map[string]interface{}); ok {
					if text, ok := m["text"].(string); ok {
						return text
					}
					if link, ok := m["link"].(string); ok {
						return link
					}
				}
			}
		case map[string]interface{}:
			// 单个链接或复杂字段
			if text, ok := val["text"].(string); ok {
				return text
			}
		case float64:
			return fmt.Sprintf("%.0f", val)
		}
	}
	return ""
}

// ======================== 群历程查询 ========================

// WeeklySummary 周总结数据
type WeeklySummary struct {
	WeekStart    time.Time `json:"week_start"`
	WeekEnd      time.Time `json:"week_end"`
	Summary      string    `json:"summary"`
	MainTopics   []string  `json:"main_topics"`
	Decisions    []string  `json:"decisions"`
	Milestones   []string  `json:"milestones"`
	Participants []string  `json:"participants"`
	MessageCount int       `json:"message_count"`
}

// TimelineReport 时间线报告
type TimelineReport struct {
	GroupName       string          `json:"group_name"`
	StartDate       time.Time       `json:"start_date"`
	EndDate         time.Time       `json:"end_date"`
	TotalWeeks      int             `json:"total_weeks"`
	TotalMessages   int             `json:"total_messages"`
	WeeklySummaries []WeeklySummary `json:"weekly_summaries"`
}

// handleGroupTimeline 处理群历程查询
func (hp *HybridProcessor) handleGroupTimeline(ctx context.Context, parsed *llm.ParsedQuery, currentChatID string) (string, error) {
	// 1. 确定目标群
	chatID, groupName := hp.resolveTargetGroup(ctx, currentChatID, parsed.TargetGroup)
	if chatID == "" {
		return "请指定要查询历程的群，或在群聊中直接提问。", nil
	}

	log.Printf("Processing group timeline for: %s (chatID: %s)", groupName, chatID)

	// 2. 获取群的第一条消息，确定时间范围
	firstMsg, err := hp.svcCtx.MessageModel.GetGroupFirstMessage(ctx, chatID)
	if err != nil {
		log.Printf("Failed to get first message: %v", err)
		return fmt.Sprintf("「%s」群暂无消息记录。", groupName), nil
	}

	startDate := firstMsg.CreatedAt
	endDate := time.Now()

	// 3. 计算周数
	weekCount := int(endDate.Sub(startDate).Hours()/24/7) + 1
	log.Printf("Timeline spans %d weeks (from %s to %s)", weekCount, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))

	// 4. 分周处理并生成总结
	weeklySummaries, err := hp.generateWeeklySummaries(ctx, chatID, startDate, endDate)
	if err != nil {
		log.Printf("Failed to generate weekly summaries: %v", err)
		return "生成历程总结时出错，请稍后重试。", err
	}

	if len(weeklySummaries) == 0 {
		return fmt.Sprintf("「%s」群暂无足够的消息来生成历程报告。", groupName), nil
	}

	// 5. 汇总所有周总结，生成最终报告
	report := TimelineReport{
		GroupName:       groupName,
		StartDate:       startDate,
		EndDate:         endDate,
		TotalWeeks:      len(weeklySummaries),
		WeeklySummaries: weeklySummaries,
	}

	// 计算总消息数
	for _, ws := range weeklySummaries {
		report.TotalMessages += ws.MessageCount
	}

	// 6. 使用LLM生成最终的历程报告
	finalReport, err := hp.generateFinalTimelineReport(ctx, parsed.RawQuery, report)
	if err != nil {
		log.Printf("Failed to generate final report: %v", err)
		// 降级：直接返回周总结列表
		return hp.formatWeeklySummariesFallback(report), nil
	}

	return finalReport, nil
}

// resolveTargetGroup 解析目标群
func (hp *HybridProcessor) resolveTargetGroup(ctx context.Context, currentChatID, targetGroup string) (chatID, groupName string) {
	// 优先使用用户指定的群
	if targetGroup != "" {
		foundID, foundName := hp.findChatByName(ctx, targetGroup)
		if foundID != "" {
			return foundID, foundName
		}
	}

	// 如果是群聊，使用当前群
	if !isPrivateChat(currentChatID) {
		// 获取群名
		group, err := hp.svcCtx.GroupModel.FindByChatID(ctx, currentChatID)
		if err == nil && group.ChatName.Valid {
			return currentChatID, group.ChatName.String
		}
		return currentChatID, "当前群"
	}

	return "", ""
}

// generateWeeklySummaries 分周生成总结
func (hp *HybridProcessor) generateWeeklySummaries(ctx context.Context, chatID string, startDate, endDate time.Time) ([]WeeklySummary, error) {
	var summaries []WeeklySummary

	// 计算每周的开始日期（周一）
	weekStart := startDate.Truncate(24 * time.Hour)
	// 调整到周一
	for weekStart.Weekday() != time.Monday {
		weekStart = weekStart.AddDate(0, 0, -1)
	}

	// 限制处理的最大周数（避免处理过多历史数据）
	maxWeeks := 52 // 最多处理52周
	processedWeeks := 0

	for weekStart.Before(endDate) && processedWeeks < maxWeeks {
		weekEnd := weekStart.AddDate(0, 0, 7)
		if weekEnd.After(endDate) {
			weekEnd = endDate
		}

		// 获取本周消息
		messages, err := hp.svcCtx.MessageModel.GetMessagesByDateRange(ctx, chatID, weekStart, weekEnd, 200)
		if err != nil {
			log.Printf("Failed to get messages for week %s: %v", weekStart.Format("2006-01-02"), err)
			weekStart = weekEnd
			processedWeeks++
			continue
		}

		// 跳过没有消息的周
		if len(messages) == 0 {
			weekStart = weekEnd
			processedWeeks++
			continue
		}

		// 获取本周参与者
		participants, _ := hp.svcCtx.MessageModel.GetDistinctSendersByDateRange(ctx, chatID, weekStart, weekEnd)

		// 生成本周总结
		weeklySummary, err := hp.summarizeWeekMessages(ctx, messages, weekStart, weekEnd)
		if err != nil {
			log.Printf("Failed to summarize week %s: %v", weekStart.Format("2006-01-02"), err)
			// 即使LLM失败，也记录基本信息
			weeklySummary = &WeeklySummary{
				WeekStart:    weekStart,
				WeekEnd:      weekEnd,
				Summary:      fmt.Sprintf("本周有 %d 条消息", len(messages)),
				Participants: participants,
				MessageCount: len(messages),
			}
		} else {
			weeklySummary.WeekStart = weekStart
			weeklySummary.WeekEnd = weekEnd
			weeklySummary.Participants = participants
			weeklySummary.MessageCount = len(messages)
		}

		summaries = append(summaries, *weeklySummary)
		log.Printf("Week %s: %d messages, summary generated", weekStart.Format("2006-01-02"), len(messages))

		weekStart = weekEnd
		processedWeeks++
	}

	return summaries, nil
}

// summarizeWeekMessages 总结单周消息
func (hp *HybridProcessor) summarizeWeekMessages(ctx context.Context, messages []*model.ChatMessage, weekStart, weekEnd time.Time) (*WeeklySummary, error) {
	if len(messages) == 0 {
		return nil, nil
	}

	// 限制消息数量，避免 Token 超限
	maxMessages := 100
	if len(messages) > maxMessages {
		// 均匀采样
		step := len(messages) / maxMessages
		var sampled []*model.ChatMessage
		for i := 0; i < len(messages); i += step {
			sampled = append(sampled, messages[i])
		}
		messages = sampled
	}

	// 格式化消息
	var msgTexts []string
	for _, msg := range messages {
		senderName := "未知"
		if msg.SenderName.Valid && msg.SenderName.String != "" {
			senderName = msg.SenderName.String
		}
		content := ""
		if msg.Content.Valid {
			content = msg.Content.String
		}
		if content == "" {
			continue
		}
		msgTexts = append(msgTexts, fmt.Sprintf("[%s] %s: %s",
			msg.CreatedAt.Format("01-02 15:04"),
			senderName,
			content))
	}

	if len(msgTexts) == 0 {
		return nil, nil
	}

	// 拼接消息文本，限制长度
	messageContent := strings.Join(msgTexts, "\n")
	if len(messageContent) > 6000 {
		messageContent = messageContent[:6000] + "\n...(内容已截断)"
	}

	// 调用 LLM 生成周总结
	return hp.generateWeeklySummaryWithLLM(ctx, messageContent, weekStart, weekEnd)
}

// generateWeeklySummaryWithLLM 使用 LLM 生成周总结
func (hp *HybridProcessor) generateWeeklySummaryWithLLM(ctx context.Context, messageContent string, weekStart, weekEnd time.Time) (*WeeklySummary, error) {
	prompt := fmt.Sprintf(`分析 %s 至 %s 这周的群聊消息，提取关键信息。

消息记录：
%s

返回JSON格式（确保有效JSON）：
{
    "summary": "本周概述（1-2句话）",
    "main_topics": ["主要话题1", "主要话题2"],
    "decisions": ["重要决议1", "重要决议2"],
    "milestones": ["里程碑事件（如有）"]
}

要求：
1. summary: 简明概括核心内容
2. main_topics: 最多5个主要话题
3. decisions: 明确的决议/结论，无则空数组
4. milestones: 重大事件（上线、发布等），无则空数组

只返回JSON:`,
		weekStart.Format("01月02日"),
		weekEnd.Format("01月02日"),
		messageContent)

	resp, err := hp.llmClient.GenerateResponse(ctx, prompt, nil)
	if err != nil {
		return nil, err
	}

	// 解析 JSON 响应
	resp = strings.TrimSpace(resp)
	resp = strings.TrimPrefix(resp, "```json")
	resp = strings.TrimPrefix(resp, "```")
	resp = strings.TrimSuffix(resp, "```")
	resp = strings.TrimSpace(resp)

	var result struct {
		Summary    string   `json:"summary"`
		MainTopics []string `json:"main_topics"`
		Decisions  []string `json:"decisions"`
		Milestones []string `json:"milestones"`
	}

	if err := json.Unmarshal([]byte(resp), &result); err != nil {
		// 解析失败时返回原始响应作为 summary
		return &WeeklySummary{
			Summary: resp,
		}, nil
	}

	return &WeeklySummary{
		Summary:    result.Summary,
		MainTopics: result.MainTopics,
		Decisions:  result.Decisions,
		Milestones: result.Milestones,
	}, nil
}

// generateFinalTimelineReport 生成最终的历程报告
func (hp *HybridProcessor) generateFinalTimelineReport(ctx context.Context, userQuery string, report TimelineReport) (string, error) {
	// 构建周总结摘要
	var weekSummaries []string
	for _, ws := range report.WeeklySummaries {
		weekInfo := fmt.Sprintf("【%s ~ %s】(%d条消息)\n概述: %s",
			ws.WeekStart.Format("2006-01-02"),
			ws.WeekEnd.Format("2006-01-02"),
			ws.MessageCount,
			ws.Summary)

		if len(ws.MainTopics) > 0 {
			weekInfo += fmt.Sprintf("\n主题: %s", strings.Join(ws.MainTopics, "、"))
		}
		if len(ws.Decisions) > 0 {
			weekInfo += fmt.Sprintf("\n决议: %s", strings.Join(ws.Decisions, "；"))
		}
		if len(ws.Milestones) > 0 {
			weekInfo += fmt.Sprintf("\n里程碑: %s", strings.Join(ws.Milestones, "；"))
		}
		if len(ws.Participants) > 0 && len(ws.Participants) <= 10 {
			weekInfo += fmt.Sprintf("\n参与者: %s", strings.Join(ws.Participants, "、"))
		} else if len(ws.Participants) > 10 {
			weekInfo += fmt.Sprintf("\n参与者: %d人", len(ws.Participants))
		}
		weekSummaries = append(weekSummaries, weekInfo)
	}

	summaryContent := strings.Join(weekSummaries, "\n\n")

	// 限制长度
	if len(summaryContent) > 8000 {
		summaryContent = summaryContent[:8000] + "\n...(内容已截断)"
	}

	prompt := fmt.Sprintf(`用户问题：%s

群聊信息：
- 群名: %s
- 起始时间: %s
- 结束时间: %s
- 总周数: %d 周
- 总消息数: %d 条

各周总结：
%s

请生成群历程报告。

格式要求：
1. 开头简述基本信息（起始时间、活跃周数）
2. 按时间线列出关键阶段/里程碑
3. 主要话题和演进
4. 重大决议汇总（如有）
5. 参与人员变化（如明显）
6. 整体评价

用清晰结构和标题，使用emoji增强可读性。`,
		userQuery,
		report.GroupName,
		report.StartDate.Format("2006年01月02日"),
		report.EndDate.Format("2006年01月02日"),
		report.TotalWeeks,
		report.TotalMessages,
		summaryContent)

	return hp.llmClient.GenerateResponse(ctx, prompt, nil)
}

// formatWeeklySummariesFallback 降级格式化（LLM失败时使用）
func (hp *HybridProcessor) formatWeeklySummariesFallback(report TimelineReport) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("📅 「%s」群历程报告\n\n", report.GroupName))
	sb.WriteString(fmt.Sprintf("📍 时间范围: %s ~ %s\n",
		report.StartDate.Format("2006-01-02"),
		report.EndDate.Format("2006-01-02")))
	sb.WriteString(fmt.Sprintf("📊 统计: %d 周，共 %d 条消息\n\n",
		report.TotalWeeks, report.TotalMessages))

	sb.WriteString("=== 各周概览 ===\n\n")

	for i, ws := range report.WeeklySummaries {
		sb.WriteString(fmt.Sprintf("**第 %d 周** (%s ~ %s)\n",
			i+1,
			ws.WeekStart.Format("01-02"),
			ws.WeekEnd.Format("01-02")))
		sb.WriteString(fmt.Sprintf("消息数: %d | 参与者: %d人\n",
			ws.MessageCount, len(ws.Participants)))
		if ws.Summary != "" {
			sb.WriteString(fmt.Sprintf("概述: %s\n", ws.Summary))
		}
		if len(ws.MainTopics) > 0 {
			sb.WriteString(fmt.Sprintf("话题: %s\n", strings.Join(ws.MainTopics, "、")))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
