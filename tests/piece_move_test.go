package tests

import (
	"chess_server/database"
	"chess_server/models"
	"chess_server/utils"
	"log"
	"testing"

	"github.com/joho/godotenv"
)

// TODO this needs to be cleaned up
var game = models.Game{}
var playerGameState, opponentGameState = models.GameState{}, models.GameState{}

func Test_InitializeEnironment(t *testing.T) {

	err := godotenv.Load("../.env")

	if err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	db.Init()

	testGameID := 602 //TODO get this from the env file

	db.DB.Where("game_id = ? AND user_id = ?", game.ID, game.Player2ID).First(&opponentGameState)
	db.DB.Where("game_id = ? AND user_id = ?", game.ID, game.Player1ID).First(&playerGameState)
	db.DB.Where("id = ?", testGameID).First(&game)

}

func TestGenericHandleMove_PawnMove(t *testing.T) {

	game.PlayerTurn = 1

	game.Board = "8/8/8/8/8/8/4P3/8"

	message := utils.Message{
		Data: []byte(`{
			"from":"e2",
			"to":"e3"
		}`),
	}

	err := utils.GenericHandleMove(&game, &playerGameState, &opponentGameState, &message)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if game.PlayerTurn != 2 {
		t.Fatalf("expected turn %d got %d", 2, message.Turn)
	}
}

