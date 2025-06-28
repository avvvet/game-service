package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	config "github.com/avvvet/bingo-services/configs"
	"github.com/avvvet/bingo-services/internal/comm"
	"github.com/avvvet/bingo-services/internal/gamesvc/db"
	natscli "github.com/avvvet/bingo-services/internal/nats"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ledongthuc/pdf"
	"github.com/nats-io/nats.go"
	"github.com/shopspring/decimal"
	log "github.com/sirupsen/logrus"
)

const SERVICE_NAME = "pay"

var instanceId string

func init() {
	instanceId = "001" //config.CreateUniqueInstance(SERVICE_NAME)
	config.Logging(SERVICE_NAME + "_service_" + instanceId)
	config.LoadEnv(SERVICE_NAME)
}

var (
	// 2) compile once
	numRe = regexp.MustCompile(`[\d,]+\.\d+|[\d,]+`)

	// 4) reuse one client
	httpClient = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	// Telegram bot instance
	telegramBot *tgbotapi.BotAPI
)

// TelegramNotifier handles sending notifications to multiple users
type TelegramNotifier struct {
	bot     *tgbotapi.BotAPI
	chatIDs []int64
}

// NewTelegramNotifier creates a new Telegram notifier
func NewTelegramNotifier(botToken string, chatIDs []int64) (*TelegramNotifier, error) {
	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %v", err)
	}

	return &TelegramNotifier{
		bot:     bot,
		chatIDs: chatIDs,
	}, nil
}

// SendNotification sends a message to all configured chat IDs
func (tn *TelegramNotifier) SendNotification(message string) {
	if tn == nil || tn.bot == nil {
		return
	}

	for _, chatID := range tn.chatIDs {
		msg := tgbotapi.NewMessage(chatID, message)
		msg.ParseMode = "Markdown"

		go func(cid int64) {
			if _, err := tn.bot.Send(tgbotapi.NewMessage(cid, message)); err != nil {
				log.Errorf("Failed to send telegram message to chat %d: %v", cid, err)
			}
		}(chatID)
	}
}

// Initialize Telegram notifier
func initTelegramNotifier() *TelegramNotifier {
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		log.Warn("TELEGRAM_BOT_TOKEN not set, notifications disabled")
		return nil
	}

	// Parse chat IDs from environment variables
	var chatIDs []int64
	for i := 1; i <= 3; i++ {
		chatIDStr := os.Getenv(fmt.Sprintf("TELEGRAM_CHAT_ID_%d", i))
		if chatIDStr != "" {
			if chatID, err := strconv.ParseInt(chatIDStr, 10, 64); err == nil {
				chatIDs = append(chatIDs, chatID)
			} else {
				log.Errorf("Invalid TELEGRAM_CHAT_ID_%d format: %v", i, err)
			}
		}
	}

	if len(chatIDs) == 0 {
		log.Warn("No valid telegram chat IDs found, notifications disabled")
		return nil
	}

	notifier, err := NewTelegramNotifier(botToken, chatIDs)
	if err != nil {
		log.Errorf("Failed to initialize Telegram notifier: %v", err)
		return nil
	}

	log.Infof("Telegram notifier initialized with %d chat IDs", len(chatIDs))
	return notifier
}

var telegramNotifier *TelegramNotifier

type PaymentInfo struct {
	Receiver    string
	TotalAmount float64
	PaymentDate string
}

type WithdrawalRequest struct {
	ID              int64           `json:"id"`
	UserID          int64           `json:"user_id"`
	Amount          decimal.Decimal `json:"amount"`
	BalanceSnapshot decimal.Decimal `json:"balance_snapshot"`
	AccountType     string          `json:"account_type"`
	AccountNo       string          `json:"account_no"`
	Name            string          `json:"name"`
	Status          string          `json:"status"`
}

func main() {
	// Initialize Telegram notifier
	telegramNotifier = initTelegramNotifier()

	// pg connection
	dbpool, err := db.Connect()
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}
	defer db.ClosePool()
	log.Printf("pg connection established successfully")

	// Connect to NATS
	nc, err := natscli.Connect()
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer nc.Conn.Close()
	log.Infof("NATS connected at %s", nc.Url)

	// Subscribe to payment service
	_, err = nc.Conn.Subscribe("payment.service", func(m *nats.Msg) {
		handlePaymentService(nc, dbpool, m)
	})
	if err != nil {
		log.Fatalf("Subscribe payment.service error: %v", err)
	}

	select {}
}

