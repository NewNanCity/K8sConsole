package mccontrol

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/xrjr/mcutils/pkg/rcon"
)

// rconExecutor 使用RCON协议的命令执行器实现
type rconExecutor struct {
	client        *rcon.RCONClient // RCON客户端
	serverIP      string           // 服务器IP
	port          int              // RCON端口
	password      string           // RCON密码
	connected     bool             // 是否已连接
	authenticated bool             // 是否已认证

	// 会话控制
	lastUsed time.Time  // 上次使用时间
	mutex    sync.Mutex // 互斥锁，保护会话操作

	// 重连控制
	maxRetries    int           // 最大重试次数
	retryDelay    time.Duration // 重试延迟基准时间
	maxRetryDelay time.Duration // 最大重试延迟
}

// newRconExecutor 创建一个新的RCON执行器
func newRconExecutor(serverIP string, port int, password string) *rconExecutor {
	return &rconExecutor{
		serverIP:      serverIP,
		port:          port,
		password:      password,
		maxRetries:    5,
		retryDelay:    500 * time.Millisecond,
		maxRetryDelay: 10 * time.Second,
	}
}

// Connect 连接到RCON服务器并进行认证
func (e *rconExecutor) Connect() error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	// 如果已连接，不需要重新连接
	if e.connected && e.authenticated {
		return nil
	}

	// 创建新客户端
	e.client = rcon.NewClient(e.serverIP, e.port)

	// 尝试连接
	err := e.client.Connect()
	if err != nil {
		e.connected = false
		e.authenticated = false
		return fmt.Errorf("连接RCON失败: %v", err)
	}

	e.connected = true

	// 尝试认证
	ok, err := e.client.Authenticate(e.password)
	if err != nil {
		e.client.Disconnect()
		e.connected = false
		e.authenticated = false
		return fmt.Errorf("RCON认证错误: %v", err)
	}

	if !ok {
		e.client.Disconnect()
		e.connected = false
		e.authenticated = false
		return fmt.Errorf("RCON认证失败: 密码错误")
	}

	e.authenticated = true
	e.lastUsed = time.Now()
	return nil
}

// ExecuteCommand 执行RCON命令，包含重连逻辑
func (e *rconExecutor) ExecuteCommand(cmd string) (string, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	// 更新最后使用时间
	e.lastUsed = time.Now()

	// 如果没有连接或者没有认证，尝试连接
	if !e.connected || !e.authenticated {
		e.mutex.Unlock() // 解锁以便Connect方法可以获取锁
		if err := e.Connect(); err != nil {
			e.mutex.Lock() // 重新获取锁
			return "", err
		}
		e.mutex.Lock() // 重新获取锁
	}

	// 执行命令，带重试逻辑
	var response string
	var err error
	var retryCount int

	for retryCount = 0; retryCount <= e.maxRetries; retryCount++ {
		response, err = e.client.Command(cmd)
		if err == nil {
			break // 命令执行成功
		}

		// 命令执行失败，可能需要重连
		e.connected = false
		e.authenticated = false

		// 如果已经是最后一次重试，则返回错误
		if retryCount == e.maxRetries {
			return "", fmt.Errorf("RCON命令执行失败，已尝试重连%d次: %v", retryCount, err)
		}

		// 计算本次重试延迟
		delay := time.Duration(float64(e.retryDelay) * math.Pow(1.5, float64(retryCount)))
		if delay > e.maxRetryDelay {
			delay = e.maxRetryDelay
		}

		// 释放锁，等待后重试连接
		e.mutex.Unlock()
		time.Sleep(delay)

		// 重新连接
		if err := e.Connect(); err != nil {
			e.mutex.Lock() // 重新获取锁
			continue       // 连接失败，继续重试
		}

		e.mutex.Lock() // 重新获取锁
	}

	return response, err
}

// Disconnect 断开与RCON服务器的连接
func (e *rconExecutor) Disconnect() {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if e.connected && e.client != nil {
		e.client.Disconnect()
	}

	e.connected = false
	e.authenticated = false
}

// IsConnected 检查是否已连接
func (e *rconExecutor) IsConnected() bool {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	return e.connected && e.authenticated
}
