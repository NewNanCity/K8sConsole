package v1

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"city.newnan/k8s-console/internal/config"
	"city.newnan/k8s-console/internal/middleware"
	"city.newnan/k8s-console/internal/model"
	"city.newnan/k8s-console/internal/service"
)

// UserController 用户相关API控制器
type UserController struct {
	UserService *service.UserService
	Config      *config.Config
}

// NewUserController 创建用户控制器
func NewUserController(cfg *config.Config) *UserController {
	return &UserController{
		UserService: service.NewUserService(cfg),
		Config:      cfg,
	}
}

// Register 用户注册
// @Summary 用户注册
// @Description 创建新用户账号
// @Tags 用户管理
// @Accept json
// @Produce json
// @Param user body model.UserRegister true "用户注册信息"
// @Success 200 {object} model.Response{data=model.UserResponse} "注册成功"
// @Failure 400 {object} model.Response "请求参数错误"
// @Failure 500 {object} model.Response "服务器内部错误"
// @Router /api/v1/user/register [post]
func (c *UserController) Register(ctx *gin.Context) {
	var req model.UserRegister
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, model.ErrorResponse(http.StatusBadRequest, "无效的请求参数: "+err.Error()))
		return
	}

	user, token, err := c.UserService.Register(req)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, model.ErrorResponse(http.StatusInternalServerError, "注册失败: "+err.Error()))
		return
	}

	// 设置JWT Token到Cookie
	ctx.SetCookie(
		"token",
		token,
		int(c.Config.JWTExpireTime.Seconds()),
		"/",
		"",
		c.Config.JWTCookieSecure,
		c.Config.JWTCookieHTTPOnly,
	)

	ctx.JSON(http.StatusOK, model.SuccessResponse(map[string]interface{}{
		"user":  user.ToUserResponse(),
		"token": token,
	}))
}

// Login 用户登录
// @Summary 用户登录
// @Description 用户登录并获取认证Token
// @Tags 用户管理
// @Accept json
// @Produce json
// @Param login body model.UserLogin true "登录信息"
// @Success 200 {object} model.Response{data=map[string]interface{}} "登录成功"
// @Failure 400 {object} model.Response "请求参数错误"
// @Failure 401 {object} model.Response "认证失败"
// @Failure 500 {object} model.Response "服务器内部错误"
// @Router /api/v1/user/login [post]
func (c *UserController) Login(ctx *gin.Context) {
	var req model.UserLogin
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, model.ErrorResponse(http.StatusBadRequest, "无效的请求参数: "+err.Error()))
		return
	}

	user, token, err := c.UserService.Login(req)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, model.ErrorResponse(http.StatusUnauthorized, "登录失败: "+err.Error()))
		return
	}

	// 设置JWT Token到Cookie
	ctx.SetCookie(
		"token",
		token,
		int(c.Config.JWTExpireTime.Seconds()),
		"/",
		"",
		c.Config.JWTCookieSecure,
		c.Config.JWTCookieHTTPOnly,
	)

	ctx.JSON(http.StatusOK, model.SuccessResponse(map[string]interface{}{
		"user":  user.ToUserResponse(),
		"token": token,
	}))
}

// GetProfile 获取当前用户信息
// @Summary 获取当前用户信息
// @Description 获取当前登录用户的详细信息
// @Tags 用户管理
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} model.Response{data=model.UserResponse} "获取成功"
// @Failure 401 {object} model.Response "未授权"
// @Failure 500 {object} model.Response "服务器内部错误"
// @Router /api/v1/user/profile [get]
func (c *UserController) GetProfile(ctx *gin.Context) {
	userID := middleware.GetCurrentUserID(ctx)
	user, err := c.UserService.GetUserByID(userID)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, model.ErrorResponse(http.StatusInternalServerError, "获取用户信息失败: "+err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, model.SuccessResponse(user.ToUserResponse()))
}

// UpdateProfile 更新当前用户信息
// @Summary 更新当前用户信息
// @Description 更新当前登录用户的个人信息
// @Tags 用户管理
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param update body model.UserUpdate true "用户信息更新"
// @Success 200 {object} model.Response{data=model.UserResponse} "更新成功"
// @Failure 400 {object} model.Response "请求参数错误"
// @Failure 401 {object} model.Response "未授权"
// @Failure 500 {object} model.Response "服务器内部错误"
// @Router /api/v1/user/profile [put]
func (c *UserController) UpdateProfile(ctx *gin.Context) {
	var req model.UserUpdate
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, model.ErrorResponse(http.StatusBadRequest, "无效的请求参数: "+err.Error()))
		return
	}

	userID := middleware.GetCurrentUserID(ctx)
	user, err := c.UserService.UpdateUser(userID, req)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, model.ErrorResponse(http.StatusInternalServerError, "更新用户信息失败: "+err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, model.SuccessResponse(user.ToUserResponse()))
}

