package mccontrol

import (
	"time"
)

// CommandExecutor 命令执行器接口
type CommandExecutor interface {
	// ExecuteCommand 执行命令并返回结果
	ExecuteCommand(cmd string) (string, error)

	// Connect 连接到服务器
	Connect() error

	// Disconnect 断开与服务器的连接
	Disconnect()

	// IsConnected 检查是否已连接
	IsConnected() bool
}

// ExecutorType 表示命令执行器的类型
type ExecutorType string

const (
	// ExecutorRcon 使用RCON协议执行命令
	ExecutorRcon ExecutorType = "rcon"

	// ExecutorAttach 使用kubectl attach执行命令
	ExecutorAttach ExecutorType = "attach"

	// ExecutorExec 使用kubectl exec + 重定向到/proc/1/fd/0执行命令
	ExecutorExec ExecutorType = "exec"

	// ExecutorAuto 自动选择最合适的执行器
	ExecutorAuto ExecutorType = "auto"
)

// MinecraftStatusData 相关结构体 - 用于解析Ping返回的JSON数据

// MCModInfo 表示Minecraft模组信息
type MCModInfo struct {
	ID string `json:"modid"` // 模组ID
}

// MCDescriptionExtraItem 表示描述中的额外格式化文本项
type MCDescriptionExtraItem struct {
	Text  string `json:"text"`  // 文本内容
	Color string `json:"color"` // 颜色
}

// MCOnlinePlayer 表示在线玩家信息
type MCOnlinePlayer struct {
	ID   string `json:"id"`   // 玩家UUID
	Name string `json:"name"` // 玩家名称
}

// Version 表示服务器版本信息
type Version struct {
	Name     string `json:"name"`     // 版本名称
	Protocol int    `json:"protocol"` // 协议版本
}

// Players 表示玩家信息
type Players struct {
	Max    int              `json:"max"`    // 最大玩家数
	Online int              `json:"online"` // 在线玩家数
	Sample []MCOnlinePlayer `json:"sample"` // 在线玩家样本
}

// ModInfo 表示模组信息
type ModInfo struct {
	Type    string      `json:"type"`    // 模组类型
	ModList []MCModInfo `json:"modList"` // 模组列表
}

// Description 表示服务器描述
type Description struct {
	Text  string                   `json:"text"`  // 纯文本描述
	Extra []MCDescriptionExtraItem `json:"extra"` // 额外格式化文本
}

// RawDescription 用于处理不同格式的描述字段
type RawDescription struct {
	Description interface{} `json:"description"` // 可能是字符串或对象
}

// MinecraftStatus 表示Minecraft服务器状态的完整数据结构
type MinecraftStatus struct {
	Version     Version     `json:"version"`     // 版本信息
	Players     Players     `json:"players"`     // 玩家信息
	Description interface{} `json:"description"` // 服务器描述，可能是字符串或对象
	Favicon     string      `json:"favicon"`     // 服务器图标（Base64编码）
	ModInfo     ModInfo     `json:"modinfo"`     // 模组信息
}

// GetDescriptionText 从不同格式的描述字段中提取纯文本
func (m *MinecraftStatus) GetDescriptionText() string {
	// 处理不同类型的描述
	switch desc := m.Description.(type) {
	case string:
		// 直接是字符串的情况
		return desc
	case map[string]interface{}:
		// 是对象的情况
		if text, ok := desc["text"].(string); ok {
			var result string = text

			// 处理可能存在的额外文本
			if extra, ok := desc["extra"].([]interface{}); ok {
				for _, item := range extra {
					if extraItem, ok := item.(map[string]interface{}); ok {
						if extraText, ok := extraItem["text"].(string); ok {
							result += extraText
						}
					}
				}
			}

			return result
		}
	}

	// 默认返回空字符串
	return ""
}

// ServerStatus 包含Minecraft服务器状态信息
type ServerStatus struct {
	// 基本状态

	Online      bool      // 服务器是否在线
	LastChecked time.Time // 最后检查时间
	LastError   string    // 最后一次错误信息

	// 服务器信息

	Players     int    // 当前在线玩家数量
	MaxPlayers  int    // 最大玩家数量
	Version     string // 服务器版本
	Description string // 服务器描述
	Latency     int    // 延迟，单位：毫秒

	// Kubernetes信息

	PodName    string // Pod名称
	PodStatus  string // Pod状态
	ClusterIP  string // 集群内IP
	ExternalIP string // 外部IP（如果有）
}

// LogOptions 包含日志获取的配置选项
type LogOptions struct {
	// 日志范围选项

	TailLines *int64     // 获取最近多少行日志，为nil则不限制行数
	SinceTime *time.Time // 从何时开始获取日志，为nil则不限制起始时间
	UntilTime *time.Time // 获取到何时的日志，为nil则不限制结束时间

	// 容器选项

	Container string // 容器名称，为空则使用默认容器
	Previous  bool   // 是否获取以前终止的容器的日志

	// 回调相关选项

	BatchSize   int           // 批量回调大小，每收集到这么多行日志就触发一次回调，默认为10
	MaxWaitTime time.Duration // 最大等待时间，即使缓冲区未满，但过了这个时间也会触发回调，默认为1秒

	// 控制选项

	StopSignal <-chan struct{} // 用于主动停止流式日志监听的信号通道
}

// K8sConfig 包含Kubernetes配置选项
type K8sConfig struct {
	// 连接配置

	RunMode        string // 运行模式：InCluster（集群内）或OutOfCluster（集群外）
	KubeconfigPath string // 当RunMode为OutOfCluster时使用的kubeconfig文件路径
	Namespace      string // 命名空间

	// 资源选择器

	PodLabelSelector     string // 用于选择Pod的标签（如app=minecraft）
	ServiceLabelSelector string // 用于选择Service的标签，为空则使用PodLabelSelector

	// 容器配置

	ContainerName string // 容器名称（在Pod中）
}
