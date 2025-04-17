package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"city.newnan/k8s-console/internal/config"
	"city.newnan/k8s-console/internal/db"
	"city.newnan/k8s-console/internal/middleware"
	"city.newnan/k8s-console/internal/model"
	"city.newnan/k8s-console/internal/router"
	"city.newnan/k8s-console/internal/service"
	"city.newnan/k8s-console/internal/sse"
	"city.newnan/k8s-console/internal/websocket"
)

// @title           K8s Console API
// @version         1.0
// @description     Kubernetes 管理控制台 API
// @termsOfService  http://swagger.io/terms/

// @contact.name   API 支持
// @contact.url    http://www.newnan.city/support
// @contact.email  support@newnan.city

// @license.name  MIT
// @license.url   https://opensource.org/licenses/MIT

// @host      localhost:8080
// @BasePath  /

// @securityDefinitions.apikey  ApiKeyAuth
// @in                          header
// @name                        Authorization
// @description                 Bearer 认证, 例如: "Bearer {token}"

func main() {
	// 加载配置
	cfg := config.LoadConfig()

	// 初始化数据库
	if err := db.InitDB(cfg); err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}
	defer db.CloseDB()

	// 数据库模型自动迁移
	if err := db.AutoMigrate(&model.User{}, &model.Role{}); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}

	// 初始化Casbin
	if err := middleware.InitCasbin(cfg.CasbinModelPath); err != nil {
		log.Fatalf("初始化Casbin失败: %v", err)
	}

	// 设置初始角色和权限
	roleService := service.NewRoleService()
	if err := roleService.SetupInitialRoles(); err != nil {
		log.Printf("设置初始角色和权限失败: %v", err)
	}

	// 启动WebSocket管理器
	websocket.GlobalManager.Start()

	// 启动SSE代理
	sse.GlobalBroker.Start()

	// 初始化路由
	r := router.SetupRouter(cfg)

	// 创建HTTP服务器
	srv := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", cfg.ServerHost, cfg.ServerPort),
		Handler: r,
	}

	// 启动服务器（非阻塞）
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("监听失败: %v", err)
		}
	}()

	log.Printf("服务器开始运行，监听: %s:%d", cfg.ServerHost, cfg.ServerPort)

	// 等待中断信号以优雅地关闭服务器
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("正在关闭服务器...")

	// 设置关闭超时
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("服务器被强制关闭:", err)
	}

	log.Println("服务器优雅退出")
}
