package test

import (
	"fmt"
	"time"

	"city.newnan/k8s-console/pkg/mccontrol"
)

func Example_inCluster() {
	// 在K8s集群内运行的配置
	k8sConfig := mccontrol.K8sConfig{
		RunMode:              "InCluster",
		Namespace:            "newnancity",
		PodLabelSelector:     "app=server-main",
		ServiceLabelSelector: "app=server-main-svc",
		ContainerName:        "container-newnancity-server-main",
	}

	controller, err := mccontrol.NewMinecraftController(
		k8sConfig,
		25565,                // 游戏端口
		25575,                // RCON 端口
		"minecraft-password", // RCON 密码
	)
	if err != nil {
		fmt.Printf("创建控制器失败: %v\n", err)
		return
	}
	defer controller.Close()

	// 使用控制器...
}

func Example_outOfCluster() {
	// 在K8s集群外运行的配置
	k8sConfig := mccontrol.K8sConfig{
		RunMode:          "OutOfCluster",
		KubeconfigPath:   "", // 空字符串将使用默认位置 ~/.kube/config
		Namespace:        "minecraft",
		PodLabelSelector: "app=minecraft",
		ContainerName:    "minecraft-server",
	}

	controller, err := mccontrol.NewMinecraftController(
		k8sConfig,
		25565,                // 游戏端口
		25575,                // RCON 端口
		"minecraft-password", // RCON 密码
	)
	if err != nil {
		fmt.Printf("创建控制器失败: %v\n", err)
		return
	}
	defer controller.Close()

	// 获取服务器状态
	status, err := controller.CheckServerStatus()
	if err != nil {
		fmt.Printf("检查服务器状态失败: %v\n", err)
	} else if status.Online {
		fmt.Printf("服务器在线! 版本: %s, 玩家: %d/%d\n",
			status.Version, status.Players, status.MaxPlayers)
	} else {
		fmt.Printf("服务器离线: %s\n", status.LastError)
	}
}

// Example_smartPodUpdates 展示智能Pod信息更新机制的使用方法
func Example_smartPodUpdates() {
	// 创建控制器
	k8sConfig := mccontrol.K8sConfig{
		RunMode:          "OutOfCluster",
		Namespace:        "minecraft",
		PodLabelSelector: "app=minecraft",
		ContainerName:    "minecraft-server",
	}

	controller, err := mccontrol.NewMinecraftController(
		k8sConfig,
		25565, 25575, "minecraft-password",
	)
	if err != nil {
		fmt.Printf("创建控制器失败: %v\n", err)
		return
	}
	defer controller.Close()

	// 配置Pod信息更新间隔
	controller.SetPodInfoUpdateInterval(3 * time.Minute)

	// 启动后台Pod信息监控
	controller.StartPodInfoMonitoring(5 * time.Minute)

	// 在需要时强制更新Pod信息
	if err := controller.ForceUpdatePodInfo(); err != nil {
		fmt.Printf("强制更新Pod信息失败: %v\n", err)
	}

	// 启动状态监控（每30秒自动检查服务器状态）
	controller.StartStatusMonitoring(30 * time.Second)

	// 模拟主程序运行（实际应用中可能是Web服务器或其他长期运行的进程）
	fmt.Println("服务启动成功，使用智能Pod更新机制...")
	// 在实际应用中，这里可能是main函数的阻塞代码，如http.ListenAndServe
}

// Example_logsUsage 展示改进的日志获取功能的使用方法
func Example_logsUsage() {
	controller, err := mccontrol.NewMinecraftController(
		mccontrol.K8sConfig{
			RunMode:          "OutOfCluster",
			Namespace:        "minecraft",
			PodLabelSelector: "app=minecraft",
			ContainerName:    "minecraft-server",
		},
		25565, 25575, "minecraft-password",
	)
	if err != nil {
		fmt.Printf("创建控制器失败: %v\n", err)
		return
	}
	defer controller.Close()

	// 示例1: 一次性获取最近100行日志
	tailLines := int64(100)
	logs, err := controller.FetchLogs(mccontrol.LogOptions{
		TailLines: &tailLines,
	}, nil)
	if err != nil {
		fmt.Printf("获取日志失败: %v\n", err)
	} else {
		fmt.Printf("获取到%d行日志\n", len(logs))
		// 处理日志...
	}

	// 示例2: 获取特定时间段的日志
	startTime := time.Now().Add(-1 * time.Hour)
	endTime := time.Now()
	logs, err = controller.FetchLogs(mccontrol.LogOptions{
		SinceTime: &startTime,
		UntilTime: &endTime,
	}, nil)
	if err != nil {
		fmt.Printf("获取时间段日志失败: %v\n", err)
	}

	// 示例3: 流式获取日志（每收集10行或最多等待500ms回调一次）
	doneChan := make(chan struct{})
	go func() {
		controller.FetchLogs(mccontrol.LogOptions{
			BatchSize:   10,
			MaxWaitTime: 500 * time.Millisecond,
			UntilTime:   &endTime, // 只获取到endTime为止
		}, func(logs []string) {
			fmt.Printf("收到%d行新日志\n", len(logs))
			// 处理日志...

			// 检查是否需要停止（实际应用中可能通过channel或context控制）
			if time.Now().After(endTime) {
				close(doneChan)
			}
		})
	}()

	// 等待日志获取完成
	<-doneChan
	fmt.Println("日志获取完成")
}

// Example_rconCommands 展示RCON命令执行
func Example_rconCommands() {
	controller, err := mccontrol.NewMinecraftController(
		mccontrol.K8sConfig{
			RunMode:          "OutOfCluster",
			Namespace:        "minecraft",
			PodLabelSelector: "app=minecraft",
			ContainerName:    "minecraft-server",
		},
		25565, 25575, "minecraft-password",
	)
	if err != nil {
		fmt.Printf("创建控制器失败: %v\n", err)
		return
	}
	defer controller.Close()

	// 执行列出玩家的命令
	response, err := controller.ExecuteRconCommand("list")
	if err != nil {
		fmt.Printf("执行RCON命令失败: %v\n", err)
	} else {
		fmt.Printf("命令响应: %s\n", response)
	}

	// 执行给玩家物品的命令
	playerName := "Steve"
	command := fmt.Sprintf("give %s diamond 64", playerName)
	response, err = controller.ExecuteRconCommand(command)
	if err != nil {
		fmt.Printf("给予物品失败: %v\n", err)
	} else {
		fmt.Printf("给予物品成功: %s\n", response)
	}
}
