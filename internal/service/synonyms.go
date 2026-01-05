package service

import (
	"context"
	"log"
	"math"
	"strings"
	"time"
	"unicode"

	"team-assistant/pkg/embedding"
)

// SynonymExpander 同义词扩展器
type SynonymExpander struct {
	synonymMap      map[string][]string
	embeddingClient *embedding.OllamaClient // 用于语义相似度计算
}

// SemanticSynonymExpander 基于 Embedding 的语义同义词扩展器
type SemanticSynonymExpander struct {
	embeddingClient    *embedding.OllamaClient
	similarityThreshold float64 // 相似度阈值，默认 0.75
}

// NewSemanticSynonymExpander 创建语义同义词扩展器
func NewSemanticSynonymExpander(embeddingClient *embedding.OllamaClient) *SemanticSynonymExpander {
	return &SemanticSynonymExpander{
		embeddingClient:    embeddingClient,
		similarityThreshold: 0.75,
	}
}

// SetThreshold 设置相似度阈值
func (e *SemanticSynonymExpander) SetThreshold(threshold float64) {
	e.similarityThreshold = threshold
}

// AreSynonyms 判断两个词是否是语义同义词
func (e *SemanticSynonymExpander) AreSynonyms(ctx context.Context, word1, word2 string) (bool, float64) {
	if e.embeddingClient == nil {
		return false, 0
	}

	// 获取两个词的 embedding
	embeddings, err := e.embeddingClient.GetEmbeddings(ctx, []string{word1, word2})
	if err != nil || len(embeddings) != 2 {
		return false, 0
	}

	// 计算余弦相似度
	similarity := cosineSimilarity(embeddings[0], embeddings[1])
	return similarity >= e.similarityThreshold, similarity
}

// FindSynonymsInContent 从内容中找出与关键词语义相似的词
// 用于：当搜索"签名错误"时，能匹配到包含"签名验证失败"的内容
func (e *SemanticSynonymExpander) FindSynonymsInContent(ctx context.Context, keyword string, content string) ([]string, error) {
	if e.embeddingClient == nil {
		return nil, nil
	}

	// 从内容中提取候选词（简单分词）
	candidates := extractCandidateTerms(content)
	if len(candidates) == 0 {
		return nil, nil
	}

	// 获取关键词的 embedding
	keywordEmb, err := e.embeddingClient.GetEmbeddings(ctx, []string{keyword})
	if err != nil || len(keywordEmb) == 0 {
		return nil, err
	}

	// 批量获取候选词的 embedding
	candidateEmbs, err := e.embeddingClient.GetEmbeddings(ctx, candidates)
	if err != nil {
		return nil, err
	}

	// 找出相似度超过阈值的词
	var synonyms []string
	for i, emb := range candidateEmbs {
		similarity := cosineSimilarity(keywordEmb[0], emb)
		if similarity >= e.similarityThreshold && candidates[i] != keyword {
			synonyms = append(synonyms, candidates[i])
		}
	}

	return synonyms, nil
}

// ExpandWithSemantics 语义扩展关键词（结合静态规则和动态语义）
func (e *SemanticSynonymExpander) ExpandWithSemantics(ctx context.Context, keywords []string) []string {
	// 先用静态规则扩展
	staticExpander := NewSynonymExpander()
	expanded := staticExpander.Expand(keywords)

	// 如果没有 embedding 客户端，只返回静态扩展结果
	if e.embeddingClient == nil {
		return expanded
	}

	// 语义扩展：为每个关键词生成相似的变体
	// 这里使用一个预定义的常见变体模式
	variants := generateSemanticVariants(keywords)
	if len(variants) == 0 {
		return expanded
	}

	// 批量计算相似度
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	allTerms := append(keywords, variants...)
	embeddings, err := e.embeddingClient.GetEmbeddings(ctx, allTerms)
	if err != nil {
		log.Printf("Semantic expansion failed: %v", err)
		return expanded
	}

	// 找出与原关键词相似的变体
	keywordCount := len(keywords)
	for i := 0; i < keywordCount; i++ {
		for j := keywordCount; j < len(embeddings); j++ {
			similarity := cosineSimilarity(embeddings[i], embeddings[j])
			if similarity >= e.similarityThreshold {
				variant := allTerms[j]
				// 去重添加
				found := false
				for _, ex := range expanded {
					if ex == variant {
						found = true
						break
					}
				}
				if !found {
					expanded = append(expanded, variant)
					log.Printf("[Semantic] Found synonym: %s -> %s (similarity: %.2f)", keywords[i], variant, similarity)
				}
			}
		}
	}

	return expanded
}

