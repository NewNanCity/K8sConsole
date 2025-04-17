package mccontrol

import (
	"fmt"
	"time"

	"github.com/bytedance/sonic"
	"github.com/xrjr/mcutils/pkg/ping"
)

// CheckServerStatus 检查服务器状态
func (m *MinecraftController) CheckServerStatus() (*ServerStatus, error) {
	// 更新Pod状态
	_, err := m.updatePodInfoIfNeeded(false)
	if err != nil {
		m.status.LastError = fmt.Sprintf("更新Pod信息失败: %v", err)
		return &m.status, err
	}

	// 检查Minecraft服务器状态
	properties, latency, err := ping.Ping(m.serverIP, m.gamePort)
	if err != nil {
		// 如果Ping失败，可能是Pod信息已过期，尝试强制更新一次
		updated, updateErr := m.updatePodInfoIfNeeded(true)
		if updated && updateErr == nil {
			// 更新成功后重试Ping
			properties, latency, err = ping.Ping(m.serverIP, m.gamePort)
			if err != nil {
				m.status.Online = false
				m.status.LastError = fmt.Sprintf("即使更新Pod信息后，Ping服务器仍然失败: %v", err)
				m.status.LastChecked = time.Now()
				return &m.status, nil
			}
		} else {
			// Ping失败且无法更新Pod信息
			m.status.Online = false
			m.status.LastError = fmt.Sprintf("Ping服务器失败: %v", err)
			m.status.LastChecked = time.Now()
			return &m.status, nil
		}
	}

	m.status.Online = true
	m.status.Latency = int(latency)
	m.status.LastChecked = time.Now()
	m.status.LastError = ""

	// 使用 sonic 解析 JSON 数据
	var mcStatus MinecraftStatus

	// 将 map[string]interface{} 转换为 JSON 字符串
	jsonData, err := sonic.Marshal(properties)
	if err != nil {
		m.status.LastError = fmt.Sprintf("序列化服务器属性失败: %v", err)
		return &m.status, fmt.Errorf("序列化服务器属性失败: %v", err)
	}

	// 解析 JSON 到结构体
	if err := sonic.Unmarshal(jsonData, &mcStatus); err != nil {
		m.status.LastError = fmt.Sprintf("解析服务器状态失败: %v", err)
		return &m.status, fmt.Errorf("解析服务器状态失败: %v", err)
	}

	// 从解析后的结构体设置状态信息
	if mcStatus.Version.Name != "" {
		m.status.Version = mcStatus.Version.Name
	}

	m.status.Players = mcStatus.Players.Online
	m.status.MaxPlayers = mcStatus.Players.Max

	// 使用辅助方法从不同格式的描述字段中提取文本
	descText := mcStatus.GetDescriptionText()
	if descText != "" {
		m.status.Description = descText
	} else {
		// 兼容性处理：如果辅助方法无法提取文本，尝试直接处理原始properties中的描述
		if desc, ok := properties["description"]; ok {
			switch d := desc.(type) {
			case string:
				m.status.Description = d
			case map[string]interface{}:
				if text, ok := d["text"].(string); ok {
					m.status.Description = text
				}
			}
		}
	}

	return &m.status, nil
}

// StartStatusMonitoring 定期监控服务器状态
func (m *MinecraftController) StartStatusMonitoring(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-m.ctx.Done():
				return
			case <-ticker.C:
				m.CheckServerStatus()
			}
		}
	}()
}
