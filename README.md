# Brale (Break a leg) 🎭

> **AI-Driven Multi-Agent Quantitative Strategy Engine**
>
> *Since it's a performance (trading), I wish you to "Break a leg" (Success/Make a fortune)!*

[![Chinese Documentation](https://img.shields.io/badge/lang-中文-red.svg)](doc/README_CN.md)
[![Go Version](https://img.shields.io/badge/go-1.24.0-blue.svg)](go.mod)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

**Brale** is a quantitative trading system that perfectly decouples **"AI's Deep Thinking"** from **"Quantitative Ultimate Execution"**. It generates high-win-rate decisions through multi-agent collaborative analysis (Trend, Pattern, Momentum) combined with LLMs (GPT, Claude, DeepSeek...), and performs millisecond-level risk alignment via a highly optimized execution engine.
[Chinese Video Introduction](https://www.bilibili.com/video/BV1Ab2aB2EUY)

## ✨ Core Features

- 🧠 **Dual-Loop Architecture**:
  - **Slow Loop**: Triggered by the K-line alignment scheduler, utilizing large models for multi-dimensional deep reasoning.
  - **Fast Loop**: Driven by the `Plan Scheduler`, providing millisecond-level price monitoring to ensure Take Profit/Stop Loss (TP/SL) are triggered precisely.
- 🤖 **Multi-Agent Distributed Reasoning**:
  - **Indicator Agent**: Focuses on trend resonance of mathematical indicators like RSI, MACD, ATR, etc.
  - **Pattern Agent**: Identifies Price Action, SMC liquidity zones, and classic K-line patterns.
  - **Trend Agent**: Filters out noise and focuses on large structure judgment across multiple cycles.
- ⚙️ **Highly Configurable**:
  - **Dynamic Prompt Injection**: Supports configuring independent prompt libraries for different coins (e.g., BTC, ETH, SOL).
  - **Flexible Strategy Library**: Define complex exit plans (scaled take profit, dynamic ATR trailing stop) via YAML.
- 🛡️ **Passive Executor Mode**: Seamlessly integrates **Freqtrade** as the execution terminal. Brale retains full control, using Freqtrade's stable infrastructure for physical order placement, eliminating redundancy at the strategy level.
- ⚡ **High-Performance Go Kernel**: Concurrently handles multi-currency market data fetching, indicator calculation, and Agent scheduling.

## 🏗️ Architecture Flow

![Architecture Diagram](doc/Reasoning-Edition.png)

## ⚠️ Risk Disclaimer

**Brale is an open-source tool for quantitative trading research and development; it is NOT financial investment advice. Cryptocurrency trading is highly speculative and involves significant risk. You may lose part or all of your investment capital. Do not invest money you cannot afford to lose. Past performance is not indicative of future results. Use Brale at your own risk.**

## 🚀 Quick Start (Docker)

### 1. Prepare Configuration

```bash
# Copy configuration files
cp configs/config.example.yaml configs/config.yaml
cp configs/user_data/freqtrade-config.example.json configs/user_data/freqtrade-config.json

# Note:
# 1. Fill in your LLM API Key in configs/config.yaml
# 2. Configure Exchange API in configs/user_data/freqtrade-config.json (or use dry-run mode)
# 3. Modify [ai.multi_agent], [ai.provider_preference], and cycle parameters in config.yaml / profiles.yaml according to your selected model
```

### 2. Start Services

It is recommended to use the Make command for a one-click start. It will automatically clean the environment, prepare data directories, and start services in dependency order:

```bash
make start
```

## 🔌 Execution Layer (Pluggable)

Brale issues real orders through the `Execution Engine` abstraction. The default implementation is [Freqtrade](https://github.com/freqtrade/freqtrade), but the core logic is decoupled from it:

- **Decoupled Logic**: Set `freqtrade.enabled` to `false` in `configs/config.yaml` to run only AI strategies and indicator analysis.
- **Passive Control**: Brale performs Force Entry and Force Exit via the Freqtrade API. Freqtrade's own strategy file (`BraleSharedStrategy.py`) remains empty and is used only as an execution terminal.
- **Exit Plan Synchronization**: Brale's `Plan Scheduler` calculates TP/SL points in real-time and sends close position commands to Freqtrade when triggered, implementing AI logic that is more flexible than Freqtrade's native stop loss.

## 🧩 Indicator System

Brale calculates multi-dimensional technical indicators based on `go-talib`, supporting automatic adjustment based on configuration:

- **Trend**: EMA (21/50/200), MACD (bullish/bearish/flat)
- **Momentum**: RSI (overbought/oversold), ROC, Stochastic Oscillator
- **Volatility**: ATR (used for dynamic stop loss or slippage estimation)
- **Derivatives Data**: Open Interest (OI), Funding Rate - Requires exchange support.

## 🤝 Contribution Guidelines

Issues and Pull Requests are welcome!
1. Fork this repository
2. Create your feature branch (`git checkout -b feature/AmazingFeature`)
3. Commit your changes (`git commit -m 'Add some AmazingFeature'`)
4. Push to the branch (`git push origin feature/AmazingFeature`)
5. Open a Pull Request

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- [Freqtrade](https://github.com/freqtrade/freqtrade) - Excellent cryptocurrency trading bot
- [NoFxAiOS/nofx](https://github.com/NoFxAiOS/nofx) - Inspiration for Multi-Agent Decision Prompts
- [adshao/go-binance](https://github.com/adshao/go-binance) - Elegant Go Language Binance SDK
- [go-talib](https://github.com/markcheno/go-talib) - Go Language Technical Analysis Library
