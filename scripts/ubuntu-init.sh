#!/usr/bin/env bash
set -e

echo "📦 更新系統套件..."
sudo apt update -y
sudo apt upgrade -y

echo "🔧 安裝必要套件..."
sudo apt install -y git curl emacs ca-certificates gnupg lsb-release

echo "🐳 安裝 Docker..."
# 移除舊版本
sudo apt remove -y docker docker-engine docker.io containerd runc || true

# 添加 Docker 官方 GPG key
sudo mkdir -p /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg

# 添加 Docker 軟件源
echo \
  "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] \
  https://download.docker.com/linux/ubuntu \
  $(lsb_release -cs) stable" | \
  sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

# 安裝 Docker Engine
sudo apt update -y
sudo apt install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

echo "🧩 安裝 docker-compose (獨立版本)..."
sudo curl -L "https://github.com/docker/compose/releases/latest/download/docker-compose-$(uname -s)-$(uname -m)" \
  -o /usr/local/bin/docker-compose
sudo chmod +x /usr/local/bin/docker-compose

echo "⚙️  添加使用者到 docker 群組..."
sudo usermod -aG docker $USER

echo "🚀 啟動並啟用 Docker..."
sudo systemctl enable docker
sudo systemctl start docker

echo "✅ 安裝完成！請重新登錄以使 docker 群組權限生效。"
echo "檢查版本："
echo "  git --version"
echo "  docker --version"
echo "  docker-compose --version"
echo "  emacs --version"
echo "  curl --version"
