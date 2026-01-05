package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
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
	IntentQA               Intent = "qa"                // 基于聊天记录的问答
	IntentSiteQuery        Intent = "site_query"        // 查询站点信息
	IntentGroupTimeline    Intent = "group_timeline"    // 群历程查询
	IntentHelp             Intent = "help"              // 帮助
	IntentUnknown          Intent = "unknown"           // 未知意图
)

// TimeRange 时间范围
type TimeRange string

const (
	TimeRangeToday       TimeRange = "today"
	TimeRangeYesterday   TimeRange = "yesterday"
	TimeRangeThisWeek    TimeRange = "this_week"
	TimeRangeLastWeek    TimeRange = "last_week"
	TimeRangeThisMonth   TimeRange = "this_month"
	TimeRangeLastMonth   TimeRange = "last_month"
	TimeRangeRecentMonth TimeRange = "recent_month" // 最近30天（不是上个自然月）
	TimeRangeCustom      TimeRange = "custom"
	TimeRangeAll         TimeRange = "all" // 全部历史（用于群历程查询）
)

// ParsedQuery 解析后的查询
type ParsedQuery struct {
	Intent      Intent            `json:"intent"`
	TimeRange   TimeRange         `json:"time_range"`
	TargetUsers []string          `json:"target_users"` // 目标用户名
	TargetGroup string            `json:"target_group"` // 目标群名（如：印尼群、研发群）
	Keywords    []string          `json:"keywords"`     // 关键词
	Repository  string            `json:"repository"`   // 仓库名
	SitePrefix  string            `json:"site_prefix"`  // 站点前缀（如：l08, b01）
	SiteID      string            `json:"site_id"`      // 站点ID（纯数字，如：3040）
	Params      map[string]string `json:"params"`       // 其他参数
	RawQuery    string            `json:"raw_query"`    // 原始查询
}

// ProxyConfig 代理配置
type ProxyConfig struct {
	Host     string
	Port     int
	User     string
	Password string
}

// VisionConfig 视觉模型配置
type VisionConfig struct {
	Model    string // 视觉模型名称
	Endpoint string // 视觉模型端点（可选，为空则使用主端点）
	APIKey   string // 视觉模型 API Key（可选，为空则使用主 API Key）
}

// Client LLM客户端
type Client struct {
	apiKey       string
	endpoint     string
	model        string
	provider     string // "openai", "anthropic", "groq"
	client       *http.Client
	visionConfig *VisionConfig // 视觉模型配置（可选）
}

// NewClient 创建LLM客户端
func NewClient(apiKey, endpoint, model string) *Client {
	return NewClientWithProxy(apiKey, endpoint, model, nil)
}

// NewClientWithProxy 创建带代理的LLM客户端
func NewClientWithProxy(apiKey, endpoint, model string, proxyConfig *ProxyConfig) *Client {
	// 自动检测 provider
	provider := "openai"
	if strings.Contains(endpoint, "anthropic.com") {
		provider = "anthropic"
	} else if strings.Contains(endpoint, "groq.com") {
		provider = "groq"
	}

	// 创建 HTTP 客户端
	httpClient := &http.Client{
		Timeout: 120 * time.Second, // Claude 可能需要更长时间，特别是通过代理
	}

	// 如果配置了代理，设置 HTTP 代理
	if proxyConfig != nil && proxyConfig.Host != "" && proxyConfig.Port > 0 {
		// 构建 HTTP 代理 URL
		var proxyURL string
		if proxyConfig.User != "" {
			if proxyConfig.Password != "" {
				proxyURL = fmt.Sprintf("http://%s:%s@%s:%d",
					url.QueryEscape(proxyConfig.User),
					url.QueryEscape(proxyConfig.Password),
					proxyConfig.Host,
					proxyConfig.Port)
			} else {
				// 只有用户名，没有密码
				proxyURL = fmt.Sprintf("http://%s@%s:%d",
					url.QueryEscape(proxyConfig.User),
					proxyConfig.Host,
					proxyConfig.Port)
			}
		} else {
			// 无认证
			proxyURL = fmt.Sprintf("http://%s:%d", proxyConfig.Host, proxyConfig.Port)
		}

		proxyURLParsed, err := url.Parse(proxyURL)
		if err == nil {
			httpClient.Transport = &http.Transport{
				Proxy: http.ProxyURL(proxyURLParsed),
			}
		}
	}

	return &Client{
		apiKey:   apiKey,
		endpoint: endpoint,
		model:    model,
		provider: provider,
		client:   httpClient,
	}
}

