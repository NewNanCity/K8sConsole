package websocket

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"city.newnan/k8s-console/internal/middleware"
)

// MessageType 消息类型
const (
	MessageTypeText     = "text"     // 文本消息
	MessageTypePing     = "ping"     // 心跳消息
	MessageTypePong     = "pong"     // 心跳响应
	MessageTypeJoin     = "join"     // 加入房间
	MessageTypeLeave    = "leave"    // 离开房间
	MessageTypeNotify   = "notify"   // 通知
	MessageTypeError    = "error"    // 错误
	MessageTypeCommand  = "command"  // 命令
	MessageTypeResponse = "response" // 响应
	MessageTypeEvent    = "event"    // 事件
)

// Message WebSocket消息结构
type Message struct {
	Type    string      `json:"type"`
	Content interface{} `json:"content"`
}

// HandleWebSocket 处理WebSocket连接
func HandleWebSocket(c *gin.Context) {
	// 从上下文中获取用户信息
	userID := middleware.GetCurrentUserID(c)
	username := middleware.GetCurrentUsername(c)
	roleName, _ := c.Get("role_name")

	// 获取查询参数
	room := c.Query("room")

	// 升级HTTP连接为WebSocket连接
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("升级WebSocket连接失败: %v", err)
		return
	}

	// 创建客户端
	client := &Client{
		ID:         uuid.New().String(),
		Conn:       conn,
		Send:       make(chan []byte, 256),
		UserID:     userID,
		Username:   username,
		RoleName:   roleName.(string),
		Room:       room,
		LastPingAt: time.Now(),
		Manager:    GlobalManager,
		Closed:     false,
	}

	// 注册客户端
	GlobalManager.Register(client)

	// 发送欢迎消息
	welcomeMsg := map[string]interface{}{
		"message":  fmt.Sprintf("欢迎 %s!", username),
		"clientID": client.ID,
	}
	client.Send <- MarshalMessage(MessageTypeJoin, welcomeMsg)

	// 启动goroutine处理WebSocket通信
	go client.writePump()
	go client.readPump()
}

// readPump 从WebSocket连接读取消息
func (c *Client) readPump() {
	defer func() {
		c.Manager.Unregister(c)
		c.Conn.Close()
	}()

	// 设置读取超时
	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		c.LastPingAt = time.Now()
		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("读取WebSocket消息错误: %v", err)
			}
			break
		}

		// 处理接收到的消息
		c.handleMessage(message)
	}
}

// writePump 向WebSocket连接写入消息
func (c *Client) writePump() {
	// 创建ping定时器
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			// 设置写入超时
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				// 通道已关闭
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			// 写入消息
			writer, err := c.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			writer.Write(message)

			// 添加可能排队的消息
			n := len(c.Send)
			for i := 0; i < n; i++ {
				writer.Write([]byte("\n"))
				writer.Write(<-c.Send)
			}

			if err := writer.Close(); err != nil {
				return
			}

		case <-ticker.C:
			// 发送ping消息
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
			// 也发送自定义ping消息
			c.Send <- MarshalMessage(MessageTypePing, nil)
		}
	}
}

// handleMessage 处理接收到的消息
func (c *Client) handleMessage(data []byte) {
	// 更新最后ping时间
	c.LastPingAt = time.Now()

	// 解析消息
	var message Message
	if err := json.Unmarshal(data, &message); err != nil {
		log.Printf("解析消息失败: %v", err)
		c.Send <- MarshalMessage(MessageTypeError, "无效的消息格式")
		return
	}

	// 处理不同类型的消息
	switch message.Type {
	case MessageTypePing:
		// 响应ping
		c.Send <- MarshalMessage(MessageTypePong, nil)

	case MessageTypeJoin:
		// 处理加入房间请求
		if content, ok := message.Content.(map[string]interface{}); ok {
			if roomName, exists := content["room"].(string); exists && roomName != "" {
				// 更新客户端房间
				c.Manager.mutex.Lock()

				// 从旧房间移除
				if c.Room != "" {
					if room, ok := c.Manager.rooms[c.Room]; ok {
						delete(room, c.ID)
						// 如果房间为空，删除房间
						if len(room) == 0 {
							delete(c.Manager.rooms, c.Room)
						}
					}
				}

				// 加入新房间
				c.Room = roomName
				if _, ok := c.Manager.rooms[roomName]; !ok {
					c.Manager.rooms[roomName] = make(map[string]*Client)
				}
				c.Manager.rooms[roomName][c.ID] = c

				c.Manager.mutex.Unlock()

				// 通知客户端已加入房间
				c.Send <- MarshalMessage(MessageTypeJoin, map[string]string{
					"room":    roomName,
					"message": fmt.Sprintf("已加入房间: %s", roomName),
				})

				// 通知房间内其他成员
				c.Manager.Broadcast(&BroadcastMessage{
					Room: roomName,
					Type: MessageTypeNotify,
					Content: map[string]string{
						"message": fmt.Sprintf("用户 %s 加入了房间", c.Username),
					},
					Exclude: c.ID,
				})
			}
		}

	case MessageTypeLeave:
		// 处理离开房间请求
		if c.Room != "" {
			roomName := c.Room

			c.Manager.mutex.Lock()
			// 从房间移除
			if room, ok := c.Manager.rooms[roomName]; ok {
				delete(room, c.ID)
				// 如果房间为空，删除房间
				if len(room) == 0 {
					delete(c.Manager.rooms, roomName)
				}
			}
			c.Room = ""
			c.Manager.mutex.Unlock()

			// 通知客户端已离开房间
			c.Send <- MarshalMessage(MessageTypeLeave, map[string]string{
				"room":    roomName,
				"message": fmt.Sprintf("已离开房间: %s", roomName),
			})

			// 通知房间内其他成员
			c.Manager.Broadcast(&BroadcastMessage{
				Room: roomName,
				Type: MessageTypeNotify,
				Content: map[string]string{
					"message": fmt.Sprintf("用户 %s 离开了房间", c.Username),
				},
			})
		}

	case MessageTypeText:
		// 处理文本消息
		if c.Room == "" {
			c.Send <- MarshalMessage(MessageTypeError, "未加入任何房间，无法发送消息")
			return
		}

		// 广播消息到房间
		c.Manager.Broadcast(&BroadcastMessage{
			Room: c.Room,
			Type: MessageTypeText,
			Content: map[string]interface{}{
				"from":    c.Username,
				"message": message.Content,
				"time":    time.Now().Format("15:04:05"),
			},
		})

	case MessageTypeCommand:
		// 处理命令消息 - 这里根据实际需求实现
		// 示例: 根据用户角色判断是否有权限执行命令
		c.Send <- MarshalMessage(MessageTypeResponse, map[string]string{
			"message": "收到命令，但当前不支持此操作",
		})

	default:
		// 未知消息类型
		c.Send <- MarshalMessage(MessageTypeError, "不支持的消息类型")
	}
}

// MarshalMessage 将消息编码为JSON字符串
func MarshalMessage(msgType string, content interface{}) []byte {
	msg := Message{
		Type:    msgType,
		Content: content,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("编码消息失败: %v", err)
		return []byte(`{"type":"error","content":"消息编码失败"}`)
	}
	return data
}
