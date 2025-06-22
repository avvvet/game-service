// cmd/robotsvc/main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	config "github.com/avvvet/bingo-services/configs"
	"github.com/avvvet/bingo-services/internal/comm"
	"github.com/avvvet/bingo-services/internal/gamesvc/db"
	natscli "github.com/avvvet/bingo-services/internal/nats"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	log "github.com/sirupsen/logrus"
)

const SERVICE_NAME = "robot"

var instanceId string

func init() {
	instanceId = "001"
	config.Logging(SERVICE_NAME + "_service_" + instanceId)
	config.LoadEnv(SERVICE_NAME)
	rand.Seed(time.Now().UnixNano())
}

// Robot user IDs - sequential starting from 9000000001
var robotUserIDs = []int64{
	9000000001, 9000000002, 9000000003, 9000000004, 9000000005,
	9000000006, 9000000007, 9000000008, 9000000009, 9000000010,
	9000000011, 9000000012, 9000000013, 9000000014, 9000000015,
}

// Robot names - mix of first names only and first+last names
var robotNames = []string{
	"Abelo", "meron bekele", "dawit", "mulugeta", "ted",
	"yonas", "liya", "Bereket Alemu", "Eden", "Samuel Yimer",
	"rahel", "Daniel Negash", "Bethel", "Kidus Wolde", "Natan",
}

// Game types to monitor (1-6)
var gameTypes = []int{1, 2, 3, 4, 5, 6}

// Game represents a game from the database
type Game struct {
	ID         int    `json:"id"`
	GameTypeID int    `json:"game_type_id"`
	Status     string `json:"status"`
}

// GamePlayer represents a player in a game
type GamePlayer struct {
	UserID int64  `json:"user_id"`
	CardSN string `json:"card_sn"`
}

// RobotGameState tracks robot's state in active games
type RobotGameState struct {
	GameID      int    `json:"game_id"`
	UserID      int64  `json:"user_id"`
	CardSN      string `json:"card_sn"`
	CardNumbers []int  `json:"card_numbers"` // 25 numbers from database card
	MarkedCells []bool `json:"marked_cells"` // 25 boolean flags for marked numbers
	HasBingo    bool   `json:"has_bingo"`
	Gtype       int    `json:"gtype"` // Game type for claims
}

// Global robot game states
var robotGameStates = make(map[string]*RobotGameState) // key: "gameID_userID"

func main() {
	log.Printf("Starting Robot Service...")

	// pg connection (same pattern as payment service)
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

	ctx := context.Background()

	// 2. Ensure robot accounts exist
	if err := ensureRobotAccounts(ctx, dbpool); err != nil {
		log.Fatalf("Failed to ensure robot accounts: %v", err)
	}
	log.Printf("Robot accounts verified/created successfully")

	// 3. Top-up robot wallets (only if no previous entries)
	if err := topUpRobotWallets(ctx, dbpool); err != nil {
		log.Errorf("Failed to top-up robot wallets: %v", err)
	} else {
		log.Printf("Robot wallets top-up completed")
	}

	log.Printf("Robot Service Phase 1 setup completed successfully!")

	// 4. Start game monitoring loop (Phase 2)
	log.Printf("Starting game monitoring loop...")
	go startGameMonitoring(ctx, dbpool, nc)

	// 5. Subscribe to game.service for bingo calls and other events
	log.Printf("Subscribing to game.service...")
	_, err = nc.Conn.Subscribe("game.service", func(m *nats.Msg) {
		handleGameServiceMessage(dbpool, nc, m)
	})
	if err != nil {
		log.Fatalf("Failed to subscribe to game.service: %v", err)
	}

	log.Printf("Robot Service fully operational!")

	// Keep service running
	select {}
}

// handleGameServiceMessage processes all messages from game.service topic
func handleGameServiceMessage(dbpool *pgxpool.Pool, nc *natscli.Nats, msg *nats.Msg) {
	var ws comm.WSMessage
	if err := json.Unmarshal(msg.Data, &ws); err != nil {
		log.Errorf("Failed to unmarshal WSMessage: %v", err)
		return
	}

	switch ws.Type {
	case "bingo-call":
		handleBingoCall(dbpool, nc, ws)
	case "game-started":
		handleGameStarted(dbpool, ws)
	case "game-finished":
		handleGameFinished(ws)
	}
}

