package notify

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewReturnsNilWhenCredsMissing(t *testing.T) {
	cases := []struct{ token, chat string }{
		{"", ""}, {"token", ""}, {"", "chat"},
	}
	for _, c := range cases {
		if n := New(c.token, c.chat); n != nil {
			t.Errorf("New(%q,%q) = non-nil, want nil", c.token, c.chat)
		}
	}
}

func TestNewReturnsNotifierWhenCredsPresent(t *testing.T) {
	if n := New("token", "chat"); n == nil {
		t.Fatal("New with both creds returned nil")
	}
}

func TestNilNotifierSendIsNoOp(t *testing.T) {
	var n *Notifier
	n.Send("hello") // must not panic
}

func TestSendPostsToTelegram(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := New("TESTTOKEN", "12345").WithAPIBase(srv.URL)
	n.Send("진입 테스트")

	if !strings.Contains(gotPath, "/botTESTTOKEN/sendMessage") {
		t.Errorf("path = %q, want .../botTESTTOKEN/sendMessage", gotPath)
	}
	if !strings.Contains(gotBody, "12345") {
		t.Errorf("body missing chat_id: %q", gotBody)
	}
	if !strings.Contains(gotBody, "진입 테스트") {
		t.Errorf("body missing text: %q", gotBody)
	}
	if !strings.Contains(gotBody, "HTML") {
		t.Errorf("body missing parse_mode HTML: %q", gotBody)
	}
}

func TestSendSwallowsNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()
	n := New("t", "c").WithAPIBase(srv.URL)
	n.Send("x") // must not panic on API error
}

func TestFormatStartHasConfig(t *testing.T) {
	msg := FormatStart("volatility_breakout", "4h", []string{"WLDUSDT", "NEARUSDT"}, 3, 1.0, 50.0, false)
	for _, want := range []string{"봇 시작", "volatility_breakout", "4h", "3x", "WLDUSDT", "NEARUSDT", "1.0%", "50.00", "실거래"} {
		if !strings.Contains(msg, want) {
			t.Errorf("FormatStart missing %q in:\n%s", want, msg)
		}
	}
}

func TestFormatOpenRich(t *testing.T) {
	msg := FormatOpen("WLDUSDT", "LONG", 30.0, 0.6000, 0.5880, 0.6240, 3, 0.72, 0.6, 18.0, "20봉 채널 상단 돌파", false)
	for _, want := range []string{"신규 진입", "롱", "WLDUSDT", "0.6000", "0.5880", "0.6240", "3x", "18.00", "72%", "돌파", "실거래"} {
		if !strings.Contains(msg, want) {
			t.Errorf("FormatOpen missing %q in:\n%s", want, msg)
		}
	}
	// SL is 2% below entry, TP is 4% above → check the computed pct text shows up.
	if !strings.Contains(msg, "2.00%") || !strings.Contains(msg, "4.00%") {
		t.Errorf("FormatOpen missing computed SL/TP pct:\n%s", msg)
	}
}

func TestFormatCloseWinAndLoss(t *testing.T) {
	win := FormatClose("WLDUSDT", "LONG", 30, 0.60, 0.66, 18.0, 3*time.Hour+12*time.Minute, false)
	if !strings.Contains(win, "수익") || !strings.Contains(win, "+18.00") || !strings.Contains(win, "3시간 12분") {
		t.Errorf("win close wrong:\n%s", win)
	}
	// LONG 0.60→0.66 = +10%
	if !strings.Contains(win, "+10.00%") {
		t.Errorf("win ROI%% wrong:\n%s", win)
	}
	loss := FormatClose("WLDUSDT", "SHORT", 30, 0.60, 0.66, -18.0, time.Hour, false)
	if !strings.Contains(loss, "손실") || !strings.Contains(loss, "-18.00") {
		t.Errorf("loss close wrong:\n%s", loss)
	}
	// SHORT 0.60→0.66 = -10%
	if !strings.Contains(loss, "-10.00%") {
		t.Errorf("loss ROI%% wrong:\n%s", loss)
	}
}

func TestFormatStopHitPicksWordByPnL(t *testing.T) {
	tp := FormatStopHit("WLDUSDT", "LONG", 30, 0.60, 0.624, 7.2, false)
	if !strings.Contains(tp, "익절 체결") || !strings.Contains(tp, "자동 청산") {
		t.Errorf("TP stop-hit wrong:\n%s", tp)
	}
	sl := FormatStopHit("WLDUSDT", "LONG", 30, 0.60, 0.588, -3.6, false)
	if !strings.Contains(sl, "손절 체결") {
		t.Errorf("SL stop-hit wrong:\n%s", sl)
	}
}

func TestFormatTrailing(t *testing.T) {
	msg := FormatTrailing("WLDUSDT", "LONG", 0.5880, 0.5950, 0.6100, false)
	for _, want := range []string{"손절선 이동", "0.5880", "0.5950", "0.6100"} {
		if !strings.Contains(msg, want) {
			t.Errorf("FormatTrailing missing %q in:\n%s", want, msg)
		}
	}
}

func TestFormatSkipAndError(t *testing.T) {
	skip := FormatSkip("WLDUSDT", 10.0, false)
	if !strings.Contains(skip, "진입 보류") || !strings.Contains(skip, "10.0%") {
		t.Errorf("FormatSkip wrong:\n%s", skip)
	}
	er := FormatError("WLDUSDT", io.EOF)
	if !strings.Contains(er, "오류") || !strings.Contains(er, "WLDUSDT") {
		t.Errorf("FormatError wrong:\n%s", er)
	}
}

func TestPaperTag(t *testing.T) {
	msg := FormatStop(true)
	if !strings.Contains(msg, "모의") {
		t.Errorf("paper tag missing:\n%s", msg)
	}
	live := FormatStop(false)
	if !strings.Contains(live, "실거래") {
		t.Errorf("live tag missing:\n%s", live)
	}
}

func TestHumanDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30초"},
		{45 * time.Minute, "45분"},
		{2 * time.Hour, "2시간"},
		{3*time.Hour + 12*time.Minute, "3시간 12분"},
	}
	for _, c := range cases {
		if got := humanDuration(c.d); got != c.want {
			t.Errorf("humanDuration(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}
