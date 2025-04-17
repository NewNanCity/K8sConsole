package sse

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"city.newnan/k8s-console/internal/middleware"
)

// Client SSE客户端
type Client struct {
	ID        string
	Channel   chan []byte
	UserID    uint
	Username  string
	RoleName  string
	Topic     string
	CreatedAt time.Time
}

// Broker 管理所有SSE连接
type Broker struct {
	// 客户端映射表
	clients map[string]*Client
	// 按主题分组的客户端
	topics map[string]map[string]*Client
	// 新客户端通道
	newClients chan *Client
	// 关闭客户端通道
	closingClients chan string
	// 消息通道
	messages chan *Message
	// 互斥锁
	mutex sync.RWMutex
}

// Message SSE消息结构
type Message struct {
	Topic   string      `json:"topic"`
	Event   string      `json:"event"`
	Data    interface{} `json:"data"`
	ID      string      `json:"id,omitempty"`
	Retry   int         `json:"retry,omitempty"`
	Private bool        `json:"private,omitempty"`
	UserID  uint        `json:"user_id,omitempty"`
}

// 全局SSE代理
var GlobalBroker = NewBroker()

// NewBroker 创建新的SSE代理
func NewBroker() *Broker {
	return &Broker{
		clients:        make(map[string]*Client),
		topics:         make(map[string]map[string]*Client),
		newClients:     make(chan *Client),
		closingClients: make(chan string),
		messages:       make(chan *Message),
		mutex:          sync.RWMutex{},
	}
}

// Start 启动SSE代理
func (b *Broker) Start() {
	go b.listen()
}

// listen 监听SSE事件
func (b *Broker) listen() {
	for {
		select {
		case client := <-b.newClients:
			// 添加新客户端
			b.mutex.Lock()
			b.clients[client.ID] = client

			// 如果客户端订阅了特定主题，将其添加到该主题
			if client.Topic != "" {
				if _, ok := b.topics[client.Topic]; !ok {
					b.topics[client.Topic] = make(map[string]*Client)
				}
				b.topics[client.Topic][client.ID] = client
			}
			b.mutex.Unlock()

			log.Printf("SSE客户端已连接: ID=%s, 用户=%s, 主题=%s", client.ID, client.Username, client.Topic)

		case clientID := <-b.closingClients:
			// 关闭客户端
			b.mutex.Lock()
			if client, ok := b.clients[clientID]; ok {
				// 如果客户端在某个主题中，将其从主题中移除
				if client.Topic != "" {
					if topicClients, ok := b.topics[client.Topic]; ok {
						delete(topicClients, client.ID)
						// 如果主题为空，删除主题
						if len(topicClients) == 0 {
							delete(b.topics, client.Topic)
						}
					}
				}

				// 关闭通道
				close(client.Channel)
				// 从客户端映射表中删除
				delete(b.clients, clientID)

				log.Printf("SSE客户端已断开连接: ID=%s, 用户=%s, 主题=%s", client.ID, client.Username, client.Topic)
			}
			b.mutex.Unlock()

		case message := <-b.messages:
			// 发送消息到客户端
			b.mutex.RLock()

			if message.Topic != "" {
				// 发送到特定主题
				if topicClients, ok := b.topics[message.Topic]; ok {
					for _, client := range topicClients {
						// 如果是私有消息，检查用户ID
						if message.Private && message.UserID > 0 && client.UserID != message.UserID {
							continue
						}
						b.sendMessageToClient(client, message)
					}
				}
			} else {
				// 广播到所有客户端
				for _, client := range b.clients {
					// 如果是私有消息，检查用户ID
					if message.Private && message.UserID > 0 && client.UserID != message.UserID {
						continue
					}
					b.sendMessageToClient(client, message)
				}
			}

			b.mutex.RUnlock()
		}
	}
}

// sendMessageToClient 向客户端发送SSE消息
func (b *Broker) sendMessageToClient(client *Client, message *Message) {
	// 格式化SSE消息
	var sseMessage string
	if message.Event != "" {
		sseMessage += fmt.Sprintf("event: %s\n", message.Event)
	}
	if message.ID != "" {
		sseMessage += fmt.Sprintf("id: %s\n", message.ID)
	}
	if message.Retry > 0 {
		sseMessage += fmt.Sprintf("retry: %d\n", message.Retry)
	}

	// 将数据编码为JSON
	dataJSON, err := json.Marshal(message.Data)
	if err != nil {
		log.Printf("编码SSE消息失败: %v", err)
		return
	}
	sseMessage += fmt.Sprintf("data: %s\n\n", dataJSON)

	// 将消息写入客户端通道（非阻塞）
	select {
	case client.Channel <- []byte(sseMessage):
		// 发送成功
	default:
		// 通道已满或已关闭，关闭客户端连接
		b.closingClients <- client.ID
	}
}

// ServeHTTP 处理SSE HTTP连接
func (b *Broker) ServeHTTP(c *gin.Context) {
	// 从上下文中获取用户信息
	userID := middleware.GetCurrentUserID(c)
	username := middleware.GetCurrentUsername(c)
	roleName, _ := c.Get("role_name")
	roleNameStr, _ := roleName.(string)

	// 获取主题参数
	topic := c.Query("topic")

	// 设置SSE头部
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no") // Nginx特定头部，禁用代理缓冲

	// 创建新的SSE客户端
	clientID := uuid.New().String()
	client := &Client{
		ID:        clientID,
		Channel:   make(chan []byte, 256),
		UserID:    userID,
		Username:  username,
		RoleName:  roleNameStr,
		Topic:     topic,
		CreatedAt: time.Now(),
	}

	// 注册新客户端
	b.newClients <- client

	// 通知连接成功
	connectionMsg := &Message{
		Event: "connected",
		Data: map[string]interface{}{
			"client_id": clientID,
			"message":   "已建立SSE连接",
			"time":      time.Now().Format(time.RFC3339),
		},
	}
	b.sendMessageToClient(client, connectionMsg)

	// 设置检测客户端断开连接
	notify := c.Writer.CloseNotify()
	go func() {
		<-notify
		b.closingClients <- clientID
	}()

	// 将消息流式传输到客户端
	c.Stream(func(w io.Writer) bool {
		// 等待消息
		msg, ok := <-client.Channel
		if !ok {
			return false
		}
		// 写入消息
		c.Writer.Write(msg)
		c.Writer.Flush()
		return true
	})
}

// Publish 发布消息到所有客户端或特定主题
func (b *Broker) Publish(message *Message) {
	b.messages <- message
}

// GetClientCount 获取连接的客户端总数
func (b *Broker) GetClientCount() int {
	b.mutex.RLock()
	defer b.mutex.RUnlock()
	return len(b.clients)
}

// GetTopicClientCount 获取特定主题的客户端数
func (b *Broker) GetTopicClientCount(topic string) int {
	b.mutex.RLock()
	defer b.mutex.RUnlock()
	if topicClients, ok := b.topics[topic]; ok {
		return len(topicClients)
	}
	return 0
}

// HandleSSE 处理SSE请求
func HandleSSE(c *gin.Context) {
	GlobalBroker.ServeHTTP(c)
}
