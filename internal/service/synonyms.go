package service

import (
	"strings"
	"unicode"
)

// SynonymExpander 同义词扩展器
type SynonymExpander struct {
	synonymMap map[string][]string
}

// 默认同义词表（技术术语 + 常用词）
var defaultSynonyms = map[string][]string{
	// 技术问题相关
	"bug": {"错误", "问题", "异常", "故障", "缺陷", "报错"},
	"错误":  {"bug", "问题", "异常", "故障", "缺陷", "报错"},
	"问题":  {"bug", "错误", "异常", "故障", "issue"},
	"异常":  {"bug", "错误", "问题", "故障", "exception"},
	"报错":  {"bug", "错误", "异常", "error"},

	// 登录认证相关
	"登录":    {"登陆", "认证", "鉴权", "signin", "login"},
	"登陆":    {"登录", "认证", "鉴权", "signin", "login"},
	"认证":    {"登录", "鉴权", "auth", "authentication"},
	"鉴权":    {"登录", "认证", "授权", "auth"},
	"授权":    {"鉴权", "权限", "authorization"},
	"login": {"登录", "登陆", "signin"},

	// 支付交易相关
	"支付": {"付款", "收款", "交易", "转账", "pay"},
	"付款": {"支付", "交易", "转账", "payment"},
	"交易": {"支付", "付款", "转账", "transaction"},
	"订单": {"order", "购买", "交易"},

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
	"超时":  {"timeout", "延迟", "慢"},
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
