package livehttp

import (
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"brale/internal/gateway/database"
	"brale/internal/logger"

	"github.com/gin-gonic/gin"
)

var adminTemplateFuncs = template.FuncMap{
	"formatTime": func(t time.Time) string {
		if t.IsZero() {
			return "-"
		}
		return t.Format("01-02 15:04:05")
	},
	"toUpper": strings.ToUpper,
	"toLower": strings.ToLower,
	"formatMillis": func(ms int64) string {
		if ms <= 0 {
			return "-"
		}
		return time.UnixMilli(ms).Format("01-02 15:04:05")
	},
	"formatHourMinute": func(ms int64) string {
		if ms <= 0 {
			return "--:--"
		}
		return time.UnixMilli(ms).Format("15:04")
	},
	"formatMonthDay": func(ms int64) string {
		if ms <= 0 {
			return "-"
		}
		return time.UnixMilli(ms).Format("Jan 02")
	},
	"stageClass": func(stage string) string {
		s := strings.ToLower(strings.TrimSpace(stage))
		switch {
		case s == "final":
			return "stage-final"
		case s == "provider":
			return "stage-provider"
		case strings.HasPrefix(s, "agent"):
			return "stage-agent"
		default:
			return "stage-other"
		}
	},
	"formatDuration": func(ms int64) string {
		if ms <= 0 {
			return "-"
		}
		d := time.Duration(ms) * time.Millisecond
		hours := int(d.Hours())
		minutes := int(d.Minutes()) % 60
		if hours >= 48 {
			days := hours / 24
			hours = hours % 24
			return fmt.Sprintf("%dd %dh", days, hours)
		}
		if hours > 0 {
			return fmt.Sprintf("%dh %dm", hours, minutes)
		}
		return fmt.Sprintf("%dm", minutes)
	},
	"sub": func(a, b int) int {
		return a - b
	},
	"add": func(a, b int) int {
		return a + b
	},
	"inSlice": func(val string, slice []string) bool {
		for _, item := range slice {
			if item == val {
				return true
			}
		}
		return false
	},
	"formatPercent": func(val float64) string {
		return fmt.Sprintf("%.2f%%", val*100)
	},
	"formatPrice": func(val float64) string {

		if val < 1 {
			return fmt.Sprintf("%.5f", val)
		}
		return fmt.Sprintf("%.2f", val)
	},
	"formatUSD": func(val float64) string {
		prefix := ""
		if val >= 0 {
			prefix = "+"
		}
		return fmt.Sprintf("%s$%.2f", prefix, val)
	},
	"absPercent": func(val float64) float64 {
		return math.Abs(val) * 100
	},
	"operationLabel": func(op database.OperationType) string {
		switch op {
		case database.OperationOpen:
			return "OPEN"
		case database.OperationTakeProfit:
			return "TAKE_PROFIT"
		case database.OperationStopLoss:
			return "STOP_LOSS"
		case database.OperationAdjust:
			return "ADJUST"
		case database.OperationUpdatePlan:
			return "UPDATE_PLAN"
		case database.OperationFinalStop:
			return "FINAL_STOP"
		case database.OperationForceExit:
			return "FORCE_EXIT"
		case database.OperationFailed:
			return "FAILED"
		default:
			return fmt.Sprintf("OP-%d", op)
		}
	},
	"toJSON": func(v any) template.JS {
		b, err := json.Marshal(v)
		if err != nil {
			return template.JS("null")
		}
		return template.JS(b)
	},
}

func registerAdminRoutes(router *gin.Engine, logs *database.DecisionLogStore, freq FreqtradeWebhookHandler, defaultSymbols []string, symbolDetails map[string]SymbolDetail) {
	h := &adminHandler{
		logs:           logs,
		freq:           freq,
		defaultSymbols: defaultSymbols,
		symbolDetails:  symbolDetails,
	}

	router.SetFuncMap(adminTemplateFuncs)

	router.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/admin/desk")
	})
	router.GET("/admin", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/admin/desk")
	})
	router.GET("/admin/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/admin/desk")
	})

	g := router.Group("/admin")
	{
		g.GET("/desk", h.renderDesk)
		g.GET("/book/decisions", h.renderDecisions)
		g.GET("/book/decision/:id", h.renderDecisionDetail)
		g.GET("/book/positions", h.renderPositions)
		g.GET("/book/position/:id", h.renderPositionDetail)
	}
}

type adminHandler struct {
	logs           *database.DecisionLogStore
	freq           FreqtradeWebhookHandler
	defaultSymbols []string
	symbolDetails  map[string]SymbolDetail
}

func (h *adminHandler) renderApp(c *gin.Context, view string, extras gin.H) {
	payload := gin.H{
		"InitialView":    view,
		"DefaultSymbols": h.defaultSymbols,
		"SymbolDetails":  h.symbolDetails,
	}
	for k, v := range extras {
		payload[k] = v
	}
	c.HTML(http.StatusOK, "index.html", payload)
}

func (h *adminHandler) renderDesk(c *gin.Context) {
	logger.Infof("[admin] desk refresh ip=%s", c.ClientIP())
	h.renderApp(c, "desk", nil)
}

func (h *adminHandler) renderDecisions(c *gin.Context) {
	if h.logs == nil {
		c.String(http.StatusServiceUnavailable, "Logs not available")
		return
	}
	logger.Infof("[admin] blue book ip=%s", c.ClientIP())
	h.renderApp(c, "decisions", gin.H{
		"SelectedSymbols": c.QueryArray("symbol"),
	})
}

func (h *adminHandler) renderPositions(c *gin.Context) {
	if h.freq == nil {
		c.String(http.StatusServiceUnavailable, "Freqtrade handler not available")
		return
	}
	logger.Infof("[admin] red book ip=%s", c.ClientIP())
	h.renderApp(c, "positions", gin.H{
		"Symbol": strings.ToUpper(strings.TrimSpace(c.Query("symbol"))),
	})
}

func (h *adminHandler) renderDecisionDetail(c *gin.Context) {
	if h.logs == nil {
		c.String(http.StatusServiceUnavailable, "Logs not available")
		return
	}
	clientIP := c.ClientIP()
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if id <= 0 {
		c.String(http.StatusBadRequest, "Invalid Decision ID")
		return
	}
	logger.Infof("[admin] decision detail ip=%s id=%d", clientIP, id)
	h.renderApp(c, "decisionDetail", gin.H{
		"DecisionID": id,
	})
}

func (h *adminHandler) renderPositionDetail(c *gin.Context) {
	if h.freq == nil {
		c.String(http.StatusServiceUnavailable, "Freqtrade handler not available")
		return
	}
	clientIP := c.ClientIP()

	tradeID, _ := strconv.Atoi(c.Param("id"))
	if tradeID == 0 {
		c.String(http.StatusBadRequest, "Invalid Trade ID")
		return
	}

	logger.Infof("[admin] position detail ip=%s trade_id=%d", clientIP, tradeID)
	h.renderApp(c, "positionDetail", gin.H{
		"TradeID": tradeID,
	})
}
