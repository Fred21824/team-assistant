package config

// Config 应用配置
type Config struct {
	Server      ServerConfig      `yaml:"Server"`
	MySQL       MySQLConfig       `yaml:"MySQL"`
	Redis       RedisConfig       `yaml:"Redis"`
	Lark        LarkConfig        `yaml:"Lark"`
	GitHub      GitHubConfig      `yaml:"GitHub"`
	LLM         LLMConfig         `yaml:"LLM"`
	Dify        DifyConfig        `yaml:"Dify"`
	VectorDB    VectorDBConfig    `yaml:"VectorDB"`
	Bitable     BitableConfig     `yaml:"Bitable"`
	AutoSync    AutoSyncConfig    `yaml:"AutoSync"`
	Permissions PermissionsConfig `yaml:"Permissions"`
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
	// 代理配置（用于香港等受限地区访问 Claude API）
	ProxyHost     string `yaml:"ProxyHost"`     // 代理主机，如 52.41.128.82
	ProxyPort     int    `yaml:"ProxyPort"`     // 代理端口，如 9662
	ProxyUser     string `yaml:"ProxyUser"`     // 代理用户名
	ProxyPassword string `yaml:"ProxyPassword"` // 代理密码
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

// BitableConfig 多维表格配置
type BitableConfig struct {
	Enabled  bool   `yaml:"Enabled"`  // 是否启用 Bitable 查询
	AppToken string `yaml:"AppToken"` // 多维表格 App Token
	TableID  string `yaml:"TableID"`  // 表格 ID
}

// AutoSyncConfig 定时增量同步配置
type AutoSyncConfig struct {
	Enabled bool                 `yaml:"Enabled"` // 是否启用定时同步
	Chats   []AutoSyncChatConfig `yaml:"Chats"`   // 需要同步的群列表
}

// AutoSyncChatConfig 单个群的同步配置
type AutoSyncChatConfig struct {
	ChatID          string `yaml:"ChatID"`          // 群ID
	Name            string `yaml:"Name"`            // 群名（仅用于日志）
	Interval        int    `yaml:"Interval"`        // 同步间隔（秒），最小10秒
	LookbackMinutes int    `yaml:"LookbackMinutes"` // 每次拉取最近多少分钟的消息
}

// PermissionsConfig 权限控制配置
type PermissionsConfig struct {
	// 私聊白名单：只有这些用户可以使用私聊功能（用户名列表，不区分大小写）
	PrivateChatAllowedUsers []string `yaml:"PrivateChatAllowedUsers"`
	// 群聊最小成员数：只有成员数 >= 此值的群才能使用机器人
	GroupMinMembers int `yaml:"GroupMinMembers"`
}
