package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	config "github.com/avvvet/bingo-services/configs"
	"github.com/avvvet/bingo-services/internal/comm"
	"github.com/avvvet/bingo-services/internal/gamesvc/db"
	natscli "github.com/avvvet/bingo-services/internal/nats"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	log "github.com/sirupsen/logrus"
)

const SERVICE_NAME = "claim"

var instanceId string

func init() {
	instanceId = config.CreateUniqueInstance(SERVICE_NAME)
	config.Logging(SERVICE_NAME + "_service_" + instanceId)
	config.LoadEnv(SERVICE_NAME)
}

// in-memory decks: gameID -> history of called numbers (concurrent safe)
var decks sync.Map // key: int, value: []int

func main() {
	// PostgreSQL connection
	// pg connection
	dbpool, err := db.Connect()
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}
	defer db.ClosePool()
	log.Printf("pg connection established successfully")

	// NATS connection
	n, err := natscli.Connect()
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer n.Conn.Close()
	log.Infof("NATS connected at %s", n.Url)

	// ----------------------------------------------------------------
	// Subscribe to outgoing Bingo calls (published by callersvc on "game.service")
	// This subscription listens for "bingo-call" messages to build in-memory decks
	_, err = n.Conn.Subscribe("game.service", func(m *nats.Msg) {
		var ws comm.WSMessage
		if err := json.Unmarshal(m.Data, &ws); err != nil {
			return
		}
		if ws.Type != "bingo-call" {
			return
		}
		var call comm.CallMessage
		if err := json.Unmarshal(ws.Data, &call); err != nil {
			return
		}
		// Store the latest history for this game ID
		decks.Store(call.Gid, call.History)
	})
	if err != nil {
		log.Fatalf("Subscribe game.service error: %v", err)
	}

	// ----------------------------------------------------------------
	// Subscribe to incoming player claims on "bingo-claim"
	// Clients publish their bingo-claim messages here, and we validate them
	_, err = n.Conn.Subscribe("bingo.claim", func(m *nats.Msg) {
		handleClaim(n, dbpool, m)
	})
	if err != nil {
		log.Fatalf("Subscribe bingo-claim error: %v", err)
	}

	select {} // run forever
}

// ClaimMessage payload for player claims
// Clients send this via WebSocket or NATS when they press "BINGO!"
type ClaimMessage struct {
	GameID int   `json:"gameId"`
	Gtype  int   `json:"gtype"`
	UserID int64 `json:"userId"`
	Marks  []int `json:"marks"`
}

