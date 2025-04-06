package mccontrol

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	// 第三方库
	"github.com/bytedance/sonic"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	// Minecraft 相关库
	"github.com/xrjr/mcutils/pkg/ping"
	"github.com/xrjr/mcutils/pkg/rcon"
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

// RconSession 表示与Minecraft服务器的RCON会话
type RconSession struct {
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

// newRconSession 创建一个新的RCON会话
func newRconSession(serverIP string, port int, password string) *RconSession {
	return &RconSession{
		serverIP:      serverIP,
		port:          port,
		password:      password,
		maxRetries:    5,
		retryDelay:    500 * time.Millisecond,
		maxRetryDelay: 10 * time.Second,
	}
}

// connect 连接到RCON服务器并进行认证
func (s *RconSession) connect() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// 如果已连接，不需要重新连接
	if s.connected && s.authenticated {
		return nil
	}

	// 创建新客户端
	s.client = rcon.NewClient(s.serverIP, s.port)

	// 尝试连接
	err := s.client.Connect()
	if err != nil {
		s.connected = false
		s.authenticated = false
		return fmt.Errorf("连接RCON失败: %v", err)
	}

	s.connected = true

	// 尝试认证
	ok, err := s.client.Authenticate(s.password)
	if err != nil {
		s.client.Disconnect()
		s.connected = false
		s.authenticated = false
		return fmt.Errorf("RCON认证错误: %v", err)
	}

	if !ok {
		s.client.Disconnect()
		s.connected = false
		s.authenticated = false
		return fmt.Errorf("RCON认证失败: 密码错误")
	}

	s.authenticated = true
	s.lastUsed = time.Now()
	return nil
}

// command 执行RCON命令，包含重连逻辑
func (s *RconSession) command(cmd string) (string, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// 更新最后使用时间
	s.lastUsed = time.Now()

	// 如果没有连接或者没有认证，尝试连接
	if !s.connected || !s.authenticated {
		s.mutex.Unlock() // 解锁以便connect方法可以获取锁
		if err := s.connect(); err != nil {
			s.mutex.Lock() // 重新获取锁
			return "", err
		}
		s.mutex.Lock() // 重新获取锁
	}

	// 执行命令，带重试逻辑
	var response string
	var err error
	var retryCount int

	for retryCount = 0; retryCount <= s.maxRetries; retryCount++ {
		response, err = s.client.Command(cmd)
		if err == nil {
			break // 命令执行成功
		}

		// 命令执行失败，可能需要重连
		s.connected = false
		s.authenticated = false

		// 如果已经是最后一次重试，则返回错误
		if retryCount == s.maxRetries {
			return "", fmt.Errorf("RCON命令执行失败，已尝试重连%d次: %v", retryCount, err)
		}

		// 计算本次重试延迟
		delay := time.Duration(float64(s.retryDelay) * math.Pow(1.5, float64(retryCount)))
		if delay > s.maxRetryDelay {
			delay = s.maxRetryDelay
		}

		// 释放锁，等待后重试连接
		s.mutex.Unlock()
		time.Sleep(delay)

		// 重新连接
		if err := s.connect(); err != nil {
			s.mutex.Lock() // 重新获取锁
			continue       // 连接失败，继续重试
		}

		s.mutex.Lock() // 重新获取锁
	}

	return response, err
}

// disconnect 断开与RCON服务器的连接
func (s *RconSession) disconnect() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.connected && s.client != nil {
		s.client.Disconnect()
	}

	s.connected = false
	s.authenticated = false
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

// MinecraftController 管理与K8s中的Minecraft服务器的交互
type MinecraftController struct {
	// Kubernetes配置

	clientset        *kubernetes.Clientset // K8s客户端
	namespace        string                // 命名空间
	podLabelSelector string                // Pod标签选择器
	containerName    string                // 容器名称

	// 资源信息

	currentPodName       string // 当前选中的Pod名称
	serviceLabelSelector string // 服务标签选择器
	serverIP             string // 服务器IP地址

	// Pod信息更新控制

	lastPodInfoUpdate     time.Time     // 上次更新Pod信息的时间
	podInfoUpdateInterval time.Duration // Pod信息更新的最小间隔
	podInfoUpdateMutex    sync.Mutex    // 更新Pod信息时的互斥锁

	// Minecraft服务器配置

	gamePort     int    // 游戏端口
	rconPort     int    // RCON端口
	rconPassword string // RCON密码

	// 状态管理

	status ServerStatus // 服务器状态信息

	// 上下文控制

	ctx        context.Context    // 上下文
	cancelFunc context.CancelFunc // 取消函数

	// RCON会话
	rconSession *RconSession // RCON会话

	// 会话管理
	sessionManager *sessionManager // 会话管理器
}

