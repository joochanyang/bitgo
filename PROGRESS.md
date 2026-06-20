# go-bot (ANTIGRAVITY AI 코인선물 자동매매) — PROGRESS

위치: `/Users/mr.joo/Desktop/go` · 모듈 `go-bot` · Go 1.25 · repo `github.com/joochanyang/bitgo`(main)
스택: Bybit V5 Linear Futures + Gemini/OpenAI 결정 + 페이퍼/라이브 + 웹 대시보드(embed) + 백테스터

---

## 🔜 다음 세션 재개 지점 — "go 봇"

### ✅ [파라미터 최적화] 변동성돌파 그리드 스윕 도구 추가 — 완료 (2026-06-21, TDD + 라이브검증)

**사용자 요청**: "파라미터 최적화". 결정: **CLI 스윕 스크립트 + 그리드 스캔 + OOS 검증**. 결과 보고 후 **기본값 유지로 확정**(코드값 미변경).
- **전략 파라미터화**(외과적, 동작 불변): `VolatilityBreakout`에 `lookback·rewardRisk·atrK` 인스턴스 필드 추가. `NewVolatilityBreakout()`은 기존 기본값(20/2.0/1.5) 위임 → **라이브·백테스트 동작 100% 불변**. 신규 `NewVolatilityBreakoutWithParams(lb,rr,k)`(비양수→기본값 폴백)로만 스윕. `Evaluate`가 인스턴스 필드 사용, minHistory=`lookback+15`(기본 35 보존).
- **atrK 스코프 격리**: `sizing.go`에 `atrStopLossPctK(atr,price,k)` 추가, 기존 `atrStopLossPct`는 기본 atrK 위임. → **trend/mean_reversion 공유 상수 안 건드림**(이 둘 동작 불변).
- **신규 `cmd/optimize`**: 라이브 Bybit 4h 데이터(`NewBybitExchange("","",false)`=공개 kline, 무인증) 그리드 스윕 + 기존 `RunBacktestSplit`(70/30 OOS) 재사용 → **백테스터·라이브·config 안 건드림(읽기전용)**. 심볼당 1회 fetch 후 조합 재사용. OOS 통과 심볼수 우선→avgOOS% 순 랭킹. flags: `-symbols -interval -candles -lookbacks -rr -atrk -risk -leverage -top`.
- **🔑 라이브 스윕 결과**(WLD·NEAR·RENDER, 4h, 1000캔들, 60조합, risk 1%): **현재 기본값(20/2.0/1.5)=60중 4위**(3/3 OOS통과, avgOOS+5.62% PF1.94). 1위(10/2.5/1.5, +6.51%)와 격차 ~0.9pp且 1위 우위는 RENDER 단일심볼 얇은 11거래 스파이크 의존 → **그리드가 교체 정당화 안 함, 기본값 유지가 맞음**. ⚠️OOS 절대수치(~+5%)가 헤드라인(+35.9%)보다 작은 건 risk 1%(드라이런 config)+최근 30%구간(~50일)만 측정해서 — 상대랭킹이 핵심, funding/슬리피지 여전히 미반영.
- **검증**: gofmt 클린·`go build`·`go vet`·**전 패키지 `-race` green**(신규 4테스트: WithParams 기본값일치·비양수폴백·R:R→TP변화·atrK→SL변화) + 라이브 스윕 E2E 실행(3심볼 1000캔들 fetch→60조합 OOS 랭킹 출력).
- **재실행법**: `cd ~/Desktop/go && go run ./cmd/optimize -symbols WLDUSDT,NEARUSDT,RENDERUSDT -interval 4h`. 다른 그리드는 `-lookbacks 10,20,30 -rr 1.5,2,2.5 -atrk 1,1.5,2`. 향후 더 넓은 전수/walk-forward 원하면 이 도구 확장.


### ✅ [git 초기화 + 첫 푸시] 이제 일반 커밋/푸시 가능 (2026-06-19)

**프로젝트가 처음으로 git 레포가 됨**(이전 모든 세션 "git 아님·remote 미설정·미커밋"은 해소). `git init`→`.gitignore`→첫 커밋(48파일, `e050f02`)→`origin=https://github.com/joochanyang/bitgo`(joochanyang 계정)→`main` 푸시 완료. 로컬=원격 동기화.
- ⚠️ **`.gitignore`로 비밀/상태파일 영구 제외**(절대 커밋 금지·추적 안 됨): `config.json`(Bybit/Gemini API키 자리)·`logs.json`·`trades.json`·`.env`·빌드산출물(`gobot-*`). 푸시 전·후 2회 점검=비밀파일 0개 확인.
- gh 활성계정 joochanyang(레포주인과 일치) → 계정전환 불필요. 앞으로 커밋 시 푸시까지 함께(사용자 워크플로).

### ✅ [대시보드 전면 리디자인] 다크 카지노 → 정제된 퀀트 터미널 (2026-06-19, frontend-design 스킬)

