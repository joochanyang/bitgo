# AI 트레이딩 에이전트 Phase 2-E 설계 — cmd/agent 페이퍼 가동 (DeepSeek 자동 선택)

> 선행: Phase 1(guard·memory·brain 토대), Phase 2-A~D(`pkg/ai.CallChatJSON`·SafeDecision·LLMCouncil·OpenAICompatLLM·runner.RunOnce). 본 문서는 **2-E = 독립 실행파일 `cmd/agent`로 페이퍼 가동**을 다룬다.

## 목표

Phase 2-A~D에서 만든 부품(council→guard→execute→memory)을 실제로 돌리는 **진입점**을 만든다. 실거래 봇(`pkg/bot`·웹 대시보드)과 **완전히 분리된 별도 실행파일**로, 기존 실거래 코드는 한 줄도 건드리지 않는다(회귀 위험 0). council은 환경변수로 자동 선택: `DEEPSEEK_API_KEY`가 있으면 실 DeepSeek, 없으면 MockCouncil. 페이퍼 모드라 **주문은 0**(진입 의도를 로그·메모리에만 기록).

## 비목표

- 실거래 주문 집행(페이퍼는 로그·메모리만).
- 프롬프트 튜닝·백테스트(2-F 이후).
- 기존 `pkg/bot`·웹 대시보드와의 통합(의도적으로 독립).
- 회고 Close 자동화(포지션 청산 감지→memory.Close)는 본 단계 밖(executor가 주문을 안 하므로 청산도 없음). 다음 단계.

## 아키텍처

```
cmd/agent/main.go
  ├─ config.Load()                    # 실거래 config.json 재사용 (symbols·interval·risk·leverage·minConfidence)
  ├─ exchange.NewBybitExchange(key,secret,false)  # 공개 kline + 잔고/포지션 조회 (페이퍼=주문 안 함)
  ├─ pickCouncil(env)                 # DEEPSEEK_API_KEY 있으면 LLMCouncil(OpenAICompatLLM), 없으면 MockCouncil
  ├─ memory.New("agent_memory.json")
  ├─ guard.New(minConfidence)
  └─ runner.Runner{Council, Guard, Execute, Record, OnReject, NowNano, Nonce}
        └─ 매 interval 틱마다, 심볼별로:
             buildContext(ex, symbol, mem, k) → brain.Context  (regime·price·recalled past)
             buildAccount(ex, symbol, cfg)    → agent.AccountState
             runner.RunOnce(ctx, acc)
```

모든 신규 코드는 `cmd/agent/` 안에 둔다. `pkg/*`는 읽기 전용으로 재사용한다.

## 컴포넌트

### `cmd/agent/main.go` — 진입점·틱 루프
- config 로드(기존 `pkg/config`), exchange 생성, council 선택, memory/guard/runner 배선.
- `time.Ticker`로 interval마다 심볼 순회. interval→Duration 변환은 작은 순수 헬퍼(`tickInterval(s string) time.Duration`, 기본 4h)로 분리(테스트 가능). 기존 엔진의 `parseInterval`은 `pkg/bot` 비공개라 재사용 불가 → cmd/agent에 동일 매핑을 작게 둔다.
- 심볼별 처리에서 한 심볼이 에러를 던져도 다른 심볼·다음 틱은 계속(에러 격리). SIGINT로 깨끗이 종료.

### `cmd/agent/council.go` — council 자동 선택
- `pickCouncil(env func(string) string) (brain.Council, string)` — 순수 함수(환경 조회를 주입). `DEEPSEEK_API_KEY` 비어있지 않으면 `brain.NewLLMCouncil(brain.NewOpenAICompatLLM(baseURL, key, model))` + 라벨 "deepseek", 비면 `brain.NewMockCouncil(HOLD)` + 라벨 "mock".
- 기본값: baseURL `https://api.deepseek.com/v1`(env `DEEPSEEK_BASE_URL`로 오버라이드), model `deepseek-v4-flash`(env `DEEPSEEK_MODEL`로 오버라이드). 라벨은 기동 로그에 찍어 어떤 council인지 가시화.

### `cmd/agent/context.go` — `buildContext`
- `buildContext(ex exchange.Exchange, symbol, interval string, mem *memory.Store, recallK int) (brain.Context, error)`.
- 최근 캔들 fetch(`GetKlines(symbol, interval, lookback+여유)` — interval은 config에서, Bybit는 빈 interval 거부하므로 반드시 전달) → 직전 N봉(현재봉 제외) rollingHigh/rollingLow로 채널 계산 → `classifyRegime(price, low, high)`(이미 runner에 있으나 비공개 → cmd/agent에 동일한 작은 순수 헬퍼를 둔다. 두 곳의 분류 기준이 같아야 하므로 상수/로직을 동일하게 유지).
- price = 마지막 종가. `mem.Recall(symbol, regime, recallK)`로 Past 채움.
- 캔들 부족·fetch 실패 → 에러 반환(호출측이 그 심볼 skip).

