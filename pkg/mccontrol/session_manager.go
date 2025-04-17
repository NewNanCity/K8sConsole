package mccontrol

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// CommandSession 表示与Minecraft服务器的命令会话
type CommandSession struct {
	id           string          // 会话唯一标识符
	executor     CommandExecutor // 命令执行器
	executorType ExecutorType    // 执行器类型
	lastUsed     time.Time       // 最后使用时间
	idleTimeout  time.Duration   // 空闲超时时间
	mutex        sync.Mutex      // 互斥锁
}

// sessionManager 管理命令会话
type sessionManager struct {
	sessions     map[string]*CommandSession // 会话映射 (ID -> 会话)
	mutex        sync.Mutex                 // 互斥锁
	cleanupTimer *time.Ticker               // 清理计时器
}

// CreateCommandSession 创建一个新的命令会话
// executorType 指定要使用的执行器类型，使用ExecutorAuto自动选择最适合的执行器
func (m *MinecraftController) CreateCommandSession(idleTimeout time.Duration, executorType ExecutorType) (*CommandSession, error) {
	// 如果没有指定执行器类型，使用自动选择
	if executorType == "" {
		executorType = ExecutorAuto
	}

	// 创建命令执行器
	executor, err := m.CreateCommandExecutor(executorType)
	if err != nil {
		return nil, fmt.Errorf("创建命令执行器失败: %v", err)
	}

	if err := executor.Connect(); err != nil {
		return nil, fmt.Errorf("连接执行器失败: %v", err)
	}

	// 创建会话
	session := &CommandSession{
		id:           uuid.New().String(),
		executor:     executor,
		executorType: executorType,
		lastUsed:     time.Now(),
		idleTimeout:  idleTimeout,
	}

	// 将会话添加到管理器
	m.sessionManager.mutex.Lock()
	m.sessionManager.sessions[session.id] = session
	m.sessionManager.mutex.Unlock()

	return session, nil
}

// ExecuteCommand 在会话中执行命令
func (s *CommandSession) ExecuteCommand(command string) (string, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// 更新最后使用时间
	s.lastUsed = time.Now()

	// 执行命令
	response, err := s.executor.ExecuteCommand(command)
	return response, err
}

// Close 关闭会话
func (s *CommandSession) Close() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// 断开执行器连接
	s.executor.Disconnect()
}

// IsIdle 检查会话是否空闲
func (s *CommandSession) IsIdle() bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return time.Since(s.lastUsed) > s.idleTimeout
}

// GetID 获取会话ID
func (s *CommandSession) GetID() string {
	return s.id
}

// GetExecutorType 获取执行器类型
func (s *CommandSession) GetExecutorType() ExecutorType {
	return s.executorType
}

// SessionExecuteCommand 使用指定会话执行命令
func (m *MinecraftController) SessionExecuteCommand(sessionID, command string) (string, error) {
	m.sessionManager.mutex.Lock()
	session, ok := m.sessionManager.sessions[sessionID]
	m.sessionManager.mutex.Unlock()

	if !ok {
		return "", fmt.Errorf("会话不存在: %s", sessionID)
	}

	return session.ExecuteCommand(command)
}

// CloseCommandSession 关闭指定的命令会话
func (m *MinecraftController) CloseCommandSession(sessionID string) error {
	m.sessionManager.mutex.Lock()
	session, ok := m.sessionManager.sessions[sessionID]
	if ok {
		delete(m.sessionManager.sessions, sessionID)
	}
	m.sessionManager.mutex.Unlock()

	if !ok {
		return fmt.Errorf("会话不存在: %s", sessionID)
	}

	session.Close()
	return nil
}

// CloseAllCommandSessions 关闭所有命令会话
func (m *MinecraftController) CloseAllCommandSessions() {
	m.sessionManager.mutex.Lock()
	sessions := make([]*CommandSession, 0, len(m.sessionManager.sessions))
	for _, session := range m.sessionManager.sessions {
		sessions = append(sessions, session)
	}
	m.sessionManager.sessions = make(map[string]*CommandSession)
	m.sessionManager.mutex.Unlock()

	// 关闭所有会话
	for _, session := range sessions {
		session.Close()
	}
}

// ListCommandSessions 列出所有活跃的命令会话
func (m *MinecraftController) ListCommandSessions() []string {
	m.sessionManager.mutex.Lock()
	defer m.sessionManager.mutex.Unlock()

	ids := make([]string, 0, len(m.sessionManager.sessions))
	for id := range m.sessionManager.sessions {
		ids = append(ids, id)
	}

	return ids
}

// cleanupIdleSessions 清理空闲的会话
func (sm *sessionManager) cleanupIdleSessions() {
	sm.mutex.Lock()
	idleSessions := make([]string, 0)

	// 查找空闲会话
	for id, session := range sm.sessions {
		if session.IsIdle() {
			idleSessions = append(idleSessions, id)
		}
	}

	// 删除空闲会话
	for _, id := range idleSessions {
		session := sm.sessions[id]
		delete(sm.sessions, id)
		sm.mutex.Unlock() // 在可能阻塞的操作前解锁

		// 关闭会话
		session.Close()

		sm.mutex.Lock() // 重新获取锁
	}

	sm.mutex.Unlock()
}
