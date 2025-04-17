package middleware

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"city.newnan/k8s-console/internal/config"
	"city.newnan/k8s-console/internal/model"
)

// JWTClaims 自定义JWT载荷
type JWTClaims struct {
	jwt.RegisteredClaims
	UserID   uint   `json:"user_id"`
	Username string `json:"username"`
	RoleID   uint   `json:"role_id"`
	RoleName string `json:"role_name"`
}

// GenerateToken 生成JWT Token
func GenerateToken(user model.User, cfg *config.Config) (string, error) {
	// 设置JWT声明
	claims := JWTClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(cfg.JWTExpireTime)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    cfg.JWTIssuer,
			Subject:   user.Username,
		},
		UserID:   user.ID,
		Username: user.Username,
		RoleID:   user.RoleID,
		RoleName: user.Role.Name,
	}

	// 创建Token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(cfg.JWTSecret))

	if err != nil {
		return "", err
	}

	return tokenString, nil
}

// ParseToken 解析JWT Token
func ParseToken(tokenString string, cfg *config.Config) (*JWTClaims, error) {
	// 解析Token
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		// 验证签名方法
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(cfg.JWTSecret), nil
	})

	if err != nil {
		return nil, err
	}

	// 提取Claims
	if claims, ok := token.Claims.(*JWTClaims); ok && token.Valid {
		return claims, nil
	}
	return nil, errors.New("无效的Token")
}

// JWTAuth JWT认证中间件
func JWTAuth(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从请求头或Cookie获取Token
		tokenString := c.GetHeader("Authorization")
		if tokenString == "" {
			// 尝试从Cookie获取
			cookie, err := c.Cookie("token")
			if err != nil {
				c.JSON(401, model.ErrorResponse(401, "未授权: 缺少Token"))
				c.Abort()
				return
			}
			tokenString = cookie
		} else {
			// 处理 Bearer Token
			if strings.HasPrefix(tokenString, "Bearer ") {
				tokenString = strings.TrimPrefix(tokenString, "Bearer ")
			}
		}

		// 解析Token
		claims, err := ParseToken(tokenString, cfg)
		if err != nil {
			c.JSON(401, model.ErrorResponse(401, "未授权: "+err.Error()))
			c.Abort()
			return
		}

		// 将用户信息保存到上下文中
		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("role_id", claims.RoleID)
		c.Set("role_name", claims.RoleName)

		c.Next()
	}
}

// GetCurrentUserID 从上下文中获取当前用户ID
func GetCurrentUserID(c *gin.Context) uint {
	userID, _ := c.Get("user_id")
	uid, _ := userID.(uint)
	return uid
}

// GetCurrentUsername 从上下文中获取当前用户名
func GetCurrentUsername(c *gin.Context) string {
	username, _ := c.Get("username")
	name, _ := username.(string)
	return name
}

// RefreshToken 刷新Token
func RefreshToken(c *gin.Context, cfg *config.Config) (string, error) {
	// 获取当前用户信息
	userID := GetCurrentUserID(c)
	username, _ := c.Get("username")
	roleID, _ := c.Get("role_id")
	roleName, _ := c.Get("role_name")

	// 创建用户对象
	user := model.User{
		Username: username.(string),
		RoleID:   roleID.(uint),
	}
	user.ID = userID
	user.Role.Name = roleName.(string)

	// 生成新Token
	return GenerateToken(user, cfg)
}
