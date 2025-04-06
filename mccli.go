package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
	"golang.org/x/term"

	"city.newnan/k8s-console/pkg/mccontrol"
)

// CLI选项
type cliOptions struct {
	// K8s配置选项
	runMode              string
	kubeconfigPath       string
	namespace            string
	podLabelSelector     string
	serviceLabelSelector string
	containerName        string

	// Minecraft服务器配置
	gamePort     int
	rconPort     int
	rconPassword string

	// CLI配置
	updateInterval time.Duration
	maxLogLines    int64
	enableColor    bool
}

// CLI颜色设置
var (
	// logColor     = color.New(color.FgWhite)
	// infoColor    = color.New(color.FgBlue)
	errorColor   = color.New(color.FgRed)
	successColor = color.New(color.FgGreen)
	promptColor  = color.New(color.FgCyan, color.Bold)
)

// LogLevel 表示日志级别
type LogLevel string

// 日志级别常量
const (
	LogLevelInfo  LogLevel = "INFO"
	LogLevelDebug LogLevel = "DEBUG"
	LogLevelWarn  LogLevel = "WARN"
	LogLevelError LogLevel = "ERROR"
)

// LogLevelColor 表示日志级别对应的ANSI颜色代码
var LogLevelColor = map[LogLevel]string{
	LogLevelInfo:  "\033[37m", // 白色
	LogLevelDebug: "\033[34m", // 蓝色
	LogLevelWarn:  "\033[33m", // 黄色
	LogLevelError: "\033[31m", // 红色
}

// MinecraftFormatCode 表示Minecraft格式控制符的颜色和样式映射
var MinecraftFormatCode = map[rune]string{
	// 颜色代码
	'0': "\033[30m",   // 黑色
	'1': "\033[34;1m", // 深蓝色
	'2': "\033[32;1m", // 深绿色
	'3': "\033[36;1m", // 湖蓝色
	'4': "\033[31;1m", // 深红色
	'5': "\033[35;1m", // 紫色
	'6': "\033[33m",   // 金色
	'7': "\033[37m",   // 灰色
	'8': "\033[30;1m", // 深灰色
	'9': "\033[34m",   // 蓝色
	'a': "\033[32m",   // 绿色
	'b': "\033[36m",   // 天蓝色
	'c': "\033[31m",   // 红色
	'd': "\033[35m",   // 粉红色
	'e': "\033[33m",   // 黄色
	'f': "\033[37;1m", // 白色

	// 格式化代码
	'k': "\033[5m", // 随机字符 (闪烁)
	'l': "\033[1m", // 粗体
	'm': "\033[9m", // 删除线
	'n': "\033[4m", // 下划线
	'o': "\033[3m", // 斜体
	'r': "\033[0m", // 重置
	'x': "",        // 不支持的格式代码
}

// parseMinecraftFormat 解析Minecraft格式控制符并转换为ANSI转义序列
// 支持基于日志级别设置颜色，使 §r 能够重置到日志级别对应的颜色而非白色
func parseMinecraftFormat(text string, logLevel LogLevel) string {
	result := ""
	runes := []rune(text) // 将字符串转换为rune切片以正确处理Unicode字符

	// 获取日志级别对应的颜色代码，默认为白色(INFO)
	logLevelColor := LogLevelColor[LogLevelInfo]
	if color, ok := LogLevelColor[logLevel]; ok {
		logLevelColor = color
	}

	// 在文本开头应用日志级别对应的颜色
	result += logLevelColor

	for i := 0; i < len(runes); i++ {
		// 检查Minecraft格式控制符
		if i < len(runes)-1 && runes[i] == '§' {
			codeIndex := i + 1
			if codeIndex < len(runes) {
				code := runes[codeIndex]
				if ansiCode, ok := MinecraftFormatCode[code]; ok {
					// 特殊处理 §r (重置)，让它重置到日志级别对应的颜色，而非默认的白色
					if code == 'r' {
						result += "\033[0m" + logLevelColor // 先重置所有属性，然后应用日志级别颜色
					} else {
						result += ansiCode
					}
					i += 1 // 跳过格式代码（已经按rune迭代，所以只需+1）
					continue
				}
			}
		}

		// 普通字符
		result += string(runes[i])
	}

	// 确保最后有一个重置符
	if !strings.HasSuffix(result, "\033[0m") {
		result += "\033[0m"
	}

	return result
}

