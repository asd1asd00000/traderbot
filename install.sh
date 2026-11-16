#!/bin/bash

# توقف اسکریپت در صورت بروز خطا
set -e

# رنگ‌ها برای نمایش بهتر در ترمینال
GREEN='\033[0;32m'
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0;39m'

# آدرس خام (Raw) ریپازیتوری گیت‌هاب شما (در صورت تغییر نام ریپازیتوری، این خط را ویرایش کنید)
REPO_RAW_URL="https://raw.githubusercontent.com/asd1asd00000/traderbot/main"

INSTALL_DIR="/opt/traderbot"
SERVICE_FILE="/etc/systemd/system/traderbot.service"
ENV_FILE="$INSTALL_DIR/.env"

# بررسی دسترسی روت
if [ "$EUID" -ne 0 ]; then
    echo -e "${RED}❌ لطفاً اسکریپت را با دسترسی root اجرا کنید (sudo bash install.sh)${NC}"
    exit 1
fi

clear
echo -e "${CYAN}======================================================${NC}"
echo -e "${GREEN}      🚀 نصب و راه‌اندازی ربات تریدر Gemini API        ${NC}"
echo -e "${CYAN}======================================================${NC}"
echo ""
echo "1) نصب یا بروزرسانی ربات (Install / Update)"
echo "2) مشاهده لاگ‌های زنده ربات (Live Logs)"
echo "3) تغییر تنظیمات و کلیدهای API (Edit Config)"
echo "4) ری‌استارت سرویس (Restart)"
echo "5) حذف کامل ربات (Uninstall)"
echo "0) خروج"
echo ""
read -p "لطفاً یک گزینه را انتخاب کنید [1-5]: " MENU_OPTION < /dev/tty

case $MENU_OPTION in
    2)
        echo -e "${YELLOW}در حال نمایش لاگ‌های زنده (برای خروج Ctrl+C بزنید)...${NC}"
        journalctl -u traderbot -f
        exit 0
        ;;
    3)
        if [ ! -f "$ENV_FILE" ]; then
            echo -e "${RED}ربات هنوز نصب نشده است! ابتدا گزینه 1 را انتخاب کنید.${NC}"
            exit 1
        fi
        nano "$ENV_FILE" < /dev/tty > /dev/tty
        systemctl restart traderbot
        echo -e "${GREEN}✅ تنظیمات جدید اعمال و سرویس ری‌استارت شد.${NC}"
        exit 0
        ;;
    4)
        systemctl restart traderbot
        echo -e "${GREEN}✅ سرویس traderbot با موفقیت ری‌استارت شد.${NC}"
        systemctl status traderbot --no-pager
        exit 0
        ;;
    5)
        systemctl stop traderbot 2>/dev/null || true
        systemctl disable traderbot 2>/dev/null || true
        rm -f "$SERVICE_FILE"
        systemctl daemon-reload
        rm -rf "$INSTALL_DIR"
        echo -e "${GREEN}🗑 ربات و سرویس مربوطه به طور کامل حذف شدند.${NC}"
        exit 0
        ;;
    0)
        exit 0
        ;;
    1)
        echo -e "${CYAN}⏳ شروع مراحل نصب...${NC}"
        ;;
    *)
        echo -e "${RED}گزینه نامعتبر!${NC}"
        exit 1
        ;;
esac

# ۱. دریافت اطلاعات از کاربر در ترمینال
echo ""
echo -e "${YELLOW}--- تنظیمات اولیه ربات ---${NC}"
read -p "🔑 کلید GEMINI_API_KEY را وارد کنید: " INPUT_GEMINI_KEY < /dev/tty
read -p "🤖 توکن ربات تلگرام (TELEGRAM_TOKEN) را وارد کنید: " INPUT_TG_TOKEN < /dev/tty
read -p "👤 آیدی عددی تلگرام (TELEGRAM_CHAT_ID) را وارد کنید: " INPUT_TG_CHAT_ID < /dev/tty
read -p "📊 نماد معاملاتی [پیش‌فرض: USDEUR=X]: " INPUT_SYMBOL < /dev/tty
INPUT_SYMBOL=${INPUT_SYMBOL:-USDEUR=X}

read -p "⏱ تایم‌فریم چارت (5m, 15m, 30m, 1h) [پیش‌فرض: 30m]: " INPUT_INTERVAL < /dev/tty
INPUT_INTERVAL=${INPUT_INTERVAL:-30m}

