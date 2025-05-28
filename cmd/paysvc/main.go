package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
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

	// Subscribe to incoming payment requests
	_, err = nc.Conn.Subscribe("payment.request", func(m *nats.Msg) {
		handlePayment(nc, dbpool, m)
	})
	if err != nil {
		log.Fatalf("Subscribe payment.request error: %v", err)
	}

	select {}
}

func handlePayment(nc *natscli.Nats, pool *pgxpool.Pool, msg *nats.Msg) {

	// Decode wrapper
	var ws comm.WSMessage
	if err := json.Unmarshal(msg.Data, &ws); err != nil {
		log.Errorf("invalid WSMessage: %v", err)
		return
	}
	if ws.Type != "deposite" {
		return
	}

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
			Status: "error",
		}
		PublishDepositeRes(nc, res, ws.SocketId)
		return
	}

	// Validate receiver
	const expectedReceiver = "AWET TSEGAZEAB GEBREAMLAK"
	if info.Receiver != expectedReceiver {

		res := comm.DepositeRes{
			Status: "invalid-receiver",
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
			Status: "error",
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
			Status: "error",
		}
		PublishDepositeRes(nc, res, ws.SocketId)
		return
	}
	if exists {
		res := comm.DepositeRes{
			Status: "duplicate",
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
			Status: "error",
		}
		PublishDepositeRes(nc, res, ws.SocketId)
		return
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		log.Errorf("commit tx: %v", err)

		res := comm.DepositeRes{
			Status: "error",
		}
		PublishDepositeRes(nc, res, ws.SocketId)
		return
	}

	// Build and publish win data
	res := comm.DepositeRes{
		Status: "success",
	}
	PublishDepositeRes(nc, res, ws.SocketId)
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
