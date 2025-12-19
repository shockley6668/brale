package main

import (
	"context"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"brale/internal/app"
	brcfg "brale/internal/config"
	"brale/internal/logger"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfgPath := os.Getenv("BRALE_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/config.yaml"
	}

	cfg, err := brcfg.Load(cfgPath)
	if err != nil {
		log.Fatalf("读取配置失败: %v", err)
	}
	logFile, err := setupLogOutput(cfg.App.LogPath)
	if err != nil {
		log.Fatalf("初始化日志文件失败: %v", err)
	}
	if logFile != nil {
		defer func() {
			if err := logFile.Close(); err != nil {
				log.Printf("关闭日志文件失败: %v", err)
			}
		}()
	}
	logger.SetLLMWriter(nil)
	if cfg.App.LLMDump {
		f, err := setupLLMLogOutput(cfg.App.LLMLog)
		if err != nil {
			log.Fatalf("初始化 LLM 日志失败: %v", err)
		}
		if f != nil {
			defer func() {
				if err := f.Close(); err != nil {
					log.Printf("关闭 LLM 日志文件失败: %v", err)
				}
			}()
		}
	}
	logger.SetLevel(cfg.App.LogLevel)
	logger.EnableLLMPayloadDump(cfg.App.LLMDump)
	logger.Infof("✓ 配置加载成功（环境=%s，profiles=%s）", cfg.App.Env, cfg.AI.ProfilesPath)

	application, err := app.NewApp(cfg)
	if err != nil {
		log.Fatalf("初始化应用失败: %v", err)
	}

	// 运行应用，当 context 被取消（收到信号）时会触发清理
	if err := application.Run(ctx); err != nil {
		// context.Canceled 是正常的优雅关闭
		if ctx.Err() == context.Canceled {
			logger.Infof("✓ 收到关闭信号，正在优雅关闭...")
		} else {
			log.Fatalf("运行失败: %v", err)
		}
	}

	logger.Infof("✓ 应用已安全退出，数据库已刷新")
}

func setupLogOutput(path string) (*os.File, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil, nil
	}
	dir := filepath.Dir(trimmed)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	file, err := os.OpenFile(trimmed, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	mw := io.MultiWriter(os.Stdout, file)
	log.SetOutput(mw)
	logger.SetOutput(mw)
	return file, nil
}

func setupLLMLogOutput(path string) (*os.File, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(trimmed), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(trimmed, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	logger.SetLLMWriter(f)
	return f, nil
}
