package mccontrol

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FetchLogs 统一的日志获取方法，支持一次性获取和流式获取
// 如果提供了callback参数，将启动流式日志获取并通过回调函数增量返回日志和错误信息
// 如果没有提供callback，则仅执行一次性查询并返回结果
func (m *MinecraftController) FetchLogs(options LogOptions, callback func([]string, string)) ([]string, error) {
	// 使用智能更新 Pod 信息，只在必要时更新
	if _, err := m.updatePodInfoIfNeeded(false); err != nil {
		// 如果连 Pod 信息都获取不到，直接返回错误
		if callback != nil {
			callback(nil, fmt.Sprintf("无法获取 Pod 信息: %v", err))
		}
		return nil, fmt.Errorf("更新Pod信息失败: %v", err)
	}

	// 构建日志查询选项
	podLogOpts := corev1.PodLogOptions{
		Container:  options.Container,
		TailLines:  options.TailLines,
		Previous:   options.Previous,
		Timestamps: true, // 开启时间戳以支持断点续传和补全
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

	// 获取日志流的函数，封装了重试逻辑
	getStream := func(opts corev1.PodLogOptions) (io.ReadCloser, error) {
		req := m.clientset.CoreV1().Pods(m.namespace).GetLogs(m.currentPodName, &opts)
		stream, err := req.Stream(m.ctx)
		if err != nil {
			// 如果获取日志流失败，可能是Pod信息已过期，尝试强制更新一次
			if _, forceUpdateErr := m.updatePodInfoIfNeeded(true); forceUpdateErr == nil {
				// 更新成功后重试获取日志流
				req = m.clientset.CoreV1().Pods(m.namespace).GetLogs(m.currentPodName, &opts)
				stream, err = req.Stream(m.ctx)
				if err != nil {
					return nil, fmt.Errorf("即使更新Pod信息后，获取日志流仍然失败: %w", err)
				}
			} else {
				// 强制更新也失败了
				return nil, fmt.Errorf("获取日志流失败，且无法更新Pod信息: %w, updateErr: %v", err, forceUpdateErr)
			}
		}
		return stream, nil
	}

	stream, err := getStream(podLogOpts)
	if err != nil {
		errMsg := fmt.Sprintf("初始化获取日志流失败: %v", err)
		if callback != nil {
			callback(nil, errMsg) // 通过回调通知错误
		}
		return nil, errors.New(errMsg)
	}
	defer stream.Close() // 确保初始流被关闭

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

	// 提取日志内容和时间戳的辅助函数
	parseLogLine := func(line string) (string, time.Time, bool) {
		if tsEnd := strings.IndexByte(line, ' '); tsEnd > 0 {
			tsStr := line[:tsEnd]
			if ts, tsErr := time.Parse(time.RFC3339Nano, tsStr); tsErr == nil {
				return strings.TrimRight(line[tsEnd+1:], "\n"), ts, true
			}
		}
		return strings.TrimRight(line, "\n"), time.Time{}, false // 没有有效时间戳
	}

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
			if line != "" {
				content, _, _ := parseLogLine(line)
				logEntries = append(logEntries, content)
			}
		}
		return logEntries, nil
	}

	// 对于流式获取模式，在goroutine中处理
	go func() {
		currentStream := stream // 将初始流赋值给 currentStream
		currentReader := reader // 将初始 reader 赋值给 currentReader
		defer func() {
			if currentStream != nil {
				currentStream.Close() // 确保 goroutine 退出时关闭当前流
			}
		}()

		var buffer []string
		lastCallbackTime := time.Now()
		var lastLogTimestamp time.Time // 记录最后一条成功处理的日志时间戳

		// 初始化 lastLogTimestamp (如果 options.SinceTime 提供了)
		if options.SinceTime != nil {
			lastLogTimestamp = *options.SinceTime
		}

		// 重试相关参数
		maxRetries := 5
		retryCount := 0
		retryDelay := 1 * time.Second
		maxRetryDelay := 30 * time.Second

		// 定义重试获取日志流的函数 (包含补全逻辑)
		tryReconnect := func(lastTimestamp time.Time) (io.ReadCloser, *bufio.Reader, time.Time, error) {
			latestTimestamp := lastTimestamp // 用于记录补全日志后的最新时间戳

			// 确保我们有最新的Pod信息
			if _, updateErr := m.updatePodInfoIfNeeded(true); updateErr != nil {
				// 如果更新 Pod 信息失败，重连可能无意义，但还是尝试一下
				callback(nil, fmt.Sprintf("重连前更新 Pod 信息失败: %v", updateErr))
			}

			// --- 补全日志 ---
			if !lastTimestamp.IsZero() { // 只有在收到过日志后才需要补全
				catchUpOpts := corev1.PodLogOptions{
					Container:  podLogOpts.Container,
					Previous:   podLogOpts.Previous,
					Timestamps: true,
					Follow:     false, // 非Follow模式
				}
				// 从上一条日志之后 1 纳秒开始获取
				catchUpSince := lastTimestamp.Add(time.Nanosecond)
				catchUpMetaTime := metav1.NewTime(catchUpSince)
				catchUpOpts.SinceTime = &catchUpMetaTime

				catchUpStream, catchUpErr := getStream(catchUpOpts) // 使用封装的 getStream
				if catchUpErr == nil {
					func() { // 使用匿名函数确保 catchUpStream 被关闭
						defer catchUpStream.Close()
						catchUpReader := bufio.NewReader(catchUpStream)
						var missedLogs []string
						var currentBatchLatestTimestamp time.Time // 记录当前批次中最新的时间戳

						for {
							line, readErr := catchUpReader.ReadString('\n')
							if line != "" {
								content, ts, ok := parseLogLine(line)
								if ok && ts.After(latestTimestamp) { // 只添加比已知时间戳更新的日志
									missedLogs = append(missedLogs, content)
									if ts.After(currentBatchLatestTimestamp) {
										currentBatchLatestTimestamp = ts
									}
								} else if !ok { // 没有时间戳的也添加
									missedLogs = append(missedLogs, content)
								}
							}
							// 批量发送补全的日志
							if len(missedLogs) >= batchSize || (readErr != nil && len(missedLogs) > 0) {
								callback(missedLogs, "") // 发送补全的日志
								missedLogs = nil         // 清空缓冲区
								if currentBatchLatestTimestamp.After(latestTimestamp) {
									latestTimestamp = currentBatchLatestTimestamp // 更新最新时间戳
								}
								currentBatchLatestTimestamp = time.Time{} // 重置批次时间戳
							}

							if readErr != nil {
								if readErr != io.EOF {
									callback(nil, fmt.Sprintf("补全日志读取时出错: %v", readErr))
								}
								break // EOF 或其他错误，结束补全读取
							}
						}
					}()
				} else {
					callback(nil, fmt.Sprintf("尝试补全日志失败: %v", catchUpErr))
				}
			}
			// --- 补全日志结束 ---

			// --- 重新建立 Follow 连接 ---
			followOpts := corev1.PodLogOptions{
				Container:  podLogOpts.Container,
				Previous:   podLogOpts.Previous,
				Timestamps: true,
				Follow:     true,
			}
			if !latestTimestamp.IsZero() {
				followSince := latestTimestamp.Add(time.Nanosecond) // 从最后一条日志之后开始
				followMetaTime := metav1.NewTime(followSince)
				followOpts.SinceTime = &followMetaTime
			} else if options.SinceTime != nil {
				// 如果从未收到过日志，但用户指定了起始时间，则使用用户指定的时间
				followMetaTime := metav1.NewTime(*options.SinceTime)
				followOpts.SinceTime = &followMetaTime
			}
			// 重连时不应再使用 TailLines
			followOpts.TailLines = nil

			newStream, err := getStream(followOpts) // 使用封装的 getStream
			if err != nil {
				return nil, nil, latestTimestamp, fmt.Errorf("重新建立 Follow 连接失败: %w", err)
			}
			return newStream, bufio.NewReader(newStream), latestTimestamp, nil
		}

	readLoop:
		for {
			// 检查是否需要结束处理
			select {
			case <-m.ctx.Done():
				// 上下文取消，停止处理
				if len(buffer) > 0 {
					callback(buffer, "") // 发送剩余日志
				}
				return
			case <-options.StopSignal: // 监听停止信号
				if len(buffer) > 0 {
					callback(buffer, "") // 发送剩余的日志
				}
				callback(nil, "日志流监听已由 StopSignal 主动停止")
				return // 收到信号，退出 goroutine
			default:
				// 检查是否超过了结束时间
				if options.UntilTime != nil && time.Now().After(*options.UntilTime) {
					if len(buffer) > 0 {
						callback(buffer, "") // 发送剩余日志
					}
					return
				}
			}

			// 尝试读取一行日志
			line, err := currentReader.ReadString('\n')
			if line != "" {
				content, ts, ok := parseLogLine(line)
				buffer = append(buffer, content)
				if ok && ts.After(lastLogTimestamp) {
					lastLogTimestamp = ts // 更新最后已知的时间戳
				}
			}

			readErr := err // 保存读取错误以供后续判断

			// 判断是否需要触发回调：缓冲区达到阈值或者距离上次回调的时间超过maxWaitTime，或者发生读取错误时也要处理缓冲区
			if len(buffer) >= batchSize || (len(buffer) > 0 && time.Since(lastCallbackTime) > maxWaitTime) || (readErr != nil && len(buffer) > 0) {
				callback(buffer, "") // 发送日志
				buffer = nil         // 清空缓冲区
				lastCallbackTime = time.Now()
			}

			// 处理读取错误
			if readErr != nil {
				if readErr == io.EOF {
					// 流正常结束? 对于 Follow 流，EOF 通常意味着中断
					// 如果设置了 UntilTime 并且已到期，则正常结束
					if options.UntilTime != nil && time.Now().After(*options.UntilTime) {
						callback(nil, "日志流到达指定结束时间")
						return
					}
					// 否则，认为是非预期 EOF，尝试重连
					readErr = errors.New("unexpected EOF, attempting reconnect") // 模拟连接错误以触发重连逻辑
				}

				// 判断是否是可重试的网络连接错误
				isRetryableError := errors.Is(readErr, context.Canceled) || // Context canceled is not retryable
					strings.Contains(readErr.Error(), "http2: response body closed") ||
					strings.Contains(readErr.Error(), "connection reset by peer") ||
					strings.Contains(readErr.Error(), "broken pipe") ||
					strings.Contains(readErr.Error(), "unexpected EOF") // 将 EOF 也视为可重试

				if isRetryableError && !errors.Is(readErr, context.Canceled) { // 确保不是 context canceled
					// 尝试重连
					if retryCount >= maxRetries {
						callback(nil, fmt.Sprintf("日志流连接持续失败，已尝试重连%d次: %v", retryCount, readErr))
						return // 超过最大重试次数，退出
					}

					retryCount++
					callback(nil, fmt.Sprintf("日志流连接中断，正在尝试重新连接 (尝试 %d/%d): %v", retryCount, maxRetries, readErr))

					// 清理当前连接
					currentStream.Close()
					currentStream = nil // 标记为 nil

					// 使用指数退避策略计算延迟时间
					currentDelay := time.Duration(math.Min(
						float64(retryDelay)*math.Pow(2, float64(retryCount-1)), // retryCount 从 1 开始
						float64(maxRetryDelay),
					))

					// 等待一段时间后重试
					select {
					case <-time.After(currentDelay):
						// 继续重试
					case <-m.ctx.Done():
						return // 上下文取消
					case <-options.StopSignal: // 在等待重连时也检查停止信号
						callback(nil, "日志流监听在重连等待期间由 StopSignal 主动停止")
						return
					}

					// 尝试重新建立连接并补全日志
					newStream, newReader, updatedTimestamp, reconnectErr := tryReconnect(lastLogTimestamp)
					if reconnectErr != nil {
						callback(nil, fmt.Sprintf("重新连接失败 (尝试 %d/%d): %v", retryCount, maxRetries, reconnectErr))
						// 不需要 continue readLoop，因为下次循环会再次检查错误并重试
						continue // 继续外层循环，会再次尝试重连（如果未达最大次数）
					}

					// 更新连接和时间戳
					currentStream = newStream
					currentReader = newReader
					lastLogTimestamp = updatedTimestamp // 使用 tryReconnect 返回的最新时间戳
					retryCount = 0                      // 重置重试计数

					callback(nil, "日志流连接已成功重新建立，继续监控日志...")
					lastCallbackTime = time.Now() // 重置回调时间

					continue readLoop // 连接成功，继续读取新流
				} else {
					// 不可重试的错误，或 context canceled
					errMsg := fmt.Sprintf("读取日志流时发生不可恢复错误: %v", readErr)
					if errors.Is(readErr, context.Canceled) {
						errMsg = "日志流读取被取消"
					}
					callback(nil, errMsg)
					return // 退出 goroutine
				}
			} else {
				// 成功读取一行，重置重试计数
				retryCount = 0
			}
		}
	}()

	// 对于流式获取模式，返回空初始日志和nil错误，实际日志通过回调传递
	return []string{}, nil
}