// generateSemanticVariants 生成关键词的语义变体
func generateSemanticVariants(keywords []string) []string {
	// 常见的语义变体模式
	patterns := map[string][]string{
		"错误":  {"失败", "异常", "问题", "故障"},
		"失败":  {"错误", "异常", "问题", "故障"},
		"验证":  {"校验", "检验", "核验"},
		"签名":  {"sign", "signature"},
		"配置":  {"设置", "config"},
		"支付":  {"付款", "交易"},
		"订单":  {"order", "单号"},
		"超时":  {"timeout", "延迟"},
		"异常":  {"exception", "错误", "问题"},
	}

	var variants []string
	seen := make(map[string]bool)

	for _, kw := range keywords {
		// 拆分组合词并生成变体
		// 例如 "签名错误" -> ["签名失败", "签名异常", "签名验证失败"]
		for pattern, alternatives := range patterns {
			if strings.Contains(kw, pattern) {
				for _, alt := range alternatives {
					variant := strings.Replace(kw, pattern, alt, 1)
					if !seen[variant] && variant != kw {
						seen[variant] = true
						variants = append(variants, variant)
					}
				}
			}
		}

		// 添加组合变体
		// "签名错误" -> "签名验证失败"
		if strings.Contains(kw, "错误") {
			variant := strings.Replace(kw, "错误", "验证失败", 1)
			if !seen[variant] {
				seen[variant] = true
				variants = append(variants, variant)
			}
		}
		if strings.Contains(kw, "失败") && !strings.Contains(kw, "验证") {
			variant := strings.Replace(kw, "失败", "验证失败", 1)
			if !seen[variant] {
				seen[variant] = true
				variants = append(variants, variant)
			}
		}
	}

	return variants
}

// extractCandidateTerms 从内容中提取候选词组
func extractCandidateTerms(content string) []string {
	// 简单实现：按标点和空格分割，然后提取2-6字的词组
	var terms []string
	seen := make(map[string]bool)

	// 分割成句子
	sentences := strings.FieldsFunc(content, func(r rune) bool {
		return r == '。' || r == '，' || r == '、' || r == '\n' || r == ' ' || r == '!' || r == '?'
	})

	for _, sentence := range sentences {
		runes := []rune(strings.TrimSpace(sentence))
		// 提取不同长度的子串作为候选词
		for length := 2; length <= 6 && length <= len(runes); length++ {
			for i := 0; i <= len(runes)-length; i++ {
				term := string(runes[i : i+length])
				if !seen[term] && isValidTerm(term) {
					seen[term] = true
					terms = append(terms, term)
				}
			}
		}
	}

	// 限制候选词数量，避免 embedding 请求过大
	if len(terms) > 50 {
		terms = terms[:50]
	}

	return terms
}

// isValidTerm 判断是否是有效的候选词
func isValidTerm(term string) bool {
	// 过滤纯数字、纯标点等
	hasLetter := false
	for _, r := range term {
		if unicode.IsLetter(r) {
			hasLetter = true
			break
		}
	}
	return hasLetter
}

