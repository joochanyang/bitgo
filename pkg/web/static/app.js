// --- State Variables ---
let socket = null;
let chart = null;
let candleSeries = null;
let currentSymbol = "WLDUSDT";
let reconnectInterval = 3000;
let systemState = {};
let priceLines = []; // chart price lines for the open position (entry/SL/TP), cleared each refresh
let tradeScope = "all"; // trade history filter: all | open | closed
let countdownTimer = null; // interval driving the next-tick countdown

// --- Auth token (only needed when the server sets DASHBOARD_TOKEN) ---
// Provide it once via ?token=... in the URL; it is remembered in localStorage.
(function () {
    const urlToken = new URLSearchParams(window.location.search).get("token");
    if (urlToken) localStorage.setItem("dashboardToken", urlToken);
})();
function authToken() {
    return localStorage.getItem("dashboardToken") || "";
}
// apiFetch is fetch() with the auth header attached when a token is set.
function apiFetch(url, options = {}) {
    const t = authToken();
    if (t) {
        options.headers = Object.assign({}, options.headers, { "X-Auth-Token": t });
    }
    return fetch(url, options);
}

// --- Initialize App ---
document.addEventListener("DOMContentLoaded", () => {
    initChart();
    connectWebSocket();
    setupEventListeners();
});

// --- Chart Initialization (TradingView Lightweight Charts) ---
function initChart() {
    const chartContainer = document.getElementById("priceChart");
    chartContainer.innerHTML = ""; // Clear loader

    chart = LightweightCharts.createChart(chartContainer, {
        layout: {
            backgroundColor: "#0c101d",
            textColor: "#9ca3af",
            fontSize: 12,
            fontFamily: "Inter, sans-serif"
        },
        grid: {
            vertLines: { color: "rgba(255, 255, 255, 0.03)" },
            horzLines: { color: "rgba(255, 255, 255, 0.03)" }
        },
        crosshair: {
            mode: LightweightCharts.CrosshairMode.Normal,
        },
        timeScale: {
            timeVisible: true,
            secondsVisible: false,
            borderColor: "rgba(255, 255, 255, 0.08)",
        },
        rightPriceScale: {
            borderColor: "rgba(255, 255, 255, 0.08)",
        }
    });

    candleSeries = chart.addCandlestickSeries({
        upColor: "#10b981",
        downColor: "#ef4444",
        borderDownColor: "#ef4444",
        borderUpColor: "#10b981",
        wickDownColor: "#ef4444",
        wickUpColor: "#10b981",
    });

    // Make chart responsive
    const resizeObserver = new ResizeObserver(entries => {
        if (entries.length === 0 || !chart) return;
        const { width, height } = entries[0].contentRect;
        chart.resize(width, height);
    });
    resizeObserver.observe(chartContainer);

    // Fetch initial chart data
    loadChartData(currentSymbol);
}

// --- Fetch & Load Candlestick Data ---
async function loadChartData(symbol) {
    if (!candleSeries) return;

    try {
        const interval = systemState.config ? systemState.config.interval : "1h";
        // Map the bot interval to Bybit's kline code and pick how many candles to load.
        // Short timeframes get MORE candles so older trades still fall inside the chart
        // window (otherwise their entry/exit markers get filtered out as off-screen).
        // Bybit caps kline limit at 1000.
        const intervalMap = {
            "5m":  { code: "5",   limit: 1000 }, // ~3.5 days
            "15m": { code: "15",  limit: 1000 }, // ~10 days
            "30m": { code: "30",  limit: 1000 }, // ~20 days
            "1h":  { code: "60",  limit: 500 },  // ~20 days
            "4h":  { code: "240", limit: 300 },  // ~50 days
        };
        const m = intervalMap[interval] || { code: interval, limit: 500 };
        const bybitInterval = m.code;

        const response = await fetch(`https://api.bybit.com/v5/market/kline?category=linear&symbol=${symbol}&interval=${bybitInterval}&limit=${m.limit}`);
        const result = await response.json();
        
        if (result.retCode === 0 && result.result && result.result.list) {
            // Bybit returns newest first, reverse and format for Lightweight Charts
            const candles = result.result.list.map(item => {
                return {
                    time: parseInt(item[0]) / 1000,
                    open: parseFloat(item[1]),
                    high: parseFloat(item[2]),
                    low: parseFloat(item[3]),
                    close: parseFloat(item[4]),
                };
            }).reverse();

            candleSeries.setData(candles);

            // Add Trade Markers
            applyChartMarkers(symbol, candles);

            // Draw entry / stop-loss / take-profit horizontal lines for an open position.
            applyPositionPriceLines(symbol);

            chart.timeScale().fitContent();
        }
    } catch (e) {
        console.error("Failed to load chart data:", e);
    }
}

// Marker colors shared by live + backtest chart markers.
const MARKER_LONG = "#10b981";   // emerald — long entry
const MARKER_SHORT = "#ef4444";  // rose — short entry
const MARKER_TP = "#10b981";     // emerald — take-profit / winning exit
const MARKER_SL = "#ef4444";     // rose — stop-loss / losing exit
const MARKER_CLOSE = "#9ca3af";  // slate — neutral close (manual/signal/switch)

