package mccontrol

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// MinecraftController 管理与K8s中的Minecraft服务器的交互
type MinecraftController struct {
	// Kubernetes配置
	clientset        *kubernetes.Clientset // K8s客户端
	restConfig       *rest.Config          // REST配置
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
		restConfig:            k8sConfig, // 保存REST配置
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
}
