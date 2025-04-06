# Minecraft Kubernetes 控制器

## 项目概述

`mccontrol` 包提供了一个完整的控制器，用于监控和管理部署在 Kubernetes 集群中的 Minecraft 服务器。该控制器支持实时日志监控、RCON 命令执行和服务器状态检测，可以作为后端为管理界面前端提供服务。

## 技术架构

### 核心组件

1. **MinecraftController**：主控制器类，负责协调所有操作
2. **K8s API 交互**：使用官方客户端库与 Kubernetes 集群通信
3. **Minecraft 协议**：利用 mcutils 库实现 Ping 和 RCON 协议

### 依赖关系

- `github.com/xrjr/mcutils`：提供 Minecraft 协议实现
- `k8s.io/client-go`：Kubernetes 客户端库
- Go 标准库的各个组件

## 功能详解

### 1. Pod 状态智能管理

控制器采用智能 Pod 信息更新机制，减少不必要的 API 调用：

```go
// 设置Pod信息更新的最小间隔
controller.SetPodInfoUpdateInterval(5 * time.Minute)

// 在需要时手动强制更新
err := controller.ForceUpdatePodInfo()

// 启动后台自动更新
controller.StartPodInfoMonitoring(10 * time.Minute)
```

Pod 信息更新特点：
- 仅在信息过期或操作失败时更新，避免频繁 API 调用
- 可配置的更新间隔，默认为 5 分钟
- 支持失败自动重试机制，提高系统稳定性

### 2. 统一的日志管理

#### 灵活的日志获取选项

新的 `FetchLogs` 方法提供了统一且灵活的日志获取接口：

```go
// LogOptions 包含日志获取的配置选项
type LogOptions struct {
    // 日志范围选项
    TailLines *int64     // 获取最近多少行日志
    SinceTime *time.Time // 从何时开始获取日志
    UntilTime *time.Time // 获取到何时的日志

    // 容器选项
    Container string // 容器名称，为空则使用默认容器
    Previous  bool   // 是否获取以前终止的容器的日志

    // 回调相关选项
    BatchSize   int           // 批量回调大小
    MaxWaitTime time.Duration // 最大等待时间
}
```

#### 一次性获取日志

```go
// 获取最近100行日志
tailLines := int64(100)
logs, err := controller.FetchLogs(mccontrol.LogOptions{
    TailLines: &tailLines,
}, nil)

// 获取特定时间段的日志
startTime := time.Now().Add(-1 * time.Hour)
endTime := time.Now()
logs, err := controller.FetchLogs(mccontrol.LogOptions{
    SinceTime: &startTime,
    UntilTime: &endTime,
}, nil)
```

#### 流式获取日志

```go
// 流式获取日志，每20行或最多1秒回调一次
controller.FetchLogs(mccontrol.LogOptions{
    BatchSize: 20,
    MaxWaitTime: time.Second,
}, func(logs []string) {
    for _, log := range logs {
        fmt.Print(log)
    }
})
```

### 3. 服务器状态检测

使用 mcutils 的 Ping 功能检查服务器状态：

```go
status, err := controller.CheckServerStatus()
```

状态检测内容包括：
- 在线状态
- 延迟
- 玩家数量
- 服务器版本
- 服务器描述 (MOTD)
- Kubernetes 资源信息 (Pod名称、状态、IP)

### 4. RCON 命令执行

允许远程执行 Minecraft 服务器命令：

```go
response, err := controller.ExecuteRconCommand("list")
```

### 5. 服务选择器支持

控制器支持单独指定服务标签选择器，解决服务选择器与Pod选择器不同的情况：

```go
k8sConfig := mccontrol.K8sConfig{
    // ...
    PodLabelSelector: "app=minecraft",
    ServiceLabelSelector: "app=minecraft-service", // 可选，默认使用PodLabelSelector
}
```

## 灵活的部署配置

控制器支持在两种环境中运行：

1. **集群内运行**：使用 `InClusterConfig`
   ```go
   config.RunMode = "InCluster"
   ```

2. **集群外运行**：使用 kubeconfig 文件
   ```go
   config.RunMode = "OutOfCluster"
   config.KubeconfigPath = "/path/to/kubeconfig" // 可选，默认使用 ~/.kube/config
   ```

## 最佳实践

### 初始化控制器

```go
k8sConfig := mccontrol.K8sConfig{
    RunMode:             "InCluster",  // 或 "OutOfCluster"
    Namespace:           "minecraft",
    PodLabelSelector:    "app=minecraft",
    ServiceLabelSelector: "app=minecraft-svc", // 可选
    ContainerName:       "minecraft-server",
}

controller, err := mccontrol.NewMinecraftController(
    k8sConfig,
    25565,           // 游戏端口
    25575,           // RCON 端口
    "minecraft-password", // RCON 密码
)
if err != nil {
    // 处理错误
}
defer controller.Close()
```

### 实时日志监控

```go
controller.FetchLogs(mccontrol.LogOptions{
    BatchSize: 10,
    MaxWaitTime: 500 * time.Millisecond,
}, func(logs []string) {
    for _, log := range logs {
        fmt.Print(log)
    }
})
```

### 智能资源监控

```go
// 开启智能Pod信息更新
controller.SetPodInfoUpdateInterval(3 * time.Minute)
controller.StartPodInfoMonitoring(5 * time.Minute)

// 自动状态更新
controller.StartStatusMonitoring(30 * time.Second) // 每30秒检查一次
```

## 技术细节

### 数据结构

1. **ServerStatus**：存储服务器状态信息
   - 基本状态：在线状态、检查时间、错误信息
   - 服务器信息：玩家数量、版本、描述、延迟
   - Kubernetes信息：Pod名称、状态、内部IP、外部IP

2. **LogOptions**：日志获取配置
   - 日志范围：行数限制、起止时间
   - 容器选项：容器名称、历史日志
   - 回调相关：批量大小、最大等待时间

### 异步处理

控制器使用 goroutines 和 context 处理异步操作：

- 背景日志监控
- 定期状态检查
- 智能Pod信息更新
- 优雅关闭机制

## 性能优化

1. **智能Pod更新**：减少不必要的Kubernetes API调用
2. **高效日志流**：使用K8s原生流式日志功能，无需轮询
3. **批量回调**：日志回调支持批量处理和最大等待时间设置
4. **错误自愈能力**：在操作失败时自动重试并更新资源信息

## 注意事项

1. **RCON 安全性**：确保 RCON 密码安全存储，最好使用 Kubernetes Secrets

2. **网络连接**：确保控制器所在 Pod 可以访问 Minecraft 服务器的网络

3. **错误处理**：控制器内置对暂时性错误的重试机制

4. **资源管理**：监控长时间运行的控制器的内存使用情况
