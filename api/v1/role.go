package v1

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"city.newnan/k8s-console/internal/model"
	"city.newnan/k8s-console/internal/service"
)

// RoleController 角色相关API控制器
type RoleController struct {
	RoleService *service.RoleService
}

// NewRoleController 创建角色控制器
func NewRoleController() *RoleController {
	return &RoleController{
		RoleService: service.NewRoleService(),
	}
}

// ListRoles 获取角色列表
// @Summary 获取角色列表
// @Description 获取系统中的角色列表
// @Tags 角色管理
// @Produce json
// @Security ApiKeyAuth
// @Param page query int false "页码" default(1)
// @Param pageSize query int false "每页数量" default(10)
// @Success 200 {object} model.PagedResponse{items=[]model.Role} "获取成功"
// @Failure 401 {object} model.Response "未授权"
// @Failure 403 {object} model.Response "权限不足"
// @Failure 500 {object} model.Response "服务器内部错误"
// @Router /api/v1/roles [get]
func (c *RoleController) ListRoles(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("pageSize", "10"))

	roles, total, err := c.RoleService.ListRoles(page, pageSize)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, model.ErrorResponse(http.StatusInternalServerError, "获取角色列表失败: "+err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, model.NewPagedResponse(total, pageSize, page, roles))
}

// GetRole 获取角色详情
// @Summary 获取角色详情
// @Description 获取指定角色的详细信息
// @Tags 角色管理
// @Produce json
// @Security ApiKeyAuth
// @Param id path int true "角色ID"
// @Success 200 {object} model.Response{data=model.Role} "获取成功"
// @Failure 400 {object} model.Response "请求参数错误"
// @Failure 401 {object} model.Response "未授权"
// @Failure 403 {object} model.Response "权限不足"
// @Failure 404 {object} model.Response "角色不存在"
// @Failure 500 {object} model.Response "服务器内部错误"
// @Router /api/v1/roles/{id} [get]
func (c *RoleController) GetRole(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, model.ErrorResponse(http.StatusBadRequest, "无效的角色ID"))
		return
	}

	role, err := c.RoleService.GetRoleByID(uint(id))
	if err != nil {
		ctx.JSON(http.StatusNotFound, model.ErrorResponse(http.StatusNotFound, "获取角色信息失败: "+err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, model.SuccessResponse(role))
}

// CreateRole 创建角色
// @Summary 创建角色
// @Description 创建新的角色
// @Tags 角色管理
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param role body model.Role true "角色信息"
// @Success 200 {object} model.Response{data=model.Role} "创建成功"
// @Failure 400 {object} model.Response "请求参数错误"
// @Failure 401 {object} model.Response "未授权"
// @Failure 403 {object} model.Response "权限不足"
// @Failure 500 {object} model.Response "服务器内部错误"
// @Router /api/v1/roles [post]
func (c *RoleController) CreateRole(ctx *gin.Context) {
	var role model.Role
	if err := ctx.ShouldBindJSON(&role); err != nil {
		ctx.JSON(http.StatusBadRequest, model.ErrorResponse(http.StatusBadRequest, "无效的请求参数: "+err.Error()))
		return
	}

	createdRole, err := c.RoleService.CreateRole(role)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, model.ErrorResponse(http.StatusInternalServerError, "创建角色失败: "+err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, model.SuccessResponse(createdRole))
}

// UpdateRole 更新角色
// @Summary 更新角色
// @Description 更新指定角色的信息
// @Tags 角色管理
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path int true "角色ID"
// @Param role body model.Role true "角色信息"
// @Success 200 {object} model.Response{data=model.Role} "更新成功"
// @Failure 400 {object} model.Response "请求参数错误"
// @Failure 401 {object} model.Response "未授权"
// @Failure 403 {object} model.Response "权限不足"
// @Failure 404 {object} model.Response "角色不存在"
// @Failure 500 {object} model.Response "服务器内部错误"
// @Router /api/v1/roles/{id} [put]
func (c *RoleController) UpdateRole(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, model.ErrorResponse(http.StatusBadRequest, "无效的角色ID"))
		return
	}

	var role model.Role
	if err := ctx.ShouldBindJSON(&role); err != nil {
		ctx.JSON(http.StatusBadRequest, model.ErrorResponse(http.StatusBadRequest, "无效的请求参数: "+err.Error()))
		return
	}

	updatedRole, err := c.RoleService.UpdateRole(uint(id), role)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, model.ErrorResponse(http.StatusInternalServerError, "更新角色失败: "+err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, model.SuccessResponse(updatedRole))
}

