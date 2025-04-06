/*
Package mccontrol 提供了与在Kubernetes集群中部署的Minecraft服务器交互的功能。

主要特性:

  - 服务器状态监控：检测服务器在线状态、玩家数量、版本等信息
  - 日志管理：获取历史日志和实时日志流
  - 命令执行：通过RCON协议执行Minecraft服务器命令
  - 灵活部署：支持在Kubernetes集群内部或外部运行

此包依赖于github.com/xrjr/mcutils来实现与Minecraft服务器的通信协议。

基本用法:

	k8sConfig := mccontrol.K8sConfig{
		RunMode:         "InCluster",  // 或 "OutOfCluster"
		Namespace:       "minecraft",
		PodLabelSelector: "app=minecraft",
		ContainerName:   "minecraft-server",
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

	// 执行命令
	response, err := controller.ExecuteRconCommand("list")

	// 获取日志
	logs, err := controller.GetInitialLogs(100)
*/
package mccontrol
