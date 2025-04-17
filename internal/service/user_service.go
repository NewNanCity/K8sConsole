package service

import (
	"errors"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"city.newnan/k8s-console/internal/config"
	"city.newnan/k8s-console/internal/db"
	"city.newnan/k8s-console/internal/middleware"
	"city.newnan/k8s-console/internal/model"
)

// UserService 提供用户相关功能
type UserService struct {
	Config *config.Config
}

// NewUserService 创建用户服务实例
func NewUserService(cfg *config.Config) *UserService {
	return &UserService{
		Config: cfg,
	}
}

// Register 注册新用户
func (s *UserService) Register(user model.UserRegister) (*model.User, string, error) {
	// 检查用户名是否已存在
	var existingUser model.User
	if err := db.DB.Where("username = ?", user.Username).First(&existingUser).Error; err == nil {
		return nil, "", errors.New("用户名已存在")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, "", err
	}

	// 检查邮箱是否已存在
	if err := db.DB.Where("email = ?", user.Email).First(&existingUser).Error; err == nil {
		return nil, "", errors.New("邮箱已存在")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, "", err
	}

	// 哈希密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, "", err
	}

	// 获取默认角色
	var defaultRole model.Role
	if err := db.DB.Where("name = ?", "user").First(&defaultRole).Error; err != nil {
		// 如果默认角色不存在，创建
		if errors.Is(err, gorm.ErrRecordNotFound) {
			defaultRole = model.Role{
				Name:        "user",
				Description: "普通用户",
			}
			if err := db.DB.Create(&defaultRole).Error; err != nil {
				return nil, "", err
			}
		} else {
			return nil, "", err
		}
	}

	// 创建新用户
	newUser := model.User{
		Username:  user.Username,
		Password:  string(hashedPassword),
		Email:     user.Email,
		RoleID:    defaultRole.ID,
		LastLogin: time.Now(),
		Status:    1,
	}

	if err := db.DB.Create(&newUser).Error; err != nil {
		return nil, "", err
	}

	// 关联用户角色
	newUser.Role = defaultRole

	// 生成JWT令牌
	token, err := middleware.GenerateToken(newUser, s.Config)
	if err != nil {
		return nil, "", err
	}

	return &newUser, token, nil
}

// Login 用户登录
func (s *UserService) Login(login model.UserLogin) (*model.User, string, error) {
	var user model.User
	if err := db.DB.Preload("Role").Where("username = ?", login.Username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, "", errors.New("用户不存在")
		}
		return nil, "", err
	}

	// 验证密码
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(login.Password)); err != nil {
		return nil, "", errors.New("密码错误")
	}

	// 检查用户状态
	if user.Status != 1 {
		return nil, "", errors.New("账号已禁用")
	}

	// 更新最后登录时间
	user.LastLogin = time.Now()
	if err := db.DB.Save(&user).Error; err != nil {
		return nil, "", err
	}

	// 生成JWT令牌
	token, err := middleware.GenerateToken(user, s.Config)
	if err != nil {
		return nil, "", err
	}

	return &user, token, nil
}

// GetUserByID 根据ID获取用户
func (s *UserService) GetUserByID(id uint) (*model.User, error) {
	var user model.User
	if err := db.DB.Preload("Role").First(&user, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("用户不存在")
		}
		return nil, err
	}
	return &user, nil
}

// UpdateUser 更新用户信息
func (s *UserService) UpdateUser(id uint, update model.UserUpdate) (*model.User, error) {
	user, err := s.GetUserByID(id)
	if err != nil {
		return nil, err
	}

	// 更新电子邮件
	if update.Email != "" && update.Email != user.Email {
		// 检查邮箱是否已被他人使用
		var existingUser model.User
		if err := db.DB.Where("email = ? AND id != ?", update.Email, id).First(&existingUser).Error; err == nil {
			return nil, errors.New("邮箱已被其他用户使用")
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}

		user.Email = update.Email
	}

	// 更新密码
	if update.Password != "" {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(update.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		user.Password = string(hashedPassword)
	}

	// 保存更新
	if err := db.DB.Save(user).Error; err != nil {
		return nil, err
	}

	return user, nil
}

// ChangeUserRole 更改用户角色
func (s *UserService) ChangeUserRole(userID uint, roleID uint) error {
	// 检查用户是否存在
	var user model.User
	if err := db.DB.First(&user, userID).Error; err != nil {
		return err
	}

	// 检查角色是否存在
	var role model.Role
	if err := db.DB.First(&role, roleID).Error; err != nil {
		return err
	}

	// 更新用户角色
	user.RoleID = roleID
	if err := db.DB.Save(&user).Error; err != nil {
		return err
	}

	return nil
}

// ListUsers 获取用户列表（分页）
func (s *UserService) ListUsers(page, pageSize int, query string) ([]model.User, int64, error) {
	var users []model.User
	var total int64

	// 构建查询
	db := db.DB.Model(&model.User{}).Preload("Role")

	// 添加搜索条件
	if query != "" {
		db = db.Where("username LIKE ? OR email LIKE ?", "%"+query+"%", "%"+query+"%")
	}

	// 获取总数
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询
	if err := db.Offset((page - 1) * pageSize).Limit(pageSize).Find(&users).Error; err != nil {
		return nil, 0, err
	}

	return users, total, nil
}

// DeleteUser 删除用户
func (s *UserService) DeleteUser(id uint) error {
	if err := db.DB.Delete(&model.User{}, id).Error; err != nil {
		return err
	}
	return nil
}

// DisableUser 禁用用户
func (s *UserService) DisableUser(id uint) error {
	return db.DB.Model(&model.User{}).Where("id = ?", id).Update("status", 0).Error
}

// EnableUser 启用用户
func (s *UserService) EnableUser(id uint) error {
	return db.DB.Model(&model.User{}).Where("id = ?", id).Update("status", 1).Error
}