// handleBingoCall processes incoming bingo number calls
func handleBingoCall(dbpool *pgxpool.Pool, nc *natscli.Nats, ws comm.WSMessage) {
	var call comm.CallMessage
	if err := json.Unmarshal(ws.Data, &call); err != nil {
		log.Errorf("Failed to unmarshal CallMessage: %v", err)
		return
	}

	log.Printf("Received bingo call: #%d for game %d (history: %d numbers)",
		call.Number, call.Gid, len(call.History))

	// Process call for all robots in this game
	processedCount := 0
	for _, robotState := range robotGameStates {
		if robotState.GameID == call.Gid && !robotState.HasBingo {
			if processRobotBingoCall(robotState, call, nc) {
				processedCount++
			}
		}
	}

	if processedCount > 0 {
		log.Printf("Processed bingo call #%d for %d robots in game %d",
			call.Number, processedCount, call.Gid)
	}
}

// handleGameStarted loads robot card data when games start
func handleGameStarted(dbpool *pgxpool.Pool, ws comm.WSMessage) {
	var gt comm.GameType
	if err := json.Unmarshal(ws.Data, &gt); err != nil {
		log.Errorf("Failed to unmarshal GameType: %v", err)
		return
	}

	log.Printf("Game %d started - loading robot cards", gt.Gid)
	ctx := context.Background()

	// Load card data for all robots in this game
	for _, robotState := range robotGameStates {
		if robotState.GameID == gt.Gid {
			cardNumbers, err := getPlayerCard(ctx, dbpool, gt.Gid, robotState.UserID)
			if err != nil {
				log.Errorf("Failed to load card for robot %d in game %d: %v",
					robotState.UserID, gt.Gid, err)
				continue
			}

			robotState.CardNumbers = cardNumbers
			robotState.Gtype = gt.Gtype
			robotState.MarkedCells = make([]bool, 25)
			// Mark center as free space (index 12)
			robotState.MarkedCells[12] = true

			log.Printf("Loaded card data for robot %d in game %d: %v",
				robotState.UserID, gt.Gid, cardNumbers)
		}
	}
}

// handleGameFinished cleans up robot states when games end
func handleGameFinished(ws comm.WSMessage) {
	var winData comm.WinData
	if err := json.Unmarshal(ws.Data, &winData); err != nil {
		log.Errorf("Failed to unmarshal WinData: %v", err)
		return
	}

	log.Printf("Game %d finished - winner: %d, cleaning up robot states",
		winData.Gid, winData.PlayerId)

	// Clean up robot states for this game
	for stateKey, robotState := range robotGameStates {
		if robotState.GameID == winData.Gid {
			delete(robotGameStates, stateKey)
			log.Printf("Cleaned up state for robot %d in game %d",
				robotState.UserID, winData.Gid)
		}
	}
}

// processRobotBingoCall processes a bingo call for a specific robot
func processRobotBingoCall(robotState *RobotGameState, call comm.CallMessage, nc *natscli.Nats) bool {
	// Skip if robot doesn't have card data loaded yet
	if len(robotState.CardNumbers) != 25 {
		log.Printf("⏳ Robot %d card not loaded yet for game %d",
			robotState.UserID, robotState.GameID)
		return false
	}

	// Find if the called number is on robot's card
	position := findNumberOnCard(robotState.CardNumbers, call.Number)
	if position >= 0 && !robotState.MarkedCells[position] {
		// Mark the number
		robotState.MarkedCells[position] = true

		row, col := position/5, position%5
		log.Printf("🔹 Robot %d marked #%d at position [%d,%d] in game %d",
			robotState.UserID, call.Number, row, col, robotState.GameID)

		// Check for bingo after each mark
		if hasBingo(robotState.MarkedCells) {
			robotState.HasBingo = true
			log.Printf("🎉 Robot %d achieved BINGO in game %d!",
				robotState.UserID, robotState.GameID)

			// Add random delay before claiming (1-3 seconds for realism)
			delay := time.Duration(1+rand.Intn(3)) * time.Second
			go func() {
				time.Sleep(delay)
				publishBingoClaim(nc, robotState)
			}()

			return true
		}
		return true
	} else if position >= 0 && robotState.MarkedCells[position] {
		// Number already marked
		log.Printf("⚪ Robot %d already marked #%d in game %d",
			robotState.UserID, call.Number, robotState.GameID)
		return true
	} else {
		// Number not on card
		log.Printf("❌ Robot %d does not have #%d on card in game %d",
			robotState.UserID, call.Number, robotState.GameID)
		return true
	}
}