// exitStyle maps an exit to a {color, label} pair for chart markers.
// `reason` is the backtest exit_reason ("TP"/"SL"/"LIQUIDATION"/...) when available;
// live trades have no reason, so the PnL sign decides win (익절) vs loss (손절).
function exitStyle(reason, pnl) {
    if (reason === "TP") return { color: MARKER_TP, label: "익절" };
    if (reason === "SL" || reason === "LIQUIDATION") return { color: MARKER_SL, label: "손절" };
    if (reason) return { color: MARKER_CLOSE, label: "청산" }; // CLOSE / SWITCH / FORCE_CLOSE
    // No reason (live trade): infer from realized PnL.
    if (pnl > 0) return { color: MARKER_TP, label: "익절" };
    if (pnl < 0) return { color: MARKER_SL, label: "손절" };
    return { color: MARKER_CLOSE, label: "청산" };
}

// --- Map Executed Trades onto the Candlestick Chart ---
function applyChartMarkers(symbol, candles) {
    if (!systemState.trades || systemState.trades.length === 0) return;

    const markers = [];
    const minTime = candles[0].time;
    const maxTime = candles[candles.length - 1].time;

    // Filter trades for this symbol
    systemState.trades.forEach(trade => {
        if (trade.symbol !== symbol) return;

        const tradeTime = new Date(trade.timestamp).getTime() / 1000;

        // Only map if it fits within our current chart bounds
        if (tradeTime >= minTime && tradeTime <= maxTime) {
            if (trade.status === "OPEN") {
                const isBuy = trade.side === "LONG";
                markers.push({
                    time: tradeTime,
                    position: isBuy ? "belowBar" : "aboveBar",
                    color: isBuy ? MARKER_LONG : MARKER_SHORT,
                    shape: isBuy ? "arrowUp" : "arrowDown",
                    text: `진입 ${trade.side} @${trade.entry_price}`,
                });
            } else if (trade.status === "CLOSED") {
                const isShortClose = trade.side === "SHORT"; // Closing SHORT = Buy order
                const style = exitStyle(null, trade.realized_pnl); // live trades carry no exit_reason
                markers.push({
                    time: tradeTime,
                    position: isShortClose ? "belowBar" : "aboveBar",
                    color: style.color,
                    shape: "circle",
                    text: `${style.label} ${trade.side} @${trade.exit_price} (${trade.realized_pnl >= 0 ? '+' : ''}${trade.realized_pnl.toFixed(2)})`,
                });
            }
        }
    });

    // Sort markers chronologically
    markers.sort((a, b) => a.time - b.time);
    candleSeries.setMarkers(markers);
}

// --- Draw entry / stop-loss / take-profit price lines for an open position ---
// Horizontal lines make "여기서 사서, 여기서 손절, 여기서 익절" obvious at a glance.
// Removed and redrawn each refresh so they always reflect the current position
// (and disappear when the position closes). SL/TP come from the exchange position
// (Bybit), so they only show on live/dry-run positions, not on paper mock positions.
function applyPositionPriceLines(symbol) {
    if (!candleSeries) return;

    // Clear previously drawn lines.
    priceLines.forEach(pl => { try { candleSeries.removePriceLine(pl); } catch (e) {} });
    priceLines = [];

    const positions = systemState.positions || [];
    const pos = positions.find(p => p.symbol === symbol && p.size > 0);
    if (!pos) return;

    const addLine = (price, color, title) => {
        if (!price || price <= 0) return;
        priceLines.push(candleSeries.createPriceLine({
            price: price,
            color: color,
            lineWidth: 2,
            lineStyle: title === "진입" ? LightweightCharts.LineStyle.Solid : LightweightCharts.LineStyle.Dashed,
            axisLabelVisible: true,
            title: title,
        }));
    };

    addLine(pos.entry_price, "#ffffff", "진입");          // white solid — where we got in
    addLine(pos.stop_loss_price, "#ef4444", "손절");      // rose dashed — exit on loss
    addLine(pos.take_profit_price, "#10b981", "익절");    // emerald dashed — exit on profit
}

// --- WebSocket connection ---
function connectWebSocket() {
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const t = authToken();
    const wsUrl = `${protocol}//${window.location.host}/ws${t ? `?token=${encodeURIComponent(t)}` : ""}`;

    socket = new WebSocket(wsUrl);

    socket.onopen = () => {
        updateConnectionStatus(true);
        console.log("WebSocket connected.");
    };

    socket.onmessage = (event) => {
        try {
            const data = JSON.parse(event.data);
            systemState = data;
            updateUI();
        } catch (e) {
            console.error("Failed to parse websocket message:", e);
        }
    };

    socket.onclose = () => {
        updateConnectionStatus(false);
        console.log("WebSocket disconnected. Reconnecting...");
        setTimeout(connectWebSocket, reconnectInterval);
    };

    socket.onerror = (err) => {
        console.error("WebSocket error:", err);
    };
}

