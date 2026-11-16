package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// ==================== تنظیمات اصلی (خوانده شده از .env) ====================
var (
	GeminiAPIKey    = getEnv("GEMINI_API_KEY", "YOUR_GEMINI_API_KEY")
	GeminiModel     = getEnv("GEMINI_MODEL", "gemini-4-argon")
	TelegramToken   = getEnv("TELEGRAM_TOKEN", "YOUR_TELEGRAM_BOT_TOKEN")
	TelegramChatID  = getEnv("TELEGRAM_CHAT_ID", "YOUR_CHAT_ID")
	Symbol          = getEnv("SYMBOL", "USDEUR=X")
	CheckMinutes    = getEnvInt("CHECK_MINUTES", 15)
	SendOnlySignals = getEnvBool("SEND_ONLY_SIGNALS", false)
)

// تایم‌فریم‌هایی که همزمان بررسی و ادغام می‌شوند
var Timeframes = []struct {
	Interval string
	Range    string
	Limit    int
}{
	{Interval: "1h", Range: "1mo", Limit: 25},
	{Interval: "30m", Range: "5d", Limit: 25},
	{Interval: "15m", Range: "5d", Limit: 25},
	{Interval: "5m", Range: "5d", Limit: 30},
}

// کلاینت اختصاصی IPv4 (مشابه curl -4) با تایم‌اوت ۱۸۰ ثانیه برای تحلیل عمیق Argon
var httpClient = &http.Client{
	Timeout: 180 * time.Second,
	Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
			return d.DialContext(ctx, "tcp4", addr)
		},
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 170 * time.Second,
	},
}