// publishBingoClaim publishes a bingo claim using the claim service format
func publishBingoClaim(nc *natscli.Nats, robotState *RobotGameState) {
	// Create marks array with actual numbers from card where marked
	marks := make([]int, 25)
	for i := 0; i < 25; i++ {
		if robotState.MarkedCells[i] {
			marks[i] = robotState.CardNumbers[i]
		} else {
			marks[i] = 0 // Not marked
		}
	}

	claimMsg := struct {
		GameID int   `json:"gameId"`
		Gtype  int   `json:"gtype"`
		UserID int64 `json:"userId"`
		Marks  []int `json:"marks"`
	}{
		GameID: robotState.GameID,
		Gtype:  robotState.Gtype,
		UserID: robotState.UserID,
		Marks:  marks,
	}

	claimData, err := json.Marshal(claimMsg)
	if err != nil {
		log.Errorf("Failed to marshal robot bingo claim: %v", err)
		return
	}

	wsMsg := comm.WSMessage{
		Type:     "claim-bingo",
		Data:     claimData,
		SocketId: "", // Robot claims don't need socket ID
	}

	payload, err := json.Marshal(wsMsg)
	if err != nil {
		log.Errorf("Failed to marshal WSMessage for robot claim: %v", err)
		return
	}

	// Publish to bingo.claim topic (same as human players)
	err = nc.Conn.Publish("bingo.claim", payload)
	if err != nil {
		log.Errorf("Failed to publish robot bingo claim: %v", err)
		return
	}

	log.Printf("📢 Robot %d published BINGO claim for game %d (marked %d numbers)",
		robotState.UserID, robotState.GameID, countMarkedCells(robotState.MarkedCells))

	// Log the winning pattern for debugging
	logWinningCard(robotState)
}

// getPlayerCard retrieves card data from database (same logic as claim service)
func getPlayerCard(ctx context.Context, pool *pgxpool.Pool, gameID int, userID int64) ([]int, error) {
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
		return nil, fmt.Errorf("getPlayerCard: could not find card_sn: %w", err)
	}

	// 2) Read the CSV-style `data` field
	var dataStr string
	if err := pool.QueryRow(ctx,
		`SELECT data FROM cards WHERE card_sn=$1`,
		cardSN,
	).Scan(&dataStr); err != nil {
		return nil, fmt.Errorf("getPlayerCard: could not fetch card data: %w", err)
	}

	// 3) Split on commas, trim spaces, parse ints
	parts := strings.Split(dataStr, ",")
	nums := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("getPlayerCard: invalid number %q: %w", p, err)
		}
		nums = append(nums, n)
	}

	// 4) Sanity check
	if len(nums) != 25 {
		return nil, fmt.Errorf("getPlayerCard: expected 25 numbers, got %d", len(nums))
	}
	return nums, nil
}

// findNumberOnCard finds the position (0-24) of a number on the card
func findNumberOnCard(cardNumbers []int, number int) int {
	for i, cardNum := range cardNumbers {
		if cardNum == number {
			return i
		}
	}
	return -1 // Not found
}