// --- Connection UI Updater ---
function updateConnectionStatus(isConnected) {
    const badge = document.getElementById("wsStatus");
    const text = document.getElementById("wsStatusText");

    if (isConnected) {
        badge.className = "status-badge connect";
        text.innerText = "연결됨";
    } else {
        badge.className = "status-badge disconnect";
        text.innerText = "연결 끊김";
        // Reset balance/PnL display to show connection is down
        document.getElementById("balanceVal").innerText = "--.-- USDT";
        document.getElementById("unrealizedPnLVal").innerText = "--.-- USDT";
    }
}

// --- Full UI Updater ---
function updateUI() {
    if (!systemState) return;

    // 1. Balance & Realized PnL
    document.getElementById("balanceVal").innerText = `${systemState.balance.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })} USDT`;

    // Calculate total realized profit from closed trades (cumulative across all history).
    let totalRealized = 0;
    let wins = 0;
    let closedTradesCount = 0;

    if (systemState.trades) {
        systemState.trades.forEach(t => {
            if (t.status === "CLOSED") {
                totalRealized += t.realized_pnl;
                closedTradesCount++;
                if (t.realized_pnl > 0) {
                    wins++;
                }
            }
        });
    }

    const realizedVal = document.getElementById("realizedPnLVal");
    realizedVal.innerText = `${totalRealized >= 0 ? '+' : ''}${totalRealized.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })} USDT`;
    realizedVal.className = totalRealized >= 0 ? "metric-value text-emerald" : "metric-value text-rose";
    document.getElementById("realizedScope").textContent = "누적";

    // Win Rate
    const winRateVal = document.getElementById("winRateVal");
    if (closedTradesCount > 0) {
        const rate = (wins / closedTradesCount) * 100;
        winRateVal.innerText = `${rate.toFixed(1)}%`;
    } else {
        winRateVal.innerText = "0.0%";
    }
    document.getElementById("winRateScope").textContent = `${closedTradesCount}건`;

    // 2. Active Positions & Unrealized PnL
    let totalUnrealized = 0;
    let totalRiskExposure = 0; // sum of at-stop loss across open positions (USDT)
    const posListContainer = document.getElementById("positionList");
    posListContainer.replaceChildren();

    const positions = systemState.positions || [];
    document.getElementById("positionCount").textContent = String(positions.length);

    if (positions.length > 0) {
        positions.forEach(pos => {
            totalUnrealized += pos.unrealized_pnl;

            const isLong = pos.side === "LONG";
            const notional = pos.entry_price * pos.size;
            const margin = pos.leverage > 0 ? notional / pos.leverage : notional;
            // PnL as % of margin (the capital actually locked). unrealized_pnl is already
            // the leveraged absolute P&L from the exchange, so we divide by margin once —
            // never multiply by leverage again (that double-counts it).
            const pnlPercent = margin > 0 ? (pos.unrealized_pnl / margin) * 100 : 0;
            // At-stop loss: what we'd lose (USDT) if the position hits its stop.
            const slDist = Math.abs(pos.entry_price - (pos.stop_loss_price || 0));
            const riskAtStop = pos.stop_loss_price > 0 ? pos.size * slDist : 0;
            totalRiskExposure += riskAtStop;

            posListContainer.appendChild(buildPositionItem(pos, isLong, margin, pnlPercent, riskAtStop));
        });
    } else {
        const noData = document.createElement("div");
        noData.className = "no-data";
        noData.textContent = "보유 중인 포지션이 없습니다.";
        posListContainer.appendChild(noData);
    }

    const unrealizedVal = document.getElementById("unrealizedPnLVal");
    unrealizedVal.innerText = `${totalUnrealized >= 0 ? '+' : ''}${totalUnrealized.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })} USDT`;
    unrealizedVal.className = totalUnrealized >= 0 ? "metric-value text-emerald" : "metric-value text-rose";

    // Risk exposure KPI: at-stop loss across open positions vs the portfolio risk budget.
    const riskVal = document.getElementById("riskExposureVal");
    riskVal.innerText = `${totalRiskExposure.toFixed(2)} USDT`;
    const maxPortfolioRisk = (systemState.config && systemState.config.max_portfolio_risk_pct) || 10;
    const riskBudget = (systemState.balance || 0) * (maxPortfolioRisk / 100);
    const riskPctOfBudget = riskBudget > 0 ? (totalRiskExposure / riskBudget) * 100 : 0;
    document.getElementById("riskScope").textContent = `/ 예산 ${maxPortfolioRisk}% = ${riskBudget.toFixed(0)}USDT`;
    riskVal.className = riskPctOfBudget > 80 ? "metric-value text-rose" : (riskPctOfBudget > 0 ? "metric-value text-warn" : "metric-value");

    // 3. Engine Running Button / Badge + Live ribbon + header tint
    const toggleBtn = document.getElementById("toggleBotBtn");
    const modeBadge = document.getElementById("modeBadge");
    const liveRibbon = document.getElementById("liveRibbon");
    const appHeader = document.getElementById("appHeader");

    if (systemState.is_running) {
        setBtnLabel(toggleBtn, "fa-pause", "봇 정지");
        toggleBtn.className = "btn btn-secondary";
    } else {
        setBtnLabel(toggleBtn, "fa-play", "봇 시작");
        toggleBtn.className = "btn btn-primary start-btn";
    }

    if (systemState.is_paper_trading) {
        modeBadge.className = "mode-badge paper";
        modeBadge.innerText = "모의 매매";
        liveRibbon.style.display = "none";
        appHeader.classList.remove("live-header");
    } else {
        modeBadge.className = "mode-badge live";
        modeBadge.innerText = "실전 매매";
        liveRibbon.style.display = "flex";
        appHeader.classList.add("live-header");
    }

    // Next-tick countdown (driven by systemState.next_tick_at, RFC3339).
    updateCountdown(systemState.is_running, systemState.next_tick_at);

    // 3b. Trade history table (renders backend trades, filtered by tradeScope).
    renderTradeTable();

    // 4. Fill Config Settings Form (only once, to prevent overwriting user input while they type)
    const settingsForm = document.getElementById("settingsForm");
    if (settingsForm && !settingsForm.dataset.initialized && systemState.config) {
        document.getElementById("strategySelect").value = systemState.config.active_strategy || "trend_following";
        document.getElementById("aiProviderSelect").value = systemState.config.ai_provider;
        document.getElementById("intervalSelect").value = systemState.config.interval;
        document.getElementById("leverageInput").value = systemState.config.leverage;
        document.getElementById("riskInput").value = systemState.config.risk_percentage;
        document.getElementById("paperTradingCheckbox").checked = systemState.config.is_paper_trading;
        
        document.getElementById("geminiKeyInput").value = systemState.config.gemini_api_key;
        document.getElementById("openaiKeyInput").value = systemState.config.openai_api_key;
        document.getElementById("bybitKeyInput").value = systemState.config.bybit_api_key;
        document.getElementById("bybitSecretInput").value = systemState.config.bybit_api_secret;

        toggleStrategyVisibility();
        settingsForm.dataset.initialized = "true";
    }

    // 5. System Logs console
    const logConsole = document.getElementById("logConsole");
    if (systemState.logs) {
        logConsole.innerHTML = "";
        systemState.logs.forEach(log => {
            const row = document.createElement("div");
            row.className = `log-row ${log.level.toLowerCase()}`;
            row.innerText = `[${log.timestamp}] [${log.level}] ${log.message}`;
            logConsole.appendChild(row);
        });
    }

    // 6. Current Situation (beginner-friendly cards)
    renderSituations();

    // Update active chart markers
    if (candleSeries) {
        loadChartData(currentSymbol);
    }
}

