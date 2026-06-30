# 설계서: AI 트레이딩 에이전트 Phase 2 (두뇌 + 오케스트레이션)

작성일: 2026-06-30 · 프로젝트: go-bot · 선행: Phase 1(토대, main 머지 완료)

---

## 1. 목표

Phase 1에서 만든 토대(guard·memory·brain 인터페이스)에 **실제 AI 두뇌(Kimi 기반 Bull/Bear/Risk 심의)**와 **오케스트레이션(runner)**을 붙여, 페이퍼 모드에서 한 사이클이 끝까지 도는 "생각하는 트레이더"를 완성한다.

여전히 **검증 우선**: 실제 Kimi 호출 없이 MockCouncil로 전체 파이프라인을 먼저 검증하고, Kimi 실호출은 잔액 충전 후 페이퍼에서 켠다. 실거래 룰봇은 끝까지 무변경.

---

## 2. 확정 사항 (Phase 1 + 사용자 결정)

- **AI 모델**: Kimi (Moonshot AI). 엔드포인트 `https://api.moonshot.ai/v1/chat/completions`(글로벌), 모델 `kimi-k2.6`. **OpenAI Chat Completions 완전 호환** → 기존 `pkg/ai/ai.go`의 `callOpenAI` 패턴 재사용. `response_format: json_object` 지원(구조화 결정 필수).
- **심의 구조**: **1호출 통합** — 한 번의 Kimi 호출 안에서 Bull 관점·Bear 관점·Risk 종합을 모두 생성(구조화 프롬프트). 이벤트 기반이라 충분하고 저렴·빠름. (호출 분리는 LLMClient 추상화 덕에 후일 확장 가능.)
- **키 보관**: `.env`의 `MOONSHOT_API_KEY`·`MOONSHOT_MODEL`. gitignore 보호. (현재 계정 잔액 0 → 실호출 정지 상태, 충전 후 활성화.)

---

## 3. 신규/변경 컴포넌트

### 3.1 `pkg/ai` 확장 — Kimi(OpenAI호환) 호출 (기존 파일 최소 변경)
- 현 `callOpenAI`가 엔드포인트를 하드코딩(`api.openai.com`). **base URL을 인자/필드로 빼서** 같은 코드로 Kimi(`api.moonshot.ai`)도 호출 가능하게 한다. OpenAI 경로는 동작 불변(기본값 유지).
- 신규 얇은 헬퍼: `CallChatJSON(baseURL, apiKey, model, systemPrompt, userPrompt) (string, error)` — 기존 callOpenAI 본문 재사용, 응답 content(JSON 문자열) 반환. brain이 이걸 쓴다.
- **외과적**: 기존 `AnalyzeMarket`·`Explain` 등 동작 불변. Kimi 추가는 base URL 분기뿐.

### 3.2 `pkg/agent/brain` 확장 — 실제 Council (KimiCouncil)
- `KimiCouncil` struct: `LLMClient`(호출 추상) + 모델명 보유. `Council` 인터페이스(`Deliberate(Context) (Decision, error)`) 구현.
- **LLMClient 인터페이스**(brain 내): `Complete(systemPrompt, userPrompt string) (string, error)`. 실구현은 `pkg/ai.CallChatJSON` 래핑. **모델 교체 = 이 구현체 교체**(Claude/GPT도 동일 인터페이스로 후일 추가).
- `Deliberate` 흐름:
  1. `buildPrompt(Context)` — 시장 스냅샷(심볼·가격·지표·regime) + `Context.Past`(memory가 회수한 유사 과거사례)를 프롬프트로 직렬화.
  2. 시스템 프롬프트: "너는 신중한 선물 트레이더. Bull 관점과 Bear 관점을 모두 따진 뒤, 과거 교훈을 반영해 최종 결정을 JSON으로." + 출력 스키마 명시(action·size_pct·stop_loss_pct·take_profit_pct·confidence·reasoning + bull_case·bear_case).
  3. LLM 호출 → JSON 파싱 → `agent.Decision` 매핑. **파싱 실패·타임아웃·빈 응답 → HOLD 폴백**(절대 크래시 안 함).
  4. `sanitizeDecision` 류 1차 클램프(기존 ai.go 패턴 참고) — 단 **진짜 안전은 guard가 책임**(여기선 형식 보정만).
- bull_case/bear_case는 로깅·메모리 기록용(판단 근거 보존 → 회고·신뢰).

### 3.3 `pkg/agent/runner` — 오케스트레이션 (신규 패키지)
Phase 1 최종리뷰가 지목한 "빠진 연결고리". 한 사이클을 엮는다:
```
TriggerEvent(symbol)
  → buildContext: 시장데이터(exchange/indicators) + memory.Recall(symbol,regime,k) → brain.Context
  → council.Deliberate(ctx) → Decision
  → buildAccountState: exchange 잔고·포지션·심볼필터 → agent.AccountState
  → guard.Validate(decision, acc) → SafeDecision + rejections
  → executor.Execute(SafeDecision)  (페이퍼/실거래 = 기존 exchange 인터페이스)
  → memory.Record(episode)  (진입 시) / memory.Close(...)  (청산 시)
  → notify(텔레그램)
```
- **어댑터 책임 분리**: `contextBuilder`(시장→brain.Context), `accountBuilder`(exchange→AccountState). 각각 작고 테스트 가능.
- **regime 분류기**: 간단 규칙(추세/횡보 — 기존 indicators 활용)로 `Context.Regime` 채움. memory 매칭 키.

