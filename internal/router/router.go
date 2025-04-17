package router

import (
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	v1 "city.newnan/k8s-console/api/v1"
	"city.newnan/k8s-console/internal/config"
	"city.newnan/k8s-console/internal/middleware"
)

// SetupRouter 设置路由
func SetupRouter(cfg *config.Config) *gin.Engine {
	// 设置Gin模式
	gin.SetMode(cfg.Mode)

	// 创建路由引擎
	r := gin.New()

	// 使用中间件
	r.Use(gin.Logger())
	r.Use(gin.Recovery())

	// 配置跨域
	corsConfig := cors.DefaultConfig()
	corsConfig.AllowOrigins = cfg.AllowedOrigins
	corsConfig.AllowCredentials = true
	corsConfig.AllowMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	corsConfig.AllowHeaders = []string{"Origin", "Content-Type", "Accept", "Authorization"}
	r.Use(cors.New(corsConfig))

	// 默认路由
	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "欢迎使用K8s Console API",
		})
	})

	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})

	// API文档
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// 创建控制器实例
	userController := v1.NewUserController(cfg)
	roleController := v1.NewRoleController()
	realtimeController := v1.NewRealtimeController()

	// API v1 路由组
	api := r.Group("/api/v1")
	{
		// 公开路由
		api.POST("/user/register", userController.Register)
		api.POST("/user/login", userController.Login)
		api.POST("/user/logout", userController.Logout)

		// 需要认证的路由
		auth := api.Group("")
		auth.Use(middleware.JWTAuth(cfg))
		{
			// 用户相关
			auth.GET("/user/profile", userController.GetProfile)
			auth.PUT("/user/profile", userController.UpdateProfile)
			auth.GET("/user/refresh-token", userController.RefreshToken)

			// 实时通信
			auth.GET("/ws", realtimeController.HandleWebSocket)
			auth.GET("/sse", realtimeController.HandleSSE)
			auth.GET("/realtime/stats", realtimeController.GetRealtimeStats)

			// 需要权限验证的路由
			authorized := auth.Group("")
			authorized.Use(middleware.Authorize())
			{
				// 用户管理
				authorized.GET("/users", userController.ListUsers)
				authorized.GET("/users/:id", userController.GetUser)
				authorized.PUT("/users/:id", userController.UpdateUser)
				authorized.DELETE("/users/:id", userController.DeleteUser)
				authorized.PUT("/users/:id/disable", userController.DisableUser)
				authorized.PUT("/users/:id/enable", userController.EnableUser)
				authorized.PUT("/users/:id/role", userController.ChangeUserRole)

				// 角色管理
				authorized.GET("/roles", roleController.ListRoles)
				authorized.GET("/roles/:id", roleController.GetRole)
				authorized.POST("/roles", roleController.CreateRole)
				authorized.PUT("/roles/:id", roleController.UpdateRole)
				authorized.DELETE("/roles/:id", roleController.DeleteRole)
				authorized.GET("/roles/:id/permissions", roleController.GetRolePermissions)
				authorized.POST("/roles/:id/permissions", roleController.AddRolePermission)
				authorized.DELETE("/roles/:id/permissions", roleController.RemoveRolePermission)

				// 实时通信管理（仅管理员可用）
				authorized.POST("/ws/broadcast", realtimeController.BroadcastMessage)
				authorized.POST("/sse/publish", realtimeController.PublishSSEEvent)
			}
		}
	}

	return r
}
