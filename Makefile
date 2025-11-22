# Brale 项目 Makefile（位于 brale 子模块内）
#
# 用途：在本目录下进行构建、测试、运行等常用操作。
# 说明：本目录是一个独立的 Go 模块（包含 go.mod）。

###########
#  1. 首次或全量部署：make stack-up（如需先停再启可 make stack-down && make stack-up）。
#  2. 仅改配置：make brale-up。
#  3. 改了代码需 rebuild：make brale-restart。

BIN_DIR := bin
BIN := $(BIN_DIR)/brale

# 配置文件路径（可在命令行覆盖：make run BRALE_CONFIG=...）
BRALE_CONFIG ?= ./configs/config.toml
DATA_DIR := $(CURDIR)/data

FREQ_DATA_ROOT ?= $(CURDIR)/data/freqtrade
FREQ_USER_DATA_DIR := $(FREQ_DATA_ROOT)/user_data
FREQ_USER_LOG_DIR := $(FREQ_USER_DATA_DIR)/logs
FREQTRADE_API_URL ?= http://localhost:8080/api/v1/status
FREQTRADE_API_USER ?= lauk
FREQTRADE_API_PASS ?= 881016
FREQTRADE_WAIT_TIMEOUT ?= 90

.PHONY: docker-build docker-up docker-down docker-logs docker-deploy init-env run-freq run-brale data-reset freqtrade-prepare freqtrade-up freqtrade-down freqtrade-logs freqtrade-wait brale-up brale-stop brale-logs brale-restart stack-up stack-down stack-restart stack-init up
DOCKER_COMPOSE ?= docker compose
DEPLOY_HOST ?= user@server
DEPLOY_PATH ?= /opt/brale
SECRET := "break-a-leg-lauk-helloworld"

.PHONY: help tidy fmt test build run clean

help:
	@echo "可用目标："
	@echo "  make tidy   - 拉取依赖并生成 go.sum（go mod tidy）"
	@echo "  make fmt    - 格式化当前模块所有 Go 源码"
	@echo "  make test   - 运行 ./internal 下的单元测试"
	@echo "  make build  - 构建可执行文件到 $(BIN)"
	@echo "  make run    - 运行 Brale（默认使用 $(BRALE_CONFIG)）"
	@echo "  make clean  - 清理构建产物 $(BIN_DIR)"
	@echo "  make freqtrade-up - 准备挂载目录并启动 freqtrade 容器"
	@echo "  make brale-up     - 快速重启 brale（仅配置变更时使用，跳过 build）"
	@echo "  make brale-restart - 保留数据的前提下重新构建并启动 brale（用于代码更新）"
	@echo "  make stack-up     - 一次性启动 freqtrade + brale"
	@echo "  make stack-restart - 快速重启两者，自动确保目录权限"
	@echo "  make stack-init   - 首次启动：清空 ./data 后重新部署整个栈"

tidy:
	go mod tidy

fmt:
	go fmt ./...

test:
	go test ./internal/... -count=1 -v

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN) ./cmd/brale
	@echo "已生成：$(BIN)"

run:
	BRALE_CONFIG=$(BRALE_CONFIG) go run ./cmd/brale

clean:
	rm -rf $(BIN_DIR)
	@echo "已清理：$(BIN_DIR)"

# --- 容器化辅助 ---
data-reset:
	@echo "清空数据目录：$(DATA_DIR)"
	@rm -rf $(DATA_DIR)
	@mkdir -p $(DATA_DIR)

freqtrade-prepare:
	@echo "准备 freqtrade 数据目录：$(FREQ_USER_DATA_DIR)"
	@mkdir -p $(FREQ_USER_LOG_DIR)
	@chmod -R 777 $(FREQ_DATA_ROOT)

docker-build:
	docker build -t brale:latest -f Dockerfile.brale .

docker-down:
	$(DOCKER_COMPOSE) down

docker-logs:
	$(DOCKER_COMPOSE) logs -f

docker-deploy:
	ssh $(DEPLOY_HOST) 'cd $(DEPLOY_PATH) && $(DOCKER_COMPOSE) pull && $(DOCKER_COMPOSE) up -d'

init-env:
	@if [ ! -f .env.brale ]; then cp .env.brale.example .env.brale; fi
	@if [ ! -f .env.freqtrade ]; then cp .env.freqtrade.example .env.freqtrade; fi
	@if [ -n "$(SECRET)" ]; then \
		sed -i 's/please-change/$(SECRET)/g' .env.brale; \
		sed -i 's/please-change/$(SECRET)/g' .env.freqtrade; \
	fi

freqtrade-up: freqtrade-prepare
	$(DOCKER_COMPOSE) up -d freqtrade

freqtrade-wait:
	@echo "等待 freqtrade API ($(FREQTRADE_API_URL)) 就绪..."
	@timeout=$(FREQTRADE_WAIT_TIMEOUT); \
	end=$$((`date +%s` + $$timeout)); \
	while true; do \
		if curl -su $(FREQTRADE_API_USER):$(FREQTRADE_API_PASS) --max-time 2 --silent "$(FREQTRADE_API_URL)" >/dev/null 2>&1; then \
			echo "freqtrade API 已就绪"; \
			break; \
		fi; \
		if [ `date +%s` -ge $$end ]; then \
			echo "等待 freqtrade API 超时 ($(FREQTRADE_WAIT_TIMEOUT)s)"; \
			exit 1; \
		fi; \
		sleep 2; \
	done

freqtrade-down:
	$(DOCKER_COMPOSE) stop freqtrade

brale-up:
	$(MAKE) freqtrade-up
	$(MAKE) freqtrade-wait
	$(DOCKER_COMPOSE) up -d brale


