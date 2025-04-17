package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config 存储应用程序配置
type Config struct {
	// 服务器配置
	ServerPort     int
	ServerHost     string
	Mode           string
	AllowedOrigins []string

	// 数据库配置
	DBType     string
	DBHost     string
	DBPort     int
	DBUser     string
	DBPassword string
	DBName     string
	DBPath     string // 用于SQLite

	// JWT配置
	JWTSecret         string
	JWTExpireTime     time.Duration
	JWTRefreshTime    time.Duration
	JWTIssuer         string
	JWTCookieSecure   bool
	JWTCookieHTTPOnly bool

	// 路径配置
	CasbinModelPath string
	LogPath         string
	SwaggerPath     string
}

// GetEnv 从环境变量中获取字符串值，如果不存在则返回默认值
func GetEnv(key, defaultValue string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		return defaultValue
	}
	return value
}

// GetEnvInt 从环境变量中获取整数值，如果不存在或解析失败则返回默认值
func GetEnvInt(key string, defaultValue int) int {
	value, exists := os.LookupEnv(key)
	if !exists {
		return defaultValue
	}
	intValue, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return intValue
}

// GetEnvBool 从环境变量中获取布尔值，如果不存在则返回默认值
func GetEnvBool(key string, defaultValue bool) bool {
	value, exists := os.LookupEnv(key)
	if !exists {
		return defaultValue
	}
	boolValue, err := strconv.ParseBool(value)
	if err != nil {
		return defaultValue
	}
	return boolValue
}

// GetEnvDuration 从环境变量中获取时间间隔，如果不存在则返回默认值
func GetEnvDuration(key string, defaultValue time.Duration) time.Duration {
	value, exists := os.LookupEnv(key)
	if !exists {
		return defaultValue
	}
	durationValue, err := time.ParseDuration(value)
	if err != nil {
		return defaultValue
	}
	return durationValue
}

// LoadConfig 从环境变量加载配置
func LoadConfig() *Config {
	return &Config{
		// 服务器配置
		ServerPort:     GetEnvInt("SERVER_PORT", 8080),
		ServerHost:     GetEnv("SERVER_HOST", "0.0.0.0"),
		Mode:           GetEnv("GIN_MODE", "debug"),
		AllowedOrigins: []string{GetEnv("ALLOWED_ORIGINS", "*")},

		// 数据库配置
		DBType:     GetEnv("DB_TYPE", "sqlite"),
		DBHost:     GetEnv("DB_HOST", "localhost"),
		DBPort:     GetEnvInt("DB_PORT", 3306),
		DBUser:     GetEnv("DB_USER", "root"),
		DBPassword: GetEnv("DB_PASSWORD", "password"),
		DBName:     GetEnv("DB_NAME", "k8sconsole"),
		DBPath:     GetEnv("DB_PATH", "k8sconsole.db"),

		// JWT配置
		JWTSecret:         GetEnv("JWT_SECRET", "your-secret-key"),
		JWTExpireTime:     GetEnvDuration("JWT_EXPIRE_TIME", 24*time.Hour),
		JWTRefreshTime:    GetEnvDuration("JWT_REFRESH_TIME", 7*24*time.Hour),
		JWTIssuer:         GetEnv("JWT_ISSUER", "k8sconsole"),
		JWTCookieSecure:   GetEnvBool("JWT_COOKIE_SECURE", false),
		JWTCookieHTTPOnly: GetEnvBool("JWT_COOKIE_HTTP_ONLY", true),

		// 路径配置
		CasbinModelPath: GetEnv("CASBIN_MODEL_PATH", "config/rbac_model.conf"),
		LogPath:         GetEnv("LOG_PATH", "logs"),
		SwaggerPath:     GetEnv("SWAGGER_PATH", "docs/swagger"),
	}
}

// GetDBConnString 根据数据库类型返回相应的连接字符串
func (c *Config) GetDBConnString() string {
	switch c.DBType {
	case "mysql":
		return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
			c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName)
	case "sqlite":
		return c.DBPath
	default:
		return c.DBPath
	}
}
