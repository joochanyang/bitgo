// --- State Variables ---
let socket = null;
let chart = null;
let candleSeries = null;
let currentSymbol = "WLDUSDT";
let reconnectInterval = 3000;
let systemState = {};
let priceLines = []; // chart price lines for the open position (entry/SL/TP), cleared each refresh

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
    
    // Calculate total realized profit from closed trades
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

    // Win Rate
    const winRateVal = document.getElementById("winRateVal");
    if (closedTradesCount > 0) {
        const rate = (wins / closedTradesCount) * 100;
        winRateVal.innerText = `${rate.toFixed(1)}%`;
    } else {
        winRateVal.innerText = "0.0%";
    }

    // 2. Active Positions & Unrealized PnL
    let totalUnrealized = 0;
    const posListContainer = document.getElementById("positionList");
    posListContainer.innerHTML = "";

    if (systemState.positions && systemState.positions.length > 0) {
        systemState.positions.forEach(pos => {
            totalUnrealized += pos.unrealized_pnl;
            
            const isLong = pos.side === "LONG";
            const pnlPercent = (pos.unrealized_pnl / (pos.entry_price * pos.size)) * 100 * pos.leverage;

            const posItem = document.createElement("div");
            posItem.className = "position-item";
            posItem.innerHTML = `
                <div class="pos-main-info">
                    <div class="pos-title-block">
                        <span class="pos-symbol">${pos.symbol}</span>
                        <span class="pos-side ${isLong ? 'long' : 'short'}">${pos.side} ${pos.leverage}x</span>
                        <span class="pos-size">수량: ${pos.size.toFixed(3)}</span>
                    </div>
                    <div class="pos-pnl">
                        <div class="pnl-amount ${pos.unrealized_pnl >= 0 ? 'text-emerald' : 'text-rose'}">
                            ${pos.unrealized_pnl >= 0 ? '+' : ''}${pos.unrealized_pnl.toFixed(2)} USDT
                        </div>
                        <div class="pnl-pct ${pos.unrealized_pnl >= 0 ? 'text-emerald' : 'text-rose'}">
                            ${pos.unrealized_pnl >= 0 ? '+' : ''}${pnlPercent.toFixed(2)}%
                        </div>
                    </div>
                </div>
                <div class="pos-details-grid">
                    <div class="detail-item">
                        <span class="detail-label">진입가</span>
                        <span class="detail-val">${pos.entry_price.toFixed(4)}</span>
                    </div>
                    <div class="detail-item">
                        <span class="detail-label">마크가</span>
                        <span class="detail-val">${pos.mark_price.toFixed(4)}</span>
                    </div>
                    <div class="detail-item">
                        <span class="detail-label">증거금</span>
                        <span class="detail-val">${((pos.entry_price * pos.size) / pos.leverage).toFixed(2)} USDT</span>
                    </div>
                </div>
                <button onclick="closePosition('${pos.symbol}')" class="btn btn-danger btn-sm btn-block">
                    <i class="fa-solid fa-square-xmark"></i> 시장가 청산
                </button>
            `;
            posListContainer.appendChild(posItem);
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

    // 3. Engine Running Button / Badge
    const toggleBtn = document.getElementById("toggleBotBtn");
    const modeBadge = document.getElementById("modeBadge");

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
    } else {
        modeBadge.className = "mode-badge live";
        modeBadge.innerText = "실전 매매";
    }

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
        const item = document.createElement("div");
        item.className = "situation-item";

        const head = document.createElement("div");
        head.className = "situation-headline";
        head.textContent = s.headline || "";
        item.appendChild(head);

        const detail = document.createElement("div");
        detail.className = "situation-detail";
        detail.textContent = s.detail || "";
        item.appendChild(detail);

        box.appendChild(item);
    });
}

// --- Bind Button Actions ---
function setupEventListeners() {
    // Symbol select
    const symbolSelect = document.getElementById("chartSymbolSelect");
    symbolSelect.addEventListener("change", (e) => {
        currentSymbol = e.target.value;
        loadChartData(currentSymbol);
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