func TestGenericHandleMove_InvalidJSON(t *testing.T) {
	newGame := models.Game{}

	message := utils.Message{
		Data: []byte(`invalid json`),
	}

	err := utils.GenericHandleMove(&newGame, &playerGameState, &opponentGameState, &message)

	if err == nil {
		t.Fatal("expected error")
	}

	if err.Error() != "Error unmarshaling move data" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGenericHandleMove_InvalidMoveNotation(t *testing.T) {
	game.Board = "8/8/8/8/8/8/8/8"

	message := utils.Message{
		Data: []byte(`{
			"from":"z9",
			"to":"e4"
		}`),
	}

	err := utils.GenericHandleMove(&game, &playerGameState, &opponentGameState, &message)

	if err == nil {
		t.Fatal("expected error")
	}

	expected := "Invalid move to"

	if err.Error() != expected {
		t.Fatalf("expected %q got %q", expected, err.Error())
	}
}

func TestGenericHandleMove_InvalidFromMove(t *testing.T) {
	game.Board = "invalid-fen"

	message := utils.Message{
		Data: []byte(`{
			"from":"z9",
			"to":"e4"
		}`),
	}

	err := utils.GenericHandleMove(&game, &playerGameState, &opponentGameState, &message)

	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGenericHandleMove_EmptyPiece(t *testing.T) {
	game.Board = "8/8/8/8/8/8/8/8"

	message := utils.Message{
		Data: []byte(`{
			"from":"e2",
			"to":"e4"
		}`),
	}

	err := utils.GenericHandleMove(&game, &playerGameState, &opponentGameState, &message)

	if err == nil {
		t.Fatal("expected error")
	}

	if err.Error() != "Invalid piece" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGenericHandleMove_GameStateNotFound(t *testing.T) {
	newGame := game
	newGame.ID = 9999999999999999
	newGame.Board = "8/8/8/8/8/8/4P3/8"

	message := utils.Message{
		Data: []byte(`{"from":"e2","to":"e3"}`),
	}

	err := utils.GenericHandleMove(&newGame, &playerGameState, &opponentGameState, &message)

	if err == nil {
		t.Fatal("expected error when game state is missing from DB")
	}
}

func TestGenericHandleMove_KnightMove(t *testing.T) {
	game.PlayerTurn = 1
	game.Board = "8/8/8/8/8/2N5/8/8"

	message := utils.Message{
		Data: []byte(`{"from":"c3","to":"a2"}`),
	}

	err := utils.GenericHandleMove(&game, &playerGameState, &opponentGameState, &message)

	if err != nil {
		t.Fatalf("unexpected error moving knight: %v", err)
	}
}

func TestGenericHandleMove_MalformedFEN(t *testing.T) {
	game.Board = "not-a-fen-string"

	message := utils.Message{
		Data: []byte(`{"from":"e2","to":"e4"}`),
	}

	err := utils.GenericHandleMove(&game, &playerGameState, &opponentGameState, &message)

	if err == nil {
		t.Fatal("expected error due to malformed FEN")
	}
}

func TestGenericHandleMove_TurnToggle(t *testing.T) {
	game1 := game
	game1.PlayerTurn = 1
	game1.Board = "8/8/8/8/8/8/4P3/8"
	game2 := game
	game2.PlayerTurn = 2
	game2.Board = "8/4p3/8/8/8/8/8/8"

	msg1 := utils.Message{Data: []byte(`{"from":"e2","to":"e3"}`)}

	utils.GenericHandleMove(&game1, &playerGameState, &opponentGameState, &msg1)

	if msg1.Turn != 2 {
		t.Errorf("expected turn to switch to 2, got %d", msg1.Turn)
	}

	msg2 := utils.Message{Data: []byte(`{"from":"e7","to":"e6"}`)}

	utils.GenericHandleMove(&game2, &playerGameState, &opponentGameState, &msg2)

	if msg2.Turn != 1 {
		t.Errorf("expected turn to switch back to 1, got %d", msg2.Turn)
	}
}

func TestGenericHandleMove_IllegalBishopMove(t *testing.T) {
	game.Board = "8/8/8/8/8/8/4B3/8"

	message := utils.Message{
		Data: []byte(`{"from":"e2","to":"e3"}`),
	}

	err := utils.GenericHandleMove(&game, &playerGameState, &opponentGameState, &message)

	if err == nil {
		t.Fatal("expected error for illegal bishop move")
	}
}

func TestGenericHandleMove_CorrectPlayerState(t *testing.T) {
	newGame := game

	newGame.Player2ID = 99
	newGame.PlayerTurn = 2
	newGame.Board = "4k3/8/8/8/8/8/4P3/4K3"

	message := utils.Message{
		Data: []byte(`{"from":"e8","to":"d8"}`),
	}

	err := utils.GenericHandleMove(&newGame, &playerGameState, &opponentGameState, &message)

	if err != nil && err.Error() == "record not found" {
		t.Log("Successfully verified it looks for the current player's state")
	}
}

func TestGenericHandleMove_BoundaryMove(t *testing.T) {
	game.PlayerTurn = 1
	game.Board = "7R/8/8/8/8/8/8/8"

	message := utils.Message{
		Data: []byte(`{"from":"h8","to":"h7"}`),
	}

	err := utils.GenericHandleMove(&game, &playerGameState, &opponentGameState, &message)
	if err != nil {
		t.Fatalf("Failed move on board boundary: %v", err)
	}
}

func TestGenericHandleMove_UnsupportedPiece(t *testing.T) {
	game.Board = "8/8/8/8/8/8/4X3/8"

	message := utils.Message{
		Data: []byte(`{"from":"e2","to":"e3"}`),
	}

	err := utils.GenericHandleMove(&game, &playerGameState, &opponentGameState, &message)

	if err == nil {
		t.Fatal("Expected error for unsupported piece type 'X'")
	}

	expected := "invalid piece: X"
	if err.Error() != expected {
		t.Fatalf("Expected %q, got %q", expected, err.Error())
	}
}

func TestGenericHandleMove_UpdatesFEN(t *testing.T) {
	initialFen := "8/8/8/8/8/8/4P3/8"

	game.Board = initialFen
	game.PlayerTurn = 1

	message := utils.Message{
		Board:           initialFen,
		Turn:            game.PlayerTurn,
		EnpassantSquare: opponentGameState.Enpassant,
		Status:          "failed",
		Data:            []byte(`{"from":"e2","to":"e3"}`),
	}

	err := utils.GenericHandleMove(&game, &playerGameState, &opponentGameState, &message)
	if err != nil {
		t.Fatal(err)
	}

	if game.Board == initialFen {
		t.Fatal("FEN string was not updated in the message")
	}
}

func TestGenericHandleMove_RookBlocked(t *testing.T) {
	game.Board = "8/8/8/8/8/8/4P3/4R3"

	message := utils.Message{
		Data: []byte(`{"from":"e1","to":"e3"}`),
	}

	err := utils.GenericHandleMove(&game, &playerGameState, &opponentGameState, &message)

	if err == nil {
		t.Fatal("Expected error: Rook cannot jump over the pawn at e2")
	}
}

func TestGenericHandleMove_WhitePinnedPieces(t *testing.T) {
	testCases := []struct {
		board string
		from  string
		to    string
	}{
		{
			board: "4r3/8/8/8/8/8/4R3/4K3",
			from:  "e2",
			to:    "f2",
		},
		{
			board: "8/8/b7/8/8/3B4/4K3/8",
			from:  "d3",
			to:    "e4",
		},
		{
			board: "4r3/8/8/8/8/8/4N3/4K3",
			from:  "e2",
			to:    "f4",
		},
		{
			board: "4r3/8/8/8/8/8/4Q3/4K3",
			from:  "e2",
			to:    "f2",
		},
		{
			board: "4r3/8/8/8/8/8/4P3/4K3",
			from:  "e2",
			to:    "f3",
		},

		{
			board: "8/8/8/8/r2BK3/8/8/8",
			from:  "d4",
			to:    "c5",
		},
		{
			board: "8/8/8/8/8/8/8/K1N4q",
			from:  "c1",
			to:    "d3",
		},
		{
			board: "8/8/8/8/b7/8/2N5/3K4",
			from:  "c2",
			to:    "e3",
		},
		{
			board: "8/8/5q2/8/3P4/2K5/8/8",
			from:  "d4",
			to:    "d5",
		},
	}

	for _, testCase := range testCases {
		newGame := game
		newGame.Board = testCase.board

		message := utils.Message{
			Data: []byte(`{
					"from":"` + testCase.from + `",
					"to":"` + testCase.to + `"
				}`),
		}

		err := utils.GenericHandleMove(&newGame, &playerGameState, &opponentGameState, &message)

		if err == nil {
			t.Fatalf("expected pin move to fail")
		}
	}
}

func TestGenericHandleMove_WhiteCastling(t *testing.T) {
	testCases := []struct {
		board         string
		from          string
		to            string
		expectedBoard string
	}{
		{
			board:         "8/8/8/8/8/8/8/4K2R",
			from:          "e1",
			to:            "g1",
			expectedBoard: "8/8/8/8/8/8/8/5RK1",
		},
		{
			board:         "8/8/8/8/8/8/8/R3K3",
			from:          "e1",
			to:            "c1",
			expectedBoard: "8/8/8/8/8/8/8/2KR4",
		},
	}

	newGame := game
	for i, testCase := range testCases {
		newGame.PlayerTurn = 1
		newGame.Board = testCase.board
		playerGameState.CanKingSideCastle = true
		playerGameState.CanLongCastle = true

		message := utils.Message{
			Data: []byte(`{
                    "from":"` + testCase.from + `",
                    "to":"` + testCase.to + `"
                }`),
		}

		err := utils.GenericHandleMove(&newGame, &playerGameState, &opponentGameState, &message)

		if err != nil {
			t.Fatalf("case %d: unexpected castling error: %v", i, err)
		}

		if newGame.Board != testCase.expectedBoard {
			t.Errorf("case %d: castling board state mismatch\nexpected: %s\ngot:      %s",
				i, testCase.expectedBoard, newGame.Board)
		}
	}
}

func TestGenericHandleMove_BlackCastling(t *testing.T) {
	testCases := []struct {
		board         string
		from          string
		to            string
		expectedBoard string
	}{
		{
			board:         "4k2r/8/8/8/8/8/8/8",
			from:          "e8",
			to:            "g8",
			expectedBoard: "5rk1/8/8/8/8/8/8/8",
		},
		{
			board:         "r3k3/8/8/8/8/8/8/8",
			from:          "e8",
			to:            "c8",
			expectedBoard: "2kr4/8/8/8/8/8/8/8",
		},
	}

	newGame := game
	for i, testCase := range testCases {
		newGame.PlayerTurn = 2
		newGame.Board = testCase.board
		playerGameState.CanKingSideCastle = true
		playerGameState.CanLongCastle = true

		message := utils.Message{
			Data: []byte(`{
                    "from":"` + testCase.from + `",
                    "to":"` + testCase.to + `"
                }`),
		}

		err := utils.GenericHandleMove(&newGame, &playerGameState, &opponentGameState, &message)

		if err != nil {
			t.Fatalf("case %d: unexpected castling error: %v", i, err)
		}

		if newGame.Board != testCase.expectedBoard {
			t.Errorf("case %d: black castling board state mismatch\nexpected: %s\ngot:      %s",
				i, testCase.expectedBoard, newGame.Board)
		}
	}
}

func TestGenericHandleMove_InvalidCastling(t *testing.T) {

	whiteTestCases := []struct {
		board string
		from  string
		to    string
	}{
		{
			board: "4r1k1/8/8/8/8/8/8/4K2R",
			from:  "e1",
			to:    "g1",
		},
		{
			board: "5rk1/8/8/8/8/8/8/4K2R",
			from:  "e1",
			to:    "g1",
		},
		{
			board: "4k3/8/8/8/8/8/8/4KBRR",
			from:  "e1",
			to:    "g1",
		},
		{
			board: "4r3/8/8/8/8/8/8/4K2R",
			from:  "e1",
			to:    "g1",
		},
	}

	blackTestCases := []struct {
		board string
		from  string
		to    string
	}{
		{
			board: "4k2r/8/8/8/8/8/8/4R1K1",
			from:  "e8",
			to:    "g8",
		},
		{
			board: "4k2r/8/8/8/8/8/8/5RK1",
			from:  "e8",
			to:    "g8",
		},
		{
			board: "4kbrr/8/8/8/8/8/8/4K3",
			from:  "e8",
			to:    "g8",
		},
		{
			board: "4k2r/8/8/8/8/8/8/4R3",
			from:  "e8",
			to:    "g8",
		},
	}

	newGame := game
	newGame.PlayerTurn = 1
	for _, testCase := range whiteTestCases {
		newGame.Board = testCase.board

		message := utils.Message{
			Data: []byte(`{"from":"` + testCase.from + `","to":"` + testCase.to + `"}`),
		}

		err := utils.GenericHandleMove(&newGame, &playerGameState, &opponentGameState, &message)
		if err == nil {
			t.Fatalf("expected castling move to fail for White")
		}
	}

	newGame = game
	newGame.PlayerTurn = 2
	for _, testCase := range blackTestCases {
		newGame.Board = testCase.board

		message := utils.Message{
			Data: []byte(`{"from":"` + testCase.from + `","to":"` + testCase.to + `"}`),
		}

		err := utils.GenericHandleMove(&newGame, &playerGameState, &opponentGameState, &message)
		if err == nil {
			t.Fatalf("expected castling move to fail for Black")
		}
	}
}

func TestGenericHandleMove_EnPassant(t *testing.T) {

	whiteTestCases := []struct {
		             string
		board            string
		from             string
		to               string
		expectedBoard    string
		enpassant_square string
	}{
		{
			board:            "8/8/8/3pP3/8/8/8/8",
			from:             "e5",
			to:               "d6",
			enpassant_square: "d5",
			expectedBoard:    "8/8/3P4/8/8/8/8/8",
		},
		{
			board:            "8/8/8/4Pp2/8/8/8/8",
			from:             "e5",
			to:               "f6",
			enpassant_square: "f5",
			expectedBoard:    "8/8/5P2/8/8/8/8/8",
		},
	}

	blackTestCases := []struct {
		board            string
		from             string
		to               string
		expectedBoard    string
		enpassant_square string
	}{
		{
			board:            "8/8/8/8/2Pp4/8/8/8",
			from:             "d4",
			to:               "c3",
			enpassant_square: "c4",
			expectedBoard:    "8/8/8/8/8/2p5/8/8",
		},
		{
			board:            "8/8/8/8/3pP3/8/8/8",
			from:             "d4",
			to:               "e3",
			enpassant_square: "e4",
			expectedBoard:    "8/8/8/8/8/4p3/8/8",
		},
	}

	newGame := game
	for _, testCase := range whiteTestCases {
		newGame.PlayerTurn = 1
		newGame.Board = testCase.board
		opponentGameState.Enpassant = testCase.enpassant_square

		message := utils.Message{
			Data: []byte(`{"from":"` + testCase.from + `","to":"` + testCase.to + `"}`),
		}

		err := utils.GenericHandleMove(&newGame, &playerGameState, &opponentGameState, &message)
		if err != nil {
			t.Fatalf("unexpected en passant error for White: %v", err)
		}

		if newGame.Board != testCase.expectedBoard {
			t.Errorf("en passant board state mismatch for White\nexpected: %s\ngot:      %s",
				testCase.expectedBoard, newGame.Board)
		}
	}

	newGame = game
	for _, testCase := range blackTestCases {
		newGame.PlayerTurn = 2
		newGame.Board = testCase.board
		opponentGameState.Enpassant = testCase.enpassant_square

		message := utils.Message{
			Data: []byte(`{"from":"` + testCase.from + `","to":"` + testCase.to + `"}`),
		}

		err := utils.GenericHandleMove(&newGame, &playerGameState, &opponentGameState, &message)
		if err != nil {
			t.Fatalf("unexpected en passant error for Black: %v", err)
		}

		if newGame.Board != testCase.expectedBoard {
			t.Errorf("en passant board state mismatch for Black\nexpected: %s\ngot:      %s",
				testCase.expectedBoard, newGame.Board)
		}
	}
}

func TestGenericHandleMove_SingleCheck(t *testing.T) {
	game.Board = "4r3/8/8/8/8/8/3B4/4K3"
	game.PlayerTurn = 1

	message := utils.Message{
		Data: []byte(`{
            "from": "d2",
            "to": "e3"
        }`),
	}

	err := utils.GenericHandleMove(&game, &playerGameState, &opponentGameState, &message)
	if err != nil {
		t.Fatalf("Expected move d2->e3 to legally block the check, but got error: %v", err)
	}

	game.Board = "4r3/8/8/8/8/8/3B4/4K3"

	message = utils.Message{
		Data: []byte(`{
            "from": "d2",
            "to": "c3"
        }`),
	}

	err = utils.GenericHandleMove(&game, &playerGameState, &opponentGameState, &message)
	if err == nil {
		t.Fatalf("Expected move d2->c3 to fail because it ignores the check, but it was allowed")
	}
}

func TestGenericHandleMove_DoubleCheck(t *testing.T) {
	game.Board = "4r3/8/8/b7/8/2N5/8/4K3"

	message := utils.Message{
		Data: []byte(`{
            "from": "c3",
            "to": "a5"
        }`),
	}

	err := utils.GenericHandleMove(&game, &playerGameState, &opponentGameState, &message)
	if err == nil {
		t.Fatalf("Expected Knight move to fail due to double check rules, but no error was thrown")
	}
}

func TestGenericHandleMove_AbsolutePin(t *testing.T) {
	game.Board = "8/8/5q2/8/8/2B5/8/K7"

	illegalMessage := utils.Message{
		Data: []byte(`{
            "from": "c3",
            "to": "b4"
        }`),
	}

	err := utils.GenericHandleMove(&game, &playerGameState, &opponentGameState, &illegalMessage)
	if err == nil {
		t.Fatalf("Expected Bishop move to b4 to fail because it breaks an absolute pin, but it was allowed")
	}

	game.PlayerTurn = 1
	game.Board = "8/8/5q2/8/8/2B5/8/K7"

	legalMessage := utils.Message{
		Data: []byte(`{
            "from": "c3",
            "to": "d4"
        }`),
	}

	err = utils.GenericHandleMove(&game, &playerGameState, &opponentGameState, &legalMessage)
	if err != nil {
		t.Fatalf("Expected Bishop move to d4 to be legal along the pin line, but got error: %v", err)
	}
}

func TestGenericHandleMove_PinAndCheckConflict(t *testing.T) {
	game.Board = "4r3/8/b7/8/8/8/4R3/5K2"

	message := utils.Message{
		Data: []byte(`{
            "from": "e2",
            "to": "e8"
        }`),
	}

	err := utils.GenericHandleMove(&game, &playerGameState, &opponentGameState, &message)
	if err == nil {
		t.Fatalf("Expected Rook capture on e8 to fail because the Rook is absolutely pinned by the a6 Bishop")
	}
}

func TestGenericHandleMove_RookSameAxisPin(t *testing.T) {
	game.Board = "4r3/8/8/8/8/4R3/8/4K3"

	illegalMessage := utils.Message{
		Data: []byte(`{
            "from": "e3",
            "to": "a3"
        }`),
	}
	err := utils.GenericHandleMove(&game, &playerGameState, &opponentGameState, &illegalMessage)
	if err == nil {
		t.Fatalf("Expected Rook move to a3 to fail because it leaves the vertical pin line")
	}

	game.PlayerTurn = 1
	game.Board = "4r3/8/8/8/8/4R3/8/4K3"

	legalMessage := utils.Message{
		Data: []byte(`{
            "from": "e3",
            "to": "e6"
        }`),
	}
	err = utils.GenericHandleMove(&game, &playerGameState, &opponentGameState, &legalMessage)
	if err != nil {
		t.Fatalf("Expected Rook move to e6 to be valid since it stays on the pin line, got: %v", err)
	}
}

func TestGenericHandleMove_EnPassantDiscoveredCheck(t *testing.T) {
	game.Board = "8/8/8/r3Pp1K/8/8/8/8"

	message := utils.Message{
		Data: []byte(`{
            "from": "e5",
            "to": "f6"
        }`),
	}

	err := utils.GenericHandleMove(&game, &playerGameState, &opponentGameState, &message)
	if err == nil {
		t.Fatalf("Expected En Passant to fail because removing both pawns exposes the White King to a discovered horizontal check from the a5 Rook")
	}
}