// renderSituations paints the plain-Korean "what's happening" cards from
// systemState.situations (a map of symbol -> {view, situation}). Uses DOM nodes
// (no innerHTML) since the text may include strategy reasoning.
function renderSituations() {
    const box = document.getElementById("situationList");
    if (!box) return;
    box.replaceChildren();

    const sits = systemState.situations;
    const keys = sits ? Object.keys(sits) : [];
    if (keys.length === 0) {
        const empty = document.createElement("div");
        empty.className = "no-data";
        empty.textContent = systemState.is_running
            ? "분석을 준비 중입니다. 잠시만요..."
            : "봇을 시작하면 현재 상황을 쉬운 말로 설명해 드려요.";
        box.appendChild(empty);
        return;
    }

    keys.sort().forEach(sym => {
        const s = sits[sym].situation || {};
        const view = sits[sym].view || {};
        const item = document.createElement("div");
        item.className = "situation-item";

        // Decision chip: shows the bot's latest call (LONG/SHORT/HOLD/CLOSE) at a glance,
        // so "봇이 지금 뭘 생각하나" is visible, not just the plain-Korean summary.
        if (view.decision) {
            const chipRow = document.createElement("div");
            chipRow.className = "situation-chip-row";
            const chip = document.createElement("span");
            chip.className = `decision-chip dec-${(view.decision || "").toLowerCase()}`;
            chip.textContent = decisionLabel(view.decision);
            chipRow.appendChild(chip);
            const symTag = document.createElement("span");
            symTag.className = "sit-sym";
            symTag.textContent = sym;
            chipRow.appendChild(symTag);
            item.appendChild(chipRow);
        }

        const head = document.createElement("div");
        head.className = "situation-headline";
        head.textContent = s.headline || "";
        item.appendChild(head);

        const detail = document.createElement("div");
        detail.className = "situation-detail";
        detail.textContent = s.detail || "";
        item.appendChild(detail);

        // The strategy's own reasoning (English) — surfaced as-is so an advanced user can
        // read why the signal fired, beneath the beginner-friendly Korean summary.
        if (view.reasoning) {
            const reason = document.createElement("div");
            reason.className = "situation-reason";
            reason.textContent = view.reasoning;
            item.appendChild(reason);
        }

        box.appendChild(item);
    });
}

// decisionLabel maps the internal decision token to a short Korean chip label.
function decisionLabel(d) {
    switch ((d || "").toUpperCase()) {
        case "LONG": return "LONG 매수";
        case "SHORT": return "SHORT 매도";
        case "CLOSE": return "청산";
        case "HOLD": return "관망";
        default: return d || "—";
    }
}