func main() {
	// 解析命令行参数
	options := parseFlags()

	// 创建用于监听终止信号的上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 设置信号处理
	setupSignalHandler(cancel)

	// 创建并配置控制器
	controller, err := createController(options)
	if err != nil {
		errorColor.Fprintf(os.Stderr, "创建Minecraft控制器失败: %v\n", err)
		os.Exit(1)
	}
	defer controller.Close()

	// 启动服务器状态监控
	controller.StartStatusMonitoring(options.updateInterval)
	controller.StartPodInfoMonitoring(options.updateInterval)

	// 检查服务器状态
	status, err := controller.CheckServerStatus()
	if err != nil {
		errorColor.Fprintf(os.Stderr, "检查服务器状态失败: %v\n", err)
	} else if status.Online {
		successColor.Printf("服务器在线! 版本: %s, 玩家: %d/%d\n",
			status.Version, status.Players, status.MaxPlayers)
	} else {
		errorColor.Printf("服务器离线: %s\n", status.LastError)
	}

	// 设置终端参数
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		errorColor.Fprintf(os.Stderr, "设置终端模式失败: %v\n", err)
	} else {
		defer term.Restore(int(os.Stdin.Fd()), oldState)
	}

	// 创建屏幕管理器
	screen := newScreenManager(ctx, options.enableColor)
	defer screen.cleanup()

	// 创建RCON会话
	session, err := controller.CreateCommandSession(30 * time.Minute)
	if err != nil {
		screen.printError(fmt.Sprintf("创建RCON会话失败: %v", err))
	} else {
		screen.cmdSession = session
		screen.sessionActive = true
		screen.printInfo("成功创建RCON持久会话，命令将复用连接")
		defer session.Close()
	}

	// 启动日志流
	screen.printInfo("正在连接到Minecraft服务器日志流...")

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()

		_, err := controller.FetchLogs(mccontrol.LogOptions{
			TailLines:   &options.maxLogLines,
			BatchSize:   10,
			MaxWaitTime: 500 * time.Millisecond,
		}, func(logs []string) {
			for _, line := range logs {
				screen.printLog(line)
			}
		})

		if err != nil {
			screen.printError(fmt.Sprintf("获取日志失败: %v", err))
		}
	}()

	// 启动命令处理循环
	screen.commandLoop(controller)

	// 等待日志处理完成
	wg.Wait()
}

// parseFlags 解析命令行参数
func parseFlags() cliOptions {
	options := cliOptions{}

	// K8s配置选项
	flag.StringVar(&options.runMode, "mode", "InCluster", "运行模式 (InCluster 或 OutOfCluster)")
	flag.StringVar(&options.kubeconfigPath, "kubeconfig", "", "kubeconfig 文件路径 (默认为 ~/.kube/config)")
	flag.StringVar(&options.namespace, "namespace", "default", "Kubernetes 命名空间")
	flag.StringVar(&options.podLabelSelector, "pod-selector", "app=minecraft", "Pod 标签选择器")
	flag.StringVar(&options.serviceLabelSelector, "service-selector", "", "Service 标签选择器 (默认与 pod-selector 相同)")
	flag.StringVar(&options.containerName, "container", "minecraft-server", "容器名称")

	// Minecraft服务器配置
	flag.IntVar(&options.gamePort, "game-port", 25565, "Minecraft 游戏端口")
	flag.IntVar(&options.rconPort, "rcon-port", 25575, "RCON 端口")
	flag.StringVar(&options.rconPassword, "rcon-password", "", "RCON 密码")

	// CLI配置
	flag.DurationVar(&options.updateInterval, "update-interval", 30*time.Second, "状态更新间隔")
	flag.Int64Var(&options.maxLogLines, "max-log-lines", 100, "初始显示的最大日志行数")
	flag.BoolVar(&options.enableColor, "color", isatty.IsTerminal(os.Stdout.Fd()), "启用彩色输出")

	flag.Parse()

	// 验证必需的参数
	if options.rconPassword == "" {
		fmt.Println("错误: 必须提供 RCON 密码")
		flag.Usage()
		os.Exit(1)
	}

	return options
}

