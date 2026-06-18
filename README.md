# ANTIGRAVITY AI Coin Futures Auto-Trading Bot

Go 언어 기반의 AI 코인 선물 자동매매 프로그램입니다. Bybit 선물(Linear Futures) 거래소를 지원하며, Google Gemini 1.5 또는 OpenAI GPT-4o 분석을 바탕으로 자동으로 롱(LONG), 숏(SHORT), 홀드(HOLD), 포지션 종료(CLOSE)를 결정합니다. 

자산 보호를 위한 **모의 투자(Paper Trading)** 모드가 기본적으로 제공되며, 실시간 거래 내역 및 차트는 Slate/Emerald 테마의 현대적이고 세련된 **Fintech 웹 대시보드**를 통해 한눈에 관리할 수 있습니다.

---

## 🌟 주요 특징

1. **AI 기반 하이브리드 전략**:
   - 백엔드에서 실시간 캔들 데이터와 기술 지표(RSI-14, EMA 20/50/200, MACD)를 계산합니다.
   - 가공된 시장 데이터 요약본을 Gemini/OpenAI API에 전송하여 고도화된 매매 의사결정을 유도합니다.
2. **모의 투자(Paper Trading) 모드 기본 지원**:
   - Bybit의 실시간 시세를 받아와 작동하되, 가상의 잔고와 포지션 상태를 메모리에서 관리하여 자산 노출 없이 전략의 타당성을 즉시 확인할 수 있습니다.
3. **Slate & Emerald 핀테크 웹 대시보드**:
   - Go의 `embed` 패키지를 사용해 프론트엔드 정적 파일이 하나의 실행 바이너리에 빌드됩니다.
   - 실시간 포지션 손익(PnL) 현황, 자산 성장 추이, 실시간 로그 창을 제공합니다.
   - **TradingView Lightweight Charts**가 내장되어 캔들 데이터와 AI의 진입/청산 마커를 시각적으로 연동해 보여줍니다.
4. **강력한 위험 관리**:
   - 1회 거래 시 감수할 리스크 비율(%) 설정 기능.
   - 무분별한 뇌동매매를 차단하는 최대 레버리지 제어 기능.
   - Bybit 거래소 자체 스탑로스(Stop-Loss, 손절가) 강제 주문 연동.

---

## 🛠 설치 및 실행 방법

### 1. 사전 요구사항
- **Go 1.20 이상** 설치가 필요합니다.

### 2. 패키지 다운로드 및 빌드
Go 모듈을 초기화하고 의존 모듈들을 설치합니다:
```bash
go mod tidy
```

### 3. API 키 설정
프로젝트 루트 폴더에 `.env` 파일을 생성하거나 대시보드의 설정창을 통해 API 키를 입력할 수 있습니다.

**`.env` 파일 예시:**
```env
# Gemini API Key (추천)
GEMINI_API_KEY=AIzaSy...

# OpenAI API Key (선택)
OPENAI_API_KEY=sk-proj-...

# Bybit API Credentials (실거래 시 필요)
BYBIT_API_KEY=your_bybit_api_key
BYBIT_API_SECRET=your_bybit_api_secret
```

### 4. 프로그램 실행
```bash
go run cmd/bot/main.go
```
실행이 완료되면 다음 로그가 터미널에 표시됩니다:
`[INFO] Web dashboard server listening on http://localhost:8080`

### 5. 대시보드 접속
브라우저를 열고 다음 주소에 접속합니다:
👉 **[http://localhost:8080](http://localhost:8080)**

---

## 📈 대시보드 사용 가이드

1. **상단 상태 표시줄 (Header)**:
   - **Status Badge**: 백엔드 서버와의 WebSocket 연결 상태를 표시합니다.
   - **Mode Badge**: 현재 가상 투자(PAPER TRADING) 중인지, 실거래(LIVE TRADING) 중인지 보여줍니다.
   - **START BOT / PAUSE BOT 버튼**: 자동 매매 주기를 시작하거나 일시 정지합니다.
2. **상단 대시보드 카드**:
   - **Available Balance**: 현재 사용 가능한 예수금.
   - **Unrealized PnL / Realized PnL**: 현재 포지션 미실현 손익 및 실현된 누적 수익.
   - **Win Rate**: 포지션 진입 후 익절로 마무리된 비율(승률).
3. **좌측 영역**:
   - **Active Positions**: 현재 오픈된 포지션 정보를 보여줍니다. **[Market Close Position]** 버튼을 통해 즉시 시장가 청산이 가능합니다.
   - **TradingView 캔들 차트**: 코인을 선택하여 가격 움직임을 보고, AI가 매매를 실행한 지점에 화살표 마커(Buy/Sell)를 확인합니다.
4. **우측 영역**:
   - **Bot Control Settings**: 매매 간격(5분, 15분, 1시간 등), 최대 레버리지, 리스크 비율을 조절하고 API Key를 변경하여 실시간으로 저장합니다.
   - **AI Analysis & Activity Logs**: AI가 왜 롱/숏을 쳤는지 분석 근거(Reasoning)와 프로그램 핵심 동작 로그가 실시간으로 스트리밍됩니다.

---

## 🔒 위험 고지 및 면책 조항
본 프로그램은 교육 및 연구 목적으로 제작되었습니다. 암호화폐 선물 거래는 높은 변동성과 레버리지 효과로 인해 원금 초과 손실 발생 가능성이 있습니다. 모의 투자 모드를 통해 매매 전략의 안전성을 충분히 확인한 뒤 실거래에 유의하여 사용하시기 바랍니다. 개발자는 이 봇을 사용함으로써 발생하는 어떠한 손실에 대해서도 책임을 지지 않습니다.
