package mccontrol

import (
	"fmt"
)

// CreateCommandExecutor 创建命令执行器
// 根据指定的类型创建相应的命令执行器实例
// 如果类型为ExecutorAuto，则会按照RCON、Attach、Exec的顺序尝试创建
func (m *MinecraftController) CreateCommandExecutor(executorType ExecutorType) (CommandExecutor, error) {
	// 如果是自动模式，按优先级尝试不同执行器
	if executorType == ExecutorAuto {
		// 优先尝试RCON
		executor, err := m.createRconExecutor()
		if err == nil {
			return executor, nil
		}

		// RCON失败，尝试Attach
		executor, err = m.createAttachExecutor()
		if err == nil {
			return executor, nil
		}

		// Attach失败，尝试Exec
		return m.createExecExecutor()
	}

	// 根据指定类型创建执行器
	switch executorType {
	case ExecutorRcon:
		return m.createRconExecutor()
	case ExecutorAttach:
		return m.createAttachExecutor()
	case ExecutorExec:
		return m.createExecExecutor()
	default:
		return nil, fmt.Errorf("不支持的执行器类型: %s", executorType)
	}
}

// ExecuteRconCommand 执行Minecraft命令
// 使用自动选择的命令执行器执行单个命令
func (m *MinecraftController) ExecuteCommand(command string) (string, error) {
	// 先确保我们有最新的Pod信息
	if _, err := m.updatePodInfoIfNeeded(false); err != nil {
		return "", fmt.Errorf("更新Pod信息失败: %v", err)
	}

	// 创建一次性命令执行器（自动选择最合适的方式）
	executor, err := m.CreateCommandExecutor(ExecutorAuto)
	if err != nil {
		return "", fmt.Errorf("创建命令执行器失败: %v", err)
	}
	defer executor.Disconnect()

	// 执行命令
	response, err := executor.ExecuteCommand(command)
	if err != nil {
		return "", fmt.Errorf("命令执行失败: %v", err)
	}

	return response, nil
}

// createRconExecutor 创建RCON执行器
func (m *MinecraftController) createRconExecutor() (CommandExecutor, error) {
	if m.rconPort == 0 {
		return nil, fmt.Errorf("RCON端口未设置")
	}

	// 确保有最新的Pod信息
	if _, err := m.updatePodInfoIfNeeded(false); err != nil {
		return nil, fmt.Errorf("更新Pod信息失败: %v", err)
	}

	// 创建RCON执行器
	executor := newRconExecutor(m.serverIP, m.rconPort, m.rconPassword)

	// 尝试连接
	if err := executor.Connect(); err != nil {
		return nil, fmt.Errorf("RCON连接失败: %v", err)
	}

	return executor, nil
}

// createAttachExecutor 创建Attach执行器
func (m *MinecraftController) createAttachExecutor() (CommandExecutor, error) {
	// 确保有最新的Pod信息
	if _, err := m.updatePodInfoIfNeeded(false); err != nil {
		return nil, fmt.Errorf("更新Pod信息失败: %v", err)
	}

	// 创建Attach执行器 - 传递REST配置
	executor := newAttachExecutor(m.clientset, m.restConfig, m.namespace, m.currentPodName, m.containerName)

	// 尝试连接
	if err := executor.Connect(); err != nil {
		return nil, fmt.Errorf("attach连接失败: %v", err)
	}

	return executor, nil
}

// createExecExecutor 创建Exec执行器
func (m *MinecraftController) createExecExecutor() (CommandExecutor, error) {
	// 确保有最新的Pod信息
	if _, err := m.updatePodInfoIfNeeded(false); err != nil {
		return nil, fmt.Errorf("更新Pod信息失败: %v", err)
	}

	// 创建Exec执行器 - 传递REST配置
	executor := newExecExecutor(m.clientset, m.restConfig, m.namespace, m.currentPodName, m.containerName)

	// Exec执行器不需要持久连接，因此不调用Connect

	return executor, nil
}