// setupSignalHandler 设置信号处理
func setupSignalHandler(cancelFunc context.CancelFunc) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	go func() {
		<-sigs
		cancelFunc()
	}()
}

// createController 创建并配置Minecraft控制器
func createController(options cliOptions) (*mccontrol.MinecraftController, error) {
	k8sConfig := mccontrol.K8sConfig{
		RunMode:              options.runMode,
		KubeconfigPath:       options.kubeconfigPath,
		Namespace:            options.namespace,
		PodLabelSelector:     options.podLabelSelector,
		ServiceLabelSelector: options.serviceLabelSelector,
		ContainerName:        options.containerName,
	}

	controller, err := mccontrol.NewMinecraftController(
		k8sConfig,
		options.gamePort,
		options.rconPort,
		options.rconPassword,
	)

	return controller, err
}

// ScreenManager 管理终端屏幕的显示和交互
type ScreenManager struct {
	ctx            context.Context
	commandBuffer  string
	mutex          sync.Mutex
	enableColor    bool
	termWidth      int
	termHeight     int
	displayedLines int      // 已显示的日志行数
	initialized    bool     // 屏幕是否已初始化
	lastLogLevel   LogLevel // 上一行日志的级别，用于没有明确级别的行

	// 光标和滚动相关
	cursorPos    int // 光标位置（在commandBuffer中的索引）
	scrollOffset int // 水平滚动偏移量，用于显示长命令

	// 命令历史记录相关
	commandHistory     []string // 命令历史记录
	historyMaxSize     int      // 历史记录最大条数
	historyIndex       int      // 当前历史记录索引
	historyTempCommand string   // 临时保存当前命令（浏览历史时使用）

	// RCON会话
	cmdSession    *mccontrol.CommandSession // RCON命令会话
	sessionActive bool                      // 会话是否活跃
}

// monitorTerminalSize 监听终端大小变化（平台特定实现）
func (s *ScreenManager) monitorTerminalSize() {
	prevWidth, prevHeight := s.termWidth, s.termHeight

	// 根据操作系统选择不同的实现方式
	if runtime.GOOS == "windows" {
		// Windows系统使用轮询方式检测终端大小变化
		go func() {
			for {
				select {
				case <-s.ctx.Done():
					return
				default:
					time.Sleep(1 * time.Second) // 每秒检查一次
					s.updateTermSize()

					// 如果尺寸发生变化，调用处理函数
					if prevWidth != s.termWidth || prevHeight != s.termHeight {
						prevWidth, prevHeight = s.termWidth, s.termHeight
						s.handleTerminalResize()
					}
				}
			}
		}()
	} else {
		// Unix/Linux系统使用信号处理方式检测终端大小变化
		// 创建SIGWINCH信号通道
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.Signal(0x1c)) // SIGWINCH

		// 监听信号
		go func() {
			for {
				select {
				case <-s.ctx.Done():
					return
				case <-ch:
					prevWidth, prevHeight := s.termWidth, s.termHeight
					s.updateTermSize()

					// 如果尺寸发生变化，调用处理函数
					if prevWidth != s.termWidth || prevHeight != s.termHeight {
						s.handleTerminalResize()
					}
				}
			}
		}()
	}
}

