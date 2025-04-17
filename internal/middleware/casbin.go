package middleware

import (
	"net/http"

	"github.com/casbin/casbin/v2"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/gin-gonic/gin"

	"city.newnan/k8s-console/internal/db"
	"city.newnan/k8s-console/internal/model"
)

var (
	enforcer *casbin.Enforcer
)

// InitCasbin 初始化Casbin
func InitCasbin(modelPath string) error {
	// 创建适配器
	adapter, err := gormadapter.NewAdapterByDB(db.DB)
	if err != nil {
		return err
	}

	// 创建执行器
	enforcer, err = casbin.NewEnforcer(modelPath, adapter)
	if err != nil {
		return err
	}

	// 加载策略
	if err := enforcer.LoadPolicy(); err != nil {
		return err
	}

	return nil
}

// GetEnforcer 获取Casbin执行器
func GetEnforcer() *casbin.Enforcer {
	return enforcer
}

// Authorize 授权中间件
func Authorize() gin.HandlerFunc {
	return func(c *gin.Context) {
		if enforcer == nil {
			c.JSON(http.StatusInternalServerError, model.ErrorResponse(500, "权限系统未初始化"))
			c.Abort()
			return
		}

		// 获取当前用户信息
		roleName, exists := c.Get("role_name")
		if !exists {
			c.JSON(http.StatusUnauthorized, model.ErrorResponse(401, "未授权: 无法获取用户角色"))
			c.Abort()
			return
		}

		// 获取请求路径和方法
		obj := c.Request.URL.Path
		act := c.Request.Method

		// 检查权限
		ok, err := enforcer.Enforce(roleName, obj, act)
		if err != nil {
			c.JSON(http.StatusInternalServerError, model.ErrorResponse(500, "权限检查失败: "+err.Error()))
			c.Abort()
			return
		}

		if !ok {
			c.JSON(http.StatusForbidden, model.ErrorResponse(403, "权限不足: 无权访问此资源"))
			c.Abort()
			return
		}

		c.Next()
	}
}

// AddPolicy 添加策略
func AddPolicy(role, path, method string) (bool, error) {
	return enforcer.AddPolicy(role, path, method)
}

// RemovePolicy 移除策略
func RemovePolicy(role, path, method string) (bool, error) {
	return enforcer.RemovePolicy(role, path, method)
}

// AddRoleForUser 为用户添加角色
func AddRoleForUser(user, role string) (bool, error) {
	return enforcer.AddRoleForUser(user, role)
}

// GetRolesForUser 获取用户的所有角色
func GetRolesForUser(user string) ([]string, error) {
	return enforcer.GetRolesForUser(user)
}

// GetPermissionsForRole 获取角色的所有权限
func GetPermissionsForRole(role string) [][]string {
	return enforcer.GetPermissionsForUser(role)
}