type YahooChartResponse struct {
	Chart struct {
		Result []struct {
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Open  []*float64 `json:"open"`
					High  []*float64 `json:"high"`
					Low   []*float64 `json:"low"`
					Close []*float64 `json:"close"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
	} `json:"chart"`
}

type GeminiRequest struct {
	Contents []struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	} `json:"contents"`
}

type GeminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

func main() {
	log.Printf("🚀 Multi-Timeframe Trader Bot started | Symbol: %s | Model: %s | Interval: every %dm", Symbol, GeminiModel, CheckMinutes)

	// اجرای اولیه بلافاصله پس از شروع
	runMultiTimeframeAnalysis()

	// اجرای زمان‌بندی شده
	ticker := time.NewTicker(time.Duration(CheckMinutes) * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		runMultiTimeframeAnalysis()
	}
}

func runMultiTimeframeAnalysis() {
	log.Println("⏳ Fetching candles for timeframes: 1h, 30m, 15m, 5m...")

	var combinedData strings.Builder
	var currentPrice float64

	for _, tf := range Timeframes {
		candlesText, lastClose, err := fetchCandles(Symbol, tf.Interval, tf.Range, tf.Limit)
		if err != nil {
			log.Printf("❌ Error fetching %s candles: %v\n", tf.Interval, err)
			return
		}
		combinedData.WriteString(fmt.Sprintf("\n=== تایم‌فریم %s (%d کندل آخر) ===\n%s\n", tf.Interval, tf.Limit, candlesText))
		if tf.Interval == "5m" || currentPrice == 0 {
			currentPrice = lastClose
		}
		time.Sleep(300 * time.Millisecond)
	}

	log.Printf("✅ All timeframes fetched (Current Price: %.4f). Sending to %s (please wait 30-90s for deep analysis)...", currentPrice, GeminiModel)

	var analysis string
	var err error

	for attempt := 1; attempt <= 2; attempt++ {
		analysis, err = analyzeWithGemini(combinedData.String(), currentPrice)
		if err == nil {
			break
		}
		log.Printf("⚠️ Attempt %d failed: %v", attempt, err)
		if attempt < 2 {
			log.Println("🔄 Retrying in 5 seconds...")
			time.Sleep(5 * time.Second)
		}
	}

	if err != nil {
		log.Printf("❌ Gemini API Error after retries: %v\n", err)
		return
	}

	if SendOnlySignals && strings.Contains(analysis, "وضعیت نهایی: WAIT") {
		log.Println("⏸ Status is WAIT. Skipping Telegram notification (SEND_ONLY_SIGNALS=true).")
		return
	}

	log.Println("📨 Sending Multi-Timeframe analysis to Telegram...")
	if err := sendTelegramMessage(analysis); err != nil {
		log.Printf("❌ Telegram Error: %v\n", err)
	} else {
		log.Println("✅ Analysis successfully sent to Telegram.")
	}
}

func fetchCandles(symbol, interval, rangeParam string, limit int) (string, float64, error) {
	url := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s?interval=%s&range=%s", symbol, interval, rangeParam)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	var data YahooChartResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", 0, err
	}

	if len(data.Chart.Result) == 0 || len(data.Chart.Result[0].Indicators.Quote) == 0 {
		return "", 0, fmt.Errorf("no data found for symbol %s", symbol)
	}

	res := data.Chart.Result[0]
	quote := res.Indicators.Quote[0]
	timestamps := res.Timestamp

	var validCandles []string
	var lastClose float64

	for i := 0; i < len(timestamps); i++ {
		if quote.Open[i] == nil || quote.High[i] == nil || quote.Low[i] == nil || quote.Close[i] == nil {
			continue
		}
		t := time.Unix(timestamps[i], 0).UTC().Format("01-02 15:04")
		line := fmt.Sprintf("%s | O:%.4f H:%.4f L:%.4f C:%.4f",
			t, *quote.Open[i], *quote.High[i], *quote.Low[i], *quote.Close[i])
		validCandles = append(validCandles, line)
		lastClose = *quote.Close[i]
	}

	if len(validCandles) > limit {
		validCandles = validCandles[len(validCandles)-limit:]
	}

	return strings.Join(validCandles, "\n"), lastClose, nil
}

func analyzeWithGemini(allCandles string, currentPrice float64) (string, error) {
	prompt := fmt.Sprintf(`تو یک تریدر حرفه‌ای پرایس اکشن و مولتی تایم‌فریم (Multi-Timeframe) هستی.
اطلاعات کندل‌های نماد %s در ۴ تایم‌فریم (1h, 30m, 15m, 5m) به شرح زیر است (قیمت لحظه‌ای: %.4f):
%s

وظیفه تو:
1. روند کلی و سطوح اصلی حمایت/مقاومت را از تایم‌فریم‌های 1h و 30m تشخیص بده.
2. ساختار میانی را در 15m و تریگر ورود دقیق را در 5m بررسی کن.
3. اگر تایم‌فریم‌ها هم‌جهت هستند و قیمت در ناحیه کم‌ریسکی قرار دارد، سیگنال BUY یا SELL بده؛ در غیر این صورت وضعیت نهایی را WAIT اعلام کن و بگو در چه قیمتی منتظر شکار معامله باشیم.

خروجی را دقیقاً با این فرمت فارسی و بدون حاشیه بنویس:

📊 تحلیل مولتی تایم‌فریم (Argon): %s
💵 قیمت فعلی: %.4f

🕒 وضعیت تایم‌فریم‌ها:
• 1h: [صعودی/نزولی/رنج + سطح کلیدی]
• 30m: [صعودی/نزولی/رنج + سطح کلیدی]
• 15m: [وضعیت ساختار]
• 5m: [وضعیت کندل‌های آخر]

🎯 وضعیت نهایی: [BUY یا SELL یا WAIT]
📍 نقطه ورود بهینه: [عدد یا محدوده دقیق]
🛑 حد ضرر (SL): [عدد دقیق با احتساب ۴ پوینت اسپرد]
✅ حد سود اول (TP1): [عدد دقیق]
✅ حد سود دوم (TP2): [عدد دقیق]
💡 استراتژی و دلیل تکنیکال: [حداکثر ۳ خط توضیح شفاف درباره سناریوی پیش‌رو]`,
		Symbol, currentPrice, allCandles, Symbol, currentPrice)

	reqBody := GeminiRequest{}
	reqBody.Contents = append(reqBody.Contents, struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	}{
		Parts: []struct {
			Text string `json:"text"`
		}{{Text: prompt}},
	})

	jsonData, _ := json.Marshal(reqBody)
	apiURL := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", GeminiModel)

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", GeminiAPIKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var geminiResp GeminiResponse
	if err := json.Unmarshal(bodyBytes, &geminiResp); err != nil {
		return "", err
	}

	// استخراج بخش پاسخ نهایی (در صورتی که مدل بخش Thinking هم برگرداند، آخرین Part متن نهایی است)
	if len(geminiResp.Candidates) > 0 && len(geminiResp.Candidates[0].Content.Parts) > 0 {
		parts := geminiResp.Candidates[0].Content.Parts
		return parts[len(parts)-1].Text, nil
	}

	return "", fmt.Errorf("empty response from Gemini")
}

func sendTelegramMessage(text string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", TelegramToken)
	payload := map[string]string{
		"chat_id": TelegramChatID,
		"text":    text,
	}
	jsonData, _ := json.Marshal(payload)

	resp, err := httpClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram error: %s", string(body))
	}
	return nil
}

func getEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		val = strings.ToLower(strings.TrimSpace(val))
		return val == "true" || val == "1" || val == "yes" || val == "y"
	}
	return fallback
}
