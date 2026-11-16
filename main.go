package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// ==================== تنظیمات اصلی ====================
var (
	GeminiAPIKey   = getEnv("GEMINI_API_KEY", "YOUR_GEMINI_API_KEY")
	TelegramToken  = getEnv("TELEGRAM_TOKEN", "YOUR_TELEGRAM_BOT_TOKEN")
	TelegramChatID = getEnv("TELEGRAM_CHAT_ID", "YOUR_CHAT_ID")

	// نماد USD/EUR در یاهو فایننس با USDEUR=X شناخته می‌شود
	Symbol   = getEnv("SYMBOL", "USDEUR=X")
	Interval = getEnv("INTERVAL", "30m") // تایم‌فریم: 5m, 15m, 30m, 1h
	CheckEvery = 30 * time.Minute        // فاصله زمانی هر بار بررسی

	// اگر true باشد، فقط زمانی که موقعیت خرید/فروش قطعی وجود داشته باشد پیام تلگرام می‌فرستد
	// اگر false باشد، در هر بررسی (حتی وضعیت صبر/WAIT) گزارش کامل را به تلگرام می‌فرستد
	SendOnlySignals = false
)

// ساختار دریافت دیتا از Yahoo Finance
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

// ساختار ارسال و دریافت به Gemini API
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
	log.Printf("🚀 ربات تحلیلگر %s در تایم‌فریم %s شروع به کار کرد...", Symbol, Interval)

	// اجرای اولیه بلافاصله پس از روشن شدن
	runAnalysis()

	// اجرای زمان‌بندی شده در حلقه
	ticker := time.NewTicker(CheckEvery)
	defer ticker.Stop()

	for range ticker.C {
		runAnalysis()
	}
}

func runAnalysis() {
	log.Println("⏳ در حال دریافت کندل‌های اخیر از بازار...")
	candlesText, lastPrice, err := fetchCandles(Symbol, Interval, 60)
	if err != nil {
		log.Printf("❌ خطا در دریافت کندل‌ها: %v\n", err)
		return
	}

	log.Printf("✅ کندل‌ها دریافت شد (قیمت فعلی: %.4f). در حال ارسال به Gemini...", lastPrice)
	analysis, err := analyzeWithGemini(candlesText, lastPrice)
	if err != nil {
		log.Printf("❌ خطا در ارتباط با Gemini API: %v\n", err)
		return
	}

	// بررسی اینکه آیا فقط در صورت وجود سیگنال پیام ارسال شود یا همیشه
	if SendOnlySignals && strings.Contains(analysis, "وضعیت: WAIT") {
		log.Println("⏸ موقعیت جدیدی برای ورود نیست (WAIT). پیام تلگرام ارسال نشد.")
		return
	}

	log.Println("📨 در حال ارسال تحلیل به تلگرام...")
	if err := sendTelegramMessage(analysis); err != nil {
		log.Printf("❌ خطا در ارسال به تلگرام: %v\n", err)
	} else {
		log.Println("✅ تحلیل با موفقیت به تلگرام ارسال شد.")
	}
}

// ۱. دریافت کندل‌ها از Yahoo Finance (رایگان و بدون نیاز به API Key)
func fetchCandles(symbol, interval string, limit int) (string, float64, error) {
	rangeParam := "5d"
	if interval == "1h" {
		rangeParam = "1mo"
	}

	url := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s?interval=%s&range=%s", symbol, interval, rangeParam)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	var data YahooChartResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", 0, err
	}

	if len(data.Chart.Result) == 0 || len(data.Chart.Result[0].Indicators.Quote) == 0 {
		return "", 0, fmt.Errorf("داده‌ای برای این نماد یافت نشد")
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
		line := fmt.Sprintf("%s | O: %.4f | H: %.4f | L: %.4f | C: %.4f",
			t, *quote.Open[i], *quote.High[i], *quote.Low[i], *quote.Close[i])
		validCandles = append(validCandles, line)
		lastClose = *quote.Close[i]
	}

	if len(validCandles) > limit {
		validCandles = validCandles[len(validCandles)-limit:]
	}

	return strings.Join(validCandles, "\n"), lastClose, nil
}

// ۲. ارسال کندل‌ها به Gemini API برای تشخیص موقعیت
func analyzeWithGemini(candles string, currentPrice float64) (string, error) {
	prompt := fmt.Sprintf(`تو یک تریدر حرفه‌ای پرایس اکشن هستی. اطلاعات ۶۰ کندل آخر نماد %s در تایم‌فریم %s به شرح زیر است (قیمت فعلی: %.4f):

%s

بر اساس ساختار پله‌ای قیمت، سطوح حمایت و مقاومت، و قدرت کندل‌های آخر، مشخص کن آیا در همین لحظه موقعیت کم‌ریسکی برای ورود وجود دارد یا باید صبر کرد.
خروجی را دقیقاً و به صورت خلاصه با این فرمت فارسی بنویس:

📊 نماد: %s (تایم‌فریم %s)
💵 قیمت فعلی: %.4f
🎯 وضعیت: [BUY یا SELL یا WAIT]
📍 نقطه ورود دقیق: [عدد یا محدوده]
🛑 حد ضرر (SL): [عدد با احتساب ۴ پوینت اسپرد]
✅ حد سود اول (TP1): [عدد]
✅ حد سود دوم (TP2): [عدد]
💡 دلیل تکنیکال: [حداکثر ۲ خط توضیح واضح درباره روند و سطوح کلیدی]`,
		Symbol, Interval, currentPrice, candles, Symbol, Interval, currentPrice)

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
	apiURL := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent?key=%s", GeminiAPIKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(apiURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("کد خطا %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var geminiResp GeminiResponse
	if err := json.Unmarshal(bodyBytes, &geminiResp); err != nil {
		return "", err
	}

	if len(geminiResp.Candidates) > 0 && len(geminiResp.Candidates[0].Content.Parts) > 0 {
		return geminiResp.Candidates[0].Content.Parts[0].Text, nil
	}

	return "", fmt.Errorf("پاسخی از جمینای دریافت نشد")
}

// ۳. ارسال نتیجه به تلگرام
func sendTelegramMessage(text string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", TelegramToken)
	payload := map[string]string{
		"chat_id": TelegramChatID,
		"text":    text,
	}
	jsonData, _ := json.Marshal(payload)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("خطای تلگرام: %s", string(body))
	}
	return nil
}

func getEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return fallback
}