// ListUsers 获取用户列表
// @Summary 获取用户列表
// @Description 管理员获取系统中的用户列表
// @Tags 用户管理
// @Produce json
// @Security ApiKeyAuth
// @Param page query int false "页码" default(1)
// @Param pageSize query int false "每页数量" default(10)
// @Param query query string false "搜索关键词"
// @Success 200 {object} model.PagedResponse{items=[]model.UserResponse} "获取成功"
// @Failure 401 {object} model.Response "未授权"
// @Failure 403 {object} model.Response "权限不足"
// @Failure 500 {object} model.Response "服务器内部错误"
// @Router /api/v1/users [get]
func (c *UserController) ListUsers(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("pageSize", "10"))
	query := ctx.Query("query")

	users, total, err := c.UserService.ListUsers(page, pageSize, query)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, model.ErrorResponse(http.StatusInternalServerError, "获取用户列表失败: "+err.Error()))
		return
	}

	// 转换为响应格式
	var userResponses []model.UserResponse
	for _, user := range users {
		userResponses = append(userResponses, user.ToUserResponse())
	}

	ctx.JSON(http.StatusOK, model.NewPagedResponse(total, pageSize, page, userResponses))
}

// GetUser 获取指定用户信息
// @Summary 获取指定用户信息
// @Description 管理员获取指定用户的详细信息
// @Tags 用户管理
// @Produce json
// @Security ApiKeyAuth
// @Param id path int true "用户ID"
// @Success 200 {object} model.Response{data=model.UserResponse} "获取成功"
// @Failure 401 {object} model.Response "未授权"
// @Failure 403 {object} model.Response "权限不足"
// @Failure 404 {object} model.Response "用户不存在"
// @Failure 500 {object} model.Response "服务器内部错误"
// @Router /api/v1/users/{id} [get]
func (c *UserController) GetUser(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, model.ErrorResponse(http.StatusBadRequest, "无效的用户ID"))
		return
	}

	user, err := c.UserService.GetUserByID(uint(id))
	if err != nil {
		ctx.JSON(http.StatusNotFound, model.ErrorResponse(http.StatusNotFound, "获取用户信息失败: "+err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, model.SuccessResponse(user.ToUserResponse()))
}

// UpdateUser 更新指定用户信息
// @Summary 更新指定用户信息
// @Description 管理员更新指定用户的信息
// @Tags 用户管理
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path int true "用户ID"
// @Param update body model.UserUpdate true "用户信息更新"
// @Success 200 {object} model.Response{data=model.UserResponse} "更新成功"
// @Failure 400 {object} model.Response "请求参数错误"
// @Failure 401 {object} model.Response "未授权"
// @Failure 403 {object} model.Response "权限不足"
// @Failure 404 {object} model.Response "用户不存在"
// @Failure 500 {object} model.Response "服务器内部错误"
// @Router /api/v1/users/{id} [put]
func (c *UserController) UpdateUser(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, model.ErrorResponse(http.StatusBadRequest, "无效的用户ID"))
		return
	}

	var req model.UserUpdate
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, model.ErrorResponse(http.StatusBadRequest, "无效的请求参数: "+err.Error()))
		return
	}

	user, err := c.UserService.UpdateUser(uint(id), req)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, model.ErrorResponse(http.StatusInternalServerError, "更新用户信息失败: "+err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, model.SuccessResponse(user.ToUserResponse()))
}

