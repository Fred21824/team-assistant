package service

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Reranker 重排序器
// 对初步检索结果进行多维度评分，提升最终排序质量
type Reranker struct {
	config RerankerConfig
}

// RerankerConfig 重排序器配置
type RerankerConfig struct {
	// 各维度权重（总和应为 1.0）
	SemanticWeight  float64 // 语义相似度权重，默认 0.4
	KeywordWeight   float64 // 关键词匹配权重，默认 0.3
	RecencyWeight   float64 // 时效性权重，默认 0.15
	LengthWeight    float64 // 长度匹配权重，默认 0.1
	PositionWeight  float64 // 关键词位置权重，默认 0.05

	// 时效性配置
	RecencyDecayDays int // 时效衰减天数，默认 7 天

	// 长度配置
	IdealMinLength int // 理想最小长度，默认 50
	IdealMaxLength int // 理想最大长度，默认 500
}

// DefaultRerankerConfig 默认配置
func DefaultRerankerConfig() RerankerConfig {
	return RerankerConfig{
		SemanticWeight:   0.4,
		KeywordWeight:    0.3,
		RecencyWeight:    0.15,
		LengthWeight:     0.1,
		PositionWeight:   0.05,
		RecencyDecayDays: 7,
		IdealMinLength:   50,
		IdealMaxLength:   500,
	}
}

// NewReranker 创建重排序器
func NewReranker(config RerankerConfig) *Reranker {
	// 验证并归一化权重
	total := config.SemanticWeight + config.KeywordWeight + config.RecencyWeight +
		config.LengthWeight + config.PositionWeight

	if total <= 0 {
		config = DefaultRerankerConfig()
	} else if math.Abs(total-1.0) > 0.01 {
		// 归一化
		config.SemanticWeight /= total
		config.KeywordWeight /= total
		config.RecencyWeight /= total
		config.LengthWeight /= total
		config.PositionWeight /= total
	}

	if config.RecencyDecayDays <= 0 {
		config.RecencyDecayDays = 7
	}
	if config.IdealMinLength <= 0 {
		config.IdealMinLength = 50
	}
	if config.IdealMaxLength <= 0 {
		config.IdealMaxLength = 500
	}

	return &Reranker{config: config}
}

// NewDefaultReranker 创建默认重排序器
func NewDefaultReranker() *Reranker {
	return NewReranker(DefaultRerankerConfig())
}

// RerankInput 重排序输入
type RerankInput struct {
	Query         string         // 原始查询
	Keywords      []string       // 关键词列表
	Results       []SearchResult // 初步检索结果
	SemanticScore float32        // 原始语义分数
}

// RerankOutput 重排序输出
type RerankOutput struct {
	Result      SearchResult // 搜索结果
	FinalScore  float64      // 最终综合分数
	ScoreDetail ScoreDetail  // 分数明细
}

// ScoreDetail 分数明细
type ScoreDetail struct {
	SemanticScore float64 // 语义相似度分数
	KeywordScore  float64 // 关键词匹配分数
	RecencyScore  float64 // 时效性分数
	LengthScore   float64 // 长度匹配分数
	PositionScore float64 // 关键词位置分数
}

// Rerank 对搜索结果进行重排序
func (r *Reranker) Rerank(ctx context.Context, query string, keywords []string, results []SearchResult) []SearchResult {
	if len(results) == 0 {
		return results
	}

	// 计算每个结果的综合分数
	outputs := make([]RerankOutput, len(results))
	for i, result := range results {
		outputs[i] = r.scoreResult(query, keywords, result)
	}

	// 按综合分数排序
	sort.Slice(outputs, func(i, j int) bool {
		return outputs[i].FinalScore > outputs[j].FinalScore
	})

	// 提取排序后的结果
	reranked := make([]SearchResult, len(outputs))
	for i, out := range outputs {
		reranked[i] = out.Result
		// 更新分数为综合分数
		reranked[i].Score = float32(out.FinalScore)
	}

	return reranked
}

// RerankWithDetail 重排序并返回详细分数
func (r *Reranker) RerankWithDetail(ctx context.Context, query string, keywords []string, results []SearchResult) []RerankOutput {
	if len(results) == 0 {
		return nil
	}

	outputs := make([]RerankOutput, len(results))
	for i, result := range results {
		outputs[i] = r.scoreResult(query, keywords, result)
	}

	sort.Slice(outputs, func(i, j int) bool {
		return outputs[i].FinalScore > outputs[j].FinalScore
	})

	return outputs
}

