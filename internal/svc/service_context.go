package svc

import (
	"database/sql"
	"fmt"

	"team-assistant/internal/config"
	"team-assistant/internal/model"
	"team-assistant/internal/repository"
	"team-assistant/internal/service"
	"team-assistant/pkg/dify"
	"team-assistant/pkg/lark"
	"team-assistant/pkg/llm"

	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
)

// ServiceContext 服务上下文
// 保持向后兼容，同时支持新的分层架构
type ServiceContext struct {
	Config config.Config

	// 基础设施（原有字段保持兼容）
	DB    *sql.DB
	Redis *redis.Client

	// 外部客户端（原有字段保持兼容）
	LarkClient *lark.Client

	// Models（原有字段保持兼容）
	MemberModel   *model.TeamMemberModel
	CommitModel   *model.GitCommitModel
	MessageModel  *model.ChatMessageModel
	GroupModel    *model.ChatGroupModel
	SyncTaskModel *model.MessageSyncTaskModel

	// ============================================================
	// 新架构组件
	// ============================================================

	// 外部客户端
	LLMClient  *llm.Client
	DifyClient *dify.Client

	// Repository 层
	ConversationRepo *repository.ConversationRepository

	// Service 层
	Services *Services
}

// Services 服务集合
type Services struct {
	Message *service.MessageService
	Chat    *service.ChatService
	Sync    *service.SyncService
	AI      *service.AIService
	RAG     *service.RAGService
}

// NewServiceContext 创建服务上下文
func NewServiceContext(c config.Config) (*ServiceContext, error) {
	// 初始化 MySQL
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		c.MySQL.User, c.MySQL.Password, c.MySQL.Host, c.MySQL.Database)

	if c.MySQL.SkipSSL {
		dsn += "&tls=skip-verify"
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MySQL: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping MySQL: %w", err)
	}

	// 配置连接池
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	// 初始化 Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     c.Redis.Host,
		Password: c.Redis.Password,
		DB:       c.Redis.DB,
	})

	// 初始化原有 Models（保持兼容）
	memberModel := model.NewTeamMemberModel(db)
	commitModel := model.NewGitCommitModel(db)
	messageModel := model.NewChatMessageModel(db)
	groupModel := model.NewChatGroupModel(db)
	syncTaskModel := model.NewMessageSyncTaskModel(db)

	// 初始化外部客户端
	larkClient := lark.NewClient(c.Lark.Domain, c.Lark.AppID, c.Lark.AppSecret)

	var llmClient *llm.Client
	if c.LLM.APIKey != "" {
		// 如果配置了代理，使用代理
		var proxyConfig *llm.ProxyConfig
		if c.LLM.ProxyHost != "" && c.LLM.ProxyPort > 0 {
			proxyConfig = &llm.ProxyConfig{
				Host:     c.LLM.ProxyHost,
				Port:     c.LLM.ProxyPort,
				User:     c.LLM.ProxyUser,
				Password: c.LLM.ProxyPassword,
			}
		}
		llmClient = llm.NewClientWithProxy(c.LLM.APIKey, c.LLM.Endpoint, c.LLM.Model, proxyConfig)
	}

	var difyClient *dify.Client
	if c.Dify.Enabled && c.Dify.APIKey != "" {
		difyClient = dify.NewClient(c.Dify.BaseURL, c.Dify.APIKey)
	}

	// 初始化 Repository
	conversationRepo := repository.NewConversationRepository(rdb)
	memberRepoAdapter := repository.NewMemberRepositoryAdapter(memberModel)
	commitRepoAdapter := repository.NewCommitRepositoryAdapter(commitModel)
	messageRepoAdapter := repository.NewMessageRepositoryAdapter(messageModel)
	groupRepoAdapter := repository.NewGroupRepositoryAdapter(groupModel)
	syncTaskRepoAdapter := repository.NewSyncTaskRepositoryAdapter(syncTaskModel)

	// 初始化 Service
	messageService := service.NewMessageService(messageRepoAdapter, groupRepoAdapter, larkClient)
	chatService := service.NewChatService(groupRepoAdapter, larkClient)
	syncService := service.NewSyncService(syncTaskRepoAdapter)
	aiService := service.NewAIService(
		commitRepoAdapter,
		messageRepoAdapter,
		memberRepoAdapter,
		conversationRepo,
		llmClient,
		difyClient,
		c.Dify.Enabled,
		c.Dify.DatasetID,
	)

	// 初始化永久记忆管理器
	aiService.InitMemoryManager(db, rdb)

	// 初始化 RAG 服务
	ragService := service.NewRAGService(
		c.VectorDB.QdrantEndpoint,
		c.VectorDB.OllamaEndpoint,
		c.VectorDB.EmbeddingModel,
		c.VectorDB.CollectionName,
		c.VectorDB.Enabled,
	)

	return &ServiceContext{
		Config: c,

		// 基础设施
		DB:    db,
		Redis: rdb,

		// 原有客户端
		LarkClient: larkClient,

		// 原有 Models
		MemberModel:   memberModel,
		CommitModel:   commitModel,
		MessageModel:  messageModel,
		GroupModel:    groupModel,
		SyncTaskModel: syncTaskModel,

		// 新客户端
		LLMClient:  llmClient,
		DifyClient: difyClient,

		// Repository
		ConversationRepo: conversationRepo,

		// Services
		Services: &Services{
			Message: messageService,
			Chat:    chatService,
			Sync:    syncService,
			AI:      aiService,
			RAG:     ragService,
		},
	}, nil
}

// Close 关闭所有连接
func (s *ServiceContext) Close() {
	if s.DB != nil {
		s.DB.Close()
	}
	if s.Redis != nil {
		s.Redis.Close()
	}
}