// --- Bind Button Actions ---
function setupEventListeners() {
    // Symbol select
    const symbolSelect = document.getElementById("chartSymbolSelect");
    symbolSelect.addEventListener("change", (e) => {
        currentSymbol = e.target.value;
        loadChartData(currentSymbol);
    });

    // Trade history scope segmented control (전체 / 진행중 / 종료)
    document.querySelectorAll(".seg-btn").forEach(btn => {
        btn.addEventListener("click", () => {
            document.querySelectorAll(".seg-btn").forEach(b => b.classList.remove("active"));
            btn.classList.add("active");
            tradeScope = btn.dataset.scope;
            renderTradeTable();
        });
    });

    // AI 해설 button — fetch a natural-language explanation on demand.
    const aiBtn = document.getElementById("aiExplainBtn");
    const aiBox = document.getElementById("aiExplainBox");
    aiBtn.addEventListener("click", async () => {
        aiBtn.disabled = true;
        setBtnLabel(aiBtn, "fa-spinner fa-spin", "AI 분석 중...");
        aiBox.style.display = "block";
        aiBox.textContent = "AI가 현재 상황을 살펴보는 중입니다...";
        try {
            const resp = await apiFetch("/api/explain", { method: "POST" });
            const data = await resp.json();
            aiBox.textContent = data.explanation || "해설을 가져오지 못했습니다.";
        } catch (err) {
            aiBox.textContent = "AI 해설 요청 실패: " + ((err && err.message) || err);
        } finally {
            aiBtn.disabled = false;
            setBtnLabel(aiBtn, "fa-robot", "AI 해설");
        }
    });

    // Start / Stop Bot
    const toggleBtn = document.getElementById("toggleBotBtn");
    toggleBtn.addEventListener("click", async () => {
        const action = systemState.is_running ? "stop" : "start";
        await apiFetch("/api/status", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ action: action })
        });
        // We will receive state update via WS
    });

    // Force Tick
    const forceTickBtn = document.getElementById("forceTickBtn");
    forceTickBtn.addEventListener("click", async () => {
        forceTickBtn.disabled = true;
        const icon = forceTickBtn.querySelector("i");
        icon.className = "fa-solid fa-sync fa-spin";
        
        await apiFetch("/api/status", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ action: "tick" })
        });
        
        setTimeout(() => {
            forceTickBtn.disabled = false;
            icon.className = "fa-solid fa-sync";
        }, 1500);
    });

    // Settings Submit
    const form = document.getElementById("settingsForm");
    form.addEventListener("submit", async (e) => {
        e.preventDefault();
        
        const saveBtn = document.getElementById("saveSettingsBtn");
        saveBtn.disabled = true;
        setBtnLabel(saveBtn, "fa-spinner fa-spin", "저장 중...");

        const payload = {
            active_strategy: document.getElementById("strategySelect").value,
            ai_provider: document.getElementById("aiProviderSelect").value,
            interval: document.getElementById("intervalSelect").value,
            leverage: parseInt(document.getElementById("leverageInput").value),
            risk_percentage: parseFloat(document.getElementById("riskInput").value),
            is_paper_trading: document.getElementById("paperTradingCheckbox").checked,
            gemini_api_key: document.getElementById("geminiKeyInput").value,
            openai_api_key: document.getElementById("openaiKeyInput").value,
            bybit_api_key: document.getElementById("bybitKeyInput").value,
            bybit_api_secret: document.getElementById("bybitSecretInput").value,
            symbols: systemState.config ? systemState.config.symbols : ["WLDUSDT", "FETUSDT", "NEARUSDT", "RENDERUSDT"],
            server_port: systemState.config ? systemState.config.server_port : "8080",
            openai_model: systemState.config ? systemState.config.openai_model : "gpt-4o",
            gemini_model: systemState.config ? systemState.config.gemini_model : "gemini-1.5-pro"
        };

        try {
            const resp = await apiFetch("/api/config", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify(payload)
            });
            const res = await resp.json();
            if (res.status === "saved") {
                alert("설정이 저장되었습니다!");
                form.removeAttribute("data-initialized"); // Force form re-initialization
            }
        } catch (err) {
            alert("설정 저장 실패: " + err);
        } finally {
            saveBtn.disabled = false;
            setBtnLabel(saveBtn, "fa-save", "설정 저장");
        }
    });

    // Strategy Select Visibility Toggle
    const strategySelect = document.getElementById("strategySelect");
    strategySelect.addEventListener("change", toggleStrategyVisibility);

    // Live-mode guard: switching OFF paper trading is irreversible (real money). Confirm
    // before the user can accidentally uncheck it. (Backend setPaperModeCb also refuses
    // when keys are missing; this is the UI-side defence-in-depth.)
    const paperCheckbox = document.getElementById("paperTradingCheckbox");
    paperCheckbox.addEventListener("change", () => {
        if (!paperCheckbox.checked) {
            const ok = confirm("실전 매매로 전환합니다. 저장 시 실제 자금으로 거래가 실행됩니다.\n계속하시겠습니까?");
            if (!ok) paperCheckbox.checked = true; // revert
        }
    });

    // AI Provider Select Visibility Toggle
    const aiProvider = document.getElementById("aiProviderSelect");
    aiProvider.addEventListener("change", toggleAPIKeyVisibility);

    // Backtest Form Submit
    const backtestForm = document.getElementById("backtestForm");
    backtestForm.addEventListener("submit", async (e) => {
        e.preventDefault();
        const runBtn = document.getElementById("runBacktestBtn");
        runBtn.disabled = true;
        setBtnLabel(runBtn, "fa-spinner fa-spin", "실행 중...");

        const selected = (id) => Array.from(document.getElementById(id).selectedOptions).map(o => o.value);
        const symbols = selected("backtestSymbolSelect");
        const strategies = selected("backtestStrategySelect");

        if (symbols.length === 0 || strategies.length === 0) {
            alert("심볼과 전략을 각각 하나 이상 선택하세요.");
            runBtn.disabled = false;
            setBtnLabel(runBtn, "fa-play", "백테스트 실행");
            return;
        }

        // Optional tuning params — only attached when the user filled them in.
        const tuning = {};
        const balance = parseFloat(document.getElementById("backtestBalanceInput").value);
        const fee = parseFloat(document.getElementById("backtestFeeInput").value);
        const candles = parseInt(document.getElementById("backtestCandlesInput").value, 10);
        if (!isNaN(balance)) tuning.initial_balance = balance;
        if (!isNaN(fee)) tuning.fee_rate = fee;
        if (!isNaN(candles)) tuning.candles = candles;

        const splitRatio = parseFloat(document.getElementById("backtestSplitInput").value);
        const interval = document.getElementById("backtestIntervalSelect").value;
        const reset = () => {
            runBtn.disabled = false;
            setBtnLabel(runBtn, "fa-play", "백테스트 실행");
        };

        try {
            // Out-of-sample split runs a single combo (in-sample vs out-of-sample);
            // a blank split runs the cartesian batch.
            if (!isNaN(splitRatio)) {
                if (symbols.length !== 1 || strategies.length !== 1) {
                    alert("아웃오브샘플 분할은 심볼과 전략을 정확히 하나씩만 선택해야 합니다.");
                    reset();
                    return;
                }
                const payload = { symbol: symbols[0], strategy: strategies[0], interval, split_ratio: splitRatio, ...tuning };
                const resp = await apiFetch("/api/backtest", {
                    method: "POST",
                    headers: { "Content-Type": "application/json" },
                    body: JSON.stringify(payload)
                });
                if (!resp.ok) throw new Error(await resp.text());
                const split = await resp.json();
                renderBacktestTable([
                    { symbol: `${symbols[0]} (인샘플)`, strategy: strategies[0], report: split.in_sample },
                    { symbol: `${symbols[0]} (아웃오브샘플)`, strategy: strategies[0], report: split.out_of_sample },
                ]);
                if (split.out_of_sample && symbols[0] === currentSymbol) {
                    applyBacktestMarkers(split.out_of_sample.trades);
                }
                return;
            }

            const payload = { symbols, strategies, interval, ...tuning };
            const resp = await apiFetch("/api/backtest/batch", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify(payload)
            });
            if (!resp.ok) throw new Error(await resp.text());
            const data = await resp.json();
            renderBacktestTable(data.results || []);

            // Render chart markers for the first successful combo matching the chart symbol.
            const match = (data.results || []).find(r => r.report && r.symbol === currentSymbol);
            if (match) applyBacktestMarkers(match.report.trades);

        } catch (err) {
            alert("백테스트 실패: " + err.message);
        } finally {
            reset();
        }
    });
}

