package service

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"city.newnan/k8s-console/internal/db"
	"city.newnan/k8s-console/internal/middleware"
	"city.newnan/k8s-console/internal/model"
)

// RoleService 提供角色相关功能
type RoleService struct{}

// NewRoleService 创建角色服务实例
func NewRoleService() *RoleService {
	return &RoleService{}
}

// CreateRole 创建新角色
func (s *RoleService) CreateRole(role model.Role) (*model.Role, error) {
	// 检查角色名是否已存在
	var existingRole model.Role
	if err := db.DB.Where("name = ?", role.Name).First(&existingRole).Error; err == nil {
		return nil, errors.New("角色名已存在")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// 创建新角色
	if err := db.DB.Create(&role).Error; err != nil {
		return nil, err
	}

	return &role, nil
}

// GetRoleByID 根据ID获取角色
func (s *RoleService) GetRoleByID(id uint) (*model.Role, error) {
	var role model.Role
	if err := db.DB.First(&role, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("角色不存在")
		}
		return nil, err
	}
	return &role, nil
}

// GetRoleByName 根据名称获取角色
func (s *RoleService) GetRoleByName(name string) (*model.Role, error) {
	var role model.Role
	if err := db.DB.Where("name = ?", name).First(&role).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("角色不存在")
		}
		return nil, err
	}
	return &role, nil
}

// UpdateRole 更新角色信息
func (s *RoleService) UpdateRole(id uint, update model.Role) (*model.Role, error) {
	role, err := s.GetRoleByID(id)
	if err != nil {
		return nil, err
	}

	// 检查角色名是否与其他角色重复
	if update.Name != role.Name {
		var existingRole model.Role
		if err := db.DB.Where("name = ? AND id != ?", update.Name, id).First(&existingRole).Error; err == nil {
			return nil, errors.New("角色名已被其他角色使用")
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		role.Name = update.Name
	}

	if update.Description != "" {
		role.Description = update.Description
	}

	// 保存更新
	if err := db.DB.Save(role).Error; err != nil {
		return nil, err
	}

	return role, nil
}

// ListRoles 获取所有角色
func (s *RoleService) ListRoles(page, pageSize int) ([]model.Role, int64, error) {
	var roles []model.Role
	var total int64

	// 获取总数
	if err := db.DB.Model(&model.Role{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询
	if err := db.DB.Offset((page - 1) * pageSize).Limit(pageSize).Find(&roles).Error; err != nil {
		return nil, 0, err
	}

	return roles, total, nil
}

// DeleteRole 删除角色
func (s *RoleService) DeleteRole(id uint) error {
	// 检查角色是否还有关联用户
	var count int64
	if err := db.DB.Model(&model.User{}).Where("role_id = ?", id).Count(&count).Error; err != nil {
		return err
	}

	if count > 0 {
		return errors.New("无法删除: 该角色下还有用户")
	}

	// 删除角色
	if err := db.DB.Delete(&model.Role{}, id).Error; err != nil {
		return err
	}

	return nil
}

// GetRolePermissions 获取角色权限
func (s *RoleService) GetRolePermissions(roleName string) ([][]string, error) {
	enforcer := middleware.GetEnforcer()
	if enforcer == nil {
		return nil, errors.New("权限系统未初始化")
	}

	permissions := enforcer.GetPermissionsForUser(roleName)
	return permissions, nil
}

// AddRolePermission 添加角色权限
func (s *RoleService) AddRolePermission(roleName, path, method string) (bool, error) {
	enforcer := middleware.GetEnforcer()
	if enforcer == nil {
		return false, errors.New("权限系统未初始化")
	}

	return enforcer.AddPolicy(roleName, path, method)
}

// RemoveRolePermission 移除角色权限
func (s *RoleService) RemoveRolePermission(roleName, path, method string) (bool, error) {
	enforcer := middleware.GetEnforcer()
	if enforcer == nil {
		return false, errors.New("权限系统未初始化")
	}

	return enforcer.RemovePolicy(roleName, path, method)
}

// SetupInitialRoles 设置初始角色和权限
func (s *RoleService) SetupInitialRoles() error {
	// 创建管理员角色
	adminRole, err := s.GetRoleByName("admin")
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || err.Error() == "角色不存在" {
			adminRole, err = s.CreateRole(model.Role{
				Name:        "admin",
				Description: "系统管理员",
			})
			if err != nil {
				return fmt.Errorf("创建管理员角色失败: %w", err)
			}
		} else {
			return fmt.Errorf("检查管理员角色失败: %w", err)
		}
	}

	// 创建普通用户角色
	userRole, err := s.GetRoleByName("user")
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || err.Error() == "角色不存在" {
			userRole, err = s.CreateRole(model.Role{
				Name:        "user",
				Description: "普通用户",
			})
			if err != nil {
				return fmt.Errorf("创建普通用户角色失败: %w", err)
			}
		} else {
			return fmt.Errorf("检查普通用户角色失败: %w", err)
		}
	}

	// 设置初始权限
	enforcer := middleware.GetEnforcer()
	if enforcer == nil {
		return errors.New("权限系统未初始化")
	}

	// 清空现有策略
	enforcer.ClearPolicy()

	// 管理员可以访问所有API
	enforcer.AddPolicy("admin", "*", "*")

	// 普通用户只能访问特定API
	enforcer.AddPolicy("user", "/api/v1/user/profile", "GET")
	enforcer.AddPolicy("user", "/api/v1/user/profile", "PUT")
	enforcer.AddPolicy("user", "/api/v1/user/password", "PUT")
	enforcer.AddPolicy("user", "/api/v1/ws", "GET")
	enforcer.AddPolicy("user", "/api/v1/sse", "GET")

	// 保存策略
	return enforcer.SavePolicy()
}