// newScreenManager 创建一个新的屏幕管理器
func newScreenManager(ctx context.Context, enableColor bool) *ScreenManager {
	sm := &ScreenManager{
		ctx:            ctx,
		commandBuffer:  "",
		enableColor:    enableColor,
		commandHistory: []string{},
		historyMaxSize: 100,   // 默认保存100条历史记录
		historyIndex:   -1,    // -1表示当前不在浏览历史记录
		cursorPos:      0,     // 初始化光标位置
		scrollOffset:   0,     // 初始化滚动偏移量
		sessionActive:  false, // 初始化会话状态
	}

	sm.updateTermSize()
	sm.clearScreen()

	// 启动终端大小变化监听
	go sm.monitorTerminalSize()

	return sm
}

// updateTermSize 更新终端尺寸
func (s *ScreenManager) updateTermSize() {
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err == nil {
		s.termWidth = width
		s.termHeight = height
	} else {
		// 默认大小
		s.termWidth = 80
		s.termHeight = 24
	}
}

// clearScreen 清空屏幕
func (s *ScreenManager) clearScreen() {
	fmt.Print("\033[2J\033[H")
}

// printLog 打印日志行
func (s *ScreenManager) printLog(line string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// 移除行尾的换行符
	line = strings.TrimRight(line, "\r\n")

	// 先清除当前命令行
	fmt.Print("\r")
	fmt.Print(strings.Repeat(" ", s.termWidth))
	fmt.Print("\r")

	// 如果屏幕高度大于已打印的日志行数且这是第一行日志
	// 则先输出空行使日志显示"靠底对齐"
	if s.displayedLines == 0 {
		displaySpace := s.termHeight - 1 // 减去命令行
		emptyLines := displaySpace - 1   // 减去当前要打印的日志行
		if emptyLines > 0 {
			// 输出空行使日志靠底对齐
			for i := 0; i < emptyLines; i++ {
				fmt.Println()
			}
		}
	}

	// 解析日志级别
	logLevel := s.lastLogLevel
	if s.lastLogLevel == "" {
		// 初始化默认日志级别为INFO
		s.lastLogLevel = LogLevelInfo
		logLevel = LogLevelInfo
	}

	// 尝试从日志行开头解析日志级别
	if strings.HasPrefix(line, "[") {
		// 查找第一个 "]" 的位置
		closeBracketPos := strings.Index(line, "]")
		if closeBracketPos > 0 && closeBracketPos < len(line)-1 {
			// 解析类似 [20:19:40 INFO]: 的格式
			logPrefixParts := strings.Split(line[1:closeBracketPos], " ")
			if len(logPrefixParts) >= 2 {
				logLevelStr := strings.TrimRight(logPrefixParts[len(logPrefixParts)-1], ":")

				// 检查是否为已知的日志级别
				switch strings.ToUpper(logLevelStr) {
				case string(LogLevelInfo):
					logLevel = LogLevelInfo
				case string(LogLevelDebug):
					logLevel = LogLevelDebug
				case string(LogLevelWarn):
					logLevel = LogLevelWarn
				case string(LogLevelError):
					logLevel = LogLevelError
				}

				// 更新最后识别的日志级别
				s.lastLogLevel = logLevel
			}
		}
	}

	// 打印日志行
	if s.enableColor {
		fmt.Println(parseMinecraftFormat(line, logLevel))
	} else {
		fmt.Println(line)
	}

	// 增加已显示行数计数
	s.displayedLines++

	// 重新打印命令提示符
	if s.enableColor {
		promptColor.Print("> ")
		fmt.Print(s.commandBuffer)
	} else {
		fmt.Print("> " + s.commandBuffer)
	}
}

// printInfo 打印信息消息
func (s *ScreenManager) printInfo(message string) {
	if s.enableColor {
		s.printLog(fmt.Sprintf("[INFO] %s", message))
	} else {
		s.printLog(fmt.Sprintf("[INFO] %s", message))
	}
}

// printError 打印错误消息
func (s *ScreenManager) printError(message string) {
	if s.enableColor {
		s.printLog(fmt.Sprintf("[ERROR] %s", message))
	} else {
		s.printLog(fmt.Sprintf("[ERROR] %s", message))
	}
}