// setBtnLabel sets a button's icon + text via DOM nodes (no innerHTML).
function setBtnLabel(btn, iconClasses, text) {
    btn.replaceChildren();
    const icon = document.createElement("i");
    icon.className = "fa-solid " + iconClasses;
    btn.appendChild(icon);
    btn.appendChild(document.createTextNode(" " + text));
}

// buildPositionItem constructs one position card via DOM nodes (no innerHTML), including
// the entry/mark/margin row and the stop-loss / take-profit / at-stop-loss row that the
// old UI was missing. pnlCls selects the emerald/rose colour by sign.
function buildPositionItem(pos, isLong, margin, pnlPercent, riskAtStop) {
    const item = document.createElement("div");
    item.className = "position-item";

    // Header row: symbol/side/size (left) + PnL (right)
    const main = document.createElement("div");
    main.className = "pos-main-info";

    const title = document.createElement("div");
    title.className = "pos-title-block";
    const sym = document.createElement("span");
    sym.className = "pos-symbol";
    sym.textContent = pos.symbol;
    const side = document.createElement("span");
    side.className = `pos-side ${isLong ? "long" : "short"}`;
    side.textContent = `${pos.side} ${pos.leverage}x`;
    const size = document.createElement("span");
    size.className = "pos-size";
    size.textContent = `수량: ${pos.size.toFixed(3)}`;
    title.append(sym, side, size);

    const pnlBox = document.createElement("div");
    pnlBox.className = "pos-pnl";
    const pnlCls = pos.unrealized_pnl >= 0 ? "text-emerald" : "text-rose";
    const amt = document.createElement("div");
    amt.className = `pnl-amount ${pnlCls}`;
    amt.textContent = `${pos.unrealized_pnl >= 0 ? "+" : ""}${pos.unrealized_pnl.toFixed(2)} USDT`;
    const pct = document.createElement("div");
    pct.className = `pnl-pct ${pnlCls}`;
    pct.textContent = `${pos.unrealized_pnl >= 0 ? "+" : ""}${pnlPercent.toFixed(2)}% (증거금대비)`;
    pnlBox.append(amt, pct);

    main.append(title, pnlBox);
    item.appendChild(main);

    // Entry / mark / margin row
    item.appendChild(detailGrid([
        { label: "진입가", val: pos.entry_price.toFixed(4) },
        { label: "마크가", val: pos.mark_price.toFixed(4) },
        { label: "증거금", val: `${margin.toFixed(2)} USDT` },
    ]));

    // SL / TP / at-stop-loss row — the row the old UI was missing.
    item.appendChild(detailGrid([
        { label: "손절가", val: pos.stop_loss_price > 0 ? pos.stop_loss_price.toFixed(4) : "미설정", cls: pos.stop_loss_price > 0 ? "text-rose" : "text-faint" },
        { label: "익절가", val: pos.take_profit_price > 0 ? pos.take_profit_price.toFixed(4) : "미설정", cls: pos.take_profit_price > 0 ? "text-emerald" : "text-faint" },
        { label: "손절시 손실", val: riskAtStop > 0 ? `-${riskAtStop.toFixed(2)} USDT` : "—", cls: riskAtStop > 0 ? "text-rose" : "text-faint" },
    ], "sl-tp-grid"));

    // Close button
    const btn = document.createElement("button");
    btn.className = "btn btn-danger btn-sm btn-block";
    const icon = document.createElement("i");
    icon.className = "fa-solid fa-square-xmark";
    btn.appendChild(icon);
    btn.appendChild(document.createTextNode(" 시장가 청산"));
    btn.addEventListener("click", () => closePosition(pos.symbol));
    item.appendChild(btn);

    return item;
}