### 3.4 SafeDecision 타입 강제 (최종리뷰 권고 1)
- guard.Validate가 반환하는 타입을 **`SafeDecision`(별도 타입)**으로 바꾼다. executor는 `SafeDecision`만 받는다 → **guard를 안 거친 Decision은 컴파일 단에서 executor에 못 들어감**. 규율이 아니라 타입으로 강제.
- `SafeDecision`은 `Decision`을 감싼 얇은 래퍼(`type SafeDecision struct { d Decision }` + 접근자). guard만 생성 가능(같은 패키지 또는 생성자 제한).

### 3.5 에피소드 ID 발급 (최종리뷰 권고 2)
- `memory.Record` 진입 시점에 충돌 없는 ID 발급: `{symbol}-{openedAtUnixNano}-{shortNonce}`. runner가 진입 기록 시 생성. 결정론 테스트 위해 시계·nonce는 주입 가능하게.

### 3.6 trigger (이벤트 기반)
- Phase 1 설계서의 trigger를 구현: WS/지표 감시 → 의미있는 움직임 때만 발화. 초기 조건: 돌파·급변동·포지션 SL/TP 근접. 쿨다운·일일 호출 상한(비용 가드).
- **단, Phase 2 범위는 "1심볼 폴링 기반 최소 트리거"부터** — 실시간 WS 트리거는 동작 확인 후. YAGNI.

---

## 4. 안전 (실거래 대비, 변함없음)

- **guard가 최종 안전장치**: AI(Kimi)가 무엇을 결정하든 guard.Validate 통과 필수. SafeDecision 타입으로 우회 불가를 컴파일 강제.
- **LLM 실패 = HOLD**: 호출 실패·파싱 실패·잔액부족 에러 → 조용히 HOLD(거래 안 함). 기존 포지션 SL은 거래소에 이미 걸려 보호 유지.
- **비용 가드**: 트리거 쿨다운 + 일일 호출 상한. 잔액부족 에러 감지 시 호출 중단 + 경고로그/텔레그램.
- **페이퍼 우선**: 실거래 전환은 성과 기준 충족 + 사용자 승인. Phase 2는 페이퍼까지.
- **실거래 룰봇 무변경**: 새 코드는 별 패키지·별 진입점(`cmd/agent` 또는 모드 플래그). 홈서버 실거래 봇 건드리지 않음.

---

## 5. 테스트 전략

- **brain.KimiCouncil**: LLMClient를 mock으로 주입 → 프롬프트 생성·JSON 파싱·HOLD 폴백·클램프 검증. 실제 Kimi는 통합테스트(잔액 있을 때)에서만.
- **runner**: mock exchange + MockCouncil + 임시 memory로 한 사이클 E2E(진입→기록, 청산→회고). 어댑터(context/account builder) 단위 테스트.
- **SafeDecision**: executor가 guard 안 거친 Decision을 받을 수 없음을 타입/테스트로 확인.
- **regime 분류기**: 합성 캔들로 추세/횡보 판정 검증.
- 전 패키지 `-race` green + 기존 봇 회귀 0.

---

## 6. 단계적 구현 (Phase 2 하위)

1. **2-A**: `pkg/ai` base URL 분기 + `CallChatJSON` (OpenAI 동작 불변 검증).
2. **2-B**: `SafeDecision` 타입 + guard.Validate 반환형 변경(기존 테스트 갱신).
3. **2-C**: `brain.KimiCouncil` + LLMClient(mock으로 검증, 실호출 X).
4. **2-D**: `pkg/agent/runner` + 어댑터(contextBuilder·accountBuilder·regime) + 에피소드 ID. MockCouncil로 E2E.
5. **2-E**: 최소 trigger + `cmd/agent` 진입점. 페이퍼 가동(MockCouncil).
6. **2-F**: Kimi 실호출 켜기(잔액 충전 후) — 페이퍼에서 KimiCouncil 교체, 실판단 관찰.

각 하위단계 별 plan→구현→검증. **2-A~2-D가 이번 plan 핵심**(2-E·2-F는 진행하며 구체화).

---

## 7. 비목표 (YAGNI)

- 3호출 분리 심의(현재 1호출 통합; 추상화는 열어둠)
- 임베딩 유사도 memory(규칙기반 유지)
- 실시간 WS 트리거(폴링 최소 트리거부터)
- 다중 LLM 동시(Kimi 1종; 인터페이스로 교체 가능)
- 강화학습/파인튜닝

---

## 8. 미확정 (구현 중)

- regime 분류 임계값 (2-D에서 지표 보고 튜닝)
- 트리거 조건·쿨다운 수치 (2-E)
- Kimi 프롬프트 정밀 튜닝 (2-F, 실호출 가능해진 뒤)
- 성과 전환 기준 수치 (Phase 1 설계서대로 후속)