// handleClaim validates a Bingo claim in a DB transaction and publishes result
func handleClaim(n *natscli.Nats, pool *pgxpool.Pool, msg *nats.Msg) {
	var ws comm.WSMessage
	if err := json.Unmarshal(msg.Data, &ws); err != nil {
		log.Errorf("invalid WSMessage: %v", err)
		return
	}
	if ws.Type != "claim-bingo" {
		return
	}
	var claim ClaimMessage
	if err := json.Unmarshal(ws.Data, &claim); err != nil {
		log.Errorf("invalid ClaimMessage: %v", err)
		return
	}

	ctx := context.Background()

	// Begin transaction to ensure only one winner
	tx, err := pool.Begin(ctx)
	if err != nil {
		log.Errorf("begin tx: %v", err)
		return
	}
	defer tx.Rollback(ctx)

	// Lock game row and verify status
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM games WHERE id=$1 FOR UPDATE`, claim.GameID).Scan(&status); err != nil {
		log.Errorf("lock game row: %v", err)
		return
	}
	if status != "started" {
		log.Infof("game %d not in 'started' state", claim.GameID)
		return
	}

	// Fetch player card
	card, err := GetPlayerCard(ctx, pool, claim.GameID, claim.UserID)
	if err != nil {
		log.Errorf("GetPlayerCard error: %v", err)
		return
	}

	// Retrieve deck history from in-memory map
	hv, ok := decks.Load(claim.GameID)
	if !ok {
		log.Errorf("no call history for game %d", claim.GameID)
		return
	}
	history, ok := hv.([]int)
	if !ok {
		log.Errorf("invalid history type for game %d", claim.GameID)
		return
	}

	// Validate Bingo pattern

	if !ValidateBingo(card, history, claim.Marks) {
		rej := comm.WSMessage{Type: "bingo-claim-rejected", Data: ws.Data, SocketId: ws.SocketId}
		payload, _ := json.Marshal(rej)
		n.Conn.Publish("bingo-replies", payload)
		return
	}

	// Mark game as ended and record winner
	res, err := tx.Exec(ctx, `
		UPDATE games
		SET status='ended', user_id=$1, updated_at=now()
		WHERE id=$2 AND status='started'
	`, claim.UserID, claim.GameID)
	if err != nil {
		log.Errorf("update game error: %v", err)
		return
	}
	// Check how many rows were affected
	ra := res.RowsAffected()
	if ra != 1 {
		// No row updated means another claim won first or game not in started state
		log.Infof("game %d already concluded (rows affected: %d)", claim.GameID, ra)
		return
	}

	// Commit the transaction
	if err := tx.Commit(ctx); err != nil {
		log.Errorf("commit tx: %v", err)
		return
	}

	// Broadcast game-ended to all subscribers to all subscribers
	winData := comm.WinData{
		PlayerId: claim.UserID,
		Gid:      claim.GameID,
		Gtype:    claim.Gtype,
	}

	PublishWin(n, winData, ws.SocketId)
}

func PublishWin(n *natscli.Nats, p comm.WinData, socketId string) {
	data, err := json.Marshal(p)
	if err != nil {
		log.Errorf("unable to marsahl playerData")
	}

	msg := &comm.WSMessage{
		Type:     "game-finished",
		Data:     data,
		SocketId: socketId,
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		log.Errorf("Error %s", err)
	}

	n.Conn.Publish("game.service", payload)
}

// GetPlayerCard retrieves a 25-number card for user via card_sn lookup
// GetPlayerCard retrieves a 25-number bingo card for a user by parsing
// the comma-separated `data` column in `cards`.
func GetPlayerCard(ctx context.Context, pool *pgxpool.Pool, gameID int, userID int64) ([]int, error) {
	// 1) Lookup the assigned card_sn
	var cardSN string
	if err := pool.QueryRow(ctx,
		`SELECT card_sn
           FROM game_players
          WHERE game_id=$1
            AND user_id=$2
          LIMIT 1`,
		gameID, userID,
	).Scan(&cardSN); err != nil {
		return nil, fmt.Errorf("GetPlayerCard: could not find card_sn: %w", err)
	}

	// 2) Read the CSV-style `data` field
	var dataStr string
	if err := pool.QueryRow(ctx,
		`SELECT data FROM cards WHERE card_sn=$1`,
		cardSN,
	).Scan(&dataStr); err != nil {
		return nil, fmt.Errorf("GetPlayerCard: could not fetch card data: %w", err)
	}

	// 3) Split on commas, trim spaces, parse ints
	parts := strings.Split(dataStr, ",")
	nums := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("GetPlayerCard: invalid number %q: %w", p, err)
		}
		nums = append(nums, n)
	}

	// 4) Sanity check
	if len(nums) != 25 {
		return nil, fmt.Errorf("GetPlayerCard: expected 25 numbers, got %d", len(nums))
	}
	return nums, nil
}

// ValidateBingo checks for a win (row, column or diagonal) on a 5×5 bingo card.
// - card:     25 ints, row-major
// - history:  slice of all numbers that have been called
// - marks:    25 ints (0 = not selected, >0 = the number the player marked; index 12 is the free square)
func ValidateBingo(card []int, history, marks []int) bool {
	// sanity: need exactly 25 of each
	if len(card) != 25 || len(marks) != 25 {
		return false
	}

	// build a lookup of called numbers
	called := make(map[int]bool, len(history))
	for _, c := range history {
		called[c] = true
	}

	// build a 5×5 boolean grid of what’s “covered”
	var grid [5][5]bool
	for i, _ := range card {
		r, c := i/5, i%5

		// free center always covered
		if i == 12 {
			grid[r][c] = true
			continue
		}

		// only cover if player marked it AND it’s been called
		if marks[i] != 0 && called[marks[i]] {
			grid[r][c] = true
		}
	}

	// check rows & columns
	for i := 0; i < 5; i++ {
		rowComplete, colComplete := true, true
		for j := 0; j < 5; j++ {
			if !grid[i][j] {
				rowComplete = false
			}
			if !grid[j][i] {
				colComplete = false
			}
		}
		if rowComplete || colComplete {
			return true
		}
	}

	// check both diagonals
	diag1, diag2 := true, true
	for i := 0; i < 5; i++ {
		if !grid[i][i] {
			diag1 = false
		}
		if !grid[i][4-i] {
			diag2 = false
		}
	}
	return diag1 || diag2
}