func handlePaymentService(nc *natscli.Nats, pool *pgxpool.Pool, msg *nats.Msg) {
	// Decode wrapper
	var ws comm.WSMessage
	if err := json.Unmarshal(msg.Data, &ws); err != nil {
		log.Errorf("invalid WSMessage: %v", err)
		return
	}

	switch ws.Type {
	case "deposite":
		handleDeposit(nc, pool, ws)
	case "withdrawal":
		handleWithdrawal(nc, pool, ws)
	case "transfer":
		handleTransfer(nc, pool, ws)
	case "search-user":
		handleUserSearch(nc, pool, ws)
	default:
		log.Warnf("unknown message type: %s", ws.Type)
	}
}

// Add this helper function
func extractReferenceNumber(text string) (string, error) {
	// Look for the CBE URL pattern with ?id= parameter
	pattern := `https://apps\.cbe\.com\.et:100/\?id=(FT[A-Z0-9]+)`
	re := regexp.MustCompile(pattern)

	matches := re.FindStringSubmatch(text)
	if len(matches) < 2 {
		return "", fmt.Errorf("CBE confirmation URL with reference number not found")
	}

	referenceNumber := matches[1]

	// Additional validation: ensure it starts with FT and has reasonable length
	if !strings.HasPrefix(referenceNumber, "FT") || len(referenceNumber) < 10 {
		return "", fmt.Errorf("invalid reference number format")
	}

	return referenceNumber, nil
}

