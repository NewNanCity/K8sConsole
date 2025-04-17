/*
Package mccontrol 提供了与在Kubernetes集群中部署的Minecraft服务器交互的功能。

主要特性:

  - 服务器状态监控：检测服务器在线状态、玩家数量、版本等信息
  - 日志管理：获取历史日志和实时日志流
  - 命令执行：通过RCON协议执行Minecraft服务器命令
  - 命令会话管理：支持创建持久化RCON会话以进行连续命令交互
  - 灵活部署：支持在Kubernetes集群内部或外部运行

此包依赖于github.com/xrjr/mcutils来实现与Minecraft服务器的通信协议。

基本用法:

	k8sConfig := mccontrol.K8sConfig{
		RunMode:          "InCluster",  // 或 "OutOfCluster"
		Namespace:        "minecraft",
		PodLabelSelector: "app=minecraft",
		ContainerName:    "minecraft-server",
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

	// 检查服务器状态
	status, err := controller.CheckServerStatus()

	// 直接执行单个命令
	response, err := controller.ExecuteRconCommand("list")

	// 获取日志
	logs, err := controller.FetchLogs(mccontrol.LogOptions{TailLines: int64ptr(100)}, nil)

命令会话管理:

mccontrol包支持创建持久化的RCON命令会话，适用于需要执行多条命令的场景，避免频繁的连接/断开开销：

	// 创建一个命令会话，30分钟不使用后自动关闭
	session, err := controller.CreateCommandSession(30 * time.Minute)
	if err != nil {
		// 处理错误
	}

	// 在会话中执行命令
	response, err := session.ExecuteCommand("list")

	// 继续执行其他命令...
	response, err = session.ExecuteCommand("time set day")

	// 操作完成后关闭会话
	session.Close()

	// 也可以使用会话ID管理会话
	sessionID := session.id

	// 使用ID执行命令
	response, err = controller.SessionExecuteCommand(sessionID, "weather clear")

	// 列出所有活跃会话
	sessions := controller.ListCommandSessions()

	// 关闭指定会话
	controller.CloseCommandSession(sessionID)

	// 关闭所有会话
	controller.CloseAllCommandSessions()

日志流处理:

通过提供回调函数，可以接收持续的日志流：

	// 定义日志处理函数
	logHandler := func(logs []string) {
		for _, line := range logs {
			fmt.Print(line)
		}
	}

	// 获取实时日志流
	_, err := controller.FetchLogs(mccontrol.LogOptions{}, logHandler)
*/
package mccontrol