// SetVisionConfig 设置视觉模型配置
func (c *Client) SetVisionConfig(visionModel, visionEndpoint, visionAPIKey string) {
	if visionModel != "" {
		c.visionConfig = &VisionConfig{
			Model:    visionModel,
			Endpoint: visionEndpoint,
			APIKey:   visionAPIKey,
		}
	}
}

// HasVisionSupport 检查是否支持视觉模型
func (c *Client) HasVisionSupport() bool {
	return c.visionConfig != nil && c.visionConfig.Model != ""
}

// hasImageContent 检查请求中是否包含图片
func (c *Client) hasImageContent(req ChatRequest) bool {
	for _, msg := range req.Messages {
		switch content := msg.Content.(type) {
		case []ContentPart:
			for _, part := range content {
				if part.Type == "image_url" && part.ImageURL != nil {
					return true
				}
			}
		case []interface{}:
			for _, item := range content {
				if m, ok := item.(map[string]interface{}); ok {
					if m["type"] == "image_url" {
						return true
					}
				}
			}
		}
	}
	return false
}

// ChatRequest 聊天请求
type ChatRequest struct {
	Model     string        `json:"model"`
	Messages  []ChatMessage `json:"messages"`
	MaxTokens int           `json:"max_tokens,omitempty"`
}

// ChatMessage 聊天消息
type ChatMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // 可以是 string 或 []ContentPart
}

// ContentPart 多模态消息内容部分
type ContentPart struct {
	Type     string    `json:"type"`               // "text" 或 "image_url"
	Text     string    `json:"text,omitempty"`     // 当 type="text" 时
	ImageURL *ImageURL `json:"image_url,omitempty"` // 当 type="image_url" 时
}

// ImageURL 图片URL结构
type ImageURL struct {
	URL string `json:"url"` // 可以是 URL 或 base64 data URI
}

// ChatResponse 聊天响应 (OpenAI 格式)
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

// AnthropicRequest Anthropic API 请求格式
type AnthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []AnthropicMessage `json:"messages"`
}

// AnthropicMessage Anthropic 消息格式
type AnthropicMessage struct {
	Role    string      `json:"role"` // "user" or "assistant"
	Content interface{} `json:"content"`
}

// AnthropicContentPart Anthropic 多模态内容块
type AnthropicContentPart struct {
	Type   string                `json:"type"` // "text" or "image"
	Text   string                `json:"text,omitempty"`
	Source *AnthropicImageSource `json:"source,omitempty"`
}

// AnthropicImageSource Anthropic 图片来源
type AnthropicImageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // "image/png", "image/jpeg", etc.
	Data      string `json:"data"`       // base64 encoded image data
}