func handleDeposit(nc *natscli.Nats, pool *pgxpool.Pool, ws comm.WSMessage) {
	// Decode request
	var req comm.PaymentRequest
	if err := json.Unmarshal(ws.Data, &req); err != nil {
		log.Errorf("invalid PaymentRequest: %v", err)
		res := comm.DepositeRes{
			Status:    "invalid-request",
			Message:   "Invalid request format",
			Timestamp: time.Now().Unix(),
		}
		PublishDepositeRes(nc, res, ws.SocketId)
		return
	}

	// Extract reference number from the text
	referenceNumber, err := extractReferenceNumber(req.Reference)
	if err != nil {
		log.Errorf("reference extraction error: %v", err)
		res := comm.DepositeRes{
			Status:    "invalid-reference",
			Message:   "Please include the complete CBE confirmation message with the transaction URL",
			Timestamp: time.Now().Unix(),
		}
		PublishDepositeRes(nc, res, ws.SocketId)
		return
	}

	// Fetch and parse PaymentInfo from PDF using extracted reference
	info, err := fetchPaymentInfo(strings.ToUpper(referenceNumber))
	if err != nil {
		res := comm.DepositeRes{
			Status:    "invalid-reference",
			Message:   "Reference number not found or invalid",
			Timestamp: time.Now().Unix(),
		}
		PublishDepositeRes(nc, res, ws.SocketId)
		return
	}

	// Validate receiver
	expectedReceiver := os.Getenv("DEPOSIT_CBE_ACCOUNT")
	if info.Receiver != expectedReceiver {
		res := comm.DepositeRes{
			Status:    "invalid-receiver",
			Message:   "Payment was not made to the correct account",
			Timestamp: time.Now().Unix(),
		}
		PublishDepositeRes(nc, res, ws.SocketId)
		return
	}

	// DB transaction: insert into balances table
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tx, err := pool.Begin(ctx)
	if err != nil {
		log.Errorf("begin tx: %v", err)
		res := comm.DepositeRes{
			Status:    "server-error",
			Message:   "Database connection failed. Please try again",
			Timestamp: time.Now().Unix(),
		}
		PublishDepositeRes(nc, res, ws.SocketId)
		return
	}
	defer tx.Rollback(ctx)

	// Idempotency: ensure tref not in balances (use extracted reference)
	var exists bool
	err = tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM balances WHERE tref = $1)`,
		referenceNumber,
	).Scan(&exists)
	if err != nil {
		log.Errorf("idempotency check error: %v", err)
		res := comm.DepositeRes{
			Status:    "server-error",
			Message:   "Database error occurred. Please try again",
			Timestamp: time.Now().Unix(),
		}
		PublishDepositeRes(nc, res, ws.SocketId)
		return
	}

	if exists {
		res := comm.DepositeRes{
			Status:    "duplicate",
			Message:   "This reference number has already been used",
			Timestamp: time.Now().Unix(),
		}
		PublishDepositeRes(nc, res, ws.SocketId)
		return
	}

	// Insert new balance record (use extracted reference)
	_, err = tx.Exec(ctx, `
		INSERT INTO balances (user_id, ttype, dr, cr, tref, status)
		VALUES ($1, 'deposit', $2, 0, $3, 'verified')
	`, req.UserID, info.TotalAmount, referenceNumber)
	if err != nil {
		log.Errorf("insert balance error: %v", err)
		res := comm.DepositeRes{
			Status:    "server-error",
			Message:   "Failed to process deposit. Please try again",
			Timestamp: time.Now().Unix(),
		}
		PublishDepositeRes(nc, res, ws.SocketId)
		return
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		log.Errorf("commit tx: %v", err)
		res := comm.DepositeRes{
			Status:    "server-error",
			Message:   "Failed to complete deposit. Please try again",
			Timestamp: time.Now().Unix(),
		}
		PublishDepositeRes(nc, res, ws.SocketId)
		return
	}

	// Send Telegram notification for successful deposit
	if telegramNotifier != nil {
		message := fmt.Sprintf(
			"💰 *DEPOSIT SUCCESS*\n\n"+
				"👤 *User ID:* %d\n"+
				"💵 *Amount:* %.2f ETB\n"+
				"🔗 *Reference:* %s\n"+
				"📅 *Date:* %s\n"+
				"⏰ *Time:* %s",
			req.UserID,
			info.TotalAmount,
			referenceNumber,
			info.PaymentDate,
			time.Now().Format("15:04:05"),
		)
		telegramNotifier.SendNotification(message)
	}

	// Success response
	res := comm.DepositeRes{
		Status:    "success",
		Message:   "Deposit processed successfully",
		Timestamp: time.Now().Unix(),
	}
	PublishDepositeRes(nc, res, ws.SocketId)
}

func handleWithdrawal(nc *natscli.Nats, pool *pgxpool.Pool, ws comm.WSMessage) {
	// Decode withdrawal request
	var req comm.WithdrawalRequest
	if err := json.Unmarshal(ws.Data, &req); err != nil {
		log.Errorf("invalid WithdrawalRequest: %v", err)
		publishWithdrawalRes(nc, comm.WithdrawalRes{Status: "error", Timestamp: time.Now().UnixMilli()}, ws.SocketId)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create withdrawal request
	withdrawalReq, err := createWithdrawalRequest(
		ctx, pool, req.UserID, req.Amount,
		req.AccountType, req.AccountNo, req.Name,
	)

	if err != nil {
		log.Errorf("create withdrawal request error: %v", err)
		status := "error"
		if strings.Contains(err.Error(), "insufficient balance") {
			status = "insufficient-balance"
		} else if strings.Contains(err.Error(), "pending withdrawal request") {
			status = "pending-request-exists"
		}
		publishWithdrawalRes(nc, comm.WithdrawalRes{Status: status, Timestamp: time.Now().UnixMilli()}, ws.SocketId)
		return
	}

	// Send Telegram notification for successful withdrawal
	if telegramNotifier != nil {
		message := fmt.Sprintf(
			"💸 *WITHDRAWAL REQUEST*\n\n"+
				"👤 *User ID:* %d\n"+
				"💵 *Amount:* %s ETB\n"+
				"🏦 *Account Type:* %s\n"+
				"📋 *Account No:* %s\n"+
				"👨‍💼 *Name:* %s\n"+
				"🆔 *Withdrawal ID:* %d\n"+
				"📊 *Balance Snapshot:* %s ETB\n"+
				"⏰ *Time:* %s",
			withdrawalReq.UserID,
			withdrawalReq.Amount.String(),
			withdrawalReq.AccountType,
			withdrawalReq.AccountNo,
			withdrawalReq.Name,
			withdrawalReq.ID,
			withdrawalReq.BalanceSnapshot.String(),
			time.Now().Format("2006-01-02 15:04:05"),
		)
		telegramNotifier.SendNotification(message)
	}

	// Build and publish response
	res := comm.WithdrawalRes{
		Status:       "success",
		WithdrawalID: withdrawalReq.ID,
		Amount:       withdrawalReq.Amount,
		AccountType:  withdrawalReq.AccountType,
		AccountNo:    withdrawalReq.AccountNo,
		Name:         withdrawalReq.Name,
		Timestamp:    time.Now().UnixMilli(),
	}
	publishWithdrawalRes(nc, res, ws.SocketId)
}

func handleTransfer(nc *natscli.Nats, pool *pgxpool.Pool, ws comm.WSMessage) {
	// Decode request
	var req comm.TransferRequest
	if err := json.Unmarshal(ws.Data, &req); err != nil {
		log.Errorf("invalid TransferRequest: %v", err)
		res := comm.TransferRes{
			Status:    "invalid-request",
			Message:   "Invalid request format",
			Timestamp: time.Now().Unix(),
		}
		PublishTransferRes(nc, res, ws.SocketId)
		return
	}

	// Convert string user IDs to int64
	fromUserID, err := strconv.ParseInt(req.FromUserID, 10, 64)
	if err != nil {
		log.Errorf("invalid FromUserID: %v", err)
		res := comm.TransferRes{
			Status:    "invalid-request",
			Message:   "Invalid sender user ID",
			Timestamp: time.Now().Unix(),
		}
		PublishTransferRes(nc, res, ws.SocketId)
		return
	}

	toUserID, err := strconv.ParseInt(req.ToUserID, 10, 64)
	if err != nil {
		log.Errorf("invalid ToUserID: %v", err)
		res := comm.TransferRes{
			Status:    "invalid-request",
			Message:   "Invalid recipient user ID",
			Timestamp: time.Now().Unix(),
		}
		PublishTransferRes(nc, res, ws.SocketId)
		return
	}

	// Validate request
	if fromUserID == 0 || toUserID == 0 || req.Amount <= 0 {
		res := comm.TransferRes{
			Status:    "invalid-request",
			Message:   "Missing required fields or invalid amount",
			Timestamp: time.Now().Unix(),
		}
		PublishTransferRes(nc, res, ws.SocketId)
		return
	}

	// Prevent self-transfer
	if fromUserID == toUserID {
		res := comm.TransferRes{
			Status:    "self-transfer",
			Message:   "Cannot transfer funds to yourself",
			Timestamp: time.Now().Unix(),
		}
		PublishTransferRes(nc, res, ws.SocketId)
		return
	}

	// DB transaction: transfer funds between users
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tx, err := pool.Begin(ctx)
	if err != nil {
		log.Errorf("begin tx: %v", err)
		res := comm.TransferRes{
			Status:    "server-error",
			Message:   "Database connection failed. Please try again",
			Timestamp: time.Now().Unix(),
		}
		PublishTransferRes(nc, res, ws.SocketId)
		return
	}
	defer tx.Rollback(ctx)

	// Check if recipient user exists
	var recipientExists bool
	err = tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM users WHERE user_id = $1)`,
		toUserID,
	).Scan(&recipientExists)
	if err != nil {
		log.Errorf("recipient check error: %v", err)
		res := comm.TransferRes{
			Status:    "server-error",
			Message:   "Database error occurred. Please try again",
			Timestamp: time.Now().Unix(),
		}
		PublishTransferRes(nc, res, ws.SocketId)
		return
	}

	if !recipientExists {
		res := comm.TransferRes{
			Status:    "user-not-found",
			Message:   "Recipient user not found",
			Timestamp: time.Now().Unix(),
		}
		PublishTransferRes(nc, res, ws.SocketId)
		return
	}

	// Calculate sender's current balance using same logic as withdrawal
	rows, err := tx.Query(ctx, `
		SELECT dr, cr
		FROM balances
		WHERE user_id = $1 AND status = 'verified'
		FOR UPDATE
	`, fromUserID)
	if err != nil {
		log.Errorf("lock balance records: %v", err)
		res := comm.TransferRes{
			Status:    "server-error",
			Message:   "Failed to check balance. Please try again",
			Timestamp: time.Now().Unix(),
		}
		PublishTransferRes(nc, res, ws.SocketId)
		return
	}
	defer rows.Close()

	var totalDr, totalCr decimal.Decimal
	for rows.Next() {
		var dr, cr decimal.Decimal
		if err := rows.Scan(&dr, &cr); err != nil {
			log.Errorf("scan balance row: %v", err)
			res := comm.TransferRes{
				Status:    "server-error",
				Message:   "Failed to process transfer. Please try again",
				Timestamp: time.Now().Unix(),
			}
			PublishTransferRes(nc, res, ws.SocketId)
			return
		}
		totalDr = totalDr.Add(dr)
		totalCr = totalCr.Add(cr)
	}

	senderBalance := totalDr.Sub(totalCr)
	transferAmount := decimal.NewFromFloat(req.Amount)

	// Check if sender has sufficient balance
	if senderBalance.LessThan(transferAmount) {
		res := comm.TransferRes{
			Status:    "insufficient-balance",
			Message:   "Insufficient balance for this transfer",
			Timestamp: time.Now().Unix(),
		}
		PublishTransferRes(nc, res, ws.SocketId)
		return
	}

	// Generate unique transaction reference and individual record references
	transactionID := uuid.New().String()
	baseRef := fmt.Sprintf("TXF-%s", transactionID[:8])
	senderRef := fmt.Sprintf("%s-OUT", baseRef)  // Unique reference for sender
	receiverRef := fmt.Sprintf("%s-IN", baseRef) // Unique reference for receiver

	// Insert CR (Credit/Debit from sender) record - sender loses money
	_, err = tx.Exec(ctx, `
		INSERT INTO balances (user_id, ttype, dr, cr, tref, status, created_at)
		VALUES ($1, 'transfer_out', 0, $2, $3, 'verified', NOW())
	`, fromUserID, transferAmount, senderRef)
	if err != nil {
		log.Errorf("insert sender record error: %v", err)
		res := comm.TransferRes{
			Status:    "server-error",
			Message:   "Failed to process transfer. Please try again",
			Timestamp: time.Now().Unix(),
		}
		PublishTransferRes(nc, res, ws.SocketId)
		return
	}

	// Insert DR (Debit/Credit to receiver) record - receiver gains money
	_, err = tx.Exec(ctx, `
		INSERT INTO balances (user_id, ttype, dr, cr, tref, status, created_at)
		VALUES ($1, 'transfer_in', $2, 0, $3, 'verified', NOW())
	`, toUserID, transferAmount, receiverRef)
	if err != nil {
		log.Errorf("insert receiver record error: %v", err)
		res := comm.TransferRes{
			Status:    "server-error",
			Message:   "Failed to process transfer. Please try again",
			Timestamp: time.Now().Unix(),
		}
		PublishTransferRes(nc, res, ws.SocketId)
		return
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		log.Errorf("commit tx: %v", err)
		res := comm.TransferRes{
			Status:    "server-error",
			Message:   "Failed to complete transfer. Please try again",
			Timestamp: time.Now().Unix(),
		}
		PublishTransferRes(nc, res, ws.SocketId)
		return
	}

	// Send Telegram notification for successful transfer
	if telegramNotifier != nil {
		message := fmt.Sprintf(
			"🔄 *TRANSFER SUCCESS*\n\n"+
				"📤 *From User:* %d\n"+
				"📥 *To User:* %d\n"+
				"💵 *Amount:* %.2f ETB\n"+
				"🆔 *Transaction ID:* %s\n"+
				"🔗 *Sender Ref:* %s\n"+
				"🔗 *Receiver Ref:* %s\n"+
				"⏰ *Time:* %s",
			fromUserID,
			toUserID,
			req.Amount,
			transactionID,
			senderRef,
			receiverRef,
			time.Now().Format("2006-01-02 15:04:05"),
		)
		telegramNotifier.SendNotification(message)
	}

	// Success response
	res := comm.TransferRes{
		Status:        "success",
		Message:       "Transfer completed successfully",
		TransactionID: transactionID,
		Amount:        req.Amount,
		Timestamp:     time.Now().Unix(),
	}
	PublishTransferRes(nc, res, ws.SocketId)
}

