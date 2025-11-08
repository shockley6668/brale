package livehttp

import (
	"net/http"
	"strconv"

	"brale/internal/gateway/database"

	"github.com/gin-gonic/gin"
)

// Router 暴露实盘相关的查询接口（决策/订单）。
type Router struct {
	Logs *database.DecisionLogStore
}

// NewRouter 构造 live HTTP router。
func NewRouter(logs *database.DecisionLogStore) *Router {
	return &Router{Logs: logs}
}

// Register 将 /api/live 路由挂载到给定分组下。
func (r *Router) Register(group *gin.RouterGroup) {
	if group == nil {
		return
	}
	group.GET("/decisions", r.handleLiveDecisions)
	group.GET("/orders", r.handleLiveOrders)
}

func (r *Router) handleLiveDecisions(c *gin.Context) {
	if r.Logs == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "实时日志未启用"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	query := database.LiveDecisionQuery{
		Limit:    limit,
		Provider: c.Query("provider"),
		Stage:    c.Query("stage"),
		Symbol:   c.Query("symbol"),
	}
	logs, err := r.Logs.ListDecisions(c.Request.Context(), query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"logs": logs})
}

func (r *Router) handleLiveOrders(c *gin.Context) {
	if r.Logs == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "实时日志未启用"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	symbol := c.Query("symbol")
	orders, err := r.Logs.ListOrders(c.Request.Context(), symbol, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"orders": orders})
}