// AnthropicResponse Anthropic API 响应格式
type AnthropicResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Error      *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// ParseUserQuery 解析用户查询意图
func (c *Client) ParseUserQuery(ctx context.Context, query string) (*ParsedQuery, error) {
	systemPrompt := `你是一个智能助手，负责解析用户的查询意图。
请分析用户的问题，返回JSON格式的解析结果。

支持的意图类型（按优先级排序）：
- group_timeline: 查询群聊的发展历程、时间线、重大事件（如：这个群从什么时候开始的？群里的发展历程？有哪些重大决议？什么时候讨论了什么？历史上有哪些里程碑？需求是什么时候开始的？）
  关键词：历程、发展过程、什么时候开始、从头、全部历史、重大决议、里程碑、时间线、发展史、演进过程
  注意：group_timeline 用于了解群的整体历史发展，按时间段回顾；summarize 只是总结近期讨论内容
- site_query: 查询站点信息，支持两种查询方式：
  1. 通过站点前缀查询（字母+数字组合，如l08、b01、by4、abc01等）：查询站点的详细信息
     例如："l08是什么站点？"、"by4的域名是什么？"、"查一下p03站点"
  2. 通过站点ID反查（纯数字，如3040、2888等）：查询该ID对应的站点前缀和信息
     例如："3040的站点前缀是？"、"3040是哪个站点？"、"站点ID 2888对应什么前缀？"
  站点前缀通常是1-3个字母加2-3个数字
  站点ID是纯数字（通常3-4位）
- qa: 基于聊天记录的问答和主题分析（如：虚拟币需求的产品经理是谁？谁负责支付功能？这个bug是什么原因？支付错误有哪些？）
  这是最常用的意图，当用户询问任何需要从聊天记录中查找答案的问题时使用
  **重要**：如果用户问某个特定主题（如"支付错误"、"登录问题"、"Bug情况"）的总结/汇总/分析，应该使用 qa 而不是 summarize
  例如："今天的支付错误信息总结" -> qa（需要搜索支付错误相关消息并分析）
- query_workload: 查询工作量（如：小明这周干了多少活？）
- query_commits: 查询代码提交（如：今天谁提交了代码？）
- search_message: 搜索聊天消息，用于查找特定内容（如：张三说过什么关于登录的？搜索关于支付的消息）
- summarize: 总结**整个群聊**的讨论内容（如：总结一下今天群里的讨论、总结印尼群的消息、今天大家聊了什么）
  **注意**：summarize 只用于总结群聊的整体内容，不带特定主题。如果用户要总结特定主题（如支付、错误、某功能），应该用 qa
- query_requirement: 查询需求进度（如：用户登录功能做到哪了？）
- help: 帮助信息
- unknown: 完全无法理解的问题

重要判断规则：
1. 如果用户问"历程"、"发展过程"、"从什么时候开始"、"时间线"、"里程碑"、"重大决议"，使用 group_timeline 意图
2. 如果用户提到了站点代号（如l08、b01、by4等字母+数字组合），优先使用 site_query 意图，并填写 site_prefix
3. 如果用户问某个纯数字ID对应的站点（如"3040是什么站点"、"3040的前缀"），使用 site_query 意图，并填写 site_id
4. 如果用户问"谁"、"什么"、"为什么"、"怎么"等问题（但不涉及站点或历程），优先使用 qa 意图
5. 如果问题涉及项目、需求、功能、Bug、错误、支付、人员等具体主题，优先使用 qa 意图
6. 只有明确要求"搜索"或"查找消息"时才用 search_message
7. **关键**：summarize 只用于"总结群聊整体内容"，不带特定主题。例如：
   - "总结今天群里的讨论" -> summarize（没有特定主题）
   - "今天的支付错误总结" -> qa（有特定主题：支付错误）
   - "登录问题汇总" -> qa（有特定主题：登录问题）

时间范围（请根据问题推断最合理的时间范围）：
- today: 今天（**仅当**用户明确说"今天"、"今日"时使用）
- yesterday: 昨天（**仅当**用户明确说"昨天"时使用）
- this_week: 本周（**仅当**用户明确说"本周"、"这周"时使用）
- last_week: 上周（**仅当**用户明确说"上周"时使用）
- this_month: 本月（**仅当**用户明确说"本月"、"这个月"时使用）
- last_month: 上个月（**仅当**用户明确说"上个月"、"上月"时使用，指上一个自然月）
- recent_month: 最近一个月/最近30天（当用户说"最近一个月"、"近一个月"、"过去一个月"时使用）
- custom: 只有用户明确指定了具体日期范围时才使用
- all: 全部历史（当用户询问"历程"、"从一开始"、"所有历史"、"发展过程"时使用）
- 空字符串: **默认值**，当用户没有明确指定时间范围时使用，系统会搜索全部历史

重要时间判断规则：
1. **默认不设置时间范围**：如果用户没有明确提到时间词汇，time_range 应该为空字符串 ""
2. 只有用户明确说了时间词汇（今天、昨天、本周、上周、本月等）才设置对应的时间范围
3. 询问"最新"、"最近"时也不要默认设置 today，让系统自动按时间排序即可
4. 如果是 group_timeline 意图，默认使用 all（全部历史）
5. 告警、错误、问题类查询需要查看历史记录，**绝对不要**默认设置 today

重要：如果用户提到了某个群的名称（如"印尼群"、"虚拟币群"、"支付群"等），请提取到 target_group 字段。

请严格返回以下JSON格式（不要包含其他内容）：
{
  "intent": "意图类型",
  "time_range": "时间范围",
  "target_users": ["用户名列表"],
  "target_group": "群名（如：印尼群、虚拟币群，没有则为空字符串）",
  "keywords": ["从问题中提取的关键词，用于搜索"],
  "repository": "仓库名（如有）",
  "site_prefix": "站点前缀（如：l08、b01、by4，没有则为空字符串）",
  "site_id": "站点ID（纯数字，如：3040、2888，没有则为空字符串）",
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

	systemPrompt := `你是一个专业的团队助手，负责分析告警群消息。

【问题类型理解】
当用户问"XX的问题"时，需要全面理解，包括但不限于：
- 支付失败、代付失败、余额不足
- 慢请求、超时、性能问题
- 场馆错误、接口异常、网络错误
- 任何包含"告警"、"失败"、"错误"、"异常"的消息

【回复规范】
1. 全面检索：搜索所有与查询站点相关的告警（不仅仅是精确匹配的类型）
2. 信息具体化：引用原始数据中的具体站点名、时间、数值、错误信息
3. 结构清晰：按告警类型分组展示
4. 突出重点：用emoji标记（📍站点 🐌慢请求 ❌失败 ⚠️告警 📅时间）
5. 不编造信息：如果确实没有相关数据，明确说明"未找到XX的相关告警记录"`

	userPrompt := fmt.Sprintf(`问题：%s

数据：%s

请直接回答：`, prompt, string(dataJSON))

	req := ChatRequest{
		Model: c.model,
		Messages: []ChatMessage{
			{Role: "system", Content: systemPrompt},
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

	systemPrompt := `你是专业的群消息总结助手。请对群聊消息进行精准总结。

【输出格式】
📌 **主要话题**
• [话题1]：一句话概述 + 涉及人员
• [话题2]：一句话概述 + 涉及人员

✅ **关键决策/结论**
• [决策内容] - 由谁决定/确认

⚠️ **问题/异常**（如有）
• 📍[站点/系统]：[问题描述] - [处理状态]

📋 **待办事项**（如有）
• [事项内容] - 👤负责人

【总结要求】
1. 引用具体站点名、人名、时间
2. 问题类需标注处理状态（已解决/待处理/进行中）
3. 按重要性排序，最重要的放前面
4. 省略闲聊、表情等无实质内容
5. 每个分类如无内容则省略整个分类`

	req := ChatRequest{
		Model: c.model,
		Messages: []ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: fmt.Sprintf("请总结以下群聊消息：\n\n%s", content)},
		},
		MaxTokens: 1500,
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

// AnalyzeImage 分析图片内容（使用 Vision 模型）
func (c *Client) AnalyzeImage(ctx context.Context, imageBase64 string, mimeType string) (string, error) {
	// 使用支持 Vision 的模型
	visionModel := "meta-llama/llama-4-scout-17b-16e-instruct"

	// 构建 data URI
	dataURI := fmt.Sprintf("data:%s;base64,%s", mimeType, imageBase64)

	// 构建多模态消息
	contentParts := []ContentPart{
		{
			Type: "text",
			Text: `请分析这张图片的内容。这是来自工作群聊的截图，可能是：
- 系统后台截图
- 报表/表格截图
- 错误信息截图
- 凭证类图片
- 其他工作相关截图

请提取图片中的关键信息，包括：
1. 图片类型（后台截图、报表、错误信息等）
2. 主要内容概述
3. 关键数据或信息（如有数字、日期、状态等）
4. 如果是错误截图，说明错误内容

用简洁的中文描述，便于后续搜索和分析。`,
		},
		{
			Type:     "image_url",
			ImageURL: &ImageURL{URL: dataURI},
		},
	}

	req := ChatRequest{
		Model: visionModel,
		Messages: []ChatMessage{
			{Role: "user", Content: contentParts},
		},
		MaxTokens: 1024,
	}

	resp, err := c.chat(ctx, req)
	if err != nil {
		return "", fmt.Errorf("vision analysis failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response from vision model")
	}

	return resp.Choices[0].Message.Content, nil
}

// ChatWithImage 使用视觉模型分析图片并回答问题
func (c *Client) ChatWithImage(ctx context.Context, query string, imageData []byte) (string, error) {
	if c.visionConfig == nil || c.visionConfig.Model == "" {
		return "", fmt.Errorf("vision model not configured")
	}

	// 检测图片类型
	mimeType := "image/jpeg"
	if len(imageData) > 8 {
		// PNG 文件头: 89 50 4E 47
		if imageData[0] == 0x89 && imageData[1] == 0x50 && imageData[2] == 0x4E && imageData[3] == 0x47 {
			mimeType = "image/png"
		}
		// GIF 文件头: 47 49 46
		if imageData[0] == 0x47 && imageData[1] == 0x49 && imageData[2] == 0x46 {
			mimeType = "image/gif"
		}
		// WebP 文件头: 52 49 46 46 ... 57 45 42 50
		if imageData[0] == 0x52 && imageData[1] == 0x49 && imageData[2] == 0x46 && imageData[3] == 0x46 {
			mimeType = "image/webp"
		}
	}

	// 转换为 base64
	imageBase64 := base64.StdEncoding.EncodeToString(imageData)
	dataURI := fmt.Sprintf("data:%s;base64,%s", mimeType, imageBase64)

	log.Printf("[LLM] ChatWithImage: using vision model %s, image type: %s, size: %d bytes",
		c.visionConfig.Model, mimeType, len(imageData))

	// 构建多模态消息
	contentParts := []ContentPart{
		{
			Type: "text",
			Text: query,
		},
		{
			Type:     "image_url",
			ImageURL: &ImageURL{URL: dataURI},
		},
	}

	req := ChatRequest{
		Model: c.visionConfig.Model,
		Messages: []ChatMessage{
			{Role: "user", Content: contentParts},
		},
		MaxTokens: 2048,
	}

	resp, err := c.chat(ctx, req)
	if err != nil {
		return "", fmt.Errorf("vision model request failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response from vision model")
	}

	return resp.Choices[0].Message.Content, nil
}

func (c *Client) chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	// 根据 provider 选择不同的 API 格式
	if c.provider == "anthropic" {
		return c.chatAnthropic(ctx, req)
	}
	return c.chatOpenAI(ctx, req)
}

// chatOpenAI 使用 OpenAI 兼容格式 (OpenAI, Groq, NVIDIA NIM 等)
// 包含自动重试机制：超时或网络错误时最多重试 2 次
func (c *Client) chatOpenAI(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	const maxRetries = 3
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		resp, err := c.doOpenAIRequest(ctx, req)
		if err == nil {
			return resp, nil
		}

		lastErr = err

		// 判断是否是可重试的错误（超时、网络错误）
		if !isRetryableError(err) {
			return nil, err
		}

		// 最后一次尝试不需要等待
		if attempt < maxRetries {
			log.Printf("[LLM] Request failed (attempt %d/%d): %v, retrying...", attempt, maxRetries, err)
			// 指数退避：1秒、2秒
			time.Sleep(time.Duration(attempt) * time.Second)
		}
	}

	return nil, fmt.Errorf("LLM request failed after %d attempts: %w", maxRetries, lastErr)
}

// isRetryableError 判断是否是可重试的错误
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// 超时错误
	if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "Timeout") {
		return true
	}
	// 连接错误
	if strings.Contains(errStr, "connection") || strings.Contains(errStr, "EOF") {
		return true
	}
	// 服务端错误 (5xx)
	if strings.Contains(errStr, "HTTP 5") {
		return true
	}
	// 限流错误 (429)
	if strings.Contains(errStr, "HTTP 429") || strings.Contains(errStr, "rate limit") {
		return true
	}
	return false
}

// doOpenAIRequest 执行单次 OpenAI API 请求
func (c *Client) doOpenAIRequest(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	// 检测请求中是否包含图片，如果有则切换到视觉模型
	endpoint := c.endpoint
	apiKey := c.apiKey
	if c.hasImageContent(req) && c.visionConfig != nil && c.visionConfig.Model != "" {
		log.Printf("[LLM] Detected image in request, switching to vision model: %s", c.visionConfig.Model)
		req.Model = c.visionConfig.Model
		if c.visionConfig.Endpoint != "" {
			endpoint = c.visionConfig.Endpoint
		}
		if c.visionConfig.APIKey != "" {
			apiKey = c.visionConfig.APIKey
		}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

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

	// 处理 MiniMax 等模型的 <think>...</think> 标签，只保留最终回答
	for i := range chatResp.Choices {
		chatResp.Choices[i].Message.Content = stripThinkingTags(chatResp.Choices[i].Message.Content)
	}

	return &chatResp, nil
}

// stripThinkingTags 移除模型输出中的思考过程标签
// MiniMax-M2.1 等模型会在回答中包含 <think>...</think> 标签
func stripThinkingTags(content string) string {
	// 查找 </think> 标签的位置
	thinkEndTag := "</think>"
	if idx := strings.Index(content, thinkEndTag); idx != -1 {
		// 返回 </think> 之后的内容，并去除首尾空白
		return strings.TrimSpace(content[idx+len(thinkEndTag):])
	}
	// 如果没有 think 标签，返回原内容
	return content
}

// chatAnthropic 使用 Anthropic API 格式
func (c *Client) chatAnthropic(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	// 转换请求格式
	anthropicReq := AnthropicRequest{
		Model:     c.model,
		MaxTokens: req.MaxTokens,
	}
	if anthropicReq.MaxTokens == 0 {
		anthropicReq.MaxTokens = 1024
	}

	// 提取 system prompt 和 messages
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			// system 消息只能是字符串
			if content, ok := msg.Content.(string); ok {
				anthropicReq.System = content
			}
			continue
		}

		// 处理不同类型的内容
		switch content := msg.Content.(type) {
		case string:
			// 普通文本消息
			anthropicReq.Messages = append(anthropicReq.Messages, AnthropicMessage{
				Role:    msg.Role,
				Content: content,
			})
		case []ContentPart:
			// 多模态消息（如图片+文本）
			var parts []AnthropicContentPart
			for _, part := range content {
				switch part.Type {
				case "text":
					parts = append(parts, AnthropicContentPart{
						Type: "text",
						Text: part.Text,
					})
				case "image_url":
					if part.ImageURL != nil {
						// 解析 data URI: data:image/png;base64,xxxxx
						dataURI := part.ImageURL.URL
						if strings.HasPrefix(dataURI, "data:") {
							// 提取 media type 和 base64 数据
							// 格式: data:image/png;base64,xxxxx
							uriParts := strings.SplitN(dataURI[5:], ";base64,", 2)
							if len(uriParts) == 2 {
								mediaType := uriParts[0]
								base64Data := uriParts[1]
								parts = append(parts, AnthropicContentPart{
									Type: "image",
									Source: &AnthropicImageSource{
										Type:      "base64",
										MediaType: mediaType,
										Data:      base64Data,
									},
								})
							}
						}
					}
				}
			}
			if len(parts) > 0 {
				anthropicReq.Messages = append(anthropicReq.Messages, AnthropicMessage{
					Role:    msg.Role,
					Content: parts,
				})
			}
		}
	}

	body, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Anthropic error: HTTP %d - %s", resp.StatusCode, string(respBody))
	}

	var anthropicResp AnthropicResponse
	if err := json.Unmarshal(respBody, &anthropicResp); err != nil {
		return nil, fmt.Errorf("failed to parse Anthropic response: %w", err)
	}

	if anthropicResp.Error != nil {
		return nil, fmt.Errorf("Anthropic error: %s", anthropicResp.Error.Message)
	}

	// 转换为 OpenAI 格式的响应
	var content string
	for _, c := range anthropicResp.Content {
		if c.Type == "text" {
			content += c.Text
		}
	}

	return &ChatResponse{
		Choices: []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}{
			{Message: struct {
				Content string `json:"content"`
			}{Content: content}},
		},
	}, nil
}