// hasBingo checks if the marked cells form a winning pattern (same logic as claim service)
func hasBingo(marks []bool) bool {
	if len(marks) != 25 {
		return false
	}

	// Convert to 5x5 grid for easier checking
	var grid [5][5]bool
	for i := 0; i < 25; i++ {
		r, c := i/5, i%5
		grid[r][c] = marks[i]
	}

	// Check rows & columns
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

	// Check both diagonals
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

// countMarkedCells counts total marked cells
func countMarkedCells(marks []bool) int {
	count := 0
	for _, marked := range marks {
		if marked {
			count++
		}
	}
	return count
}

// logWinningCard logs the complete winning card state
func logWinningCard(robotState *RobotGameState) {
	log.Printf("🏆 WINNING CARD for Robot %d:", robotState.UserID)

	// Log the complete card with marked indicators
	for row := 0; row < 5; row++ {
		var rowStr string
		for col := 0; col < 5; col++ {
			pos := row*5 + col
			num := robotState.CardNumbers[pos]
			if robotState.MarkedCells[pos] {
				rowStr += fmt.Sprintf("[%2d] ", num)
			} else {
				rowStr += fmt.Sprintf(" %2d  ", num)
			}
		}
		log.Printf("🏆 Row %d: %s", row, rowStr)
	}
}

// ensureRobotAccounts creates robot accounts if they don't exist
func ensureRobotAccounts(ctx context.Context, dbpool *pgxpool.Pool) error {
	// Start transaction for atomic operation
	tx, err := dbpool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	for i, userID := range robotUserIDs {
		// Check if robot account exists
		var count int
		err := tx.QueryRow(ctx, `
			SELECT COUNT(*) 
			FROM users 
			WHERE user_id = $1
		`, userID).Scan(&count)

		if err != nil {
			return fmt.Errorf("failed to check if user %d exists: %w", userID, err)
		}

		if count == 0 {
			// Create robot account
			var createdUserID int64
			err := tx.QueryRow(ctx, `
				INSERT INTO users (user_id, name, email, phone, avatar, status)
				VALUES ($1, $2, $3, $4, $5, $6)
				RETURNING user_id
			`, userID, robotNames[i], fmt.Sprintf("%d@upupgo.online", userID), "",
				"", "ACTIVE").Scan(&createdUserID) //fmt.Sprintf("https://api.dicebear.com/7.x/shapes/svg?seed=robot%d", i+1)

			if err != nil {
				return fmt.Errorf("failed to create robot account %d: %w", userID, err)
			}

			log.Printf("Created robot account: ID=%d, Name='%s'", createdUserID, robotNames[i])
		} else {
			log.Printf("Robot account already exists: ID=%d, Name='%s'", userID, robotNames[i])
		}
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// topUpRobotWallets adds initial balance to robot accounts that have no previous entries
func topUpRobotWallets(ctx context.Context, dbpool *pgxpool.Pool) error {
	// Start transaction for atomic operation
	tx, err := dbpool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, robotUserID := range robotUserIDs {
		// Check if robot has any previous balance entries
		var count int
		err := tx.QueryRow(ctx, `
			SELECT COUNT(*) 
			FROM balances 
			WHERE user_id = $1
		`, robotUserID).Scan(&count)

		if err != nil {
			log.Printf("Failed to check robot balance history for %d: %v", robotUserID, err)
			continue
		}

		// If no previous entries, create initial deposit
		if count == 0 {
			tref := fmt.Sprintf("ROBOT-INIT-%d", robotUserID)

			_, err = tx.Exec(ctx, `
				INSERT INTO balances (user_id, ttype, dr, cr, tref, status)
				VALUES ($1, 'deposit', 2000, 0, $2, 'verified')
			`, robotUserID, tref)

			if err != nil {
				log.Printf("Failed to create robot initial deposit for %d: %v", robotUserID, err)
				continue
			}

			log.Printf("Created initial deposit of 2000 for robot %d", robotUserID)
		} else {
			log.Printf("Robot %d already has balance entries, skipping top-up", robotUserID)
		}
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// isRobotUserID checks if a given user ID belongs to a robot
func isRobotUserID(userID int64) bool {
	for _, robotID := range robotUserIDs {
		if robotID == userID {
			return true
		}
	}
	return false
}

// startGameMonitoring monitors games every 5 seconds and processes waiting games
func startGameMonitoring(ctx context.Context, dbpool *pgxpool.Pool, nc *natscli.Nats) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	log.Printf("Game monitoring started - checking every 5 seconds")

	for range ticker.C {
		// Check each game type 1-6 for waiting games
		for _, gameType := range gameTypes {
			game, err := getWaitingGame(ctx, dbpool, gameType)
			if err != nil {
				log.Errorf("Error getting waiting game for type %d: %v", gameType, err)
				continue
			}

			if game != nil {
				log.Printf("Found waiting game: ID=%d, Type=%d", game.ID, game.GameTypeID)
				if err := processWaitingGame(ctx, dbpool, nc, game); err != nil {
					log.Errorf("Error processing waiting game %d: %v", game.ID, err)
				}
			}
		}
	}
}

// getWaitingGame finds a waiting game for the specified game type
func getWaitingGame(ctx context.Context, dbpool *pgxpool.Pool, gameType int) (*Game, error) {
	var game Game
	err := dbpool.QueryRow(ctx, `
		SELECT id, game_type_id, status
		FROM games
		WHERE game_type_id = $1 AND status = 'waiting'
		ORDER BY created_at ASC
		LIMIT 1
	`, gameType).Scan(&game.ID, &game.GameTypeID, &game.Status)

	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil // No waiting game found
		}
		return nil, fmt.Errorf("query waiting game: %w", err)
	}

	return &game, nil
}

// processWaitingGame analyzes a waiting game and determines robot strategy
func processWaitingGame(ctx context.Context, dbpool *pgxpool.Pool, nc *natscli.Nats, game *Game) error {
	// Get all players in this game
	players, err := getGamePlayers(ctx, dbpool, game.ID)
	if err != nil {
		return fmt.Errorf("get game players: %w", err)
	}

	// Analyze players and taken cards
	realPlayers := 0
	robotPlayers := 0
	takenCards := make([]string, 0, len(players))

	for _, player := range players {
		takenCards = append(takenCards, player.CardSN)
		if isRobotUserID(player.UserID) {
			robotPlayers++
		} else {
			realPlayers++
		}
	}

	totalPlayers := realPlayers + robotPlayers

	log.Printf("Game %d analysis: Real=%d, Robots=%d, Total=%d, Cards=%v",
		game.ID, realPlayers, robotPlayers, totalPlayers, takenCards)

	// Determine strategy
	if realPlayers == 0 && robotPlayers == 0 {
		// Empty game - add all 15 robots at once
		numRobots := 15
		log.Printf("Empty game %d - adding all %d robots at once", game.ID, numRobots)
		return addRobotsWithDelay(ctx, dbpool, nc, game.ID, numRobots, takenCards)

	} else if realPlayers > 0 {
		// Real players present - fill to 15+ total
		if totalPlayers < 15 {
			robotsToAdd := 15 - totalPlayers
			log.Printf("Game %d has real players - filling with %d robots (total will be %d)",
				game.ID, robotsToAdd, 15)
			return addRobotsWithDelay(ctx, dbpool, nc, game.ID, robotsToAdd, takenCards)
		}

	} else {
		// Only robots - wait for real players
		log.Printf("Game %d has only robots - waiting for real players", game.ID)
	}

	return nil
}

// getGamePlayers retrieves all players for a specific game
func getGamePlayers(ctx context.Context, dbpool *pgxpool.Pool, gameID int) ([]GamePlayer, error) {
	rows, err := dbpool.Query(ctx, `
		SELECT user_id, card_sn
		FROM game_players
		WHERE game_id = $1
	`, gameID)
	if err != nil {
		return nil, fmt.Errorf("query game players: %w", err)
	}
	defer rows.Close()

	var players []GamePlayer
	for rows.Next() {
		var player GamePlayer
		if err := rows.Scan(&player.UserID, &player.CardSN); err != nil {
			return nil, fmt.Errorf("scan player row: %w", err)
		}
		players = append(players, player)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return players, nil
}

// addRobotsWithDelay adds robots to a game with realistic timing delays
func addRobotsWithDelay(ctx context.Context, dbpool *pgxpool.Pool, nc *natscli.Nats, gameID, numRobots int, takenCards []string) error {
	log.Printf("Starting to add %d robots to game %d", numRobots, gameID)

	successCount := 0
	maxAttempts := numRobots * 2 // Allow extra attempts in case of failures

	for attempt := 0; attempt < maxAttempts && successCount < numRobots; attempt++ {
		// Re-fetch taken cards before each robot addition (to avoid conflicts)
		currentTakenCards, err := getCurrentTakenCards(ctx, dbpool, gameID)
		if err != nil {
			log.Errorf("Failed to get current taken cards for game %d: %v", gameID, err)
			continue
		}

		// Check available robots
		availableRobots := getAvailableRobots(ctx, dbpool, gameID)
		if len(availableRobots) == 0 {
			log.Warnf("No more available robots for game %d after %d successful additions", gameID, successCount)
			break
		}

		// Check available cards
		availableCards := getAvailableCards(currentTakenCards)
		if len(availableCards) == 0 {
			log.Warnf("No more available cards for game %d after %d successful additions", gameID, successCount)
			break
		}

		// Add single robot to game
		if err := addRandomRobotToGame(ctx, dbpool, nc, gameID, currentTakenCards); err != nil {
			log.Errorf("Attempt %d failed to add robot to game %d: %v", attempt+1, gameID, err)
			continue
		}

		successCount++
		log.Printf("Successfully added robot %d/%d to game %d", successCount, numRobots, gameID)

		// Add realistic delay between robot additions (1-2 seconds)
		// Skip delay for last robot or if we've reached target
		if successCount < numRobots {
			delay := time.Duration(1+rand.Intn(2)) * time.Second
			log.Printf("Waiting %v before adding next robot to game %d", delay, gameID)
			time.Sleep(delay)
		}
	}

	log.Printf("Completed adding %d/%d robots to game %d (attempts: %d)", successCount, numRobots, gameID, maxAttempts)

	// Verify final count
	finalCount, err := countRobotsInGame(ctx, dbpool, gameID)
	if err != nil {
		log.Errorf("Failed to verify robot count for game %d: %v", gameID, err)
	} else {
		log.Printf("✅ Verification: Game %d now has %d robots (target was %d)", gameID, finalCount, numRobots)
		if finalCount < numRobots {
			log.Warnf("⚠️  Game %d only has %d robots instead of target %d", gameID, finalCount, numRobots)
		}
	}

	return nil
}

// getCurrentTakenCards gets the current list of taken cards for a game
func getCurrentTakenCards(ctx context.Context, dbpool *pgxpool.Pool, gameID int) ([]string, error) {
	rows, err := dbpool.Query(ctx, `
		SELECT card_sn
		FROM game_players
		WHERE game_id = $1
	`, gameID)
	if err != nil {
		return nil, fmt.Errorf("query taken cards: %w", err)
	}
	defer rows.Close()

	var takenCards []string
	for rows.Next() {
		var cardSN string
		if err := rows.Scan(&cardSN); err != nil {
			return nil, fmt.Errorf("scan card: %w", err)
		}
		takenCards = append(takenCards, cardSN)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return takenCards, nil
}

// addRandomRobotToGame adds a single robot with random available card to a game
func addRandomRobotToGame(ctx context.Context, dbpool *pgxpool.Pool, nc *natscli.Nats, gameID int, takenCards []string) error {
	// Select random available robot
	availableRobots := getAvailableRobots(ctx, dbpool, gameID)
	if len(availableRobots) == 0 {
		return fmt.Errorf("no available robots for game %d (all 15 robots already in game)", gameID)
	}

	robotUserID := availableRobots[rand.Intn(len(availableRobots))]

	// Select random available card
	cardSN := selectRandomAvailableCard(takenCards)
	if cardSN == "" {
		return fmt.Errorf("no available cards for game %d (all 100 cards taken)", gameID)
	}

	// Insert robot into game_players table
	_, err := dbpool.Exec(ctx, `
		INSERT INTO game_players (game_id, user_id, card_sn, status)
		VALUES ($1, $2, $3, 'pending')
	`, gameID, robotUserID, cardSN)

	if err != nil {
		// Handle unique constraint violations (card already taken)
		if strings.Contains(err.Error(), "duplicate key value violates unique constraint") {
			return fmt.Errorf("card %s was taken by another player in game %d", cardSN, gameID)
		}
		return fmt.Errorf("insert robot into game: %w", err)
	}

	// Initialize robot game state for gameplay tracking
	stateKey := fmt.Sprintf("%d_%d", gameID, robotUserID)
	robotGameStates[stateKey] = &RobotGameState{
		GameID:      gameID,
		UserID:      robotUserID,
		CardSN:      cardSN,
		CardNumbers: nil, // Will be loaded when game starts
		MarkedCells: nil, // Will be initialized when game starts
		HasBingo:    false,
		Gtype:       0, // Will be set when game starts
	}

	log.Printf("✅ Successfully added robot %d to game %d with card %s", robotUserID, gameID, cardSN)

	return nil
}

// getAvailableRobots returns robots not already in the specified game
func getAvailableRobots(ctx context.Context, dbpool *pgxpool.Pool, gameID int) []int64 {
	// Get robots already in this game
	rows, err := dbpool.Query(ctx, `
		SELECT user_id
		FROM game_players
		WHERE game_id = $1 AND user_id >= 9000000001 AND user_id <= 9000000015
	`, gameID)
	if err != nil {
		log.Errorf("Failed to get robots in game %d: %v", gameID, err)
		return robotUserIDs // Return all robots if query fails
	}
	defer rows.Close()

	// Collect robots already in game
	robotsInGame := make(map[int64]bool)
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			continue
		}
		robotsInGame[userID] = true
	}

	// Return robots not in game
	var availableRobots []int64
	for _, robotID := range robotUserIDs {
		if !robotsInGame[robotID] {
			availableRobots = append(availableRobots, robotID)
		}
	}

	return availableRobots
}

// getAvailableCards returns a list of available card serial numbers
func getAvailableCards(takenCards []string) []string {
	availableCards := []string{}

	// Check each card 1-100
	for i := 1; i <= 100; i++ {
		cardSN := fmt.Sprintf("%d", i)

		// Check if card is not taken
		taken := false
		for _, takenCard := range takenCards {
			if takenCard == cardSN {
				taken = true
				break
			}
		}

		if !taken {
			availableCards = append(availableCards, cardSN)
		}
	}

	return availableCards
}

// countRobotsInGame counts how many robots are currently in a game
func countRobotsInGame(ctx context.Context, dbpool *pgxpool.Pool, gameID int) (int, error) {
	var count int
	err := dbpool.QueryRow(ctx, `
		SELECT COUNT(*) 
		FROM game_players 
		WHERE game_id = $1 AND user_id >= 9000000001 AND user_id <= 9000000015
	`, gameID).Scan(&count)

	if err != nil {
		return 0, fmt.Errorf("count robots in game: %w", err)
	}

	return count, nil
}

// selectRandomAvailableCard selects a random card from available cards (1-100)
func selectRandomAvailableCard(takenCards []string) string {
	// Create slice of all available cards
	availableCards := []string{}

	// Check each card 1-100
	for i := 1; i <= 100; i++ {
		cardSN := fmt.Sprintf("%d", i)

		// Check if card is not taken
		taken := false
		for _, takenCard := range takenCards {
			if takenCard == cardSN {
				taken = true
				break
			}
		}

		if !taken {
			availableCards = append(availableCards, cardSN)
		}
	}

	// Select random from available cards
	if len(availableCards) > 0 {
		return availableCards[rand.Intn(len(availableCards))]
	}

	return "" // No cards available (shouldn't happen in normal games)
}