// cosineSimilarity 计算余弦相似度
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// 默认同义词表（技术术语 + 常用词）
var defaultSynonyms = map[string][]string{
	// 技术问题相关（广义的"问题"包括各类告警）
	"bug":  {"错误", "问题", "异常", "故障", "缺陷", "报错", "告警"},
	"错误":   {"bug", "问题", "异常", "故障", "缺陷", "报错", "告警", "失败"},
	"问题":   {"bug", "错误", "异常", "故障", "issue", "告警", "失败", "慢请求", "超时"},
	"异常":   {"bug", "错误", "问题", "故障", "exception", "告警"},
	"报错":   {"bug", "错误", "异常", "error", "告警"},
	"告警":   {"问题", "错误", "异常", "警告", "alert", "失败", "慢请求"},
	"慢请求":  {"超时", "性能问题", "告警", "延迟", "慢"},
	"性能问题": {"慢请求", "超时", "延迟", "卡顿"},

	// 登录认证相关
	"登录":    {"登陆", "认证", "鉴权", "signin", "login"},
	"登陆":    {"登录", "认证", "鉴权", "signin", "login"},
	"认证":    {"登录", "鉴权", "auth", "authentication"},
	"鉴权":    {"登录", "认证", "授权", "auth"},
	"授权":    {"鉴权", "权限", "authorization"},
	"login": {"登录", "登陆", "signin"},

	// 支付交易相关（包括各类支付告警）
	"支付":   {"付款", "收款", "交易", "转账", "pay", "代付", "存款", "充值"},
	"付款":   {"支付", "交易", "转账", "payment", "代付"},
	"交易":   {"支付", "付款", "转账", "transaction"},
	"订单":   {"order", "购买", "交易"},
	"代付":   {"支付", "付款", "转账", "payout"},
	"存款":   {"充值", "deposit", "支付"},
	"支付问题": {"支付失败", "代付失败", "余额不足", "支付告警", "交易失败", "慢请求"},
	"支付失败": {"支付问题", "代付失败", "交易失败", "付款失败"},

	// 发布部署相关
	"上线":       {"发布", "部署", "发版", "release", "deploy"},
	"发布":       {"上线", "部署", "发版", "release", "deploy"},
	"部署":       {"上线", "发布", "deploy", "deployment"},
	"发版":       {"上线", "发布", "release"},
	"release":  {"上线", "发布", "发版"},
	"deploy":   {"部署", "上线", "发布"},
	"回滚":       {"rollback", "回退", "撤销"},
	"rollback": {"回滚", "回退"},

	// 前后端相关
	"后端":       {"服务端", "backend", "server", "服务器"},
	"服务端":      {"后端", "backend", "server"},
	"backend":  {"后端", "服务端", "server"},
	"前端":       {"frontend", "客户端", "web", "页面"},
	"frontend": {"前端", "客户端", "web"},
	"客户端":      {"前端", "client", "app"},

	// 数据库相关
	"数据库":      {"db", "mysql", "redis", "存储", "database"},
	"database": {"数据库", "db", "mysql"},
	"缓存":       {"cache", "redis", "内存"},
	"redis":    {"缓存", "cache"},

	// API接口相关
	"接口":  {"api", "服务", "endpoint"},
	"api": {"接口", "服务", "endpoint"},
	"请求":  {"request", "调用"},
	"响应":  {"response", "返回", "结果"},
	"超时":  {"timeout", "延迟", "慢", "连接超时", "网络超时", "请求超时"},
	"慢":   {"超时", "卡顿", "延迟", "性能"},
	"卡顿":  {"慢", "延迟", "性能问题"},
	"性能":  {"performance", "慢", "优化"},
	"优化":  {"改进", "提升", "性能"},
	"重构":  {"refactor", "优化", "改进"},

	// 用户相关
	"用户":   {"user", "客户", "会员"},
	"user": {"用户", "客户"},
	"会员":   {"用户", "vip", "客户"},

	// 测试相关
	"测试":    {"test", "验证", "检查", "qa"},
	"test":  {"测试", "验证"},
	"联调":    {"测试", "调试", "对接"},
	"调试":    {"debug", "排查", "定位"},
	"debug": {"调试", "排查"},

	// 配置相关
	"配置":     {"config", "设置", "参数"},
	"config": {"配置", "设置"},
	"参数":     {"配置", "设置", "变量"},

	// 消息通知相关
	"消息": {"message", "通知", "推送"},
	"通知": {"消息", "推送", "提醒"},
	"推送": {"通知", "消息", "push"},

	// 文档相关
	"文档":  {"doc", "document", "说明", "手册"},
	"doc": {"文档", "document"},

	// 版本相关
	"版本":      {"version", "迭代"},
	"version": {"版本", "迭代"},
	"迭代":      {"版本", "sprint"},

	// 状态相关
	"成功": {"success", "完成", "ok"},
	"失败": {"fail", "error", "错误", "异常"},
	"完成": {"done", "成功", "结束"},
	"进行": {"doing", "处理中", "进行中"},

	// 签名验证相关
	"签名错误":   {"签名验证失败", "签名失败", "签名异常", "sign error", "signature"},
	"签名验证失败": {"签名错误", "签名失败", "签名异常"},
	"签名失败":   {"签名错误", "签名验证失败"},

	// 支付错误相关
	"参数错误":  {"parameter error", "参数异常", "参数不对"},
	"余额不足":  {"insufficient balance", "余额不够", "没钱"},
	"配置缺失":  {"配置错误", "未配置", "缺少配置"},

	// 功能需求相关
	"功能": {"feature", "需求", "特性"},
	"需求": {"功能", "feature", "requirement"},
	"开发": {"develop", "编码", "实现"},
}

// NewSynonymExpander 创建同义词扩展器
func NewSynonymExpander() *SynonymExpander {
	return &SynonymExpander{
		synonymMap: defaultSynonyms,
	}
}

// NewSynonymExpanderWithCustom 创建带自定义同义词的扩展器
func NewSynonymExpanderWithCustom(custom map[string][]string) *SynonymExpander {
	// 合并默认和自定义同义词
	merged := make(map[string][]string)
	for k, v := range defaultSynonyms {
		merged[k] = v
	}
	for k, v := range custom {
		if existing, ok := merged[k]; ok {
			// 合并去重
			merged[k] = uniqueStrings(append(existing, v...))
		} else {
			merged[k] = v
		}
	}
	return &SynonymExpander{
		synonymMap: merged,
	}
}

// Expand 扩展关键词列表
func (e *SynonymExpander) Expand(keywords []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(keywords)*3)

	for _, kw := range keywords {
		// 原词先加入
		normalized := strings.ToLower(strings.TrimSpace(kw))
		if normalized == "" {
			continue
		}
		if !seen[normalized] {
			seen[normalized] = true
			result = append(result, normalized)
		}

		// 查找同义词
		if synonyms, ok := e.synonymMap[normalized]; ok {
			for _, syn := range synonyms {
				synNorm := strings.ToLower(strings.TrimSpace(syn))
				if !seen[synNorm] {
					seen[synNorm] = true
					result = append(result, synNorm)
				}
			}
		}
	}

	return result
}

// ExpandQuery 扩展查询字符串（提取词并扩展）
func (e *SynonymExpander) ExpandQuery(query string) []string {
	// 简单分词：按空格和标点分割
	words := tokenize(query)
	return e.Expand(words)
}

// tokenize 简单分词
func tokenize(text string) []string {
	var words []string
	var current strings.Builder

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
		} else {
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
		}
	}
	if current.Len() > 0 {
		words = append(words, current.String())
	}

	return words
}

// uniqueStrings 字符串去重
func uniqueStrings(strs []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(strs))
	for _, s := range strs {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}
