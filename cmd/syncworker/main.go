package main

import (
	"context"
	"database/sql"
	"flag"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"gopkg.in/yaml.v3"

	"team-assistant/internal/collector"
	"team-assistant/internal/config"
	"team-assistant/internal/model"
	"team-assistant/internal/service"
	"team-assistant/internal/svc"
	"team-assistant/pkg/lark"
	"team-assistant/pkg/llm"
)

var (
	configFile  = flag.String("f", "etc/config.yaml", "config file path")
	workers     = flag.Int("w", 3, "number of parallel workers")
	interval    = flag.Duration("i", 2*time.Second, "check interval")
)

func main() {
	flag.Parse()

	// 加载配置
	data, err := os.ReadFile(*configFile)
	if err != nil {
		log.Fatalf("Failed to read config: %v", err)
	}
	var cfg config.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("Failed to parse config: %v", err)
	}

	// 连接数据库
	dsn := cfg.MySQL.User + ":" + cfg.MySQL.Password + "@tcp(" + cfg.MySQL.Host + ")/" + cfg.MySQL.Database + "?charset=utf8mb4&parseTime=True&loc=Local"
	if cfg.MySQL.SkipSSL {
		dsn += "&tls=skip-verify"
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("Failed to connect to MySQL: %v", err)
	}
	defer db.Close()

	// 创建 LLM 客户端（支持代理）
	var proxyConfig *llm.ProxyConfig
	if cfg.LLM.ProxyHost != "" && cfg.LLM.ProxyPort > 0 {
		proxyConfig = &llm.ProxyConfig{
			Host:     cfg.LLM.ProxyHost,
			Port:     cfg.LLM.ProxyPort,
			User:     cfg.LLM.ProxyUser,
			Password: cfg.LLM.ProxyPassword,
		}
	}
	llmClient := llm.NewClientWithProxy(cfg.LLM.APIKey, cfg.LLM.Endpoint, cfg.LLM.Model, proxyConfig)

	// 初始化服务上下文
	svcCtx := &svc.ServiceContext{
		Config:        cfg,
		DB:            db,
		LarkClient:    lark.NewClient(cfg.Lark.Domain, cfg.Lark.AppID, cfg.Lark.AppSecret),
		MessageModel:  model.NewChatMessageModel(db),
		SyncTaskModel: model.NewMessageSyncTaskModel(db),
		LLMClient:     llmClient,
		Services:      &svc.Services{},
	}

	// 初始化 RAG 服务（如果启用）
	if cfg.VectorDB.Enabled {
		ragService := service.NewRAGService(
			cfg.VectorDB.QdrantEndpoint,
			cfg.VectorDB.OllamaEndpoint,
			cfg.VectorDB.EmbeddingModel,
			cfg.VectorDB.CollectionName,
			true,
		)
		svcCtx.Services.RAG = ragService
		log.Println("RAG service initialized")
	}

	// 创建同步器池
	pool := NewSyncPool(svcCtx, *workers, *interval)

	// 优雅关闭
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	log.Printf("Sync worker started with %d workers, interval: %v", *workers, *interval)
	pool.Start()

	<-sigChan
	log.Println("Shutting down...")
	pool.Stop()
	log.Println("Sync worker stopped")
}

// SyncPool 同步器池，支持并行处理
type SyncPool struct {
	svcCtx   *svc.ServiceContext
	workers  int
	interval time.Duration
	stopChan chan struct{}
	wg       sync.WaitGroup
}

func NewSyncPool(svcCtx *svc.ServiceContext, workers int, interval time.Duration) *SyncPool {
	return &SyncPool{
		svcCtx:   svcCtx,
		workers:  workers,
		interval: interval,
		stopChan: make(chan struct{}),
	}
}

func (p *SyncPool) Start() {
	// 启动多个 worker
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
}

func (p *SyncPool) Stop() {
	close(p.stopChan)
	p.wg.Wait()
}