// NewMinecraftController 创建一个新的Minecraft控制器实例
func NewMinecraftController(config K8sConfig, gamePort, rconPort int, rconPassword string) (*MinecraftController, error) {
	var k8sConfig *rest.Config
	var err error

	// 根据运行模式选择K8s配置
	if config.RunMode == "InCluster" {
		k8sConfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("获取集群内部配置失败: %v", err)
		}
	} else {
		// 使用指定的kubeconfig或默认位置
		kubeconfigPath := config.KubeconfigPath
		if kubeconfigPath == "" {
			homeDir, _ := os.UserHomeDir()
			kubeconfigPath = filepath.Join(homeDir, ".kube", "config")
		}

		k8sConfig, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("加载kubeconfig失败: %v", err)
		}
	}

	// 创建K8s客户端
	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return nil, fmt.Errorf("创建K8s客户端失败: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// 创建会话管理器
	sessionMgr := &sessionManager{
		sessions: make(map[string]*CommandSession),
	}

	controller := &MinecraftController{
		clientset:             clientset,
		namespace:             config.Namespace,
		podLabelSelector:      config.PodLabelSelector,
		containerName:         config.ContainerName,
		gamePort:              gamePort,
		rconPort:              rconPort,
		rconPassword:          rconPassword,
		ctx:                   ctx,
		cancelFunc:            cancel,
		serviceLabelSelector:  config.ServiceLabelSelector,
		podInfoUpdateInterval: 5 * time.Minute, // 默认更新间隔为5分钟
		sessionManager:        sessionMgr,
		rconSession:           newRconSession("", rconPort, rconPassword), // 初始化RCON会话
	}

	// 初始化时更新服务器信息
	err = controller.findAndUpdatePodInfo()
	if err != nil {
		return controller, fmt.Errorf("初始化Pod信息失败: %v", err)
	}

	// 启动定时清理任务
	sessionMgr.cleanupTimer = time.NewTicker(5 * time.Minute)
	go func() {
		for range sessionMgr.cleanupTimer.C {
			sessionMgr.cleanupIdleSessions()
		}
	}()

	return controller, nil
}

// updatePodInfoIfNeeded 在必要时更新Pod信息
// forceUpdate: 是否强制更新，忽略时间间隔限制
// 返回值: 是否执行了更新操作, 更新错误（如果有）
func (m *MinecraftController) updatePodInfoIfNeeded(forceUpdate bool) (bool, error) {
	// 快速检查是否需要更新（不加锁）
	if !forceUpdate && time.Since(m.lastPodInfoUpdate) < m.podInfoUpdateInterval {
		return false, nil
	}

	// 加锁进行详细检查与更新
	m.podInfoUpdateMutex.Lock()
	defer m.podInfoUpdateMutex.Unlock()

	// 再次检查是否需要更新（加锁后）
	if !forceUpdate && time.Since(m.lastPodInfoUpdate) < m.podInfoUpdateInterval {
		return false, nil // 其他协程可能已经更新过了
	}

	// 执行更新
	err := m.findAndUpdatePodInfo()
	if err != nil {
		return true, err
	}

	return true, nil
}

// SetPodInfoUpdateInterval 设置Pod信息更新的最小间隔
func (m *MinecraftController) SetPodInfoUpdateInterval(interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Minute // 默认5分钟
	}
	m.podInfoUpdateMutex.Lock()
	defer m.podInfoUpdateMutex.Unlock()
	m.podInfoUpdateInterval = interval
}

// ForceUpdatePodInfo 强制更新Pod信息，忽略时间间隔限制
func (m *MinecraftController) ForceUpdatePodInfo() error {
	_, err := m.updatePodInfoIfNeeded(true)
	return err
}

