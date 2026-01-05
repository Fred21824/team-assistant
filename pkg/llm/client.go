package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Intent 用户意图类型
type Intent string

const (
	IntentQueryWorkload    Intent = "query_workload"    // 查询工作量
	IntentQueryCommits     Intent = "query_commits"     // 查询提交记录
	IntentSearchMessage    Intent = "search_message"    // 搜索消息
	IntentSummarize        Intent = "summarize"         // 总结内容
	IntentQueryRequirement Intent = "query_requirement" // 查询需求进度
	IntentHelp             Intent = "help"              // 帮助
	IntentUnknown          Intent = "unknown"           // 未知意图
)

// TimeRange 时间范围
type TimeRange string

const (
	TimeRangeToday     TimeRange = "today"
	TimeRangeYesterday TimeRange = "yesterday"
	TimeRangeThisWeek  TimeRange = "this_week"
	TimeRangeLastWeek  TimeRange = "last_week"
	TimeRangeThisMonth TimeRange = "this_month"
	TimeRangeLastMonth TimeRange = "last_month"
	TimeRangeCustom    TimeRange = "custom"
)

// ParsedQuery 解析后的查询
type ParsedQuery struct {
	Intent      Intent            `json:"intent"`
	TimeRange   TimeRange         `json:"time_range"`
	TargetUsers []string          `json:"target_users"` // 目标用户名
	TargetGroup string            `json:"target_group"` // 目标群名（如：印尼群、研发群）
	Keywords    []string          `json:"keywords"`     // 关键词
	Repository  string            `json:"repository"`   // 仓库名
	Params      map[string]string `json:"params"`       // 其他参数
	RawQuery    string            `json:"raw_query"`    // 原始查询
}

// Client LLM客户端
type Client struct {
	apiKey   string
	endpoint string
	model    string
	client   *http.Client
}

// NewClient 创建LLM客户端
func NewClient(apiKey, endpoint, model string) *Client {
	return &Client{
		apiKey:   apiKey,
		endpoint: endpoint,
		model:    model,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ChatRequest 聊天请求
type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	MaxTokens int          `json:"max_tokens,omitempty"`
}

// ChatMessage 聊天消息
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatResponse 聊天响应
type ChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// ParseUserQuery 解析用户查询意图
func (c *Client) ParseUserQuery(ctx context.Context, query string) (*ParsedQuery, error) {
	systemPrompt := `你是一个智能助手，负责解析用户的查询意图。
请分析用户的问题，返回JSON格式的解析结果。

支持的意图类型：
- query_workload: 查询工作量（如：小明这周干了多少活？）
- query_commits: 查询代码提交（如：今天谁提交了代码？）
- search_message: 搜索聊天消息（如：张三说过什么关于登录的问题？）
- summarize: 总结内容（如：总结一下今天群里的讨论、总结印尼群的消息）
- query_requirement: 查询需求进度（如：用户登录功能做到哪了？）
- help: 帮助信息
- unknown: 无法识别

时间范围：
- today: 今天
- yesterday: 昨天
- this_week: 本周
- last_week: 上周
- this_month: 本月
- last_month: 上个月
- custom: 自定义时间（默认最近7天）

重要：如果用户提到了某个群的名称（如"印尼群"、"研发群"、"测试群"等），请提取到 target_group 字段。

请严格返回以下JSON格式（不要包含其他内容）：
{
  "intent": "意图类型",
  "time_range": "时间范围",
  "target_users": ["用户名列表"],
  "target_group": "群名（如：印尼群、研发群，没有则为空字符串）",
  "keywords": ["关键词列表"],
  "repository": "仓库名（如有）",
  "params": {}
}`

	req := ChatRequest{
		Model: c.model,
		Messages: []ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: query},
		},
		MaxTokens: 500,
	}

	resp, err := c.chat(ctx, req)
	if err != nil {
		return nil, err
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no response from LLM")
	}

	content := resp.Choices[0].Message.Content
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var parsed ParsedQuery
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		// 如果解析失败，返回未知意图
		return &ParsedQuery{
			Intent:   IntentUnknown,
			RawQuery: query,
		}, nil
	}

	parsed.RawQuery = query
	return &parsed, nil
}

// GenerateResponse 生成回复
func (c *Client) GenerateResponse(ctx context.Context, prompt string, data interface{}) (string, error) {
	dataJSON, _ := json.MarshalIndent(data, "", "  ")

	userPrompt := fmt.Sprintf(`根据以下数据生成一个友好的中文回复：

用户问题：%s

数据：
%s

请用简洁、友好的语言回复用户的问题。`, prompt, string(dataJSON))

	req := ChatRequest{
		Model: c.model,
		Messages: []ChatMessage{
			{Role: "system", Content: "你是一个友好的团队助手，帮助团队成员了解工作情况。回复要简洁、专业。"},
			{Role: "user", Content: userPrompt},
		},
		MaxTokens: 1024,
	}

	resp, err := c.chat(ctx, req)
	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response from LLM")
	}

	return resp.Choices[0].Message.Content, nil
}

// SummarizeMessages 总结消息
func (c *Client) SummarizeMessages(ctx context.Context, messages []string) (string, error) {
	if len(messages) == 0 {
		return "没有找到需要总结的消息。", nil
	}

	content := strings.Join(messages, "\n")
	if len(content) > 8000 {
		content = content[:8000] + "...(内容已截断)"
	}

	systemPrompt := `你是一个专业的团队沟通内容分析助手。请对以下群聊消息进行全面、详细的总结分析。

要求：
1. **讨论主题**：列出所有讨论的主要话题，每个话题用1-2句话概括核心内容
2. **关键决策**：明确列出已经达成的决定或结论
3. **重要信息**：提取具体的数据、日期、金额、人名、技术方案等关键信息
4. **问题与阻碍**：列出提到的问题、Bug、困难或风险
5. **待办事项**：明确谁需要做什么，如果有明确责任人请标注
6. **进展更新**：如有提到任务进度或状态变化，请列出

格式要求：
- 使用清晰的分类标题
- 每个要点简洁但完整，包含必要的上下文
- 如果某个分类没有相关内容，可以省略该分类
- 保持客观，忠实原文内容，不要添加推测`

	req := ChatRequest{
		Model: c.model,
		Messages: []ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: fmt.Sprintf("请详细总结以下群聊消息：\n\n%s", content)},
		},
		MaxTokens: 2048,
	}

	resp, err := c.chat(ctx, req)
	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response from LLM")
	}

	return resp.Choices[0].Message.Content, nil
}

func (c *Client) chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("LLM error: HTTP %d - %s", resp.StatusCode, string(respBody))
	}

	if chatResp.Error != nil {
		return nil, fmt.Errorf("LLM error: %s", chatResp.Error.Message)
	}

	return &chatResp, nil
}
