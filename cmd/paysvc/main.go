package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	config "github.com/avvvet/bingo-services/configs"
	"github.com/avvvet/bingo-services/internal/comm"
	"github.com/avvvet/bingo-services/internal/gamesvc/db"
	natscli "github.com/avvvet/bingo-services/internal/nats"
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
)

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
	default:
		log.Warnf("unknown message type: %s", ws.Type)
	}
}

func handleDeposit(nc *natscli.Nats, pool *pgxpool.Pool, ws comm.WSMessage) {
	// Decode request
	var req comm.PaymentRequest
	if err := json.Unmarshal(ws.Data, &req); err != nil {
		log.Errorf("invalid PaymentRequest: %v", err)
		return
	}

	// Fetch and parse PaymentInfo from PDF
	info, err := fetchPaymentInfo(strings.ToUpper(req.Reference))
	if err != nil {
		res := comm.DepositeRes{
			Status:    "error",
			Timestamp: time.Now().Unix(),
		}
		PublishDepositeRes(nc, res, ws.SocketId)
		return
	}

	// Validate receiver
	const expectedReceiver = "SABA TSEGAYE BEKELE" //"AWET TSEGAZEAB GEBREAMLAK"
	if info.Receiver != expectedReceiver {

		res := comm.DepositeRes{
			Status:    "invalid-receiver",
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
			Status:    "error",
			Timestamp: time.Now().Unix(),
		}
		PublishDepositeRes(nc, res, ws.SocketId)
		return
	}
	defer tx.Rollback(ctx)

	// Idempotency: ensure tref not in balances
	var exists bool
	err = tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM balances WHERE tref = $1)`,
		req.Reference,
	).Scan(&exists)
	if err != nil {
		log.Errorf("idempotency check error: %v", err)

		res := comm.DepositeRes{
			Status:    "error",
			Timestamp: time.Now().Unix(),
		}
		PublishDepositeRes(nc, res, ws.SocketId)
		return
	}
	if exists {
		res := comm.DepositeRes{
			Status:    "duplicate",
			Timestamp: time.Now().Unix(),
		}
		PublishDepositeRes(nc, res, ws.SocketId)
		return
	}

	// Insert new balance record
	_, err = tx.Exec(ctx, `
		INSERT INTO balances (user_id, ttype, dr, cr, tref, status)
		VALUES ($1, 'deposit', $2, 0, $3, 'verified')
	`, req.UserID, info.TotalAmount, req.Reference)
	if err != nil {
		log.Errorf("insert balance error: %v", err)

		res := comm.DepositeRes{
			Status:    "error",
			Timestamp: time.Now().Unix(),
		}
		PublishDepositeRes(nc, res, ws.SocketId)
		return
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		log.Errorf("commit tx: %v", err)

		res := comm.DepositeRes{
			Status:    "error",
			Timestamp: time.Now().Unix(),
		}
		PublishDepositeRes(nc, res, ws.SocketId)
		return
	}

	// Build and publish win data
	res := comm.DepositeRes{
		Status:    "success",
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
	if err != nil {
		return nil, fmt.Errorf("get user balance: %w", err)
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
