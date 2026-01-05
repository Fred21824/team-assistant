package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"team-assistant/internal/collector"
	"team-assistant/internal/config"
	"team-assistant/internal/handler"
	"team-assistant/internal/svc"

	"gopkg.in/yaml.v3"
)

var configFile = flag.String("f", "etc/config.yaml", "the config file")

func main() {
	flag.Parse()

	// 加载配置
	data, err := os.ReadFile(*configFile)
	if err != nil {
		log.Fatalf("Failed to read config file: %v", err)
	}

	var cfg config.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("Failed to parse config: %v", err)
	}

	// 初始化服务上下文
	svcCtx, err := svc.NewServiceContext(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize service context: %v", err)
	}
	defer svcCtx.Close()

	// 启动 GitHub 定时采集器（每小时采集一次）
	var githubCollector *collector.GitHubCollector
	if cfg.GitHub.Token != "" {
		githubCollector = collector.NewGitHubCollector(svcCtx, 1*time.Hour)
		go githubCollector.Start()
		log.Println("GitHub collector enabled")
	} else {
		log.Println("GitHub collector disabled (no token configured)")
	}

	// 消息同步器（仅用于创建任务，不再自动运行同步）
	// 同步任务由独立的 syncworker 进程处理
	msgSyncer := collector.NewMessageSyncer(svcCtx)
	log.Println("Message syncer initialized (sync handled by separate worker process)")

	// 启动 Dify 知识库同步器
	var difySyncer *collector.DifySyncer
	if cfg.Dify.Enabled && cfg.Dify.DatasetID != "" {
		difySyncer = collector.NewDifySyncer(svcCtx)
		if difySyncer != nil {
			go difySyncer.Start()
			log.Println("Dify syncer started")
		}
	} else {
		log.Println("Dify syncer disabled (not enabled or no dataset ID)")
	}

	// 创建路由
	mux := http.NewServeMux()

	// 飞书Webhook
	larkHandler := handler.NewLarkWebhookHandler(svcCtx)
	larkHandler.SetMessageSyncer(msgSyncer)
	mux.HandleFunc("/webhook/lark", larkHandler.Handle)

	// GitHub Webhook
	githubHandler := handler.NewGitHubWebhookHandler(svcCtx)
	mux.HandleFunc("/webhook/github", githubHandler.Handle)

	// 健康检查
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// API路由
	mux.HandleFunc("/api/stats", handler.NewStatsHandler(svcCtx).Handle)
	mux.HandleFunc("/api/members", handler.NewMemberHandler(svcCtx).Handle)

	// 手动触发采集
	mux.HandleFunc("/api/collect", func(w http.ResponseWriter, r *http.Request) {
		if githubCollector != nil {
			go githubCollector.CollectOnce()
			w.Write([]byte(`{"status": "collection started"}`))
		} else {
			w.Write([]byte(`{"status": "collector not configured"}`))
		}
	})

	// 启动服务器
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// 优雅关闭
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Println("Shutting down server...")
		if githubCollector != nil {
			githubCollector.Stop()
		}
		msgSyncer.Stop()
		if difySyncer != nil {
			difySyncer.Stop()
		}
		server.Close()
	}()

	log.Printf("Team Assistant starting on %s", addr)
	log.Printf("Lark webhook: http://localhost%s/webhook/lark", addr)
	log.Printf("GitHub webhook: http://localhost%s/webhook/github", addr)
	log.Printf("API endpoints:")
	log.Printf("  - GET  /api/stats?start=2024-01-01&end=2024-01-31")
	log.Printf("  - GET  /api/members")
	log.Printf("  - POST /api/members")
	log.Printf("  - POST /api/collect (trigger GitHub collection)")

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}

	log.Println("Server stopped")
}