// detailGrid builds a 3-column detail row from [{label,val,cls}].
function detailGrid(items, extraClass) {
    const grid = document.createElement("div");
    grid.className = "pos-details-grid" + (extraClass ? " " + extraClass : "");
    items.forEach(it => {
        const cell = document.createElement("div");
        cell.className = "detail-item";
        const label = document.createElement("span");
        label.className = "detail-label";
        label.textContent = it.label;
        const val = document.createElement("span");
        val.className = "detail-val" + (it.cls ? " " + it.cls : "");
        val.textContent = it.val;
        cell.append(label, val);
        grid.appendChild(cell);
    });
    return grid;
}

// renderTradeTable paints the backend trade history into the trades table, filtered by
// the tradeScope segmented control (all / open / closed). Newest first (backend already
// reverses). Uses DOM nodes — no innerHTML.
function renderTradeTable() {
    const tbody = document.getElementById("tradeTableBody");
    if (!tbody) return;
    tbody.replaceChildren();

    let trades = systemState.trades || [];
    if (tradeScope === "open") trades = trades.filter(t => t.status === "OPEN");
    else if (tradeScope === "closed") trades = trades.filter(t => t.status === "CLOSED");

    if (trades.length === 0) {
        const tr = document.createElement("tr");
        const td = document.createElement("td");
        td.colSpan = 8;
        td.className = "no-data-td";
        td.textContent = "거래 내역이 없습니다.";
        tr.appendChild(td);
        tbody.appendChild(tr);
        return;
    }

    trades.forEach(t => {
        const tr = document.createElement("tr");
        const cell = (text, cls) => {
            const td = document.createElement("td");
            td.textContent = text == null ? "" : String(text);
            if (cls) td.className = cls;
            tr.appendChild(td);
        };

        const ts = t.timestamp ? new Date(t.timestamp).toLocaleString("ko-KR", {
            month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit"
        }) : "—";
        cell(ts);
        cell(t.symbol);
        const sideCls = (t.side === "LONG") ? "text-emerald" : (t.side === "SHORT" ? "text-rose" : "");
        cell(t.side ? `${t.side} ${t.leverage || 0}x` : "—", sideCls);
        cell(t.size != null ? t.size.toFixed(3) : "—");
        cell(t.entry_price ? t.entry_price.toFixed(4) : "—");
        cell(t.exit_price ? t.exit_price.toFixed(4) : "—");
        if (t.status === "OPEN") {
            cell("진행중", "text-warn");
        } else {
            const pnl = t.realized_pnl || 0;
            cell(`${pnl >= 0 ? "+" : ""}${pnl.toFixed(2)}`, pnl >= 0 ? "text-emerald" : "text-rose");
        }
        cell(t.status === "OPEN" ? "진행중" : "종료", t.status === "OPEN" ? "text-warn" : "text-lo");

        tbody.appendChild(tr);
    });
}

