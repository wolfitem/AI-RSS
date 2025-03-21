package model

// ProcessParams 包含处理RSS的所有参数
type ProcessParams struct {
	OpmlFile       string         // OPML文件路径
	OutputFile     string         // 输出文件路径
	DaysBack       int            // 获取几天内的文章
	DeepseekConfig DeepseekConfig // Deepseek API配置
	PromptTemplate string         // 提示词模板
	DatabaseConfig DatabaseConfig // 数据库配置
	RssConfig      RssConfig      // RSS获取配置
}

// DeepseekConfig 包含Deepseek API的配置信息
type DeepseekConfig struct {
	APIKey    string // API密钥
	Model     string // 模型名称
	MaxTokens int    // 最大令牌数
	MaxCalls  int    // 最大调用次数
	APIUrl    string // API接口地址
}

// DatabaseConfig 包含数据库的配置信息
type DatabaseConfig struct {
	Enabled  bool   // 是否启用数据库
	FilePath string // 数据库文件路径
}

// RssConfig 包含RSS获取的配置信息
type RssConfig struct {
	Timeout          int // RSS源获取超时时间（秒）
	Concurrency      int // 并发获取RSS源的数量
	MaxRetries       int // 最大重试次数
	ResponseTimeout  int // 响应读取超时时间（秒）
	OverallTimeout   int // 整体操作超时时间（秒）
	RetryBackoffBase int // 重试退避基数（秒）
}

// RssSource 表示一个RSS源
type RssSource struct {
	Title  string // RSS源标题
	XmlUrl string // RSS源URL
}

// Article 表示一篇文章
type Article struct {
	Title       string    // 文章标题
	Content     string    // 文章内容
	Link        string    // 文章链接
	PublishDate string    // 发布日期
	Source      RssSource // 来源RSS
}

// AnalysisResult 表示分析结果
type AnalysisResult struct {
	Title         string // 标题
	Summary       string // 摘要
	SummaryLength int    // 摘要长度
	Source        string // 来源
	PubDate       string // 发布日期
	Category      string // 分类
	Link          string // 文章链接
}