// redrawScreen 重绘屏幕
func (s *ScreenManager) redrawScreen() {
	s.updateTermSize()

	s.mutex.Lock()
	defer s.mutex.Unlock()

	// 如果还没有初始化，只设置初始化标志，但不清空屏幕
	if !s.initialized {
		s.initialized = true

		// 重新打印命令提示符
		if s.enableColor {
			promptColor.Print("> ")
			fmt.Print(s.commandBuffer)
		} else {
			fmt.Print("> " + s.commandBuffer)
		}
		return
	}

	// 清除当前行
	fmt.Print("\r")
	fmt.Print(strings.Repeat(" ", s.termWidth))
	fmt.Print("\r")

	// 确定要显示的命令缓冲区部分
	visibleWidth := s.termWidth - 2 // 减去提示符和可能的光标
	displayCmd := s.commandBuffer

	// 如果命令太长需要滚动显示
	if len(s.commandBuffer) > visibleWidth {
		// 确保不会越界
		if s.scrollOffset > len(s.commandBuffer)-visibleWidth {
			s.scrollOffset = len(s.commandBuffer) - visibleWidth
		}
		if s.scrollOffset < 0 {
			s.scrollOffset = 0
		}

		// 只显示可视范围内的文本
		if s.scrollOffset+visibleWidth <= len(displayCmd) {
			displayCmd = displayCmd[s.scrollOffset : s.scrollOffset+visibleWidth]
		} else {
			displayCmd = displayCmd[s.scrollOffset:]
		}
	}

	// 重新打印命令提示符和可见部分的命令
	if s.enableColor {
		promptColor.Print("> ")
		fmt.Print(displayCmd)
	} else {
		fmt.Print("> " + displayCmd)
	}

	// 将光标定位到正确的位置
	cursorScreenPos := s.cursorPos - s.scrollOffset + 2 // +2 是因为提示符"> "
	if cursorScreenPos >= 2 && cursorScreenPos <= s.termWidth {
		// 使用ANSI转义序列将光标移动到指定位置
		// \r 将光标移动到行首
		// \033[nC 将光标向右移动n列
		fmt.Printf("\r\033[%dC", cursorScreenPos)
	}
}

// commandLoop 命令处理循环
func (s *ScreenManager) commandLoop(controller *mccontrol.MinecraftController) {
	// 创建一个新的阅读器来处理原始输入
	reader := bufio.NewReader(os.Stdin)

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			// 读取按键
			r, _, err := reader.ReadRune()
			if err != nil {
				if err.Error() == "EOF" {
					return
				}
				s.printError(fmt.Sprintf("输入错误: %v", err))
				continue
			}

			// 处理特殊键序列 (例如箭头键)
			if r == '\033' {
				// 可能是转义序列的开始
				if reader.Buffered() > 0 {
					r1, _, err := reader.ReadRune()
					if err != nil || r1 != '[' {
						continue // 不是箭头键序列
					}

					r2, _, err := reader.ReadRune()
					if err != nil {
						continue
					}

					// 处理箭头键
					switch r2 {
					case 'A': // 上箭头键
						s.navigateHistory(-1)
						s.redrawScreen()
						continue
					case 'B': // 下箭头键
						s.navigateHistory(1)
						s.redrawScreen()
						continue
					case 'C': // 右箭头键
						s.moveCursor(1)
						s.redrawScreen()
						continue
					case 'D': // 左箭头键
						s.moveCursor(-1)
						s.redrawScreen()
						continue
					}
				}
			}

			// 根据输入处理
			switch r {
			case '\r', '\n': // 回车键
				command := strings.TrimSpace(s.commandBuffer)
				if command != "" {
					// 执行命令
					s.executeCommand(controller, command)
					// 保存命令到历史记录
					s.addToHistory(command)
				}

				// 清空命令缓冲区
				s.mutex.Lock()
				s.commandBuffer = ""
				s.cursorPos = 0     // 重置光标位置
				s.scrollOffset = 0  // 重置滚动偏移
				s.historyIndex = -1 // 重置历史记录索引
				s.mutex.Unlock()
				s.redrawScreen()

			case 127, 8: // 退格键
				s.mutex.Lock()
				if len(s.commandBuffer) > 0 && s.cursorPos > 0 {
					// 删除光标前的字符
					s.commandBuffer = s.commandBuffer[:s.cursorPos-1] + s.commandBuffer[s.cursorPos:]
					s.cursorPos--
					// 调整滚动位置
					s.adjustScrollOffset()
				}
				s.mutex.Unlock()
				s.redrawScreen()

			case 3, 4: // Ctrl+C 或 Ctrl+D
				return

			default:
				// 添加普通字符
				s.mutex.Lock()
				// 在光标位置插入字符
				if s.cursorPos == len(s.commandBuffer) {
					s.commandBuffer += string(r)
				} else {
					s.commandBuffer = s.commandBuffer[:s.cursorPos] + string(r) + s.commandBuffer[s.cursorPos:]
				}
				s.cursorPos++
				// 调整滚动位置
				s.adjustScrollOffset()
				s.mutex.Unlock()
				s.redrawScreen()
			}
		}
	}
}