// findAndUpdatePodInfo 查找符合标签的Pod并更新信息
func (m *MinecraftController) findAndUpdatePodInfo() error {
	// 使用标签选择器列出所有匹配的Pod
	pods, err := m.clientset.CoreV1().Pods(m.namespace).List(m.ctx, metav1.ListOptions{
		LabelSelector: m.podLabelSelector,
	})
	if err != nil {
		return fmt.Errorf("获取Pod列表失败: %v", err)
	}

	if len(pods.Items) == 0 {
		return fmt.Errorf("未找到匹配标签 '%s' 的Pod", m.podLabelSelector)
	}

	// 选择第一个Running状态的Pod，如果没有则选时间最近的成功运行过的pod，还没有就选第一个
	var selectedPod *corev1.Pod
	var latestSucceededPod *corev1.Pod
	var latestSucceededTime time.Time
	for i := range pods.Items {
		if pods.Items[i].Status.Phase == corev1.PodRunning {
			selectedPod = &pods.Items[i]
			break
		}
		// 记录最近成功运行的Pod
		if pods.Items[i].Status.Phase == corev1.PodSucceeded {
			if latestSucceededPod == nil || pods.Items[i].Status.StartTime.Time.After(latestSucceededTime) {
				latestSucceededPod = &pods.Items[i]
				latestSucceededTime = pods.Items[i].Status.StartTime.Time
			}
		}
	}
	// 如果没有Running状态的Pod，则选择最近成功运行的Pod
	if selectedPod == nil && latestSucceededPod != nil {
		selectedPod = latestSucceededPod
	}
	// 如果还没有，就选择第一个Pod
	if selectedPod == nil {
		selectedPod = &pods.Items[0]
	}

	m.currentPodName = selectedPod.Name
	m.serverIP = selectedPod.Status.PodIP
	m.status.PodName = selectedPod.Name
	m.status.PodStatus = string(selectedPod.Status.Phase)
	m.status.ClusterIP = selectedPod.Status.PodIP

	// 使用serviceLabelSelector查询服务（如果配置了该字段）
	serviceLabelSelector := m.serviceLabelSelector
	if serviceLabelSelector == "" {
		serviceLabelSelector = m.podLabelSelector // 默认使用与Pod相同的标签选择器
	}

	// 尝试获取外部IP (如果存在LoadBalancer或NodePort服务)
	if services, err := m.clientset.CoreV1().Services(m.namespace).List(m.ctx, metav1.ListOptions{
		LabelSelector: serviceLabelSelector,
	}); err == nil {
		for _, service := range services.Items {
			// 确保服务的类型是LoadBalancer或NodePort
			if service.Spec.Type == corev1.ServiceTypeLoadBalancer || service.Spec.Type == corev1.ServiceTypeNodePort {
				for _, port := range service.Spec.Ports {
					// 检查服务端口是否与游戏端口匹配
					if port.Port == int32(m.gamePort) || port.TargetPort.IntVal == int32(m.gamePort) {
						if len(service.Status.LoadBalancer.Ingress) > 0 {
							m.status.ExternalIP = service.Status.LoadBalancer.Ingress[0].IP
						} else if len(service.Spec.ExternalIPs) > 0 {
							m.status.ExternalIP = service.Spec.ExternalIPs[0]
						}
						break
					}
				}
			}
		}
	}

	m.lastPodInfoUpdate = time.Now()
	return nil
}