**사용자 요청**: "레이아웃 구조부터 신경써서 디자인". 결정: **제가 직접 리디자인 / 깔끔·전문 금융대시보드 / 전체 레이아웃 재구성**(기능·id 전부 보존).
- **`style.css` 전면 재작성**(755→약 400줄, 토큰 기반): 컨셉 "Quant Terminal"(Bloomberg/Linear 톤). 다크 차콜 베이스(--ink-900 #0a0c10)+단일 액센트 teal-green(#2dd4a7)+손익 전용 green/red. **그라디언트 글로우·네온·이모지노이즈 제거**, 헤어라인 보더·여백·정보위계로. 미세 그리드 텍스처 배경.
- **타이포 교체**: Inter/Noto → **Sora(UI)·IBM Plex Mono(숫자/데이터, tabular-nums)·Pretendard(한글, jsDelivr CDN)**. 가격·잔고·손익은 전부 mono 정렬.
- **레이아웃**: KPI 레일(4칸, 액센트 좌측바)→상황 배너(풀폭, 액센트 보더)→워크스페이스 2단(차트 1.85fr 히어로 + 우측 1fr 제어/백테스트/로그). 반응형(1040px 1단·560px 모바일). 진입 stagger 애니메이션.
- **🔑 불변식 준수(JS 안 깨짐)**: app.js가 쓰는 **41개 id + 동적 className 전부 보존**(status-badge connect/disconnect·mode-badge paper/live·position-item 내부(pos-symbol·pos-side long/short·pnl-amount·detail-*)·log-row info/warn/error·text-emerald/rose·backtest-table·situation-*·no-data). HTML은 `<head>` 폰트링크만 교체, 본문 구조는 새 CSS에 맞게 그대로 매핑(option value 영문 유지).
- **검증(Playwright)**: 리디자인 렌더 확인(KPI mono·상황배너·2단·헤어라인) + **기능 회귀 0**: 상황카드·KPI(10,000.00 USDT)·봇토글·모드배지·**백테스트 폼 제출→결과테이블(+8.69% 8컬럼) 정상**. build OK·web 테스트 green. ⚠️차트 캔버스 렌더루프 때문에 스크린샷 도구가 "stable" 대기 타임아웃(차트 제거 시 전체샷 성공) — 기능검증이 더 강한 증명. ⚠️ go:embed→재빌드 필수.


### ✅ [초보 친화 대시보드] 상황 설명 카드 + AI 해설 + 차트 가격선 — 완료 (2026-06-19, Playwright 실검증)

**사용자 요청**: "왕초보도 알아볼 상황 설명 시각화(AI가 설명해도 좋고) + 차트에 진입/손절/익절 정확 표시". → 규칙기반 한글 설명 + AI 해설 버튼 + 차트 가격선, 전부 구현.
- **백엔드 ①(Bybit SL/TP 파싱, `bybit.go`)**: `parsePositionList` 순수함수 추출 — Bybit 포지션 응답의 `stopLoss`/`takeProfit`를 Position에 채움(기존엔 무시). TDD 3테스트. ⚠️함정 박제: `makeRequest`가 이미 `result` 봉투 벗겨 반환 → 파서는 `{list:[...]}` 받음(처음 `result` 한번 더 감쌌다가 수정).
- **백엔드 ②(상황 설명, `bot/situation.go` 신규)**: `MarketView`(심볼·실행·포지션·현재가·진입/SL/TP·손익·판단·근거) + `describeSituation` 순수함수가 **쉬운 한글 2줄**(headline+detail) 생성: 정지/대기/보유(롱숏·진입가·수익손실·손절익절). `SymbolStatus{View,Situation}`. 엔진이 매 틱 `recordMarketView`, `MarketViews()` 노출(mu보호, IsRunning 라이브 덮어쓰기). TDD 4테스트.
- **백엔드 ③(AI 해설, `ai.go` `Explain()` + `/api/explain`)**: Gemini/OpenAI로 현재상황 한글 자연어 해설. 기존 `callGemini/callOpenAI` 재사용. **키 없으면 친절한 한글 안내 반환(크래시X·500X)**. web에 `SetExplainProvider`(옵션 콜백, nil-safe), main.go가 MarketViews→프롬프트→`aiClient.Explain` 배선.
- **프론트 ④(상황 카드 `index.html`+`app.js`+`style.css`)**: 지표 그리드 아래 '지금 상황(쉽게 설명)' 카드 — `situations` 맵→headline/detail 렌더(`createElement/textContent`만=XSS안전) + 🤖 AI 해설 버튼(`/api/explain` 호출·로딩·결과박스). 좌측 emerald 보더 카드 + AI박스 blue.
- **프론트 ⑤(차트 가격선)**: `applyPositionPriceLines` — 보유 포지션의 진입가(흰 실선)·손절(빨강 점선)·익절(초록 점선)을 `createPriceLine`으로. 매 새로고침 `removePriceLine`로 정리(누적X·종료 시 사라짐). ⚠️SL/TP는 **거래소 포지션에서만**(라이브/드라이런) → 페이퍼 mock 포지션엔 진입선만.
- **🔑 검증(Playwright + API)**: 틱 강제→상황카드 `🔍 진입 신호 대기 중`+실제가0.6065 렌더 / AI버튼 클릭→로딩→결과(키없음 안내 정상) / 가격선 함수에 가짜포지션 주입→진입0.61(흰)·손절0.59(빨강)·익절0.65(초록) 3선 생성·종료시 0개 정리 확인. build/vet/gofmt clean·전 패키지 **-race green**(TDD 신규 7테스트). ⚠️스크린샷은 1000캔들 애니메이션 타임아웃(데이터검증이 더 강한 증명). ⚠️ go:embed→재빌드 필수.


### ✅ [30m 추가 + 차트 마커 정확도] 단기봉 매매 시각화 — 완료 (2026-06-18, Playwright 실검증)

**사용자 요청**: "15m/30m 위주 매매 시 그 봉 차트에서 진입/손절/익절이 캔들 위치에 정확히 보이게 — 이때 들어가서 이때 나왔구나가 정확히 시각화". 3가지 결정(30m 추가·캔들수 주기별 자동·15m 브라우저 검증).
- **30m 전역 추가**(외과적): 백엔드 `mapInterval`(kline.go: 30m→"30")·`parseInterval`(engine.go: 30m→30분 틱) + 프론트 `loadChartData` 매핑 + 드롭다운 2곳(라이브·백테스트). `mapInterval` 테스트에 30m 케이스 추가.
- **차트 캔들 수 주기별 자동 조절**(핵심 수정): 기존 **전 주기 150개 고정**(15m=37시간치라 오래된 거래 마커가 차트 밖으로 잘림)→`intervalMap`으로 단기봉 더 많이: 5m/15m/30m=**1000개**(15m≈10일·30m≈20일), 1h=500·4h=300. Bybit kline limit 상한 1000.
- **마커 정확도 = 이미 정확**(시간이 캔들 버킷과 정합): 백테스트 trade의 entry_time/exit_time이 캔들 시간과 동일 → setMarkers time이 캔들에 snap. 문제는 "잘림"이었고 캔들수 확대로 해결.
- **🔑 검증(Playwright, 15m)**: config 15m→`loadChartData`→1000캔들(10.4일) 로드→15m 백테스트→**setData/setMarkers 캡처: 마커 40개(진입20+청산20) 전부 inRange=40·exactMatchToCandle=40·outOfRange=0** = 모든 진입/익절/손절이 해당 캔들 정확 위치에 찍힘 증명. 마커 텍스트 한글+손익(`손절 SHORT @0.51 (-103.7)`). build/vet/gofmt 클린·전 패키지 -race green.
- ⚠️ 스크린샷은 1000캔들 라이브 애니메이션 때문에 Playwright 타임아웃(시각캡처 한계, 기능무관) — 데이터 검증(40/40 정합)이 더 강한 증명. ⚠️ go:embed→재빌드 필수.


### ✅ [차트 마커 개선] 진입·익절·손절 색 구분 표시 — 완료 (2026-06-18, Playwright 실검증)

**사용자 요청**: "차트에서 진입구간 표시, 익절 또는 청산 표시가 나왔으면". → 마커 개선 + **익절/손절 색 구분** 선택.
- **`app.js` 2함수만 수정**(`applyChartMarkers` 실거래, `applyBacktestMarkers` 백테스트) + 공유 헬퍼 `exitStyle(reason, pnl)` 신규.
- **진입**: 화살표 유지(롱=초록▲ belowBar, 숏=빨강▼ aboveBar). 텍스트 `진입 LONG @가격`(한글화).
- **청산 색 구분**(핵심): `exitStyle`이 `{color,label}` 반환 — **백테스트**는 `exit_reason` 기준(TP→초록"익절"·SL/LIQUIDATION→빨강"손절"·CLOSE/SWITCH/FORCE_CLOSE→회색"청산"). **실거래**(db.Trade에 exit_reason 없음)는 **realized_pnl 부호**로 익절(≥0 초록)/손절(<0 빨강) 판정. 텍스트 `익절/손절/청산 LONG @가격 (손익)`.
- 색상수 상수화(MARKER_LONG/SHORT/TP/SL/CLOSE), 기존 주황(#f59e0b) 단일 청산색 → 초록/빨강/회색 3색.
- **검증(Playwright)**: WLDUSDT 4h 백테스트→차트 마커 setMarkers 캡처: 18거래=진입18+**익절9(초록)+손절9(빨강)**, 텍스트 한글+손익 정확(`익절 LONG @0.67 (+212.2)`·`손절 LONG @0.31 (-104.0)`). 스크린샷 시각 확인. build/vet/gofmt 클린·web/strategy/backtest 테스트 green. ⚠️ go:embed→재빌드 필수.
- ⚠️ **알려진 제약**: 거래 밀집 구간(4h 초기)은 마커 텍스트 겹침(차트 줌으로 해소, LWC 기본동작). 실거래 청산사유는 PnL부호로만 구분(엔진이 라이브 청산 reason 미기록 — 정밀구분 원하면 db.Trade에 ExitReason 추가=백엔드 후속).


### ✅ [프론트 한국어화] 대시보드 UI 전체 한글 번역 — 완료 (2026-06-18, Playwright 실검증)

웹 대시보드 사용자노출 텍스트를 **전부 한국어로** 번역. 외과적(식별자·연결 불변):
- **`index.html`**: 타이틀·헤더배지(연결됨/모의 매매/봇 시작)·지표카드(사용 가능 잔고·미실현/실현 손익·승률)·카드제목(보유 포지션·시세 차트·봇 제어 설정·과거 데이터 백테스트·AI 분석 및 활동 로그)·폼라벨·드롭다운 표시텍스트·placeholder·백테스트 테이블헤더(심볼/전략/수익률/최대낙폭/승률/손익비/샤프/거래수)·푸터. **⚠️ `<option value="...">` value는 영문 유지**(trend_following·mean_reversion·volatility_breakout·ai·5m/15m/1h/4h — JS·백엔드 식별자라 절대 불변), 보이는 텍스트만 번역.
- **`app.js`**: 동적 문자열 — 상태배지(연결됨/연결 끊김)·봇 시작/봇 정지·모의 매매/실전 매매·포지션카드(진입가·마크가·증거금·수량·시장가 청산·보유 중인 포지션 없음)·버튼(저장 중/설정 저장/실행 중/백테스트 실행)·alert/confirm(설정 저장됨·청산 확인 등)·인샘플/아웃오브샘플. **innerHTML→`setBtnLabel`/`createElement+textContent`로 전환**(보안훅 차단 회피 + XSS 안전, 기존 헬퍼 재사용).
- **`server.go`·`backtest_batch.go`**: UI에 surface되는 백엔드 에러메시지도 한글화(AI 백테스트 거부·전략 없음·과거데이터 조회실패·백테스트 실행실패·초기 자본금/수수료율/캔들 수 검증·필수 입력값 누락·조합 초과). 비노출(Method not allowed 등)은 영문 유지.
- **테스트 수정**: `server_test.go`의 백테스트 에러 substring assertion 6건을 한글로 갱신(ai 거부·초기 자본금·수수료율·캔들 수). build/vet/gofmt 클린 + **전 패키지 `-race` green**.
- **검증(Playwright)**: 한글 렌더 확인 + **WS 여전히 연결·차트 캔들 렌더·백테스트 폼 실제제출→결과행(+8.69%)** = 번역이 기능 안 깨뜨림 증명. ⚠️ HTML/JS는 `go:embed`→**바이너리 재빌드 필수**.


### 🔴→✅ [프론트 치명버그 발견·수정] 차트 라이브러리 버전 불일치로 대시보드 전체 마비 (2026-06-18)

**브라우저 실검증(Playwright) 중 발견**: 대시보드가 **완전 비작동**이었음(잔액 `--.--`·WS DISCONNECTED·차트 공백·Start/Save/백테스트 버튼 전부 죽음).
- **근본원인**: `index.html`이 `unpkg.com/lightweight-charts`(**버전 미고정**)를 로드 → 최신 **v5.2.0**이 받아짐. 근데 `app.js:63`은 v4 API `chart.addCandlestickSeries()` 사용(v5에서 제거됨). `initChart()`가 `DOMContentLoaded` 핸들러 **맨 앞**(app.js:29)에서 예외 throw → **그 뒤 `connectWebSocket()`+`setupEventListeners()`가 아예 실행 안 됨** → WS 연결·모든 이벤트리스너 동반 사망. 차트 한 줄이 앱 전체를 죽이는 연쇄.
- **수정(외과적 1줄)**: `index.html:16` CDN을 `lightweight-charts@4.2.3`로 **버전 고정**(v4.2.3에 addCandlestickSeries 존재 확인, latest=v5엔 0개). app.js 수정 불필요.
- **수정 후 실검증(Playwright)**: WS CONNECTED·차트 렌더OK·잔액 10,000 USDT·**설정폼이 저장된 config 정확 반영**(volatility_breakout/4h/risk1/paper✓)·**백테스트 폼 실제 제출→결과행 렌더**(WLD/vol_breakout +8.69%/MDD4.63%/PF1.95/18trades) = 풀 UI 체인 작동 증명. 잔여 콘솔에러=favicon.ico 404(무해).
- ⚠️ **HTML은 `go:embed`** → index.html 수정 시 **바이너리 재빌드 필수**(안 하면 옛 HTML 박힌 채). `go build ./...` green.
- 📌 **교훈/함정(영구)**: CDN은 **항상 버전 고정**(unpinned=latest=깨짐). init 순서상 차트 예외가 WS+리스너까지 죽이는 단일점 실패 — 방어적으로 `initChart()`를 try/catch로 감싸거나 connectWebSocket을 먼저 호출하는 하드닝은 **후속 선택**(이번엔 스코프 밖, 버전고정으로 근본해결).


### 🟡 [실거래 드라이런] config 4h 세팅 완료·페이퍼 유지 — 다음 = 사용자가 대시보드서 라이브 전환 (2026-06-18)

**사용자 결정대로 `config.json` 드라이런 세팅 완료**(코드 변경 0, config만): `active_strategy=volatility_breakout`·`interval=4h`·`symbols=["WLDUSDT"]`(1종)·`risk_percentage=1.0`(1%)·`max_portfolio_risk_pct=10.0`·**`is_paper_trading=true` 유지**·leverage 3. config.json 권한 0600. 봇 부팅 검증=4h/전략/1% 정상 파싱·페이퍼+stopped 확인.

**🚨 라이브 전환은 사용자 액션(돈 나감 — 나는 안 함)**. 절차:
1. `config.json`에 `bybit_api_key`/`bybit_api_secret` 입력(또는 `BYBIT_API_KEY`/`BYBIT_API_SECRET` env) — **소액 잔고 계좌 권장**. ⚠️Bybit Unified, Linear Futures 권한.
2. 봇 기동: `cd ~/Desktop/go && go build -o /tmp/gobot ./cmd/bot && /tmp/gobot` → http://localhost:8080
3. 대시보드에서 **Paper→Live 토글**(또는 config `is_paper_trading=false`) 후 **Start** 클릭. ⚠️Start 누르면 실제 체결.
4. **포트 노출 시 `DASHBOARD_TOKEN` env 필수**(현재 미설정=무인증 경고).
- **사이징 주의(1% risk)**: notional ≈ balance×0.4(WLDUSDT 4h SL~2.5% 기준). **잔고 $20+면 Bybit 최소주문(~$5) 통과**, ~$15 미만이면 최소주문 미달로 거부될 수 있음. 4h봉이라 신호는 4시간마다(첫 진입까지 시간 걸림).
- **확인할 것(첫 진입 후)**: ① SetLeverage 3x 적용됐는지 ② 거래소측 하드 SL/TP 동반됐는지(Bybit 포지션 화면) ③ WS 트레일링이 SL 조이는지 ④ logs.json에 OPEN 기록.
- ⚠️ 백테스트엔 funding/슬리피지 미반영 → 실제 손익은 약간 불리. 첫 사이클은 동작 검증 목적.

### ✅ [신규 전략] 변동성 돌파(volatility_breakout) 구현·등록·백테스트 — 완료 (2026-06-18, TDD)

**4번째 룰 전략 추가**(trend_following·mean_reversion·ai에 이어). 외과적 추가 — 인프라(ATR 사이징·포트폴리오 캡·하드손절) 전부 재사용.
- **신규파일**: `pkg/strategy/volatility_breakout.go`(+`_test.go` 10테스트). 양방향 롤링채널 돌파: `close > rollingHigh(20)`→LONG, `close < rollingLow(20)`→SHORT. **돌파 레벨은 현재봉 제외한 직전 20봉**(circular 방지 — 테스트로 박제). **거래량 필터**(돌파봉 vol ≥ 직전20봉 평균; 평균=0이면 통과=백테스트 데이터 무볼륨 대응). SL=`atrStopLossPct`(공유), TP=SL×**2.0R**(`breakoutRewardRisk`). min history 35.
  - 순수헬퍼 `rollingHigh/rollingLow/avgVolume`(전부 현재봉 제외, 단위테스트). 상수 `breakoutLookback=20·breakoutMinHistory=35·breakoutRewardRisk=2.0`(config 노출 안 함, 다른 룰전략과 동일 관례).
- **등록**(외과적): `engine.go` Strategies맵 +`"volatility_breakout"` 1줄. 프론트 `index.html` 라이브 드롭다운 + 백테스트 멀티셀렉트 양쪽에 옵션 추가. 백테스트/batch는 `engine.Strategies` 맵 공유라 자동 인식(추가 배선 0).
- **검증**: build/vet/gofmt 클린 + `go test ./... -race` 전 패키지 green(회귀0). 바이너리 빌드 후 실제 Bybit public kline(paper모드 mock도 실데이터 fetch)로 라이브 백테스트 API E2E 통과.
- **🔑 백테스트 결과 — 타임프레임이 결정적(1h 부적합 / 4h 양호)**:
  - **1h(부적합)**: WLDUSDT +6.6%이나 MDD41%·PF1.08·Sharpe0.03·**OOS −6.7%(엣지 소멸)**·타심볼 −20~−47%. 1h 돌파는 노이즈에 묻혀 비robust.
  - **4h(양호·실투입 후보)**: WLDUSDT **+35.9%**(MDD26%·승률42%·PF1.36·Sharpe0.14), **OOS 70/30: in +11.6% → out +21.7%(PF1.67) = 미보유 데이터서도 엣지 유지**(1h와 정반대). 타심볼 **NEARUSDT +62.9%·RENDERUSDT +51.3%**(둘 다 MDD~24%·PF1.4~1.5). (FETUSDT=Bybit linear 데이터없음). 같은 4h서 mean_reversion −32.6%(돌파만 통함), trend_following +51%(둘 다 추세TF서 작동).
  - **결론**: 변동성돌파는 **4h 타임프레임 전용**으로 보는 게 맞음(상위TF서 돌파신호가 유의미, 1h는 채프). 4종 심볼 중 3개서 일관 +수익·낮은MDD·OOS통과 = 실투입 후보 자격. **단 백테스트엔 funding/슬리피지 미반영(사용자 제외)·실거래 미검증 → 소액 드라이런 필수.**
- 사용자 결정 대기: (a) config interval을 4h로 + 실거래 소액 드라이런 (b) 파라미터 추가 최적화(lookback·R:R) (c) 다른 방향.

---

버그 수정(P0~P2 17종) + **백테스트 고도화(1순위) + ATR 사이징(2순위) + 실시간 WS 손절감시(3순위) 전부 완료**. **고도화 3대 항목 모두 끝남** — 다음은 사용자 지정(실거래 드라이런, 또는 신규 방향).

### ✅ [3순위] 실시간 WS 손절 감시 — 완료 (2026-06-16, TDD)
**범위 의도적 축소**: PROGRESS 원문의 "폴링 전면 대체"는 과스코프로 판단(엔진은 캔들간격 타이머 틱이라 전략평가엔 REST로 충분) → 사용자 승인 하에 **트레일링 스톱 실시간 조이기**만 구현(틱 사이 가격 급등→되돌림 시 수익 유실 막는 진짜 이득 지점). private WS·폴링대체 안 함.
- **순수 로직 분리**: `pkg/bot/trailing.go`(신규) `trailingStopTarget(side,entry,currentSL,price)(newSL,shouldUpdate)` — RunTick·WS모니터 **공유**(드리프트 0). 기존 `updateTrailingStop`은 이 함수 위임으로 리팩터(동작 불변, 호출처 engine.go:212 그대로)
- **public WS 클라이언트**: `pkg/exchange/pubws.go`(신규) — Bybit `wss://stream.bybit.com/v5/public/linear`, `tickers.{sym}` 구독, 20s ping, 30s read deadline, 재연결 backoff(1→30s cap), stop 시 conn.Close. 순수 헬퍼(publicWSURL/tickerTopic/buildSubscribe/parseTickerMsg)만 단위테스트, 소켓 Run은 integration-only
- **엔진 모니터**: `runStopMonitor(stopChan)` 고루틴 — Start()에서 캡처한 stopChan으로 기동(누수가드 패턴 동일), per-symbol `lastSL` 캐시(중복 SetStopLoss 방지=레이트리밋 가드), **매 업데이트마다 fresh GetPosition**(캐시 금지)→trailingStopTarget→SetStopLoss. Stop()은 미변경(stopChan 닫힘→ws.Run 종료→conn.Close→read pump 종료)
- **동시성**: 모니터+RunTick 둘 다 SetStopLoss 호출하지만 **tighten-only + fresh position**이라 순서무관·멱등(Bybit 34040="not modified"=성공). 락 불필요, `-race` 통과로 증명
- 신규파일: trailing.go(+test), pubws.go(+test) / 수정: engine.go(struct +wsTestnet필드, runStopMonitor 메서드, Start() 1줄, updateTrailingStop 리팩터)
- **검증**: build/vet/gofmt/test -race 클린 + **바이너리 e2e smoke**(엔진 start→WS 실제 Bybit 연결·틱수신·무포지션 시 no-op→stop 깨끗, panic 0·재연결스톰 0)
- ⚠️ **안전 속성(영구 불변식)**: **모든 포지션은 진입 시 거래소측 하드 손절을 가짐**(executeDecision이 무조건 OrderOptions.StopLossPrice 설정) → WS 끊겨도 트레일링만 멈추지 손절 자체는 서버측에 남음. 재연결은 best-effort로 충분. **이 불변식 깨지면 안 됨**(포지션 진입 시 항상 SL 동반)
- ⚠️ 페이퍼도 실제 mainnet WS 가격 사용(public은 계좌무관 실가격) — 트레일링은 실시간이지만 mock 스톱아웃 자체는 다음 REST GetTicker 샘플에 발동(의도적 비대칭)

### ✅ [2순위] ATR 기반 적응형 사이징 — 완료 (2026-06-16, TDD)
**라이브 사이징 동작이 바뀜** — `cfg.RiskPercentage`의 의미가 명목스케일러 → **진짜 손실한도**로 변경됨:
- **ATR 손절**: `pkg/indicators/indicators.go`에 `CalculateATR`(Wilder, period+1 가드, 전체길이 슬라이스). 두 룰전략(trend/mean_reversion)이 고정 SL%(1.5/1.25) 대신 `atrStopLossPct = clamp((1.5×ATR/price)×100, 0.3, 10.0)` 사용. **익절도 손절에 비례**(TP=SL×원래비율: trend 3.5/1.5=2.33R, mean-rev 2.5/1.25=2.0R) → 변동성 클 때 TP<SL 역전 방지
- **리스크 사이징**: 공유 순수 헬퍼 `pkg/strategy/sizing.go`(신규) — `RiskBasedQty(balance,riskPct,slDist,price,lev) = riskPct%×balance/slDist`(손절 시 손실 ≈ riskPct%, 레버리지 무관). **명목 캡**: qty를 `명목≤balance×leverage`(증거금≤잔고)로 상한 클램프 → 타이트한 SL에도 자금초과 포지션 방지(캡 걸리면 손실<riskPct%, 안전방향). slDist=0(ai의 SL%=0)이면 레거시 명목사이징 폴백. 엔진·백테스터 **양쪽 동일 헬퍼** 호출(드리프트 0), 둘 다 slPrice-먼저-사이징 순서로 재배열
- **레버리지 역할 전환**: 이제 사이징에 안 들어가고 `SetLeverage`는 마진/청산 전용(명목 캡이 레버리지 의미 복원)
- **config 검증**(신규): `sanitizeRiskParams` — RiskPercentage (0, 20] 클램프(음수·0→기본5.0), Leverage [1, 20] 클램프. loadConfig·UpdateConfig 양쪽 적용 → 999% 타이프·0 레버리지 차단
- **stale 픽스처 삭제**: `pkg/config/config.json`(risk 999·paper=false 잔재) 제거
- 신규파일: sizing.go(+test), trend_following_test.go, mean_reversion_test.go / 수정: indicators.go(+test, ATR), trend·mean_reversion(SL/TP), engine·backtester(+test, 사이징), config.go(+test, 검증)
- **검증**: build/vet/gofmt/test -race 클린 + 바이너리 e2e smoke(실제 WLDUSDT 300캔들 → SL손실 ≈ 잔고 5%, ATR손절폭 변동, TP 2.1R) + **적대적 리뷰(18에이전트): CRITICAL 명목폭발 발견→캡으로 수정**
- ⚠️ **함정/주의 (영구)**:
  1. **`cfg.RiskPercentage` 의미 변경**: 명목스케일러→진짜 손실한도. 첫 라이브 실행부터 포지션 크기 바뀜 → **실투입 전 소액 드라이런 필수**
  2. **명목 캡 적용됨**: 저변동성(ATR 0.3% 바닥) 구간에선 명목이 balance×leverage로 캡→그 트레이드는 손실<설정 riskPct%(의도적, 안전). 즉 저변동성에서 리스크가 설계보다 작아질 수 있음(거부보단 나음)
  3. **포트폴리오 캡 적용됨**(2026-06-16): 신규 진입 시 다른 열린 포지션들의 합산 손절리스크(Σ Size×|entry-SL|)를 빼고 남은 예산만큼만 risk% 사용 → 합산 ≤ `MaxPortfolioRiskPct`(기본 10%, config). 예산 소진 시 진입 스킵. 순수함수 `AvailableRiskPct`/`PositionRiskUSDT`(sizing.go) + 엔진 `committedPortfolioRisk`(다른 심볼 GetPosition 합산). 백테스터는 단일포지션이라 미적용(엔진 전용)
  4. `ai_strategy.go`는 미변경(LLM SL%는 ai.go sanitizeDecision이 [0.5,5.0] 클램프 → slDist 항상 >0, 폴백은 사실상 데드코드)
  5. ATR 상수(period=14, k=1.5, 클램프 0.3~10.0)는 sizing.go에 하드코딩(config 노출 안 함)

### ✅ [1순위] 백테스트 고도화 — 완료 (2026-06-16, TDD)
4개 하위기능 전부 TDD로 구현·검증(go build/vet/gofmt/test -race 클린 + 실행 바이너리 e2e smoke 통과):
1. **캔들 수 확대(페이지네이션)** — `pkg/exchange/kline.go`(신규) 순수 헬퍼(mapInterval/parseKlineList/mergeKlinePages, klinePageLimit=1000) + Exchange 인터페이스에 `GetKlinesPaged` 추가. bybit는 `&end=ms` backward 페이징, mock은 클램프+위임. **`GetKlines`는 시그니처·동작 불변** → 라이브 엔진(engine.go:189 limit=200) 무영향
2. **Out-of-sample 분할** — `RunBacktestSplit`(backtester.go): in=candles[:boundary], OOS=candles[boundary-50:](경계 warmup 재사용). `RunBacktest` 가산적 변경만(startIdx→const backtestWarmup=50). UI에 split_ratio 입력칸 연결됨(단일 콤보 한정 → /api/backtest)
3. **파라미터화** — `handleBacktest` 병합 request struct: initial_balance/fee_rate/candles는 **포인터**(0 vs omitted 구분: balance/candles는 0=omitted, fee_rate는 0=유효한 무수수료), split_ratio는 plain. `resolveBacktestParams` 헬퍼+consts(기본 10000/0.0006/500, candles 51~5000, fee≤0.01). 파라미터 검증을 거래소 호출 **전에** 수행
4. **다중 심볼/전략** — `/api/backtest/batch`(신규 `pkg/web/backtest_batch.go`): buildCombos 카테시안+dedup, maxBatchCombos=20, 콤보별 ai 거부, 순차실행, 부분실패=in-body 에러(HTTP 200 유지). 프론트는 multi-select+결과 테이블
- 신규파일: kline.go(+test), backtest_batch.go(+test), backtest_testhelpers_test.go / 수정: exchange.go·bybit.go·mock.go·backtester.go(+test)·server.go·server_test.go·static{index.html,app.js,style.css}
- **함정/주의**: ① Bybit kline limit≤1000(페이징 필수) ② `/api/backtest`와 `/api/backtest/batch`는 ServeMux 정확매칭이라 충돌없음(실측 확인) ③ 프론트 렌더는 textContent/createElement만(innerHTML 금지, XSS훅) ④ split_ratio는 단일 콤보 전용(batch엔 split 없음) ⑤ 잔존 데드CSS: style.css의 `.backtest-metrics-grid/.backtest-metric/.bt-*`는 이제 미사용(외과적 보존, 필요시 삭제) ⑥ 적대적 리뷰 8에이전트 검증=REAL 버그 0(나머지 nit/cosmetic)

### 향후 후보 (고도화 3대 + 포트폴리오 캡 완료 — 아래는 선택)
- **실거래 소액 드라이런**(권장 다음 스텝): ATR 사이징·WS 손절감시·포트폴리오 캡 전부 페이퍼/백테스트만 검증됨. 실계좌 1회 소액으로 SetLeverage·trading-stop·WS 트레일링·합산리스크 라이브 확인 필요
- WS 확장: 원하면 public 캔들/private(포지션·체결) WS로 폴링 대체까지 — 단 현 틱 주기엔 실익 적음(이번에 의도적 제외)
- 비용 모델(funding/슬리피지): 사용자가 제외함. 필요 시 백테스터에 합류 가능

---

## ✅ 완료된 작업 (이전 세션, 2026-06-16)

### 진단
- 17개 에이전트 워크플로우로 서브시스템별 완성도 분석 + 적대적 검증 완료
- 초기 상태: 컴파일 OK·지표수학 정확·구조 양호 BUT 실거래 치명적 버그 다수, 테스트 1개 패키지뿐

### P0 (실거래 치명) 6종 — 완료
1. 레버리지 미설정 → `Exchange.SetLeverage` 추가(Bybit `/v5/position/set-leverage`, Mock 필드). 엔진이 주문 전 호출
2. 손절/트레일링 깨짐 → `Exchange.SetStopLoss` 추가(Bybit `/v5/position/trading-stop`). qty=0 PlaceOrder 해킹 제거
3. STOP 버튼 죽음 → NewWebServer에 stopBot 콜백, case "stop" 배선, main.go가 engine.Stop 전달
4. AI 모델명 삭제 → 프론트 payload에 openai_model/gemini_model + UpdateConfig 빈값 보존
5. AI 결정 미검증 → ai.go sanitizeDecision: {LONG,SHORT,HOLD,CLOSE} 외 → HOLD
6. SL/TP 미검증 → SL 0.5~5.0%, TP 0.5~10.0% 클램프

### P1 (정확도/동시성/보안) 5종 — 완료
1. Config 데이터레이스 → GetConfig 스냅샷복사, UpdateConfig 포인터스왑, 엔진 락 accessor(config()/exchange()), 틱당 backend 1회 캡처. SetPaperTrading 추가. (재시작 고루틴누수·틱중 스왑레이스도 동시 해소)
2. 체결가 위조 → Bybit PlaceOrder 후 GetPosition으로 실제 entryPrice. 엔진 res.Price 기록
3. 심볼 반올림 → `/v5/market/instruments-info` 조회+캐시, qty floor to qtyStep, price→tickSize
4. 무인증 → DASHBOARD_TOKEN env 시 X-Auth-Token(WS는 ?token=) 검사. 프론트 자동첨부
5. AIStrategy stale 거래소 → engine.SetExchange가 AIStrategy.ex도 갱신

### P2 (내구성/위생) 6종 — 완료
1. 백테스트 청산모델 → 마진 소진 시 강제청산(잔고 0 floor, LIQUIDATION 기록), drawdown 음수 방지
2. JSON DB 원자적쓰기 → writeFileAtomic(temp+rename), trades/logs
3. 시크릿 권한 → config.json 0644→0600, trades/logs 0600
4. UpdateTrade 안전화 → ID우선매칭 → 심볼 마지막OPEN 폴백 → 미발견 시 에러(phantom append 제거), ID보존
5. AI 백테스트 가드 → handleBacktest에서 strategy=="ai" 거부
6. gofmt 일괄 → 전 트리 gofmt -w, 13파일 클린

### 검증 상태 (2026-06-16 포트폴리오 리스크 캡 후 갱신)
- `go build ./...` ✅ `go vet ./...` ✅ `gofmt -l pkg/` 빈출력 ✅ `go test -race ./...` ✅
- 실행 바이너리 e2e smoke ✅: (백테스트) 대시보드 서빙 / batch 실제 fetch / OOS split / candles 검증 400 / ai 거부 · (ATR) 실제 WLDUSDT 300캔들 → SL손실 ≈ 잔고 5%, ATR손절폭 변동, TP 2.1R · (WS) Bybit public WS 실연결·틱수신·no-op→stop 깨끗 · (포트폴리오 캡) 엔진 틱 클린 실행
- 테스트 보유 패키지: ai, backtest(+split, +risk사이징), bot(trailing), config(-race, +포트폴리오캡 클램프), db, exchange(+kline, +pubws), indicators(+ATR), strategy(sizing+포트폴리오리스크·trend·mean-rev), web(+param/batch)

---

## ⚠️ 알려진 함정 / 주의
- **실거래 미검증**: 서명·지표·청산로직은 검증됐으나 실제 Bybit 체결·레버리지set·trading-stop은 실계좌에서 한 번도 안 돌아봄(logs.json 증거). 실투입 전 소액 드라이런 1회 필수
- 기본값 `is_paper_trading: true` 유지. 포트 노출 시 DASHBOARD_TOKEN 설정
- 기존 config.json 디스크 파일은 다음 저장 전까지 0644 → 수동 `chmod 600 config.json` 권장
- string-enum 함정: DecisionType은 진짜 enum 아님 → 검증 코드 필수(P0-5에서 처리)
- 모든 코드작업 전 karpathy-guidelines 스킬 적용(글로벌 정책), 외과적 변경 원칙
