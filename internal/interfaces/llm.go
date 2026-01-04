package interfaces

import (
	"context"

	"team-assistant/internal/model"
	"team-assistant/pkg/llm"
)

// LLMClient LLM 客户端接口
type LLMClient interface {
	// 解析用户查询意图
	ParseUserQuery(ctx context.Context, query string) (*llm.ParsedQuery, error)

	// 生成回复
	GenerateResponse(ctx context.Context, query string, data interface{}) (string, error)

	// 总结消息
	SummarizeMessages(ctx context.Context, messages []string) (string, error)
}

// AIProcessor AI 处理器接口
type AIProcessor interface {
	// 处理用户查询
	ProcessQuery(ctx context.Context, userID, query string) (string, error)

	// 清除对话历史
	ClearConversation(userID string)
}

// DifyClient Dify 客户端接口
type DifyClient interface {
	// 聊天
	Chat(ctx context.Context, req interface{}) (interface{}, error)

	// 知识库搜索
	SearchKnowledge(ctx context.Context, datasetID string, req interface{}) (interface{}, error)

	// 上传文档到知识库
	UploadDocument(ctx context.Context, datasetID, name, content string) error
}

// CommitStats 提交统计（从 model 包导出，避免循环依赖）
type CommitStats = model.CommitStats