// FetchLogs 统一的日志获取方法，支持一次性获取和流式获取
// 如果提供了callback参数，将启动流式日志获取并通过回调函数增量返回日志
// 如果没有提供callback，则仅执行一次性查询并返回结果
func (m *MinecraftController) FetchLogs(options LogOptions, callback func([]string)) ([]string, error) {
	// 使用智能更新 Pod 信息，只在必要时更新
	if _, err := m.updatePodInfoIfNeeded(false); err != nil {
		return nil, fmt.Errorf("更新Pod信息失败: %v", err)
	}

	// 构建日志查询选项
	podLogOpts := corev1.PodLogOptions{
		Container: options.Container,
		TailLines: options.TailLines,
		Previous:  options.Previous,
	}

	// 如果未指定容器，则使用默认容器
	if podLogOpts.Container == "" {
		podLogOpts.Container = m.containerName
	}

	// 设置日志起始时间
	if options.SinceTime != nil {
		sinceTime := metav1.NewTime(*options.SinceTime)
		podLogOpts.SinceTime = &sinceTime
	}

	// 确定是否使用Follow模式（仅当有回调函数时）
	if callback != nil {
		podLogOpts.Follow = true
	}

	// 获取日志流
	req := m.clientset.CoreV1().Pods(m.namespace).GetLogs(m.currentPodName, &podLogOpts)
	stream, err := req.Stream(m.ctx)
	if err != nil {
		// 如果获取日志流失败，可能是Pod信息已过期，尝试强制更新一次
		if _, forceUpdateErr := m.updatePodInfoIfNeeded(true); forceUpdateErr == nil {
			// 更新成功后重试获取日志流
			req = m.clientset.CoreV1().Pods(m.namespace).GetLogs(m.currentPodName, &podLogOpts)
			stream, err = req.Stream(m.ctx)
			if err != nil {
				errMsg := fmt.Sprintf("即使更新Pod信息后，获取日志流仍然失败: %v", err)
				if callback != nil {
					callback([]string{errMsg})
				}
				return nil, errors.New(errMsg)
			}
		} else {
			// 强制更新也失败了
			errMsg := fmt.Sprintf("获取日志流失败，无法更新Pod信息: %v, %v", err, forceUpdateErr)
			if callback != nil {
				callback([]string{errMsg})
			}
			return nil, errors.New(errMsg)
		}
	}
	defer stream.Close()

	// 设置参数默认值
	batchSize := options.BatchSize
	if batchSize <= 0 {
		batchSize = 10 // 默认批量大小为10行
	}

	maxWaitTime := options.MaxWaitTime
	if maxWaitTime <= 0 {
		maxWaitTime = time.Second // 默认最大等待时间为1秒
	}

	// 读取日志的通用逻辑
	reader := bufio.NewReader(stream)

	// 对于一次性查询模式
	if callback == nil {
		var logEntries []string
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					break
				}
				return logEntries, fmt.Errorf("读取日志行失败: %v", err)
			}
			logEntries = append(logEntries, line)
		}
		return logEntries, nil
	}

	// 对于流式获取模式，在goroutine中处理
	go func() {
		var buffer []string
		lastCallbackTime := time.Now()

		// 重试相关参数
		maxRetries := 5
		retryCount := 0
		retryDelay := 1 * time.Second
		maxRetryDelay := 30 * time.Second

		// 定义重试获取日志流的函数
		tryReconnect := func() (io.ReadCloser, *bufio.Reader, error) {
			// 确保我们有最新的Pod信息
			m.updatePodInfoIfNeeded(true) // 忽略错误，重新获取最新Pod信息

			// 重新创建日志请求
			req := m.clientset.CoreV1().Pods(m.namespace).GetLogs(m.currentPodName, &podLogOpts)
			newStream, err := req.Stream(m.ctx)
			if err != nil {
				return nil, nil, err
			}
			return newStream, bufio.NewReader(newStream), nil
		}

	readLoop:
		for {
			// 检查是否需要结束处理
			select {
			case <-m.ctx.Done():
				// 上下文取消，停止处理
				if len(buffer) > 0 {
					callback(buffer)
				}
				return
			default:
				// 检查是否超过了结束时间
				if options.UntilTime != nil && time.Now().After(*options.UntilTime) {
					if len(buffer) > 0 {
						callback(buffer)
					}
					return
				}
			}

			// 尝试读取一行日志
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					// 流正常结束，回调剩余日志
					if len(buffer) > 0 {
						callback(buffer)
					}
					return
				}

				// 发生错误，判断是否是网络连接错误
				if strings.Contains(err.Error(), "http2: response body closed") ||
					strings.Contains(err.Error(), "connection reset by peer") ||
					strings.Contains(err.Error(), "broken pipe") {

					if retryCount >= maxRetries {
						// 超过最大重试次数，通知错误并退出
						callback([]string{fmt.Sprintf("日志流连接持续失败，已尝试重连%d次: %v", retryCount, err)})
						return
					}

					// 通知用户正在重试
					callback([]string{fmt.Sprintf("日志流连接中断，正在尝试重新连接 (尝试 %d/%d): %v", retryCount+1, maxRetries, err)})

					// 清理当前连接
					stream.Close()

					// 使用指数退避策略计算延迟时间
					currentDelay := time.Duration(math.Min(
						float64(retryDelay)*math.Pow(2, float64(retryCount)),
						float64(maxRetryDelay),
					))

					// 等待一段时间后重试
					select {
					case <-time.After(currentDelay):
						// 继续重试
					case <-m.ctx.Done():
						// 上下文取消
						return
					}

					// 尝试重新建立连接
					newStream, newReader, reconnectErr := tryReconnect()
					if reconnectErr != nil {
						retryCount++
						continue
					}

					// 更新连接
					stream.Close() // 关闭旧连接
					stream = newStream
					reader = newReader
					retryCount = 0 // 重置重试计数

					// 通知重连成功
					callback([]string{"日志流连接已成功重新建立，继续监控日志..."})
					lastCallbackTime = time.Now()

					continue readLoop
				} else {
					// 其他类型的错误，可能是Pod状态变化等
					go m.updatePodInfoIfNeeded(true) // 尝试更新Pod信息
					callback([]string{fmt.Sprintf("读取日志流出错: %v", err)})
					return
				}
			}

			// 重置重试计数
			retryCount = 0

			// 添加日志行到缓冲区
			buffer = append(buffer, line)

			// 判断是否需要触发回调：缓冲区达到阈值或者距离上次回调的时间超过maxWaitTime
			if len(buffer) >= batchSize || (len(buffer) > 0 && time.Since(lastCallbackTime) > maxWaitTime) {
				callback(buffer)
				buffer = nil
				lastCallbackTime = time.Now()
			}
		}
	}()

	// 对于流式获取模式，返回空初始日志
	return []string{}, nil
}

