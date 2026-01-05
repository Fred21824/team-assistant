package logic

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"team-assistant/internal/model"
	"team-assistant/internal/svc"
	"team-assistant/pkg/llm"
)

// QueryLogic 查询逻辑处理
type QueryLogic struct {
	svcCtx    *svc.ServiceContext
	llmClient *llm.Client
}

// NewQueryLogic 创建查询逻辑处理器
func NewQueryLogic(svcCtx *svc.ServiceContext, llmClient *llm.Client) *QueryLogic {
	return &QueryLogic{
		svcCtx:    svcCtx,
		llmClient: llmClient,
	}
}

// ProcessQuery 处理用户查询
func (l *QueryLogic) ProcessQuery(ctx context.Context, query string) (string, error) {
	// 解析用户意图
	parsed, err := l.llmClient.ParseUserQuery(ctx, query)
	if err != nil {
		log.Printf("Failed to parse query: %v", err)
		return "抱歉，我无法理解您的问题，请换个方式提问。", nil
	}

	log.Printf("Parsed query: intent=%s, time_range=%s, users=%v",
		parsed.Intent, parsed.TimeRange, parsed.TargetUsers)

	// 根据意图处理
	switch parsed.Intent {
	case llm.IntentQueryWorkload, llm.IntentQueryCommits:
		return l.handleWorkloadQuery(ctx, parsed)
	case llm.IntentSearchMessage:
		return l.handleMessageSearch(ctx, parsed)
	case llm.IntentSummarize:
		return l.handleSummarize(ctx, parsed)
	case llm.IntentHelp:
		return l.getHelpMessage(), nil
	default:
		return "抱歉，我暂时无法处理这个请求。您可以问我：\n• 某人的工作量\n• 代码提交记录\n• 搜索聊天内容\n• 总结群消息", nil
	}
}

// handleWorkloadQuery 处理工作量查询
func (l *QueryLogic) handleWorkloadQuery(ctx context.Context, parsed *llm.ParsedQuery) (string, error) {
	startTime, endTime := l.getTimeRange(parsed.TimeRange)

	var stats []*model.CommitStats
	var err error

	if len(parsed.TargetUsers) > 0 {
		// 查询特定用户
		for _, user := range parsed.TargetUsers {
			// 尝试通过成员表查找GitHub用户名
			members, findErr := l.svcCtx.MemberModel.FindByName(ctx, user)
			if findErr == nil && len(members) > 0 && members[0].GitHubUsername.Valid {
				userStats, statErr := l.svcCtx.CommitModel.GetStatsByMember(ctx, members[0].ID, startTime, endTime)
				if statErr == nil {
					stats = append(stats, userStats)
				}
			} else {
				// 直接按作者名查询
				userStats, statErr := l.svcCtx.CommitModel.GetStatsByAuthorName(ctx, user, startTime, endTime)
				if statErr == nil {
					stats = append(stats, userStats)
				}
			}
		}
	} else {
		// 查询所有人
		stats, err = l.svcCtx.CommitModel.GetAllStats(ctx, startTime, endTime)
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
	response, err := l.llmClient.GenerateResponse(ctx, parsed.RawQuery, stats)
	if err != nil {
		// 回退到简单格式
		return l.formatWorkloadStats(stats, startTime, endTime), nil
	}

	return response, nil
}

// formatWorkloadStats 格式化工作量统计
func (l *QueryLogic) formatWorkloadStats(stats []*model.CommitStats, start, end time.Time) string {
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
func (l *QueryLogic) handleMessageSearch(ctx context.Context, parsed *llm.ParsedQuery) (string, error) {
	startTime, endTime := l.getTimeRange(parsed.TimeRange)

	var messages []*model.ChatMessage
	var err error

	if len(parsed.Keywords) > 0 {
		keyword := strings.Join(parsed.Keywords, " ")
		messages, err = l.svcCtx.MessageModel.SearchByContent(ctx, "", keyword, 20)
	} else if len(parsed.TargetUsers) > 0 {
		// 按发送者搜索
		for _, user := range parsed.TargetUsers {
			userMsgs, searchErr := l.svcCtx.MessageModel.SearchBySender(ctx, "", user, "", 20)
			if searchErr == nil {
				messages = append(messages, userMsgs...)
			}
		}
	} else {
		messages, err = l.svcCtx.MessageModel.GetMessagesByDateRange(ctx, "", startTime, endTime, 50)
	}

	if err != nil {
		return "搜索消息失败，请稍后重试。", err
	}

	if len(messages) == 0 {
		return "没有找到匹配的消息。", nil
	}

	// 格式化消息
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
func (l *QueryLogic) handleSummarize(ctx context.Context, parsed *llm.ParsedQuery) (string, error) {
	startTime, endTime := l.getTimeRange(parsed.TimeRange)

	messages, err := l.svcCtx.MessageModel.GetMessagesByDateRange(ctx, "", startTime, endTime, 100)
	if err != nil {
		return "获取消息失败，请稍后重试。", err
	}

	if len(messages) == 0 {
		return "没有找到需要总结的消息。", nil
	}

	// 构建消息列表
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

	// 使用LLM总结
	summary, err := l.llmClient.SummarizeMessages(ctx, msgTexts)
	if err != nil {
		return "总结消息失败，请稍后重试。", err
	}

	return fmt.Sprintf("📋 消息总结 (%s ~ %s)\n\n%s",
		startTime.Format("01-02 15:04"),
		endTime.Format("01-02 15:04"),
		summary), nil
}

// getTimeRange 获取时间范围
func (l *QueryLogic) getTimeRange(tr llm.TimeRange) (time.Time, time.Time) {
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
func (l *QueryLogic) getHelpMessage() string {
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