// DeleteUser 删除用户
// @Summary 删除用户
// @Description 管理员删除指定用户
// @Tags 用户管理
// @Produce json
// @Security ApiKeyAuth
// @Param id path int true "用户ID"
// @Success 200 {object} model.Response "删除成功"
// @Failure 400 {object} model.Response "请求参数错误"
// @Failure 401 {object} model.Response "未授权"
// @Failure 403 {object} model.Response "权限不足"
// @Failure 500 {object} model.Response "服务器内部错误"
// @Router /api/v1/users/{id} [delete]
func (c *UserController) DeleteUser(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, model.ErrorResponse(http.StatusBadRequest, "无效的用户ID"))
		return
	}

	// 确保不能删除自己
	currentUserID := middleware.GetCurrentUserID(ctx)
	if currentUserID == uint(id) {
		ctx.JSON(http.StatusBadRequest, model.ErrorResponse(http.StatusBadRequest, "不能删除自己的账号"))
		return
	}

	if err := c.UserService.DeleteUser(uint(id)); err != nil {
		ctx.JSON(http.StatusInternalServerError, model.ErrorResponse(http.StatusInternalServerError, "删除用户失败: "+err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// DisableUser 禁用用户
// @Summary 禁用用户
// @Description 管理员禁用指定用户账号
// @Tags 用户管理
// @Produce json
// @Security ApiKeyAuth
// @Param id path int true "用户ID"
// @Success 200 {object} model.Response "禁用成功"
// @Failure 400 {object} model.Response "请求参数错误"
// @Failure 401 {object} model.Response "未授权"
// @Failure 403 {object} model.Response "权限不足"
// @Failure 500 {object} model.Response "服务器内部错误"
// @Router /api/v1/users/{id}/disable [put]
func (c *UserController) DisableUser(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, model.ErrorResponse(http.StatusBadRequest, "无效的用户ID"))
		return
	}

	// 确保不能禁用自己
	currentUserID := middleware.GetCurrentUserID(ctx)
	if currentUserID == uint(id) {
		ctx.JSON(http.StatusBadRequest, model.ErrorResponse(http.StatusBadRequest, "不能禁用自己的账号"))
		return
	}

	if err := c.UserService.DisableUser(uint(id)); err != nil {
		ctx.JSON(http.StatusInternalServerError, model.ErrorResponse(http.StatusInternalServerError, "禁用用户失败: "+err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// EnableUser 启用用户
// @Summary 启用用户
// @Description 管理员启用指定用户账号
// @Tags 用户管理
// @Produce json
// @Security ApiKeyAuth
// @Param id path int true "用户ID"
// @Success 200 {object} model.Response "启用成功"
// @Failure 400 {object} model.Response "请求参数错误"
// @Failure 401 {object} model.Response "未授权"
// @Failure 403 {object} model.Response "权限不足"
// @Failure 500 {object} model.Response "服务器内部错误"
// @Router /api/v1/users/{id}/enable [put]
func (c *UserController) EnableUser(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, model.ErrorResponse(http.StatusBadRequest, "无效的用户ID"))
		return
	}

	if err := c.UserService.EnableUser(uint(id)); err != nil {
		ctx.JSON(http.StatusInternalServerError, model.ErrorResponse(http.StatusInternalServerError, "启用用户失败: "+err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// ChangeUserRole 更改用户角色
// @Summary 更改用户角色
// @Description 管理员更改指定用户的角色
// @Tags 用户管理
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path int true "用户ID"
// @Param roleID body int true "新的角色ID"
// @Success 200 {object} model.Response "更改成功"
// @Failure 400 {object} model.Response "请求参数错误"
// @Failure 401 {object} model.Response "未授权"
// @Failure 403 {object} model.Response "权限不足"
// @Failure 500 {object} model.Response "服务器内部错误"
// @Router /api/v1/users/{id}/role [put]
func (c *UserController) ChangeUserRole(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, model.ErrorResponse(http.StatusBadRequest, "无效的用户ID"))
		return
	}

	// 解析请求体
	var req struct {
		RoleID uint `json:"role_id" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, model.ErrorResponse(http.StatusBadRequest, "无效的请求参数: "+err.Error()))
		return
	}

	// 更改用户角色
	if err := c.UserService.ChangeUserRole(uint(id), req.RoleID); err != nil {
		ctx.JSON(http.StatusInternalServerError, model.ErrorResponse(http.StatusInternalServerError, "更改用户角色失败: "+err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// RefreshToken 刷新JWT令牌
// @Summary 刷新JWT令牌
// @Description 刷新当前用户的JWT认证令牌
// @Tags 用户管理
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} model.Response{data=map[string]string} "刷新成功"
// @Failure 401 {object} model.Response "未授权"
// @Failure 500 {object} model.Response "服务器内部错误"
// @Router /api/v1/user/refresh-token [get]
func (c *UserController) RefreshToken(ctx *gin.Context) {
	token, err := middleware.RefreshToken(ctx, c.Config)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, model.ErrorResponse(http.StatusInternalServerError, "刷新令牌失败: "+err.Error()))
		return
	}

	// 设置新令牌到Cookie
	ctx.SetCookie(
		"token",
		token,
		int(c.Config.JWTExpireTime.Seconds()),
		"/",
		"",
		c.Config.JWTCookieSecure,
		c.Config.JWTCookieHTTPOnly,
	)

	ctx.JSON(http.StatusOK, model.SuccessResponse(map[string]string{
		"token": token,
	}))
}

// Logout 用户登出
// @Summary 用户登出
// @Description 清除用户的认证Cookie
// @Tags 用户管理
// @Produce json
// @Success 200 {object} model.Response "登出成功"
// @Router /api/v1/user/logout [post]
func (c *UserController) Logout(ctx *gin.Context) {
	// 清除认证Cookie
	ctx.SetCookie(
		"token",
		"",
		-1,
		"/",
		"",
		c.Config.JWTCookieSecure,
		c.Config.JWTCookieHTTPOnly,
	)

	ctx.JSON(http.StatusOK, model.SuccessResponse(nil))
}