// ExecuteRconCommand 执行RCON命令
func (m *MinecraftController) ExecuteRconCommand(command string) (string, error) {
	// 先确保我们有最新的Pod信息
	if _, err := m.updatePodInfoIfNeeded(false); err != nil {
		return "", fmt.Errorf("更新Pod信息失败: %v", err)
	}

	// 更新RCON会话的服务器IP
	m.rconSession.serverIP = m.serverIP

	// 执行RCON命令
	response, err := m.rconSession.command(command)
	if err != nil {
		return "", fmt.Errorf("RCON命令执行失败: %v", err)
	}

	return response, nil
}

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

// StartPodInfoMonitoring 开始定期监控Pod信息
// 此功能会定期检查Pod状态，即使没有调用任何方法也能保持信息的更新
func (m *MinecraftController) StartPodInfoMonitoring(interval time.Duration) {
	if interval <= 0 {
		interval = m.podInfoUpdateInterval // 使用默认间隔
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-m.ctx.Done():
				return
			case <-ticker.C:
				// 静默更新，忽略错误
				go m.updatePodInfoIfNeeded(true)
			}
		}
	}()
}

// Close 关闭控制器并释放资源
func (m *MinecraftController) Close() {
	m.cancelFunc()
	m.rconSession.disconnect() // 断开RCON会话
}

// CommandSession 表示一个可重用的RCON命令会话
type CommandSession struct {
	id         string               // 会话唯一标识符
	session    *RconSession         // RCON会话对象
	timeout    time.Duration        // 会话超时时间
	lastUsed   time.Time            // 会话最后使用时间
	serverIP   string               // 服务器IP
	port       int                  // RCON端口
	password   string               // RCON密码
	controller *MinecraftController // 控制器引用
}

// ExecuteCommand 在当前会话中执行命令
// 自动处理重连和错误恢复
func (s *CommandSession) ExecuteCommand(command string) (string, error) {
	// 更新会话最后使用时间
	s.lastUsed = time.Now()

	// 检查服务器IP是否发生变化
	if _, err := s.controller.updatePodInfoIfNeeded(false); err == nil {
		if s.serverIP != s.controller.serverIP {
			// IP发生变化，更新会话信息
			s.serverIP = s.controller.serverIP
			s.session.serverIP = s.controller.serverIP
			// 重置连接状态，强制重连
			s.session.connected = false
			s.session.authenticated = false
		}
	}

	// 执行命令
	response, err := s.session.command(command)
	if err != nil {
		return "", fmt.Errorf("执行会话命令失败: %v", err)
	}

	return response, nil
}

// Close 关闭会话并释放资源
func (s *CommandSession) Close() error {
	s.controller.sessionManager.mutex.Lock()
	defer s.controller.sessionManager.mutex.Unlock()

	// 关闭会话连接
	s.session.disconnect()

	// 从管理器中删除
	delete(s.controller.sessionManager.sessions, s.id)

	return nil
}