// scoreResult 计算单个结果的综合分数
func (r *Reranker) scoreResult(query string, keywords []string, result SearchResult) RerankOutput {
	detail := ScoreDetail{}

	// 1. 语义分数（使用原始向量搜索分数）
	detail.SemanticScore = float64(result.Score)

	// 2. 关键词匹配分数
	detail.KeywordScore = r.calculateKeywordScore(result.Content, keywords)

	// 3. 时效性分数
	detail.RecencyScore = r.calculateRecencyScore(result.CreatedAt)

	// 4. 长度匹配分数
	detail.LengthScore = r.calculateLengthScore(result.Content, query)

	// 5. 关键词位置分数
	detail.PositionScore = r.calculatePositionScore(result.Content, keywords)

	// 加权求和
	finalScore := detail.SemanticScore*r.config.SemanticWeight +
		detail.KeywordScore*r.config.KeywordWeight +
		detail.RecencyScore*r.config.RecencyWeight +
		detail.LengthScore*r.config.LengthWeight +
		detail.PositionScore*r.config.PositionWeight

	return RerankOutput{
		Result:      result,
		FinalScore:  finalScore,
		ScoreDetail: detail,
	}
}

// calculateKeywordScore 计算关键词匹配分数
func (r *Reranker) calculateKeywordScore(content string, keywords []string) float64 {
	if len(keywords) == 0 {
		return 0.5 // 无关键词时给中等分数
	}

	lowerContent := strings.ToLower(content)
	matchCount := 0
	totalFreq := 0

	for _, kw := range keywords {
		lowerKw := strings.ToLower(kw)
		freq := strings.Count(lowerContent, lowerKw)
		if freq > 0 {
			matchCount++
			totalFreq += freq
		}
	}

	if matchCount == 0 {
		return 0
	}

	// 匹配率
	matchRate := float64(matchCount) / float64(len(keywords))

	// 频率加成（使用对数防止过高权重）
	freqBonus := math.Log1p(float64(totalFreq)) / 5.0
	if freqBonus > 0.3 {
		freqBonus = 0.3
	}

	return matchRate*0.7 + freqBonus + 0.0
}

// calculateRecencyScore 计算时效性分数
func (r *Reranker) calculateRecencyScore(createdAt time.Time) float64 {
	if createdAt.IsZero() {
		return 0.5 // 无时间信息给中等分数
	}

	daysSince := time.Since(createdAt).Hours() / 24

	if daysSince < 0 {
		daysSince = 0
	}

	// 指数衰减：exp(-days / decay_days)
	decay := math.Exp(-daysSince / float64(r.config.RecencyDecayDays))

	// 确保分数在 0-1 范围
	if decay < 0 {
		decay = 0
	}
	if decay > 1 {
		decay = 1
	}

	return decay
}

// calculateLengthScore 计算长度匹配分数
func (r *Reranker) calculateLengthScore(content, query string) float64 {
	contentLen := utf8.RuneCountInString(content)

	// 理想长度范围
	idealMin := r.config.IdealMinLength
	idealMax := r.config.IdealMaxLength

	if contentLen >= idealMin && contentLen <= idealMax {
		return 1.0 // 理想范围内
	}

	if contentLen < idealMin {
		// 太短，按比例降分
		return float64(contentLen) / float64(idealMin)
	}

	// 太长，使用对数衰减
	overLength := float64(contentLen - idealMax)
	decay := 1.0 / (1.0 + math.Log1p(overLength/float64(idealMax)))

	if decay < 0.3 {
		decay = 0.3 // 最低分 0.3
	}

	return decay
}

// calculatePositionScore 计算关键词位置分数
// 关键词出现在开头的权重更高
func (r *Reranker) calculatePositionScore(content string, keywords []string) float64 {
	if len(keywords) == 0 {
		return 0.5
	}

	lowerContent := strings.ToLower(content)
	contentLen := len(lowerContent)
	if contentLen == 0 {
		return 0
	}

	var positionScores []float64

	for _, kw := range keywords {
		lowerKw := strings.ToLower(kw)
		pos := strings.Index(lowerContent, lowerKw)
		if pos == -1 {
			continue
		}

		// 位置分数：出现越早分数越高
		// 使用 1 - (pos / len) 的变体
		relativePos := float64(pos) / float64(contentLen)
		score := 1.0 - relativePos*0.5 // 最高 1.0，最低 0.5

		positionScores = append(positionScores, score)
	}

	if len(positionScores) == 0 {
		return 0
	}

	// 取平均
	sum := 0.0
	for _, s := range positionScores {
		sum += s
	}

	return sum / float64(len(positionScores))
}

// GetConfig 获取配置
func (r *Reranker) GetConfig() RerankerConfig {
	return r.config
}
