package model

import (
	"time"

	"gorm.io/gorm"
)

// User 用户模型
type User struct {
	gorm.Model
	Username  string    `gorm:"size:50;not null;uniqueIndex" json:"username"`
	Password  string    `gorm:"size:100;not null" json:"-"`
	Email     string    `gorm:"size:100;uniqueIndex" json:"email"`
	RoleID    uint      `json:"role_id"`
	Role      Role      `gorm:"foreignKey:RoleID" json:"role"`
	LastLogin time.Time `json:"last_login"`
	Status    int       `gorm:"default:1" json:"status"` // 1: 活跃, 0: 禁用
}

// Role 角色模型
type Role struct {
	gorm.Model
	Name        string `gorm:"size:50;not null;uniqueIndex" json:"name"`
	Description string `gorm:"size:200" json:"description"`
	Users       []User `gorm:"foreignKey:RoleID" json:"-"`
}

// UserLogin 用户登录请求
type UserLogin struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// UserRegister 用户注册请求
type UserRegister struct {
	Username string `json:"username" binding:"required,min=3,max=30"`
	Password string `json:"password" binding:"required,min=6"`
	Email    string `json:"email" binding:"required,email"`
}

// UserUpdate 用户更新请求
type UserUpdate struct {
	Email    string `json:"email" binding:"omitempty,email"`
	Password string `json:"password" binding:"omitempty,min=6"`
}

// UserResponse 用户响应数据（不包含敏感信息）
type UserResponse struct {
	ID        uint      `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	RoleID    uint      `json:"role_id"`
	RoleName  string    `json:"role_name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	LastLogin time.Time `json:"last_login"`
	Status    int       `json:"status"`
}

// ToUserResponse 将User转换为UserResponse
func (u *User) ToUserResponse() UserResponse {
	return UserResponse{
		ID:        u.ID,
		Username:  u.Username,
		Email:     u.Email,
		RoleID:    u.RoleID,
		RoleName:  u.Role.Name,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
		LastLogin: u.LastLogin,
		Status:    u.Status,
	}
}