// updateCountdown shows "다음 분석까지 N분 N초" while running, or a stopped hint. It
// re-ticks every second via countdownTimer so the value stays live between WS pushes.
function updateCountdown(isRunning, nextTickAtISO) {
    const badge = document.getElementById("countdownBadge");
    const text = document.getElementById("countdownText");
    if (countdownTimer) { clearInterval(countdownTimer); countdownTimer = null; }

    if (!isRunning || !nextTickAtISO) {
        badge.style.display = isRunning ? "inline-flex" : "none";
        if (isRunning) text.textContent = "다음 분석 대기 중";
        return;
    }

    const target = new Date(nextTickAtISO).getTime();
    const tick = () => {
        const remain = target - Date.now();
        if (remain <= 0) {
            text.textContent = "분석 실행 중...";
            return;
        }
        const totalMin = Math.floor(remain / 60000);
        const h = Math.floor(totalMin / 60);
        const m = totalMin % 60;
        const s = Math.floor((remain % 60000) / 1000);
        text.textContent = h > 0
            ? `다음 분석까지 ${h}시간 ${m}분`
            : `다음 분석까지 ${m}분 ${s}초`;
    };
    tick();
    badge.style.display = "inline-flex";
    countdownTimer = setInterval(tick, 1000);
}

// --- Render batch backtest results into the results table ---
function renderBacktestTable(results) {
    document.getElementById("backtestResult").style.display = "block";
    const tbody = document.getElementById("backtestTableBody");
    tbody.replaceChildren();

    results.forEach(r => {
        const row = document.createElement("tr");
        const cell = (text, cls) => {
            const td = document.createElement("td");
            td.textContent = text;
            if (cls) td.className = cls;
            row.appendChild(td);
        };

        cell(r.symbol);
        cell(r.strategy);

        if (r.error) {
            const td = document.createElement("td");
            td.colSpan = 6;
            td.className = "text-rose";
            td.textContent = r.error;
            row.appendChild(td);
        } else {
            const rep = r.report;
            cell(`${rep.total_return_pct >= 0 ? '+' : ''}${rep.total_return_pct.toFixed(2)}%`,
                rep.total_return_pct >= 0 ? "text-emerald" : "text-rose");
            cell(`${rep.max_drawdown_pct.toFixed(2)}%`);
            cell(`${rep.win_rate_pct.toFixed(1)}%`);
            cell(rep.profit_factor.toFixed(2));
            cell(rep.sharpe_ratio.toFixed(2));
            cell(String(rep.total_trades));
        }
        tbody.appendChild(row);
    });
}

function toggleStrategyVisibility() {
    const val = document.getElementById("strategySelect").value;
    const aiProviderGroup = document.getElementById("aiProviderGroup");
    const geminiGroup = document.getElementById("geminiKeyGroup");
    const openaiGroup = document.getElementById("openaiKeyGroup");
    const apiKeysDivider = document.getElementById("apiKeysDivider");

    if (val === "ai") {
        aiProviderGroup.style.display = "flex";
        apiKeysDivider.style.display = "block";
        toggleAPIKeyVisibility();
    } else {
        aiProviderGroup.style.display = "none";
        geminiGroup.style.display = "none";
        openaiGroup.style.display = "none";
        apiKeysDivider.style.display = "none";
    }
}

function toggleAPIKeyVisibility() {
    const val = document.getElementById("aiProviderSelect").value;
    const geminiGroup = document.getElementById("geminiKeyGroup");
    const openaiGroup = document.getElementById("openaiKeyGroup");

    if (val === "gemini") {
        geminiGroup.style.display = "flex";
        openaiGroup.style.display = "none";
    } else {
        geminiGroup.style.display = "none";
        openaiGroup.style.display = "flex";
    }
}

// --- Close Position API Call ---
async function closePosition(symbol) {
    if (!confirm(`${symbol} 포지션을 시장가로 청산하시겠습니까?`)) {
        return;
    }

    try {
        const response = await apiFetch("/api/status", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ action: "close", symbol: symbol })
        });
        const result = await response.json();
        if (result.status !== "success") {
            alert("포지션 청산 실패: " + result.message);
        }
    } catch (e) {
        alert("청산 요청 전송 오류: " + e);
    }
}

// --- Render Backtest Trade Markers on TradingView Chart ---
function applyBacktestMarkers(trades) {
    if (!trades || trades.length === 0) return;

    const markers = [];
    
    trades.forEach(trade => {
        const tradeTime = new Date(trade.entry_time).getTime() / 1000;
        const isBuy = trade.side === "LONG";

        // Add entry marker
        markers.push({
            time: tradeTime,
            position: isBuy ? "belowBar" : "aboveBar",
            color: isBuy ? MARKER_LONG : MARKER_SHORT,
            shape: isBuy ? "arrowUp" : "arrowDown",
            text: `진입 ${trade.side} @${trade.entry_price.toFixed(2)}`,
        });

        // Add exit marker — color/label by exit_reason (TP=익절, SL/청산=손절/청산)
        const exitTime = new Date(trade.exit_time).getTime() / 1000;
        const isShortClose = trade.side === "SHORT"; // Closing SHORT = Buy order
        const style = exitStyle(trade.exit_reason, trade.pnl);
        markers.push({
            time: exitTime,
            position: isShortClose ? "belowBar" : "aboveBar",
            color: style.color,
            shape: "circle",
            text: `${style.label} ${trade.side} @${trade.exit_price.toFixed(2)} (${trade.pnl >= 0 ? '+' : ''}${trade.pnl.toFixed(1)})`,
        });
    });

    // Sort markers chronologically
    markers.sort((a, b) => a.time - b.time);
    candleSeries.setMarkers(markers);
}
