#!/bin/bash

set -e

GREEN='\033[0;32m'
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0;39m'

REPO_RAW_URL="https://raw.githubusercontent.com/asd1asd00000/traderbot/main"

INSTALL_DIR="/opt/traderbot"
SERVICE_FILE="/etc/systemd/system/traderbot.service"
ENV_FILE="$INSTALL_DIR/.env"

if [ "$EUID" -ne 0 ]; then
    echo -e "${RED}Error: Please run this script as root (sudo bash install.sh)${NC}"
    exit 1
fi

clear
echo -e "${CYAN}======================================================${NC}"
echo -e "${GREEN}     Gemini AI Multi-Timeframe Trader Bot Setup       ${NC}"
echo -e "${CYAN}======================================================${NC}"
echo ""
echo "1) Install / Update Bot from GitHub"
echo "2) View Live Logs"
echo "3) Edit Configuration (.env)"
echo "4) Restart Bot Service"
echo "5) Uninstall Bot Completely"
echo "0) Exit"
echo ""
read -p "Select an option [0-5]: " MENU_OPTION < /dev/tty

case $MENU_OPTION in
    2)
        echo -e "${YELLOW}Showing live logs (Press Ctrl+C to exit)...${NC}"
        journalctl -u traderbot -f
        exit 0
        ;;
    3)
        if [ ! -f "$ENV_FILE" ]; then
            echo -e "${RED}Bot is not installed yet! Please select option 1 first.${NC}"
            exit 1
        fi
        nano "$ENV_FILE" < /dev/tty > /dev/tty
        systemctl restart traderbot
        echo -e "${GREEN}Configuration updated and service restarted successfully!${NC}"
        exit 0
        ;;
    4)
        systemctl restart traderbot
        echo -e "${GREEN}Service traderbot restarted successfully!${NC}"
        systemctl status traderbot --no-pager
        exit 0
        ;;
    5)
        systemctl stop traderbot 2>/dev/null || true
        systemctl disable traderbot 2>/dev/null || true
        rm -f "$SERVICE_FILE"
        systemctl daemon-reload
        rm -rf "$INSTALL_DIR"
        echo -e "${GREEN}TraderBot has been completely uninstalled.${NC}"
        exit 0
        ;;
    0)
        exit 0
        ;;
    1)
        echo -e "${CYAN}Starting installation / update...${NC}"
        ;;
    *)
        echo -e "${RED}Invalid option!${NC}"
        exit 1
        ;;
esac

echo ""
echo -e "${YELLOW}--- Bot Configuration ---${NC}"
read -p "Enter GEMINI_API_KEY: " INPUT_GEMINI_KEY < /dev/tty
read -p "Enter TELEGRAM_TOKEN: " INPUT_TG_TOKEN < /dev/tty
read -p "Enter TELEGRAM_CHAT_ID: " INPUT_TG_CHAT_ID < /dev/tty

read -p "Enter AI Model [default: gemini-4-argon]: " INPUT_MODEL < /dev/tty
INPUT_MODEL=${INPUT_MODEL:-gemini-4-argon}

read -p "Enter Trading Symbol [default: USDEUR=X]: " INPUT_SYMBOL < /dev/tty
INPUT_SYMBOL=${INPUT_SYMBOL:-USDEUR=X}

read -p "Enter Check Interval in minutes [default: 15]: " INPUT_CHECK_MIN < /dev/tty
INPUT_CHECK_MIN=${INPUT_CHECK_MIN:-15}

read -p "Send Telegram message ONLY on BUY/SELL signals? (y/n) [default: n]: " INPUT_ONLY_SIGNALS < /dev/tty
if [[ "$INPUT_ONLY_SIGNALS" == "y" || "$INPUT_ONLY_SIGNALS" == "Y" ]]; then
    ONLY_SIGNALS_BOOL="true"
else
    ONLY_SIGNALS_BOOL="false"
fi

echo ""
echo -e "${CYAN}Checking prerequisites and Golang installation...${NC}"
apt-get update -y
if ! command -v go &> /dev/null; then
    apt-get install -y golang curl ca-certificates
else
    apt-get install -y curl ca-certificates
    echo -e "${GREEN}Golang is already installed.${NC}"
fi

mkdir -p "$INSTALL_DIR"
cd "$INSTALL_DIR"

echo -e "${CYAN}Downloading latest main.go from GitHub...${NC}"
curl -Ls "$REPO_RAW_URL/main.go?t=$(date +%s)" -o "$INSTALL_DIR/main.go"

echo -e "${CYAN}Compiling Go binary...${NC}"
if [ ! -f "go.mod" ]; then
    go mod init traderbot
fi
go build -ldflags="-s -w" -o "$INSTALL_DIR/traderbot" "$INSTALL_DIR/main.go"
chmod +x "$INSTALL_DIR/traderbot"

cat > "$ENV_FILE" <<EOF
GEMINI_API_KEY=$INPUT_GEMINI_KEY
GEMINI_MODEL=$INPUT_MODEL
TELEGRAM_TOKEN=$INPUT_TG_TOKEN
TELEGRAM_CHAT_ID=$INPUT_TG_CHAT_ID
SYMBOL=$INPUT_SYMBOL
CHECK_MINUTES=$INPUT_CHECK_MIN
SEND_ONLY_SIGNALS=$ONLY_SIGNALS_BOOL
EOF
chmod 600 "$ENV_FILE"

echo -e "${CYAN}Creating and enabling systemd service...${NC}"
cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=Gemini AI Multi-Timeframe Trader Bot
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

systemctl daemon-reload
systemctl enable traderbot
systemctl restart traderbot

echo ""
echo -e "${GREEN}======================================================${NC}"
echo -e "${GREEN}Installation completed successfully!${NC}"
echo -e "${GREEN}======================================================${NC}"
echo -e "Live Logs command: ${CYAN}journalctl -u traderbot -f${NC}"
echo -e "Config file path:  ${CYAN}$ENV_FILE${NC}"
echo ""

systemctl status traderbot --no-pager
