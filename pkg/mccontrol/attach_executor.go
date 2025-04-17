package mccontrol

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// attachExecutor 使用kubectl attach的命令执行器实现
type attachExecutor struct {
	clientset     *kubernetes.Clientset // K8s客户端
	restConfig    *rest.Config          // REST配置
	namespace     string                // 命名空间
	podName       string                // Pod名称
	containerName string                // 容器名称

	connected bool       // 是否已连接
	mutex     sync.Mutex // 互斥锁，保护会话操作

	// 执行配置
	timeout time.Duration // 命令执行超时时间
}

// newAttachExecutor 创建一个新的kubectl attach执行器
func newAttachExecutor(clientset *kubernetes.Clientset, restConfig *rest.Config, namespace, podName, containerName string) *attachExecutor {
	return &attachExecutor{
		clientset:     clientset,
		restConfig:    restConfig,
		namespace:     namespace,
		podName:       podName,
		containerName: containerName,
		timeout:       30 * time.Second,
	}
}

// Connect 建立与Pod的连接（测试连接是否可用）
// 利用控制器已有的Pod状态检查，简化连接验证
func (e *attachExecutor) Connect() error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if e.connected {
		return nil
	}

	// 假设Pod状态已由MinecraftController检查，这里只进行简单验证
	if e.podName == "" || e.namespace == "" || e.containerName == "" {
		return fmt.Errorf("Pod信息不完整：podName=%s, namespace=%s, containerName=%s",
			e.podName, e.namespace, e.containerName)
	}

	e.connected = true
	return nil
}

// ExecuteCommand 通过kubectl attach执行命令
func (e *attachExecutor) ExecuteCommand(cmd string) (string, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	// 命令加上换行符
	stdinBuf := bytes.NewBufferString(cmd + "\n")

	// 创建attach请求
	req := e.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(e.podName).
		Namespace(e.namespace).
		SubResource("attach")

	req.VersionedParams(&corev1.PodAttachOptions{
		Container: e.containerName,
		Stdin:     true,
		Stdout:    false,
		Stderr:    false,
		TTY:       false,
	}, scheme.ParameterCodec)

	// 创建执行器
	exec, err := remotecommand.NewSPDYExecutor(e.restConfig, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("创建SPDY执行器失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), e.timeout)
	defer cancel()

	// 执行命令
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  stdinBuf,
		Stdout: nil,     // 不捕获标准输出
		Stderr: nil,
		Tty:    false,
	})

	if err != nil {
		return "", fmt.Errorf("执行命令失败: %v", err)
	}

	return "", nil
}

// Disconnect 断开连接（对于attach方式，每次命令都是新连接，所以这里只是重置状态）
func (e *attachExecutor) Disconnect() {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	e.connected = false
}

// IsConnected 检查是否已连接
func (e *attachExecutor) IsConnected() bool {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	return e.connected
}