### `cmd/agent/account.go` — `buildAccount`
- `buildAccount(ex exchange.Exchange, symbol string, allSymbols []string, leverage int, maxPortfolioRisk float64) (agent.AccountState, error)` (config 값을 풀어서 전달 — 헬퍼가 config 구조에 의존하지 않게).
- `GetBalance()` 실패 시 `BalanceOK=false`(나머지 필드 채워 반환 — guard가 진입 차단). 성공 시 `BalanceOK=true`.
- price = `GetTicker(symbol)`. MinOrderQty = 거래소 instruments-info(없으면 0 → guard의 최소수량 규칙이 보수적으로 처리).
- CommittedRiskUSDT = 다른 심볼들의 열린 포지션 합산 리스크: 각 심볼 `GetPosition` → `strategy.PositionRiskUSDT(size, entry, sl)` 합. (기존 엔진 `committedPortfolioRisk` 로직과 동일한 공개 헬퍼 사용.)
- Leverage·MaxPortfolioRisk = config에서.

### `cmd/agent/execute.go` — 페이퍼 executor·로깅
- `func(sd agent.SafeDecision) error` — 진입 의도를 풍부한 로그로(방향·size_pct·SL/TP·신뢰도·근거). **주문 호출 0.** 항상 nil 반환(페이퍼는 실패할 게 없음).
- `OnReject func([]agent.Rejection)` — guard가 막은 이유를 로그(저신뢰·SL없음·리스크예산). 운영자가 "왜 HOLD인지" 본다.
- Record는 `memory.Store.Record`를 그대로 연결. episode에 `OpenedAt`(NowNano로 만든 시각)도 채운다.

## 데이터 흐름

1. 틱 발생 → `cfg.Symbols` 순회.
2. `buildContext` → `council.Deliberate` (DeepSeek or Mock; 실패는 council 내부 HOLD 폴백).
3. `guard.Validate` → SafeDecision + rejections. rejections는 `OnReject`로 로그.
4. 진입이면 → execute(로그) + memory.Record(episode). 비진입이면 무실행.

## 에러 처리

- council 호출 실패 → `LLMCouncil`이 이미 HOLD 폴백(루프 안 죽음).
- `buildContext`/`buildAccount` 에러 → 해당 심볼 그 틱만 skip + 경고로그. 다른 심볼·다음 틱 계속.
- `runner.ErrOrphanRecord` → 페이퍼는 주문 안 하므로 사실상 안 뜸. 발생 시 경고로그(memory 쓰기 실패).
- 기동 시 config 로드 실패·exchange 생성 실패 → 즉시 종료(fatal). 키 없으면 Mock으로 폴백(정상 동작, 비용 0).

## 테스트 (TDD)

- `tickInterval` — 순수 함수, "4h"/"1h"/"30m"/잘못된 값→기본값 단위테스트.
- `pickCouncil` — env 주입, 키 있음→deepseek 라벨·LLMCouncil 타입, 키 없음→mock 라벨·MockCouncil 타입.
- `classifyRegime`(cmd/agent 복사본) — runner의 것과 동일 케이스로 일치 검증.
- `buildContext`/`buildAccount` — **mock exchange**(기존 `pkg/exchange` mock 또는 테스트용 stub)로: regime 분류·price·recall 채움 / balance 실패 시 BalanceOK=false·committed risk 합산.
- E2E smoke: 바이너리를 mock 환경(또는 짧은 틱 + 1회 종료 플래그)으로 1사이클 돌려 panic 0.
- 전 패키지 `go build ./... && go vet ./... && go test -race ./...` green, 기존 실거래 봇 회귀 0.

## 함정·주의

- `classifyRegime`이 runner와 cmd/agent 두 곳에 생김 → 분류 기준(채널 상/하단 10%)을 **반드시 동일**하게 유지(테스트로 박제). 향후 공통화하려면 `pkg/agent`로 올리는 게 맞으나 본 단계에선 스코프 밖(runner 비공개 함수를 공개로 바꾸면 Phase 2-A~D 회귀 표면적↑).
- 페이퍼라도 exchange는 실 mainnet 공개 데이터(kline·ticker)를 씀(잔고/포지션 조회는 키 필요 — 키 없으면 BalanceOK=false로 guard가 진입 차단, 안전).
- DeepSeek base URL은 `https://api.deepseek.com` 또는 `/v1` 둘 다 OpenAI 호환(`/chat/completions` 경로가 `CallChatJSON`에서 붙음 → `/v1` 사용). 모델 `deepseek-v4-flash`(입력 $0.0028/1M·매우 저렴)·`deepseek-v4-pro`. (DeepSeek 공식 문서 확인.)
- `agent_memory.json`은 실거래 `trades.json`과 별개 파일(에이전트 전용 장기기억). `.gitignore`에 추가 필요.
