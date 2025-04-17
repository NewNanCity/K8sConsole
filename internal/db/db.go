package db

import (
	"fmt"
	"log"

	"city.newnan/k8s-console/internal/config"
	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	// DB 全局数据库连接实例
	DB *gorm.DB
)

// InitDB 初始化数据库连接
func InitDB(cfg *config.Config) error {
	var err error
	var dialector gorm.Dialector

	switch cfg.DBType {
	case "mysql":
		dsn := cfg.GetDBConnString()
		dialector = mysql.Open(dsn)
	case "sqlite":
		dialector = sqlite.Open(cfg.DBPath)
	default:
		return fmt.Errorf("不支持的数据库类型: %s", cfg.DBType)
	}

	// 配置GORM选项
	gormConfig := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	}

	// 初始化数据库连接
	DB, err = gorm.Open(dialector, gormConfig)
	if err != nil {
		return fmt.Errorf("连接数据库失败: %w", err)
	}

	log.Printf("成功连接到数据库: %s", cfg.DBType)
	return nil
}

// CloseDB 关闭数据库连接
func CloseDB() {
	if DB != nil {
		sqlDB, err := DB.DB()
		if err != nil {
			log.Printf("获取原生数据库连接失败: %v", err)
			return
		}
		if err := sqlDB.Close(); err != nil {
			log.Printf("关闭数据库连接失败: %v", err)
		}
	}
}

// AutoMigrate 自动迁移模型到数据库
func AutoMigrate(models ...interface{}) error {
	if DB == nil {
		return fmt.Errorf("数据库未初始化")
	}
	return DB.AutoMigrate(models...)
}