// executeCommand 执行Minecraft命令
func (s *ScreenManager) executeCommand(controller *mccontrol.MinecraftController, command string) {
	// 在日志中显示命令
	s.printInfo(fmt.Sprintf("执行命令: %s", command))

	// 本地命令处理
	if strings.HasPrefix(command, "/local ") {
		s.handleLocalCommand(strings.TrimPrefix(command, "/local "), controller)
		return
	}

	var response string
	var err error

	// 使用会话执行命令（如果会话有效）
	if s.sessionActive && s.cmdSession != nil {
		response, err = s.cmdSession.ExecuteCommand(command)
	} else {
		// 如果会话无效，使用一次性命令
		response, err = controller.ExecuteRconCommand(command)
	}

	if err != nil {
		s.printError(fmt.Sprintf("执行命令失败: %v", err))
	} else {
		s.printLog(fmt.Sprintf("服务器响应: %s", response))
	}
}

// handleLocalCommand 处理本地CLI命令
func (s *ScreenManager) handleLocalCommand(command string, controller *mccontrol.MinecraftController) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return
	}

	switch parts[0] {
	case "status":
		// 检查服务器状态
		status, err := controller.CheckServerStatus()
		if err != nil {
			s.printError(fmt.Sprintf("检查服务器状态失败: %v", err))
		} else if status.Online {
			s.printLog("服务器状态: 在线")
			s.printLog(fmt.Sprintf("版本: %s", status.Version))
			s.printLog(fmt.Sprintf("玩家: %d/%d", status.Players, status.MaxPlayers))
			s.printLog(fmt.Sprintf("描述: %s", status.Description))
			s.printLog(fmt.Sprintf("延迟: %d ms", status.Latency))
			s.printLog(fmt.Sprintf("Pod: %s (%s)", status.PodName, status.PodStatus))
			s.printLog(fmt.Sprintf("IP: %s (集群内), %s (外部)", status.ClusterIP, status.ExternalIP))
		} else {
			s.printError(fmt.Sprintf("服务器离线: %s", status.LastError))
		}

	case "clear":
		// 清除日志缓冲区
		s.mutex.Lock()
		s.displayedLines = 0
		s.mutex.Unlock()
		s.redrawScreen()

	case "help":
		// 显示帮助信息
		s.printLog("可用的本地命令:")
		s.printLog("  /local status  - 显示服务器状态信息")
		s.printLog("  /local clear   - 清除日志显示")
		s.printLog("  /local help    - 显示此帮助信息")
		s.printLog("  /local exit    - 退出程序")
		s.printLog("")
		s.printLog("所有其他输入将作为RCON命令发送到Minecraft服务器")

	case "exit":
		// 退出程序
		os.Exit(0)

	default:
		s.printError(fmt.Sprintf("未知的本地命令: %s", parts[0]))
		s.printLog("输入 '/local help' 获取可用命令列表")
	}
}