func (p *SyncPool) worker(id int) {
	defer p.wg.Done()

	syncer := collector.NewMessageSyncer(p.svcCtx)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	log.Printf("Worker %d started", id)

	for {
		select {
		case <-p.stopChan:
			log.Printf("Worker %d stopping", id)
			return
		case <-ticker.C:
			p.processOne(syncer, id)
		}
	}
}

func (p *SyncPool) processOne(syncer *collector.MessageSyncer, workerID int) {
	ctx := context.Background()

	// 获取并锁定一个任务（使用 FOR UPDATE SKIP LOCKED 避免冲突）
	task, err := p.claimTask(ctx)
	if err != nil {
		if err != sql.ErrNoRows {
			log.Printf("Worker %d: failed to claim task: %v", workerID, err)
		}
		return
	}

	log.Printf("Worker %d: claimed task %d for chat %s", workerID, task.ID, task.ChatID)

	// 持续处理直到任务完成
	for {
		select {
		case <-p.stopChan:
			log.Printf("Worker %d: stopping, task %d will resume later", workerID, task.ID)
			return
		default:
		}

		// 执行一批同步
		if err := syncer.SyncTask(ctx, task); err != nil {
			log.Printf("Worker %d: task %d failed: %v", workerID, task.ID, err)
			p.svcCtx.SyncTaskModel.MarkFailed(ctx, task.ID, err.Error())
			return
		}

		// 重新读取任务状态，检查是否完成
		updatedTask, err := p.getTaskStatus(ctx, task.ID)
		if err != nil {
			log.Printf("Worker %d: failed to get task status: %v", workerID, err)
			return
		}

		if updatedTask.Status == "completed" {
			log.Printf("Worker %d: task %d completed", workerID, task.ID)
			return
		}

		// 更新本地任务状态（page_token 等）
		task = updatedTask

		// 短暂休息避免频繁请求
		time.Sleep(500 * time.Millisecond)
	}
}

// getTaskStatus 获取任务当前状态
func (p *SyncPool) getTaskStatus(ctx context.Context, taskID int64) (*model.MessageSyncTask, error) {
	query := `SELECT id, chat_id, chat_name, status, total_messages, synced_messages,
              page_token, start_time, end_time, error_msg, requested_by,
              started_at, finished_at, created_at, updated_at
              FROM message_sync_tasks WHERE id = ?`
	var task model.MessageSyncTask
	err := p.svcCtx.DB.QueryRowContext(ctx, query, taskID).Scan(
		&task.ID, &task.ChatID, &task.ChatName, &task.Status, &task.TotalMessages, &task.SyncedMessages,
		&task.PageToken, &task.StartTime, &task.EndTime, &task.ErrorMsg, &task.RequestedBy,
		&task.StartedAt, &task.FinishedAt, &task.CreatedAt, &task.UpdatedAt)
	return &task, err
}

// claimTask 获取并锁定一个待处理任务
func (p *SyncPool) claimTask(ctx context.Context) (*model.MessageSyncTask, error) {
	// 使用事务 + FOR UPDATE SKIP LOCKED 来避免多个 worker 抢同一个任务
	// 只选择 pending 状态的任务，running 的任务由其对应的 worker 继续处理
	tx, err := p.svcCtx.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}

	query := `SELECT id, chat_id, chat_name, status, total_messages, synced_messages,
              page_token, start_time, end_time, error_msg, requested_by,
              started_at, finished_at, created_at, updated_at
              FROM message_sync_tasks
              WHERE status = 'pending'
              ORDER BY created_at ASC
              LIMIT 1
              FOR UPDATE SKIP LOCKED`

	var task model.MessageSyncTask
	err = tx.QueryRowContext(ctx, query).Scan(
		&task.ID, &task.ChatID, &task.ChatName, &task.Status, &task.TotalMessages, &task.SyncedMessages,
		&task.PageToken, &task.StartTime, &task.EndTime, &task.ErrorMsg, &task.RequestedBy,
		&task.StartedAt, &task.FinishedAt, &task.CreatedAt, &task.UpdatedAt)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	// 标记为运行中
	_, err = tx.ExecContext(ctx, "UPDATE message_sync_tasks SET status = 'running', started_at = NOW() WHERE id = ?", task.ID)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &task, nil
}
