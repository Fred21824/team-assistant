package service

import (
	"math"
	"strings"
	"sync"
	"unicode/utf8"
)

// BM25Scorer BM25 评分器
// BM25 是一种基于概率的信息检索算法，考虑词频、逆文档频率和文档长度
type BM25Scorer struct {
	k1        float64          // 词频饱和参数，控制词频的影响程度，默认 1.5
	b         float64          // 文档长度归一化参数，默认 0.75
	avgDocLen float64          // 平均文档长度（字符数）
	docCount  int64            // 文档总数
	docFreq   map[string]int64 // 词 -> 包含该词的文档数
	mu        sync.RWMutex     // 读写锁
}

// NewBM25Scorer 创建 BM25 评分器
// k1: 词频饱和参数，推荐 1.2-2.0，默认 1.5
// b: 文档长度归一化参数，推荐 0.5-0.8，默认 0.75
func NewBM25Scorer(k1, b float64) *BM25Scorer {
	if k1 <= 0 {
		k1 = 1.5
	}
	if b < 0 || b > 1 {
		b = 0.75
	}
	return &BM25Scorer{
		k1:        k1,
		b:         b,
		avgDocLen: 100, // 默认值，会在 UpdateStats 中更新
		docCount:  1,   // 避免除零
		docFreq:   make(map[string]int64),
	}
}

// UpdateStats 从文档集合更新统计量
// docs: 文档内容列表
func (s *BM25Scorer) UpdateStats(docs []string) {
	if len(docs) == 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 重置统计
	s.docFreq = make(map[string]int64)
	s.docCount = int64(len(docs))

	var totalLen int64
	for _, doc := range docs {
		totalLen += int64(utf8.RuneCountInString(doc))

		// 统计每个词出现在多少文档中
		seen := make(map[string]bool)
		tokens := bm25Tokenize(doc)
		for _, token := range tokens {
			if !seen[token] {
				seen[token] = true
				s.docFreq[token]++
			}
		}
	}

	s.avgDocLen = float64(totalLen) / float64(s.docCount)
}

// UpdateStatsIncremental 增量更新统计量（添加新文档）
func (s *BM25Scorer) UpdateStatsIncremental(doc string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	docLen := utf8.RuneCountInString(doc)

	// 更新平均文档长度
	oldTotal := s.avgDocLen * float64(s.docCount)
	s.docCount++
	s.avgDocLen = (oldTotal + float64(docLen)) / float64(s.docCount)

	// 更新词频统计
	seen := make(map[string]bool)
	tokens := bm25Tokenize(doc)
	for _, token := range tokens {
		if !seen[token] {
			seen[token] = true
			s.docFreq[token]++
		}
	}
}

// Score 计算 BM25 分数
// content: 文档内容
// keywords: 查询关键词列表
func (s *BM25Scorer) Score(content string, keywords []string) float32 {
	if len(keywords) == 0 {
		return 0
	}

	s.mu.RLock()
	k1 := s.k1
	b := s.b
	avgDocLen := s.avgDocLen
	docCount := s.docCount
	s.mu.RUnlock()

	// 文档长度
	docLen := float64(utf8.RuneCountInString(content))
	if docLen == 0 {
		return 0
	}

	// 文档分词并统计词频
	lowerContent := strings.ToLower(content)
	tokens := bm25Tokenize(lowerContent)
	termFreq := make(map[string]int)
	for _, token := range tokens {
		termFreq[token]++
	}

	var score float64
	for _, kw := range keywords {
		keyword := strings.ToLower(kw)

		// 获取词频 (term frequency)
		tf := float64(termFreq[keyword])

		// 如果关键词不在文档中，尝试部分匹配
		if tf == 0 {
			// 检查是否有包含关键词的 token
			for token, freq := range termFreq {
				if strings.Contains(token, keyword) || strings.Contains(keyword, token) {
					tf += float64(freq) * 0.5 // 部分匹配给一半权重
				}
			}
		}

		if tf == 0 {
			continue
		}

		// 计算 IDF (inverse document frequency)
		idf := s.idf(keyword, docCount)

		// BM25 公式
		// score = IDF * (tf * (k1 + 1)) / (tf + k1 * (1 - b + b * docLen / avgDocLen))
		numerator := tf * (k1 + 1)
		denominator := tf + k1*(1-b+b*docLen/avgDocLen)

		score += idf * numerator / denominator
	}

	// 归一化到 0-1 范围（近似）
	// 使用 sigmoid 风格的归一化
	normalized := score / (score + float64(len(keywords)))

	return float32(normalized)
}

// idf 计算逆文档频率
// IDF = log((N - n + 0.5) / (n + 0.5) + 1)
// 其中 N 是文档总数，n 是包含该词的文档数
func (s *BM25Scorer) idf(term string, docCount int64) float64 {
	s.mu.RLock()
	n := s.docFreq[term]
	s.mu.RUnlock()

	// 如果词不在索引中，假设它是稀有词
	if n == 0 {
		n = 1
	}

	N := float64(docCount)
	nf := float64(n)

	// BM25 IDF 公式，添加 +1 避免负值
	return math.Log((N-nf+0.5)/(nf+0.5) + 1)
}

// GetStats 获取当前统计信息
func (s *BM25Scorer) GetStats() (docCount int64, avgDocLen float64, vocabSize int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.docCount, s.avgDocLen, len(s.docFreq)
}

// bm25Tokenize BM25 专用分词
// 策略：按标点和空格分割，保留 2-6 字符的中文词组
func bm25Tokenize(text string) []string {
	var tokens []string

	// 分隔符
	separators := []string{
		" ", "\t", "\n", "\r",
		"，", "。", "！", "？", "；", "：", "、",
		",", ".", "!", "?", ";", ":", "/", "\\",
		"(", ")", "（", "）", "[", "]", "【", "】",
		"{", "}", "《", "》", "<", ">",
		"\"", "'",
		"\u201c", "\u201d", "\u2018", "\u2019", // 中文引号
		"-", "_", "=", "+", "|",
	}

	// 替换分隔符为空格
	result := text
	for _, sep := range separators {
		result = strings.ReplaceAll(result, sep, " ")
	}

	// 按空格分割
	parts := strings.Fields(result)

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		runeCount := utf8.RuneCountInString(part)

		// 短词直接添加
		if runeCount <= 6 {
			tokens = append(tokens, strings.ToLower(part))
			continue
		}

		// 长词进行滑动窗口分词
		runes := []rune(part)
		// 添加原词（截断）
		if runeCount > 6 {
			tokens = append(tokens, strings.ToLower(string(runes[:6])))
		}

		// 滑动窗口生成 2-4 字符的子串
		for windowSize := 2; windowSize <= 4 && windowSize <= len(runes); windowSize++ {
			for i := 0; i <= len(runes)-windowSize; i++ {
				subToken := strings.ToLower(string(runes[i : i+windowSize]))
				tokens = append(tokens, subToken)
			}
		}
	}

	return tokens
}

// countTermFrequency 统计关键词在内容中的出现次数
// 支持部分匹配
func countTermFrequency(content, keyword string) int {
	count := 0
	lowerContent := strings.ToLower(content)
	lowerKeyword := strings.ToLower(keyword)

	// 精确匹配计数
	idx := 0
	for {
		pos := strings.Index(lowerContent[idx:], lowerKeyword)
		if pos == -1 {
			break
		}
		count++
		idx += pos + len(lowerKeyword)
		if idx >= len(lowerContent) {
			break
		}
	}

	return count
}