// cleanup 清理屏幕
func (s *ScreenManager) cleanup() {
	s.clearScreen()
}

// handleTerminalResize 处理终端尺寸变化
func (s *ScreenManager) handleTerminalResize() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// 如果终端行数减小，需要重置已显示行数计数
	// 这确保在tmux等工具中分屏或调整大小时能够正确显示
	if s.displayedLines > s.termHeight-1 {
		// 将显示行数限制为终端高度-1(命令行)
		s.displayedLines = s.termHeight - 1
	}

	// 清空当前行以便重新显示命令提示符
	fmt.Print("\r")
	fmt.Print(strings.Repeat(" ", s.termWidth))
	fmt.Print("\r")

	// 重新打印命令提示符
	if s.enableColor {
		promptColor.Print("> ")
		fmt.Print(s.commandBuffer)
	} else {
		fmt.Print("> " + s.commandBuffer)
	}
}

// navigateHistory 浏览命令历史记录
func (s *ScreenManager) navigateHistory(direction int) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if len(s.commandHistory) == 0 {
		return
	}

	// 首次浏览历史记录
	if s.historyIndex == -1 {
		// 初次浏览历史记录时，保存当前命令
		s.historyTempCommand = s.commandBuffer

		// 根据方向决定从历史记录的开头还是结尾开始浏览
		if direction < 0 {
			// 向上浏览（历史记录的最新命令）
			s.historyIndex = len(s.commandHistory) - 1
		} else {
			// 向下浏览（历史记录的最早命令）
			s.historyIndex = 0
		}
	} else {
		// 已经在浏览历史记录中，继续移动索引
		newIndex := s.historyIndex + direction

		// 索引超出范围处理
		if newIndex < 0 {
			// 向上已无更多历史，保持在第一条
			newIndex = 0
		} else if newIndex >= len(s.commandHistory) {
			// 向下已达到最后一条历史，退出历史浏览模式
			s.commandBuffer = s.historyTempCommand
			s.historyIndex = -1
			return
		}

		s.historyIndex = newIndex
	}

	// 更新命令缓冲区为历史记录中的命令
	if s.historyIndex >= 0 && s.historyIndex < len(s.commandHistory) {
		s.commandBuffer = s.commandHistory[s.historyIndex]
	}
}

// addToHistory 添加命令到历史记录
func (s *ScreenManager) addToHistory(command string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// 如果命令与最后一条历史记录相同，不重复添加
	if len(s.commandHistory) > 0 && s.commandHistory[len(s.commandHistory)-1] == command {
		return
	}

	// 添加命令到历史记录
	s.commandHistory = append(s.commandHistory, command)

	// 如果历史记录超过最大条数，移除最旧的记录
	if len(s.commandHistory) > s.historyMaxSize {
		s.commandHistory = s.commandHistory[1:]
	}
}

// moveCursor 移动光标位置
func (s *ScreenManager) moveCursor(direction int) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	newPos := s.cursorPos + direction
	if newPos < 0 {
		newPos = 0
	} else if newPos > len(s.commandBuffer) {
		newPos = len(s.commandBuffer)
	}

	s.cursorPos = newPos
	s.adjustScrollOffset()
}

// adjustScrollOffset 调整滚动偏移量
func (s *ScreenManager) adjustScrollOffset() {
	// 确保光标在可视区域内
	visibleWidth := s.termWidth - 2 // 减去提示符

	// 如果光标位于滚动偏移量之前，调整滚动偏移量使光标可见
	if s.cursorPos < s.scrollOffset {
		s.scrollOffset = s.cursorPos
	} else if s.cursorPos >= s.scrollOffset+visibleWidth {
		// 如果光标超出了当前可视区域的右边界，调整滚动偏移量
		s.scrollOffset = s.cursorPos - visibleWidth + 1
	}
}