// CreateCommandSession 创建一个新的命令会话
// timeout: 会话在闲置多长时间后自动关闭
// 返回会话对象和可能的错误
func (m *MinecraftController) CreateCommandSession(timeout time.Duration) (*CommandSession, error) {
	// 先确保我们有最新的Pod信息
	if _, err := m.updatePodInfoIfNeeded(false); err != nil {
		return nil, fmt.Errorf("更新Pod信息失败: %v", err)
	}

	// 创建唯一会话ID
	sessionID := fmt.Sprintf("session-%d", time.Now().UnixNano())

	// 创建RCON会话
	session := newRconSession(m.serverIP, m.rconPort, m.rconPassword)

	// 创建CommandSession
	cmdSession := &CommandSession{
		id:         sessionID,
		session:    session,
		timeout:    timeout,
		lastUsed:   time.Now(),
		serverIP:   m.serverIP,
		port:       m.rconPort,
		password:   m.rconPassword,
		controller: m,
	}

	// 尝试建立初始连接
	if err := session.connect(); err != nil {
		return nil, fmt.Errorf("创建会话失败: %v", err)
	}

	// 注册会话
	m.sessionManager.mutex.Lock()
	m.sessionManager.sessions[sessionID] = cmdSession
	m.sessionManager.mutex.Unlock()

	return cmdSession, nil
}

// SessionExecuteCommand 在指定会话中执行命令
// sessionID: 会话ID
// command: 要执行的命令
// 返回命令响应和可能的错误
func (m *MinecraftController) SessionExecuteCommand(sessionID string, command string) (string, error) {
	m.sessionManager.mutex.Lock()
	session, exists := m.sessionManager.sessions[sessionID]
	if !exists {
		m.sessionManager.mutex.Unlock()
		return "", fmt.Errorf("会话不存在或已过期: %s", sessionID)
	}

	// 更新会话最后使用时间
	session.lastUsed = time.Now()
	m.sessionManager.mutex.Unlock()

	// 检查服务器IP是否发生变化
	if _, err := m.updatePodInfoIfNeeded(false); err == nil {
		if session.serverIP != m.serverIP {
			// IP发生变化，更新会话信息
			session.serverIP = m.serverIP
			session.session.serverIP = m.serverIP
			// 重置连接状态，强制重连
			session.session.connected = false
			session.session.authenticated = false
		}
	}

	// 执行命令
	response, err := session.session.command(command)
	if err != nil {
		return "", fmt.Errorf("执行会话命令失败: %v", err)
	}

	return response, nil
}

// CloseCommandSession 关闭指定的命令会话
// sessionID: 要关闭的会话ID
func (m *MinecraftController) CloseCommandSession(sessionID string) error {
	m.sessionManager.mutex.Lock()
	defer m.sessionManager.mutex.Unlock()

	session, exists := m.sessionManager.sessions[sessionID]
	if !exists {
		return fmt.Errorf("会话不存在或已过期: %s", sessionID)
	}

	// 关闭会话连接
	session.session.disconnect()

	// 从管理器中删除
	delete(m.sessionManager.sessions, sessionID)

	return nil
}

// ListCommandSessions 列出当前所有活跃会话
// 返回活跃会话ID列表
func (m *MinecraftController) ListCommandSessions() []string {
	m.sessionManager.mutex.Lock()
	defer m.sessionManager.mutex.Unlock()

	sessions := make([]string, 0, len(m.sessionManager.sessions))
	for id := range m.sessionManager.sessions {
		sessions = append(sessions, id)
	}

	return sessions
}

// CloseAllCommandSessions 关闭所有命令会话
func (m *MinecraftController) CloseAllCommandSessions() {
	m.sessionManager.mutex.Lock()
	defer m.sessionManager.mutex.Unlock()

	for _, session := range m.sessionManager.sessions {
		session.session.disconnect()
	}

	// 清空会话映射
	m.sessionManager.sessions = make(map[string]*CommandSession)
}

// 会话管理器
type sessionManager struct {
	sessions     map[string]*CommandSession // 活跃会话映射
	mutex        sync.Mutex                 // 并发访问锁
	cleanupTimer *time.Ticker               // 清理计时器
}

// cleanupIdleSessions 清理指定会话管理器中的闲置会话
func (manager *sessionManager) cleanupIdleSessions() {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	now := time.Now()
	for id, session := range manager.sessions {
		// 如果会话超过了超时时间未使用，则关闭它
		if now.Sub(session.lastUsed) > session.timeout {
			session.session.disconnect()
			delete(manager.sessions, id)
		}
	}
}