// Unified search method - searches both name and ID fields simultaneously
func handleUserSearch(nc *natscli.Nats, pool *pgxpool.Pool, ws comm.WSMessage) {
	// Decode request
	var req comm.UserSearchRequest
	if err := json.Unmarshal(ws.Data, &req); err != nil {
		log.Errorf("invalid UserSearchRequest: %v", err)
		res := comm.UserSearchRes{
			Status: "invalid-request",
		}
		PublishUserSearchRes(nc, res, ws.SocketId)
		return
	}

	// Validate request
	if req.Query == "" {
		res := comm.UserSearchRes{
			Status:    "invalid-request",
			Timestamp: time.Now().Unix(),
		}
		PublishUserSearchRes(nc, res, ws.SocketId)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	query := strings.TrimSpace(req.Query)
	log.Infof("Searching for user with query: %s", query)

	// Single query that searches both user_id and name fields
	var user comm.SearchUser
	err := pool.QueryRow(ctx, `
		SELECT user_id, name 
		FROM users 
		WHERE status = 'ACTIVE' 
		AND (
			-- Exact user_id match (if query is numeric)
			(user_id::text = $1)
			OR 
			-- Name search (case insensitive, partial match)
			(LOWER(name) LIKE LOWER($2))
		)
		ORDER BY 
			-- Prioritize exact user_id match
			CASE WHEN user_id::text = $1 THEN 1 ELSE 2 END,
			-- Then prioritize exact name match
			CASE WHEN LOWER(name) = LOWER($3) THEN 1 ELSE 2 END,
			-- Finally order by name
			name
		LIMIT 1
	`, query, "%"+query+"%", query).Scan(&user.UserID, &user.Name)

	if err != nil {
		if strings.Contains(err.Error(), "no rows in result set") {
			res := comm.UserSearchRes{
				Status:    "not-found",
				Timestamp: time.Now().Unix(),
			}
			PublishUserSearchRes(nc, res, ws.SocketId)
			return
		}

		log.Errorf("user search error: %v", err)
		res := comm.UserSearchRes{
			Status:    "server-error",
			Timestamp: time.Now().Unix(),
		}
		PublishUserSearchRes(nc, res, ws.SocketId)
		return
	}

	// Success response
	res := comm.UserSearchRes{
		Status:    "success",
		User:      &user,
		Timestamp: time.Now().Unix(),
	}
	PublishUserSearchRes(nc, res, ws.SocketId)
}

// PublishUserSearchRes publishes user search response to NATS
func PublishUserSearchRes(n *natscli.Nats, p comm.UserSearchRes, socketId string) {
	data, err := json.Marshal(p)
	if err != nil {
		log.Errorf("unable to marshal UserSearchRes: %v", err)
		return
	}

	msg := &comm.WSMessage{
		Type:     "user-search-res",
		Data:     data,
		SocketId: socketId,
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		log.Errorf("Error marshaling WSMessage: %s", err)
		return
	}

	n.Conn.Publish("game.service", payload)
}

func PublishDepositeRes(n *natscli.Nats, p comm.DepositeRes, socketId string) {
	data, err := json.Marshal(p)
	if err != nil {
		log.Errorf("unable to marsahl playerData")
	}

	msg := &comm.WSMessage{
		Type:     "deposite-res",
		Data:     data,
		SocketId: socketId,
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		log.Errorf("Error %s", err)
	}

	n.Conn.Publish("game.service", payload)
}

func publishWithdrawalRes(n *natscli.Nats, p comm.WithdrawalRes, socketId string) {
	data, err := json.Marshal(p)
	if err != nil {
		log.Errorf("unable to marshal withdrawal response: %v", err)
		return
	}

	msg := &comm.WSMessage{
		Type:     "withdrawal-res",
		Data:     data,
		SocketId: socketId,
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		log.Errorf("Error marshaling WSMessage: %v", err)
		return
	}

	n.Conn.Publish("game.service", payload)
}

// PublishTransferRes publishes transfer response to NATS
func PublishTransferRes(n *natscli.Nats, p comm.TransferRes, socketId string) {
	data, err := json.Marshal(p)
	if err != nil {
		log.Errorf("unable to marshal TransferRes: %v", err)
		return
	}

	msg := &comm.WSMessage{
		Type:     "transfer-res",
		Data:     data,
		SocketId: socketId,
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		log.Errorf("Error marshaling WSMessage: %s", err)
		return
	}

	n.Conn.Publish("game.service", payload)
}

func toJSON(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

func fetchPaymentInfo(ref string) (PaymentInfo, error) {
	//url := "https://apps.cbe.com.et:100/?id=" + reference

	//ref := "FT25146G8PWQ43543488" // this comes from the player

	url := "https://apps.cbe.com.et:100/?id=" + ref
	pdfBytes, err := fetchPDFBytes(url)
	if err != nil {
		log.Error("fetch PDF: %v", err)
		return PaymentInfo{}, err
	}

	payInfo, err := extractPaymentInfo(pdfBytes)
	if err != nil {
		log.Error("extract info: %v", err)
		return PaymentInfo{}, err
	}

	return *payInfo, nil
}

func fetchPDFBytes(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func extractPaymentInfo(pdfBytes []byte) (*PaymentInfo, error) {
	// feed directly into the parser
	reader := bytes.NewReader(pdfBytes)
	r, err := pdf.NewReader(reader, int64(len(pdfBytes)))
	if err != nil {
		return nil, err
	}

	numPages := r.NumPage()
	var text string
	// 3) only read page 1 (or loop till you find it)
	for i := 1; i <= numPages; i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}
		buf, err := page.GetPlainText(nil)
		if err != nil {
			return nil, err
		}
		// you could search buf for the labels and break early if all found
		text = buf
		break
	}

	receiver, total, paymentDate := parseFields(text)
	return &PaymentInfo{
		Receiver:    receiver,
		TotalAmount: total,
		PaymentDate: paymentDate,
	}, nil
}

// unchanged
func parseFields(text string) (receiver string, totalAmt float64, paymentDate string) {
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	for i, line := range lines {
		switch line {
		case "Receiver":
			if i+1 < len(lines) {
				receiver = lines[i+1]
			}
		case "Payment Date & Time":
			if i+1 < len(lines) {
				paymentDate = lines[i+1]
			}
		case "Total amount debited from customers account":
			if i+1 < len(lines) {
				match := numRe.FindString(lines[i+1])
				if match != "" {
					clean := strings.ReplaceAll(match, ",", "")
					if v, err := strconv.ParseFloat(clean, 64); err == nil {
						totalAmt = v
					}
				}
			}
		}
	}
	return
}

func createWithdrawalRequest(ctx context.Context, pool *pgxpool.Pool, userID int64, amount decimal.Decimal, accountType, accountNo, name string) (*WithdrawalRequest, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Lock balance records first, then calculate totals
	rows, err := tx.Query(ctx, `
		SELECT dr, cr
		FROM balances
		WHERE user_id = $1 AND status = 'verified'
		FOR UPDATE
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("lock balance records: %w", err)
	}
	defer rows.Close()

	var totalDr, totalCr decimal.Decimal
	for rows.Next() {
		var dr, cr decimal.Decimal
		if err := rows.Scan(&dr, &cr); err != nil {
			return nil, fmt.Errorf("scan balance row: %w", err)
		}
		totalDr = totalDr.Add(dr)
		totalCr = totalCr.Add(cr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	balance := totalDr.Sub(totalCr)

	// Check if user has sufficient balance
	if balance.LessThan(amount) {
		return nil, fmt.Errorf("insufficient balance: have %s, need %s", balance.String(), amount.String())
	}

	// Check if user has any pending withdrawal requests
	var pendingCount int
	err = tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM withdrawal_requests
		WHERE user_id = $1 AND status = 'pending'
	`, userID).Scan(&pendingCount)
	if err != nil {
		return nil, fmt.Errorf("check pending requests: %w", err)
	}
	if pendingCount > 0 {
		return nil, fmt.Errorf("user already has a pending withdrawal request")
	}

	// Insert withdrawal request
	var withdrawalID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO withdrawal_requests (
			user_id,
			amount,
			balance_snapshot,
			account_type,
			account_no,
			name,
			status,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, 'pending', now(), now())
		RETURNING id
	`, userID, amount, balance, accountType, accountNo, name).Scan(&withdrawalID)
	if err != nil {
		return nil, fmt.Errorf("insert withdrawal request: %w", err)
	}

	// Generate reference number for the withdrawal
	referenceNumber := fmt.Sprintf("WD%d", withdrawalID)

	// Insert new balance record for withdrawal (DR=0, CR=amount for withdrawal)
	_, err = tx.Exec(ctx, `
		INSERT INTO balances (user_id, ttype, dr, cr, tref, status)
		VALUES ($1, 'withdrawal', 0, $2, $3, 'verified')
	`, userID, amount, referenceNumber)
	if err != nil {
		return nil, fmt.Errorf("insert balance record: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return &WithdrawalRequest{
		ID:              withdrawalID,
		UserID:          userID,
		Amount:          amount,
		BalanceSnapshot: balance,
		AccountType:     accountType,
		AccountNo:       accountNo,
		Name:            name,
		Status:          "pending",
	}, nil
}