// DeleteRole 删除角色
// @Summary 删除角色
// @Description 删除指定角色
// @Tags 角色管理
// @Produce json
// @Security ApiKeyAuth
// @Param id path int true "角色ID"
// @Success 200 {object} model.Response "删除成功"
// @Failure 400 {object} model.Response "请求参数错误"
// @Failure 401 {object} model.Response "未授权"
// @Failure 403 {object} model.Response "权限不足"
// @Failure 500 {object} model.Response "服务器内部错误"
// @Router /api/v1/roles/{id} [delete]
func (c *RoleController) DeleteRole(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, model.ErrorResponse(http.StatusBadRequest, "无效的角色ID"))
		return
	}

	// 防止删除内置的admin和user角色
	if id == 1 || id == 2 {
		ctx.JSON(http.StatusBadRequest, model.ErrorResponse(http.StatusBadRequest, "不能删除内置角色"))
		return
	}

	if err := c.RoleService.DeleteRole(uint(id)); err != nil {
		ctx.JSON(http.StatusInternalServerError, model.ErrorResponse(http.StatusInternalServerError, "删除角色失败: "+err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// GetRolePermissions 获取角色权限
// @Summary 获取角色权限
// @Description 获取指定角色的所有权限
// @Tags 角色管理
// @Produce json
// @Security ApiKeyAuth
// @Param id path int true "角色ID"
// @Success 200 {object} model.Response{data=[][]string} "获取成功"
// @Failure 400 {object} model.Response "请求参数错误"
// @Failure 401 {object} model.Response "未授权"
// @Failure 403 {object} model.Response "权限不足"
// @Failure 404 {object} model.Response "角色不存在"
// @Failure 500 {object} model.Response "服务器内部错误"
// @Router /api/v1/roles/{id}/permissions [get]
func (c *RoleController) GetRolePermissions(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, model.ErrorResponse(http.StatusBadRequest, "无效的角色ID"))
		return
	}

	role, err := c.RoleService.GetRoleByID(uint(id))
	if err != nil {
		ctx.JSON(http.StatusNotFound, model.ErrorResponse(http.StatusNotFound, "获取角色信息失败: "+err.Error()))
		return
	}

	permissions, err := c.RoleService.GetRolePermissions(role.Name)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, model.ErrorResponse(http.StatusInternalServerError, "获取角色权限失败: "+err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, model.SuccessResponse(permissions))
}

// AddRolePermission 添加角色权限
// @Summary 添加角色权限
// @Description 为指定角色添加权限
// @Tags 角色管理
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path int true "角色ID"
// @Param permission body map[string]string true "权限信息"
// @Success 200 {object} model.Response "添加成功"
// @Failure 400 {object} model.Response "请求参数错误"
// @Failure 401 {object} model.Response "未授权"
// @Failure 403 {object} model.Response "权限不足"
// @Failure 404 {object} model.Response "角色不存在"
// @Failure 500 {object} model.Response "服务器内部错误"
// @Router /api/v1/roles/{id}/permissions [post]
func (c *RoleController) AddRolePermission(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, model.ErrorResponse(http.StatusBadRequest, "无效的角色ID"))
		return
	}

	role, err := c.RoleService.GetRoleByID(uint(id))
	if err != nil {
		ctx.JSON(http.StatusNotFound, model.ErrorResponse(http.StatusNotFound, "获取角色信息失败: "+err.Error()))
		return
	}

	var req struct {
		Path   string `json:"path" binding:"required"`
		Method string `json:"method" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, model.ErrorResponse(http.StatusBadRequest, "无效的请求参数: "+err.Error()))
		return
	}

	_, err = c.RoleService.AddRolePermission(role.Name, req.Path, req.Method)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, model.ErrorResponse(http.StatusInternalServerError, "添加角色权限失败: "+err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// RemoveRolePermission 删除角色权限
// @Summary 删除角色权限
// @Description 删除指定角色的权限
// @Tags 角色管理
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path int true "角色ID"
// @Param permission body map[string]string true "权限信息"
// @Success 200 {object} model.Response "删除成功"
// @Failure 400 {object} model.Response "请求参数错误"
// @Failure 401 {object} model.Response "未授权"
// @Failure 403 {object} model.Response "权限不足"
// @Failure 404 {object} model.Response "角色不存在"
// @Failure 500 {object} model.Response "服务器内部错误"
// @Router /api/v1/roles/{id}/permissions [delete]
func (c *RoleController) RemoveRolePermission(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, model.ErrorResponse(http.StatusBadRequest, "无效的角色ID"))
		return
	}

	role, err := c.RoleService.GetRoleByID(uint(id))
	if err != nil {
		ctx.JSON(http.StatusNotFound, model.ErrorResponse(http.StatusNotFound, "获取角色信息失败: "+err.Error()))
		return
	}

	var req struct {
		Path   string `json:"path" binding:"required"`
		Method string `json:"method" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, model.ErrorResponse(http.StatusBadRequest, "无效的请求参数: "+err.Error()))
		return
	}

	_, err = c.RoleService.RemoveRolePermission(role.Name, req.Path, req.Method)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, model.ErrorResponse(http.StatusInternalServerError, "删除角色权限失败: "+err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, model.SuccessResponse(nil))
}
