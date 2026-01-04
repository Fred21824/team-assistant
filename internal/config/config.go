package config

// Config 应用配置
type Config struct {
	Server   ServerConfig   `yaml:"Server"`
	MySQL    MySQLConfig    `yaml:"MySQL"`
	Redis    RedisConfig    `yaml:"Redis"`
	Lark     LarkConfig     `yaml:"Lark"`
	GitHub   GitHubConfig   `yaml:"GitHub"`
	LLM      LLMConfig      `yaml:"LLM"`
	Dify     DifyConfig     `yaml:"Dify"`
	VectorDB VectorDBConfig `yaml:"VectorDB"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Port int    `yaml:"Port"`
	Mode string `yaml:"Mode"` // debug, release
}

// MySQLConfig MySQL配置
type MySQLConfig struct {
	Host     string `yaml:"Host"`
	User     string `yaml:"User"`
	Password string `yaml:"Password"`
	Database string `yaml:"Database"`
	SkipSSL  bool   `yaml:"SkipSSL"` // 跳过 SSL 验证
}

// RedisConfig Redis配置
type RedisConfig struct {
	Host     string `yaml:"Host"`
	Password string `yaml:"Password"`
	DB       int    `yaml:"DB"`
}

// LarkConfig 飞书配置
type LarkConfig struct {
	Domain            string `yaml:"Domain"`            // https://open.larksuite.com 或 https://open.feishu.cn
	AppID             string `yaml:"AppID"`
	AppSecret         string `yaml:"AppSecret"`
	VerificationToken string `yaml:"VerificationToken"` // 事件验证Token
	EncryptKey        string `yaml:"EncryptKey"`        // 加密密钥（可选）
	BotOpenID         string `yaml:"BotOpenID"`         // 机器人的open_id
}

// GitHubConfig GitHub配置
type GitHubConfig struct {
	Token         string   `yaml:"Token"`         // Personal Access Token
	WebhookSecret string   `yaml:"WebhookSecret"` // Webhook Secret
	Organizations []string `yaml:"Organizations"` // 监控的组织
}

// LLMConfig LLM配置
type LLMConfig struct {
	Provider string `yaml:"Provider"` // groq, openai, anthropic
	APIKey   string `yaml:"APIKey"`
	Endpoint string `yaml:"Endpoint"` // API端点
	Model    string `yaml:"Model"`    // 模型名称
}

// DifyConfig Dify 配置
type DifyConfig struct {
	Enabled   bool   `yaml:"Enabled"`   // 是否启用 Dify
	BaseURL   string `yaml:"BaseURL"`   // Dify API 地址，如 http://localhost/v1
	APIKey    string `yaml:"APIKey"`    // Dify 应用 API Key
	DatasetID string `yaml:"DatasetID"` // 知识库 ID（可选）
}

// VectorDBConfig 向量数据库配置
type VectorDBConfig struct {
	Enabled          bool   `yaml:"Enabled"`          // 是否启用向量搜索
	QdrantEndpoint   string `yaml:"QdrantEndpoint"`   // Qdrant 地址，如 http://localhost:6333
	OllamaEndpoint   string `yaml:"OllamaEndpoint"`   // Ollama 地址，如 http://localhost:11434
	EmbeddingModel   string `yaml:"EmbeddingModel"`   // Embedding 模型，默认 nomic-embed-text
	CollectionName   string `yaml:"CollectionName"`   // 集合名称，默认 messages
}
