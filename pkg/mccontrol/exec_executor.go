package mccontrol

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// execExecutor 使用kubectl exec和重定向到进程标准输入的命令执行器实现
type execExecutor struct {
	clientset     *kubernetes.Clientset // K8s客户端
	restConfig    *rest.Config          // REST配置
	namespace     string                // 命名空间
	podName       string                // Pod名称
	containerName string                // 容器名称

	mutex sync.Mutex // 互斥锁

	// 执行配置
	timeout      time.Duration // 命令执行超时时间
	useProcessFd bool          // 是否使用/proc/1/fd/0作为标准输入
}

// newExecExecutor 创建一个新的kubectl exec执行器
func newExecExecutor(clientset *kubernetes.Clientset, restConfig *rest.Config, namespace, podName, containerName string) *execExecutor {
	return &execExecutor{
		clientset:     clientset,
		restConfig:    restConfig,
		namespace:     namespace,
		podName:       podName,
		containerName: containerName,
		timeout:       30 * time.Second,
		useProcessFd:  true, // 默认使用/proc/1/fd/0
	}
}

// Connect 连接检查（仅验证基本参数是否有效）
func (e *execExecutor) Connect() error {
	// 简单验证参数完整性
	if e.podName == "" || e.namespace == "" || e.containerName == "" {
		return fmt.Errorf("Pod信息不完整：podName=%s, namespace=%s, containerName=%s",
			e.podName, e.namespace, e.containerName)
	}

	// 执行器不需要维持连接，因此这里不进行实际连接操作
	// 只需验证参数有效性即可，Pod状态已由MinecraftController管理

	// 如果真的需要验证连接是否可用，可以执行一个简单的测试命令
	// 但通常情况下，可以假设如果MinecraftController已验证Pod正常运行，这里就不需要再次验证

	return nil
}

// ExecuteCommand 通过kubectl exec执行命令
func (e *execExecutor) ExecuteCommand(cmd string) (string, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	// 根据是否使用进程文件描述符选择不同的执行方法
	if e.useProcessFd {
		return e.executeViaProcessFd(cmd)
	} else {
		return e.executeViaDirectExec(cmd)
	}
}

// executeViaProcessFd 通过写入/proc/1/fd/0执行命令
func (e *execExecutor) executeViaProcessFd(cmd string) (string, error) {
	// 构建echo命令，将Minecraft命令写入进程的标准输入
	// 确保命令中的引号被正确转义
	escapedCmd := strings.Replace(cmd, "'", "'\\''", -1)
	echoCmd := fmt.Sprintf("echo '%s' > /proc/1/fd/0", escapedCmd)

	// 执行echo命令
	var stdout, stderr bytes.Buffer

	execReq := e.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(e.podName).
		Namespace(e.namespace).
		SubResource("exec")

	execReq.VersionedParams(&corev1.PodExecOptions{
		Container: e.containerName,
		Command:   []string{"sh", "-c", echoCmd},
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}, scheme.ParameterCodec)

	// 创建执行器
	exec, err := remotecommand.NewSPDYExecutor(e.restConfig, "POST", execReq.URL())
	if err != nil {
		return "", fmt.Errorf("创建SPDY执行器失败: %v", err)
	}

	// 设置上下文和超时
	ctx, cancel := context.WithTimeout(context.Background(), e.timeout)
	defer cancel()

	// 执行命令
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
		Tty:    false,
	})

	if err != nil {
		if stderr.Len() > 0 {
			return "", fmt.Errorf("%s: %v", stderr.String(), err)
		}
		return "", fmt.Errorf("执行命令失败: %v", err)
	}

	// 如果是命令写入没有问题，但服务器可能没有回显，需要额外读取日志
	// 这部分可能需要根据实际情况调整，或者留给调用者处理

	// 如果有标准错误输出，则返回
	if stderr.Len() > 0 {
		return stderr.String(), nil
	}

	// 否则返回标准输出
	return stdout.String(), nil
}

// executeViaDirectExec 直接通过exec执行命令
func (e *execExecutor) executeViaDirectExec(cmd string) (string, error) {
	var stdout, stderr bytes.Buffer

	// 创建exec请求
	execReq := e.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(e.podName).
		Namespace(e.namespace).
		SubResource("exec")

	execReq.VersionedParams(&corev1.PodExecOptions{
		Container: e.containerName,
		Command:   []string{"sh", "-c", cmd},
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}, scheme.ParameterCodec)

	// 创建执行器
	exec, err := remotecommand.NewSPDYExecutor(e.restConfig, "POST", execReq.URL())
	if err != nil {
		return "", fmt.Errorf("创建SPDY执行器失败: %v", err)
	}

	// 设置上下文和超时
	ctx, cancel := context.WithTimeout(context.Background(), e.timeout)
	defer cancel()

	// 执行命令
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
		Tty:    false,
	})

	if err != nil {
		if stderr.Len() > 0 {
			return "", fmt.Errorf("%s: %v", stderr.String(), err)
		}
		return "", fmt.Errorf("执行命令失败: %v", err)
	}

	// 如果有标准错误输出但没有标准输出，返回标准错误
	if stdout.Len() == 0 && stderr.Len() > 0 {
		return stderr.String(), nil
	}

	// 否则返回标准输出
	return stdout.String(), nil
}

// SetUseProcessFd 设置是否使用/proc/1/fd/0作为标准输入
func (e *execExecutor) SetUseProcessFd(use bool) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	e.useProcessFd = use
}

// Disconnect 断开连接（对于exec不需要特殊操作）
func (e *execExecutor) Disconnect() {
	// 对于exec，每次命令都是单独的连接，无需特殊断开操作
}

// IsConnected 检查是否已连接
func (e *execExecutor) IsConnected() bool {
	// 对于exec执行器，每次都是新连接，这里只验证参数是否完整
	return e.podName != "" && e.namespace != "" && e.containerName != ""
}