read -p "🔔 فقط در صورت وجود سیگنال BUY/SELL پیام بدهد؟ (y/n) [پیش‌فرض: n برای تست اولیه]: " INPUT_ONLY_SIGNALS < /dev/tty
INPUT_ONLY_SIGNALS=${INPUT_ONLY_SIGNALS:-n}

# ۲. نصب پیش‌نیازها (Go و Curl) در صورت عدم وجود
echo ""
echo -e "${CYAN}📦 در حال بررسی پیش‌نیازها و نصب Golang...${NC}"
apt-get update -y
if ! command -v go &> /dev/null; then
    apt-get install -y golang curl ca-certificates
else
    apt-get install -y curl ca-certificates
    echo -e "${GREEN}✅ زبان Go از قبل روی سرور نصب است.${NC}"
fi

# ۳. ایجاد پوشه برنامه و دریافت سورس کد از گیت‌هاب
mkdir -p "$INSTALL_DIR"
cd "$INSTALL_DIR"

if [ -f "main.go" ]; then
    echo -e "${YELLOW}📄 فایل main.go در پوشه فعلی یافت شد.${NC}"
else
    echo -e "${CYAN}⬇️ در حال دانلود main.go از گیت‌هاب...${NC}"
    curl -Ls "$REPO_RAW_URL/main.go" -o "$INSTALL_DIR/main.go"
fi

# تنظیم زمان بررسی خودکار و فیلتر سیگنال در کد قبل از کامپایل
if [[ "$INPUT_ONLY_SIGNALS" == "y" || "$INPUT_ONLY_SIGNALS" == "Y" ]]; then
    sed -i 's/SendOnlySignals = false/SendOnlySignals = true/g' "$INSTALL_DIR/main.go"
fi

case $INPUT_INTERVAL in
    5m)  sed -i 's/CheckEvery = 30 \* time.Minute/CheckEvery = 5 \* time.Minute/g' "$INSTALL_DIR/main.go" ;;
    15m) sed -i 's/CheckEvery = 30 \* time.Minute/CheckEvery = 15 \* time.Minute/g' "$INSTALL_DIR/main.go" ;;
    1h)  sed -i 's/CheckEvery = 30 \* time.Minute/CheckEvery = 60 \* time.Minute/g' "$INSTALL_DIR/main.go" ;;
esac

# ۴. کامپایل کردن برنامه Go به فایل باینری سبک
echo -e "${CYAN}🔨 در حال کامپایل کردن سورس Go...${NC}"
if [ ! -f "go.mod" ]; then
    go mod init traderbot
fi
go build -ldflags="-s -w" -o "$INSTALL_DIR/traderbot" "$INSTALL_DIR/main.go"
chmod +x "$INSTALL_DIR/traderbot"

# ۵. ساخت فایل متغیرهای محیطی (.env)
cat > "$ENV_FILE" <<EOF
GEMINI_API_KEY=$INPUT_GEMINI_KEY
TELEGRAM_TOKEN=$INPUT_TG_TOKEN
TELEGRAM_CHAT_ID=$INPUT_TG_CHAT_ID
SYMBOL=$INPUT_SYMBOL
INTERVAL=$INPUT_INTERVAL
EOF
chmod 600 "$ENV_FILE"

# ۶. ساخت سرویس systemd برای اجرای ۲۴ ساعته در پس‌زمینه
echo -e "${CYAN}⚙️ در حال ساخت و فعال‌سازی سرویس systemd...${NC}"
cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=Gemini AI Forex & Crypto Trader Bot
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
WorkingDirectory=$INSTALL_DIR
EnvironmentFile=$ENV_FILE
ExecStart=$INSTALL_DIR/traderbot
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

# ۷. راه‌اندازی سرویس
systemctl daemon-reload
systemctl enable traderbot
systemctl restart traderbot

echo ""
echo -e "${GREEN}======================================================${NC}"
echo -e "${GREEN}✅ نصب و راه‌اندازی با موفقیت به پایان رسید!${NC}"
echo -e "${GREEN}======================================================${NC}"
echo -e "📌 وضعیت سرویس: ${CYAN}systemctl status traderbot${NC}"
echo -e "📜 مشاهده لاگ لحظه‌ای: ${CYAN}journalctl -u traderbot -f${NC}"
echo -e "📂 مسیر نصب و تنظیمات: ${CYAN}$INSTALL_DIR/.env${NC}"
echo ""

# نمایش وضعیت اولیه سرویس
systemctl status traderbot --no-pager
