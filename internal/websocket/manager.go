package websocket

import (
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// 设置 websocket 连接的配置
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// 允许所有域的请求
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// Client 表示 WebSocket 客户端
type Client struct {
	ID          string
	Conn        *websocket.Conn
	Send        chan []byte
	UserID      uint
	Username    string
	RoleName    string
	Room        string
	LastPingAt  time.Time
	Manager     *Manager
	Closed      bool
	ClosedMutex sync.Mutex
}

// Manager 管理 WebSocket 连接
type Manager struct {
	// 所有客户端
	clients map[string]*Client
	// 按房间分组的客户端
	rooms map[string]map[string]*Client
	// 互斥锁
	mutex sync.RWMutex
	// 注册通道
	register chan *Client
	// 注销通道
	unregister chan *Client
	// 广播通道
	broadcast chan *BroadcastMessage
}

// BroadcastMessage 广播消息结构
type BroadcastMessage struct {
	Room    string      `json:"room,omitempty"`
	Type    string      `json:"type"`
	Content interface{} `json:"content"`
	Exclude string      `json:"exclude,omitempty"`
}

// 全局 WebSocket 管理器
var GlobalManager = NewManager()

// NewManager 创建新的管理器
func NewManager() *Manager {
	return &Manager{
		clients:    make(map[string]*Client),
		rooms:      make(map[string]map[string]*Client),
		mutex:      sync.RWMutex{},
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan *BroadcastMessage),
	}
}

// Start 启动 WebSocket 管理器
func (m *Manager) Start() {
	go m.run()
}

// run 运行 WebSocket 管理器的主循环
func (m *Manager) run() {
	// 处理心跳和断线检测
	go func() {
		heartbeatTicker := time.NewTicker(10 * time.Second)
		for range heartbeatTicker.C {
			m.checkHeartbeats()
		}
	}()

	for {
		select {
		// 注册新客户端
		case client := <-m.register:
			m.mutex.Lock()
			m.clients[client.ID] = client

			// 如果指定了房间，将客户端加入房间
			if client.Room != "" {
				if _, ok := m.rooms[client.Room]; !ok {
					m.rooms[client.Room] = make(map[string]*Client)
				}
				m.rooms[client.Room][client.ID] = client
			}

			m.mutex.Unlock()
			log.Printf("客户端注册: %s, 用户: %s, 房间: %s", client.ID, client.Username, client.Room)

		// 注销客户端
		case client := <-m.unregister:
			if client == nil {
				continue
			}

			// 防止重复关闭
			client.ClosedMutex.Lock()
			if client.Closed {
				client.ClosedMutex.Unlock()
				continue
			}
			client.Closed = true
			client.ClosedMutex.Unlock()

			m.mutex.Lock()
			// 从全局客户端列表中删除
			delete(m.clients, client.ID)

			// 从房间中删除
			if client.Room != "" {
				if room, ok := m.rooms[client.Room]; ok {
					delete(room, client.ID)
					// 如果房间为空，删除房间
					if len(room) == 0 {
						delete(m.rooms, client.Room)
					}
				}
			}
			m.mutex.Unlock()

			// 关闭发送通道
			close(client.Send)
			log.Printf("客户端注销: %s, 用户: %s, 房间: %s", client.ID, client.Username, client.Room)

		// 广播消息
		case message := <-m.broadcast:
			m.mutex.RLock()
			// 如果指定了房间
			if message.Room != "" {
				if room, ok := m.rooms[message.Room]; ok {
					for id, client := range room {
						// 排除指定客户端
						if id != message.Exclude {
							m.sendMessage(client, message)
						}
					}
				}
			} else {
				// 全局广播
				for id, client := range m.clients {
					// 排除指定客户端
					if id != message.Exclude {
						m.sendMessage(client, message)
					}
				}
			}
			m.mutex.RUnlock()
		}
	}
}

// sendMessage 向客户端发送消息
func (m *Manager) sendMessage(client *Client, message *BroadcastMessage) {
	// 检查客户端是否已关闭
	client.ClosedMutex.Lock()
	if client.Closed {
		client.ClosedMutex.Unlock()
		return
	}
	client.ClosedMutex.Unlock()

	// 将消息发送到客户端的发送通道
	// 非阻塞发送
	select {
	case client.Send <- []byte(MarshalMessage(message.Type, message.Content)):
		// 发送成功
	default:
		// 发送失败，客户端可能已断开或缓冲区已满
		client.ClosedMutex.Lock()
		if !client.Closed {
			close(client.Send)
			client.Closed = true
		}
		client.ClosedMutex.Unlock()

		m.mutex.Lock()
		delete(m.clients, client.ID)
		if client.Room != "" {
			if room, ok := m.rooms[client.Room]; ok {
				delete(room, client.ID)
				if len(room) == 0 {
					delete(m.rooms, client.Room)
				}
			}
		}
		m.mutex.Unlock()
	}
}

// checkHeartbeats 检查所有客户端的心跳
func (m *Manager) checkHeartbeats() {
	timeout := time.Now().Add(-60 * time.Second)

	m.mutex.Lock()
	defer m.mutex.Unlock()

	for id, client := range m.clients {
		if client.LastPingAt.Before(timeout) {
			log.Printf("客户端 %s 心跳超时，正在断开连接", id)

			client.ClosedMutex.Lock()
			if !client.Closed {
				client.Conn.Close()
				client.Closed = true
				close(client.Send)
			}
			client.ClosedMutex.Unlock()

			delete(m.clients, id)
			if client.Room != "" {
				if room, ok := m.rooms[client.Room]; ok {
					delete(room, id)
					if len(room) == 0 {
						delete(m.rooms, client.Room)
					}
				}
			}
		}
	}
}

// GetRoomClients 获取房间中的所有客户端
func (m *Manager) GetRoomClients(room string) []*Client {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	var clients []*Client
	if roomClients, ok := m.rooms[room]; ok {
		for _, client := range roomClients {
			clients = append(clients, client)
		}
	}
	return clients
}

// GetClient 根据ID获取客户端
func (m *Manager) GetClient(clientID string) (*Client, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if client, ok := m.clients[clientID]; ok {
		return client, nil
	}
	return nil, errors.New("客户端不存在")
}

// GetClientsByUserID 根据用户ID获取所有客户端
func (m *Manager) GetClientsByUserID(userID uint) []*Client {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	var clients []*Client
	for _, client := range m.clients {
		if client.UserID == userID {
			clients = append(clients, client)
		}
	}
	return clients
}

// GetClientsByUsername 根据用户名获取所有客户端
func (m *Manager) GetClientsByUsername(username string) []*Client {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	var clients []*Client
	for _, client := range m.clients {
		if client.Username == username {
			clients = append(clients, client)
		}
	}
	return clients
}

// Broadcast 广播消息
func (m *Manager) Broadcast(message *BroadcastMessage) {
	m.broadcast <- message
}

// Register 注册客户端
func (m *Manager) Register(client *Client) {
	m.register <- client
}

// Unregister 注销客户端
func (m *Manager) Unregister(client *Client) {
	m.unregister <- client
}
