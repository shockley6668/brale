package backtesthttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"brale/internal/backtest"
	"brale/internal/backtest/ui"
	"brale/internal/gateway/database"
	livehttp "brale/internal/transport/http/live"

	"github.com/gin-gonic/gin"
)

// Server 提供回测相关的 HTTP API。
type Server struct {
	addr      string
	svc       *backtest.Service
	sim       *backtest.Simulator
	results   *backtest.ResultStore
	router    *gin.Engine
	indexHTML []byte
	live      *livehttp.Router
}

// Config 描述回测 HTTP Server 的依赖。
type Config struct {
	Addr      string
	Svc       *backtest.Service
	Simulator *backtest.Simulator
	Results   *backtest.ResultStore
	LiveLogs  *database.DecisionLogStore
}

// NewServer 构建回测 HTTP Server。
func NewServer(cfg Config) (*Server, error) {
	if cfg.Svc == nil {
		return nil, errors.New("service 不能为空")
	}
	if cfg.Addr == "" {
		cfg.Addr = ":9991"
	}
	staticFS, err := ui.StaticFS()
	if err != nil {
		return nil, fmt.Errorf("加载前端静态资源失败: %w", err)
	}
	indexHTML, err := ui.Index()
	if err != nil {
		return nil, fmt.Errorf("加载前端首页失败: %w", err)
	}

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.StaticFS("/static", staticFS)

	var liveRouter *livehttp.Router
	if cfg.LiveLogs != nil {
		liveRouter = livehttp.NewRouter(cfg.LiveLogs)
	}
	s := &Server{
		addr:      cfg.Addr,
		svc:       cfg.Svc,
		sim:       cfg.Simulator,
		results:   cfg.Results,
		router:    router,
		indexHTML: indexHTML,
		live:      liveRouter,
	}
	s.registerRoutes()
	return s, nil
}

func (s *Server) registerRoutes() {
	s.router.GET("/", s.handleIndex)
	api := s.router.Group("/api/backtest")
	api.POST("/fetch", s.handleFetch)
	api.GET("/fetch/:id", s.handleFetchStatus)
	api.GET("/jobs", s.handleJobs)
	api.GET("/data", s.handleManifest)
	api.GET("/candles", s.handleCandles)
	api.GET("/candles/all", s.handleAllCandles)
	api.POST("/runs", s.handleRunStart)
	api.GET("/runs", s.handleRunList)
	api.GET("/runs/:id", s.handleRunDetail)
	api.GET("/runs/:id/orders", s.handleRunOrders)
	api.GET("/runs/:id/positions", s.handleRunPositions)
	api.GET("/runs/:id/snapshots", s.handleRunSnapshots)
	api.GET("/runs/:id/logs", s.handleRunLogs)

	if s.live != nil {
		s.live.Register(s.router.Group("/api/live"))
	}
}

func (s *Server) handleIndex(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", s.indexHTML)
}

func (s *Server) handleFetch(c *gin.Context) {
	var req struct {
		Exchange  string `json:"exchange"`
		Symbol    string `json:"symbol" binding:"required"`
		Timeframe string `json:"timeframe" binding:"required"`
		StartTS   int64  `json:"start_ts" binding:"required"`
		EndTS     int64  `json:"end_ts" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	job, err := s.svc.SubmitFetch(backtest.FetchParams{
		Exchange:  req.Exchange,
		Symbol:    req.Symbol,
		Timeframe: req.Timeframe,
		Start:     req.StartTS,
		End:       req.EndTS,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"job": job})
}

func (s *Server) handleFetchStatus(c *gin.Context) {
	id := c.Param("id")
	job, ok := s.svc.JobSnapshot(id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"job": job})
}

func (s *Server) handleJobs(c *gin.Context) {
	list := s.svc.JobsSnapshot()
	c.JSON(http.StatusOK, gin.H{"jobs": list})
}

func (s *Server) handleManifest(c *gin.Context) {
	symbol := c.Query("symbol")
	tf := c.Query("timeframe")
	if symbol == "" || tf == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol/timeframe 必填"})
		return
	}
	info, err := s.svc.ManifestInfo(c.Request.Context(), symbol, tf)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"manifest": info})
}

func (s *Server) handleCandles(c *gin.Context) {
	symbol := c.Query("symbol")
	tf := c.Query("timeframe")
	if symbol == "" || tf == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol/timeframe 必填"})
		return
	}
	start, _ := strconv.ParseInt(c.Query("start_ts"), 10, 64)
	end, _ := strconv.ParseInt(c.Query("end_ts"), 10, 64)
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "200"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "limit 非法"})
		return
	}
	data, err := s.svc.QueryCandles(c.Request.Context(), symbol, tf, start, end, limit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"candles": data})
}

func (s *Server) handleAllCandles(c *gin.Context) {
	symbol := c.Query("symbol")
	tf := c.Query("timeframe")
	if symbol == "" || tf == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol/timeframe 必填"})
		return
	}
	data, err := s.svc.AllCandles(c.Request.Context(), symbol, tf)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"candles": data})
}

func (s *Server) handleRunStart(c *gin.Context) {
	if s.sim == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "模拟器未启用"})
		return
	}
	var req backtest.RunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	run, err := s.sim.StartRun(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"run": run})
}

func (s *Server) handleRunList(c *gin.Context) {
	if s.results == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "结果存储未启用"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	runs, err := s.results.ListRuns(c.Request.Context(), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"runs": runs})
}

func (s *Server) handleRunDetail(c *gin.Context) {
	if s.results == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "结果存储未启用"})
		return
	}
	run, err := s.results.GetRun(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"run": run})
}

func (s *Server) handleRunOrders(c *gin.Context) {
	if s.results == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "结果存储未启用"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	orders, err := s.results.ListOrders(c.Request.Context(), c.Param("id"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"orders": orders})
}

func (s *Server) handleRunPositions(c *gin.Context) {
	if s.results == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "结果存储未启用"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	positions, err := s.results.ListPositions(c.Request.Context(), c.Param("id"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"positions": positions})
}

func (s *Server) handleRunSnapshots(c *gin.Context) {
	if s.results == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "结果存储未启用"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "400"))
	snaps, err := s.results.ListSnapshots(c.Request.Context(), c.Param("id"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"snapshots": snaps})
}

func (s *Server) handleRunLogs(c *gin.Context) {
	if s.results == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "结果存储未启用"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	logs, err := s.results.ListRunLogs(c.Request.Context(), c.Param("id"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"logs": logs})
}

// Start 启动 HTTP 服务，阻塞直到 ctx 取消或出现错误。
func (s *Server) Start(ctx context.Context) error {
	srv := &http.Server{Addr: s.addr, Handler: s.router}
	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shCtx)
		return nil
	case err := <-errCh:
		return err
	}
}
