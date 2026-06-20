# Bybit API 연결 가이드 (내일 작업용)

> 목표: **API 키 발급 → .env 주입 → 봇이 실계좌에 연결되는지만 확인** (페이퍼 모드 유지 = 실제 주문 0).
> 결정 사항: **연결만 확인(is_paper_trading=true 유지)** + **환경변수(.env)로 키 주입**.

---

## ⚠️ 먼저 알아야 할 것 (코드 실측)

1. **이 봇은 테스트넷 미지원** — 라이브 키 = 곧바로 메인넷. 그래서 첫 연결은 **소액 계좌 + 페이퍼 유지**로 한다.
2. **키를 넣어도 `is_paper_trading=true`면 실제 주문 안 나감** (`main.go:50-52`: 페이퍼면 활성 거래소=mock). 연결 확인엔 안전.
3. **env가 config.json을 덮어씀** (`config.go:95`) → 키는 **.env 한 곳에만** 둔다 (파일에 키 안 남아 안전, 이미 `.gitignore` 제외됨).
4. `.env`는 `godotenv`로 **자동 로드**됨 (`main.go:27`) — `go run` 하면 알아서 읽음.

---

## STEP 1 — Bybit에서 API 키 발급 (사용자가 거래소에서 직접)

1. Bybit 로그인 → 우상단 프로필 → **API** (또는 https://www.bybit.com/app/user/api-management)
2. **Create New Key** → **System-generated API Keys** 선택
3. 권한 설정 (연결 확인 단계에서는 최소 권한):
   - **읽기 전용으로 시작 권장**: `Read-Only` 만 켜면 잔고/포지션 조회는 되고 주문은 원천 불가 → 가장 안전한 첫 연결.
   - 나중에 실거래 전환 시: `Unified Trading` → `Trade` 권한 추가 필요.
4. **IP 제한**: 가능하면 본인 IP만 허용 (보안↑). 잘 모르면 일단 비워두고 나중에 설정.
5. 발급되면 **API Key + API Secret** 표시 → Secret은 **이때만 보임**, 복사해둘 것.

> 🔐 첫 연결 = **Read-Only 키 권장**. 잔고/포지션 조회만 검증하면 되고, 실수로도 주문이 안 나감.
> 실거래는 그 다음 별도 단계에서 Trade 권한 키로 교체.

---

## STEP 2 — .env 파일 생성 (키 주입)

`~/Desktop/go/.env` 파일을 만들고 (템플릿은 `.env.example` 참고):

```
BYBIT_API_KEY=발급받은_키
BYBIT_API_SECRET=발급받은_시크릿
```

(AI 키는 룰 전략만 쓸 거라 불필요 — 비워둬도 됨)

권한 잠그기:
```
chmod 600 ~/Desktop/go/.env
```

---

## STEP 3 — 봇 기동 + 연결 확인 (페이퍼 유지)

```
cd ~/Desktop/go
go build -o /tmp/gobot ./cmd/bot && /tmp/gobot
```

기동 로그에서 확인할 것:
- ✅ `Real Bybit Exchange client initialized.` → 키가 읽혔다는 뜻
- ✅ `Paper Trading (Mock) selected as the active exchange backend.` → 안전 (실주문 0)
- ❌ `Bybit API credentials not set` 가 뜨면 → .env 경로/내용 확인

브라우저에서 http://localhost:8080 → 대시보드 확인.

### 실계좌 연결이 진짜 되는지 확인하는 법
페이퍼 모드는 활성 거래소가 mock이라 대시보드 잔고는 10,000 USDT(가짜)로 보임.
**실계좌 잔고 조회가 되는지**를 확인하려면 둘 중 하나:
- (간단) 로그에 `Real Bybit Exchange client initialized.` 떴으면 키 인증 자체는 통과 (서명 OK).
- (확실) 잠깐 실계좌 조회를 찍어보는 1회용 확인은 다음 세션에 내가 작은 점검 명령으로 도와줄 수 있음.

---

## STEP 4 — (나중에, 별도 결정) 실거래 전환

지금은 **안 함**. 연결 확인 끝나고 사용자가 결정하면:
1. Trade 권한 키로 교체
2. 소액만 입금된 계좌 확인 ($20+ 권장, WLDUSDT 최소주문 통과용)
3. `config.json` `is_paper_trading=false` 또는 대시보드 Live 토글 → **Start 누르면 실제 체결**
4. 포트 노출 시 `DASHBOARD_TOKEN` env 필수 (무인증 방지)

첫 진입 후 확인: ① SetLeverage 3x ② 거래소측 하드 SL/TP 동반 ③ logs.json에 OPEN 기록.

---

## 현재 매매 설정 (변경 불필요, 이미 정렬됨)
- `active_strategy`: **volatility_breakout** (룰 기반, AI 안 씀 — 검증된 전략)
- `interval`: 4h · `symbols`: WLDUSDT · `risk_percentage`: 1% · `is_paper_trading`: **true**
