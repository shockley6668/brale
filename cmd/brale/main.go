package main

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"brale/internal/app"
	brcfg "brale/internal/config"
	"brale/internal/logger"
)

func main() {
	ctx := context.Background()
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
		defer logFile.Close()
	}
	logger.SetLLMWriter(nil) // Default to no LLM writer
	if cfg.App.LLMDump {
		f, err := setupLLMLogOutput(cfg.App.LLMLog)
		if err != nil {
			log.Fatalf("初始化 LLM 日志失败: %v", err)
		}
		if f != nil {
			defer f.Close()
		}
	}
	logger.SetLevel(cfg.App.LogLevel)
	logger.EnableLLMPayloadDump(cfg.App.LLMDump)
	logger.Infof("✓ 配置加载成功（环境=%s，profiles=%s）", cfg.App.Env, cfg.AI.ProfilesPath)

	app, err := app.NewApp(cfg)
	if err != nil {
		log.Fatalf("初始化应用失败: %v", err)
	}
	if err := app.Run(ctx); err != nil {
		log.Fatalf("运行失败: %v", err)
	}
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
