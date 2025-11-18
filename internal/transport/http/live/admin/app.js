const App = {
  data() {
    return {
      traces: [],
      positions: [],
      expanded: new Set(),
      activeTab: {},
      page: 1,
      pageSize: 5,
      hasNext: false,
      loading: false,
      error: '',
      refreshTimer: null,
      refreshInterval: 30,
    };
  },
  mounted() {
    this.fetchAll();
    this.setupAutoRefresh();
  },
  watch: {
    refreshInterval() {
      this.setupAutoRefresh();
    },
  },
  methods: {
    async fetchAll() {
      this.loading = true;
      this.error = '';
      try {
        await Promise.all([this.fetchDecisions(), this.fetchPositions()]);
      } catch (err) {
        console.error(err);
        this.error = err.message || '拉取数据失败';
      } finally {
        this.loading = false;
      }
    },
    async fetchDecisions(options = {}) {
      const { fromPagination = false } = options;
      const previousTraces = [...this.traces];
      const params = new URLSearchParams({
        limit: this.pageSize.toString(),
        offset: Math.max(0, (this.page - 1) * this.pageSize).toString(),
      });
      const res = await fetch(`/api/live/decisions?${params.toString()}`);
      if (!res.ok) throw new Error(await res.text());
      const data = await res.json();
      const traces = (data.traces || []).map((trace) => ({
        ...trace,
        ts: trace.ts || Date.now(),
        steps: (trace.steps || []).map((step) => ({
          ...step,
          ts: step.ts || trace.ts,
          images: step.images || [],
          decisions: step.decisions || [],
        })),
      }));
      if (fromPagination && this.page > 1 && traces.length === 0) {
        this.page = Math.max(1, this.page - 1);
        this.traces = previousTraces;
        this.hasNext = false;
        return;
      }
      this.traces = traces;
      this.hasNext = traces.length === this.pageSize;
      // 默认 tab 为 first stage
      this.traces.forEach((trace) => {
        if (!this.activeTab[trace.trace_id]) {
          this.activeTab[trace.trace_id] = 'ALL';
        }
      });
    },
    async fetchPositions() {
      const res = await fetch('/api/live/freqtrade/positions?limit=100');
      if (!res.ok) throw new Error(await res.text());
      const data = await res.json();
      this.positions = data.positions || [];
    },
    setupAutoRefresh() {
      if (this.refreshTimer) {
        clearInterval(this.refreshTimer);
        this.refreshTimer = null;
      }
      if (this.refreshInterval > 0) {
        this.refreshTimer = setInterval(() => {
          this.fetchAll();
        }, this.refreshInterval * 1000);
      }
    },
    manualRefresh() {
      this.page = 1;
      this.fetchAll();
    },
    toggleTrace(id) {
      if (this.expanded.has(id)) {
        this.expanded.delete(id);
      } else {
        this.expanded.add(id);
      }
    },
    nextPage() {
      if (this.loading || !this.hasNext) {
        return;
      }
      this.page += 1;
      this.fetchDecisions({ fromPagination: true });
    },
    prevPage() {
      if (this.loading || this.page === 1) {
        return;
      }
      this.page -= 1;
      this.fetchDecisions({ fromPagination: true });
    },
    setActiveTab(traceId, stage) {
      this.activeTab = {
        ...this.activeTab,
        [traceId]: stage,
      };
    },
    filteredSteps(trace) {
      const stage = this.activeTab[trace.trace_id];
      if (!stage || stage === 'ALL') return trace.steps;
      return trace.steps.filter((step) => step.stage === stage);
    },
    traceStages(trace) {
      const stages = [];
      const seen = new Set();
      trace.steps.forEach((step) => {
        const key = step.stage || '未命名';
        if (!seen.has(key)) {
          seen.add(key);
          stages.push(key);
        }
      });
      return stages;
    },
    stageLabel(trace, stage) {
      const match = trace.steps.find((step) => step.stage === stage);
      if (!match) return stage;
      return match.provider_id ? `${stage} · ${match.provider_id}` : stage;
    },
    formatTraceTitle(trace) {
      const symbols = (trace.symbols && trace.symbols.length)
        ? trace.symbols.join(', ')
        : (trace.candidates || []).join(', ');
      return `${trace.trace_id.slice(0, 8)} · ${symbols || '未知'}`;
    },
    formatTs(ts) {
      if (!ts) return '-';
      const date = typeof ts === 'number' ? new Date(ts) : new Date(Number(ts));
      if (Number.isNaN(date.getTime())) return '-';
      return date.toLocaleString();
    },
    formatNumber(num, digits = 2) {
      if (typeof num !== 'number' || Number.isNaN(num)) return '-';
      return num.toFixed(digits);
    },
    formatDuration(ms) {
      if (!ms || ms <= 0) return '-';
      const total = Math.floor(ms / 1000);
      const h = Math.floor(total / 3600);
      const m = Math.floor((total % 3600) / 60);
      const s = total % 60;
      const parts = [];
      if (h) parts.push(h + 'h');
      if (m) parts.push(m + 'm');
      if (!h && s) parts.push(s + 's');
      return parts.join(' ') || s + 's';
    },
    summarizeDecisions(decisions) {
      if (!decisions || !decisions.length) return '';
      return decisions
        .map((d) => `${d.symbol || ''} ${d.action || ''}`.trim())
        .join(' · ');
    },
    preview(text, max = 160) {
      if (!text) return '';
      return text.length > max ? text.slice(0, max) + '…' : text;
    },
    quickClose(pos) {
      alert(`快捷关闭功能尚未实现：${pos.symbol} ${pos.side}`);
    },
  },
};

Vue.createApp(App).mount('#app');
