package config

import (
	"log"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	OCR      OCRConfig      `mapstructure:"ocr"`
	Worker   WorkerConfig   `mapstructure:"worker"`
	RSS      RSSConfig      `mapstructure:"rss"`
	Security SecurityConfig `mapstructure:"security"`
}

type ServerConfig struct {
	Port         string        `mapstructure:"port"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

type DatabaseConfig struct {
	Host         string `mapstructure:"host"`
	Port         string `mapstructure:"port"`
	User         string `mapstructure:"user"`
	Password     string `mapstructure:"password"`
	DBName       string `mapstructure:"db_name"`
	MaxOpenConns int    `mapstructure:"max_open_conns"`
	MaxIdleConns int    `mapstructure:"max_idle_conns"`
}

type OCRConfig struct {
	BaiduAPIKey    string `mapstructure:"baidu_api_key"`
	BaiduSecretKey string `mapstructure:"baidu_secret_key"`
}

type WorkerConfig struct {
	Concurrency  int `mapstructure:"concurrency"`
	RateLimitQPS int `mapstructure:"rate_limit_qps"`
}

type RSSConfig struct {
	FeedURL   string `mapstructure:"feed_url"`
	UpdateURL string `mapstructure:"update_url"`
}

type SecurityConfig struct {
	AccountAddSalt string `mapstructure:"account_add_salt"`
}

func Load() *Config {
	viper.SetConfigName(".env")
	viper.SetConfigType("env")
	viper.AddConfigPath(".")
	viper.AddConfigPath("..")

	// 设置环境变量映射
	viper.SetEnvPrefix("")
	viper.AutomaticEnv()

	// 设置默认值
	viper.SetDefault("PORT", "6382")
	viper.SetDefault("DB_HOST", "localhost")
	viper.SetDefault("DB_PORT", "3306")
	viper.SetDefault("DB_NAME", "wjdr")
	viper.SetDefault("HTTP_READ_TIMEOUT", "10s")
	viper.SetDefault("HTTP_WRITE_TIMEOUT", "15s")
	viper.SetDefault("DB_MAX_OPEN_CONNS", 50)
	viper.SetDefault("DB_MAX_IDLE_CONNS", 20)
	viper.SetDefault("WORKER_CONCURRENCY", 16)
	viper.SetDefault("RATE_LIMIT_QPS", 8)
	viper.SetDefault("RSS_FEED_URL", "http://120.48.143.190:10082/feedAtom/4af6b7ea933926777b95712e9ec3fb1a")
	viper.SetDefault("RSS_UPDATE_URL", "http://120.48.143.190:10082/updateFeedAll?key=313b1e3098a7e7765260e9b51e16a47a")
	viper.SetDefault("ACCOUNT_ADD_SALT", "8$#@!@#J$%^&*T()_+L")

	if err := viper.ReadInConfig(); err != nil {
		log.Printf("配置文件读取失败，使用环境变量: %v", err)
	}

	var config Config

	// 映射环境变量到配置结构
	config.Server.Port = viper.GetString("PORT")
	config.Server.ReadTimeout = viper.GetDuration("HTTP_READ_TIMEOUT")
	config.Server.WriteTimeout = viper.GetDuration("HTTP_WRITE_TIMEOUT")

	config.Database.Host = viper.GetString("DB_HOST")
	config.Database.Port = viper.GetString("DB_PORT")
	config.Database.User = viper.GetString("DB_USER")
	config.Database.Password = viper.GetString("DB_PASSWORD")
	config.Database.DBName = viper.GetString("DB_NAME")
	config.Database.MaxOpenConns = viper.GetInt("DB_MAX_OPEN_CONNS")
	config.Database.MaxIdleConns = viper.GetInt("DB_MAX_IDLE_CONNS")

	config.OCR.BaiduAPIKey = viper.GetString("BAIDU_API_KEY")
	config.OCR.BaiduSecretKey = viper.GetString("BAIDU_SECRET_KEY")

	config.Worker.Concurrency = viper.GetInt("WORKER_CONCURRENCY")
	config.Worker.RateLimitQPS = viper.GetInt("RATE_LIMIT_QPS")

	config.RSS.FeedURL = viper.GetString("RSS_FEED_URL")
	config.RSS.UpdateURL = viper.GetString("RSS_UPDATE_URL")

	config.Security.AccountAddSalt = viper.GetString("ACCOUNT_ADD_SALT")

	return &config
}
