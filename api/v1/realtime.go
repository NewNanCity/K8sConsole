package v1

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"city.newnan/k8s-console/internal/middleware"
	"city.newnan/k8s-console/internal/model"
	"city.newnan/k8s-console/internal/sse"
	"city.newnan/k8s-console/internal/websocket"
)

// RealtimeController 实时通信相关API控制器
type RealtimeController struct{}

// NewRealtimeController 创建实时通信控制器
func NewRealtimeController() *RealtimeController {
	return &RealtimeController{}
}

// HandleWebSocket 处理WebSocket连接
// @Summary WebSocket连接
// @Description 建立WebSocket长连接进行实时通信
// @Tags 实时通信
// @Param room query string false "房间名称"
// @Security ApiKeyAuth
// @Success 101 {string} string "切换为WebSocket协议"
// @Failure 401 {object} model.Response "未授权"
// @Failure 403 {object} model.Response "权限不足"
// @Failure 500 {object} model.Response "服务器内部错误"
// @Router /api/v1/ws [get]
func (c *RealtimeController) HandleWebSocket(ctx *gin.Context) {
	websocket.HandleWebSocket(ctx)
}

// HandleSSE 处理服务器发送事件(SSE)
// @Summary SSE连接
// @Description 建立SSE长连接接收服务器推送事件
// @Tags 实时通信
// @Param topic query string false "主题"
// @Security ApiKeyAuth
// @Success 200 {string} string "SSE数据流"
// @Failure 401 {object} model.Response "未授权"
// @Failure 403 {object} model.Response "权限不足"
// @Failure 500 {object} model.Response "服务器内部错误"
// @Router /api/v1/sse [get]
func (c *RealtimeController) HandleSSE(ctx *gin.Context) {
	sse.HandleSSE(ctx)
}

// BroadcastMessage 广播消息到所有WebSocket客户端
// @Summary 广播WebSocket消息
// @Description 向所有WebSocket客户端或指定房间广播消息
// @Tags 实时通信
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param message body websocket.BroadcastMessage true "广播消息"
// @Success 200 {object} model.Response "广播成功"
// @Failure 400 {object} model.Response "请求参数错误"
// @Failure 401 {object} model.Response "未授权"
// @Failure 403 {object} model.Response "权限不足"
// @Failure 500 {object} model.Response "服务器内部错误"
// @Router /api/v1/ws/broadcast [post]
func (c *RealtimeController) BroadcastMessage(ctx *gin.Context) {
	var message websocket.BroadcastMessage
	if err := ctx.ShouldBindJSON(&message); err != nil {
		ctx.JSON(http.StatusBadRequest, model.ErrorResponse(http.StatusBadRequest, "无效的请求参数: "+err.Error()))
		return
	}

	websocket.GlobalManager.Broadcast(&message)
	ctx.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// PublishSSEEvent 发布SSE事件
// @Summary 发布SSE事件
// @Description 发布事件到SSE客户端
// @Tags 实时通信
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param event body sse.Message true "事件消息"
// @Success 200 {object} model.Response "发布成功"
// @Failure 400 {object} model.Response "请求参数错误"
// @Failure 401 {object} model.Response "未授权"
// @Failure 403 {object} model.Response "权限不足"
// @Failure 500 {object} model.Response "服务器内部错误"
// @Router /api/v1/sse/publish [post]
func (c *RealtimeController) PublishSSEEvent(ctx *gin.Context) {
	var message sse.Message
	if err := ctx.ShouldBindJSON(&message); err != nil {
		ctx.JSON(http.StatusBadRequest, model.ErrorResponse(http.StatusBadRequest, "无效的请求参数: "+err.Error()))
		return
	}

	sse.GlobalBroker.Publish(&message)
	ctx.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// GetRealtimeStats 获取实时连接统计
// @Summary 获取实时连接统计
// @Description 获取WebSocket和SSE的连接统计信息
// @Tags 实时通信
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} model.Response "获取成功"
// @Failure 401 {object} model.Response "未授权"
// @Failure 403 {object} model.Response "权限不足"
// @Router /api/v1/realtime/stats [get]
func (c *RealtimeController) GetRealtimeStats(ctx *gin.Context) {
	// 获取当前用户信息
	userID := middleware.GetCurrentUserID(ctx)
	username := middleware.GetCurrentUsername(ctx)

	// WebSocket的客户端连接数
	wsClients := len(websocket.GlobalManager.GetClientsByUsername(username))

	// SSE的客户端连接数
	sseClients := sse.GlobalBroker.GetClientCount()

	ctx.JSON(http.StatusOK, model.SuccessResponse(map[string]interface{}{
		"websocket_total": websocket.GlobalManager.GetClientCount(),
		"websocket_user":  wsClients,
		"sse_total":       sseClients,
		"timestamp":       time.Now().Format(time.RFC3339),
		"user_id":         userID,
		"username":        username,
	}))
}
