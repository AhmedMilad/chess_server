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
var game = models.Game{
	ID:         602,
	Player1ID:  2,
	Player2ID:  3,
	PlayerTurn: 1,
	GameTypeID: 1,
	Board:      "8/8/8/8/8/8/4P3/4R3",
}

func Test_InitializeEnironment(t *testing.T) {

	err := godotenv.Load("../.env")

	if err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	db.Init()

}

func TestGenericHandleMove_PawnMove(t *testing.T) {

	targetTurn := 1

	if game.PlayerTurn == 1 {
		targetTurn = 2
	}

	game.Board = "8/8/8/8/8/8/4P3/8"

	message := &utils.Message{
		Data: []byte(`{
			"from":"e2",
			"to":"e3"
		}`),
	}

	err := utils.GenericHandleMove(game, message)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if message.Turn != targetTurn {
		t.Fatalf("expected turn %d got %d", targetTurn, message.Turn)
	}
}

func TestGenericHandleMove_InvalidJSON(t *testing.T) {
	newGame := models.Game{}

	message := &utils.Message{
		Data: []byte(`invalid json`),
	}

	err := utils.GenericHandleMove(newGame, message)

	if err == nil {
		t.Fatal("expected error")
	}

	if err.Error() != "Error unmarshaling move data" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGenericHandleMove_InvalidMoveNotation(t *testing.T) {
	game.Board = "8/8/8/8/8/8/8/8"

	message := &utils.Message{
		Data: []byte(`{
			"from":"z9",
			"to":"e4"
		}`),
	}

	err := utils.GenericHandleMove(game, message)

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

	message := &utils.Message{
		Data: []byte(`{
			"from":"z9",
			"to":"e4"
		}`),
	}

	err := utils.GenericHandleMove(game, message)

	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGenericHandleMove_EmptyPiece(t *testing.T) {
	game.Board = "8/8/8/8/8/8/8/8"

	message := &utils.Message{
		Data: []byte(`{
			"from":"e2",
			"to":"e4"
		}`),
	}

	err := utils.GenericHandleMove(game, message)

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

	message := &utils.Message{
		Data: []byte(`{"from":"e2","to":"e3"}`),
	}

	err := utils.GenericHandleMove(newGame, message)

	if err == nil {
		t.Fatal("expected error when game state is missing from DB")
	}
}

func TestGenericHandleMove_KnightMove(t *testing.T) {
	game.Board = "8/8/8/8/8/2N5/8/8"

	message := &utils.Message{
		Data: []byte(`{"from":"c3","to":"a2"}`),
	}

	err := utils.GenericHandleMove(game, message)

	if err != nil {
		t.Fatalf("unexpected error moving knight: %v", err)
	}
}

func TestGenericHandleMove_MalformedFEN(t *testing.T) {
	game.Board = "not-a-fen-string"

	message := &utils.Message{
		Data: []byte(`{"from":"e2","to":"e4"}`),
	}

	err := utils.GenericHandleMove(game, message)

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

	utils.GenericHandleMove(game1, &msg1)

	if msg1.Turn != 2 {
		t.Errorf("expected turn to switch to 2, got %d", msg1.Turn)
	}

	msg2 := utils.Message{Data: []byte(`{"from":"e7","to":"e6"}`)}

	utils.GenericHandleMove(game2, &msg2)

	if msg2.Turn != 1 {
		t.Errorf("expected turn to switch back to 1, got %d", msg2.Turn)
	}
}

func TestGenericHandleMove_IllegalBishopMove(t *testing.T) {
	game.Board = "8/8/8/8/8/8/4B3/8"

	message := &utils.Message{
		Data: []byte(`{"from":"e2","to":"e3"}`),
	}

	err := utils.GenericHandleMove(game, message)

	if err == nil {
		t.Fatal("expected error for illegal bishop move")
	}
}

func TestGenericHandleMove_CorrectPlayerState(t *testing.T) {
	newGame := game

	newGame.Player2ID = 99
	newGame.PlayerTurn = 2
	newGame.Board = "4k3/8/8/8/8/8/4P3/4K3"

	message := &utils.Message{
		Data: []byte(`{"from":"e8","to":"d8"}`),
	}

	err := utils.GenericHandleMove(newGame, message)

	if err != nil && err.Error() == "record not found" {
		t.Log("Successfully verified it looks for the current player's state")
	}
}

func TestGenericHandleMove_BoundaryMove(t *testing.T) {
	game.Board = "7R/8/8/8/8/8/8/8"

	message := &utils.Message{
		Data: []byte(`{"from":"h8","to":"h7"}`),
	}

	err := utils.GenericHandleMove(game, message)
	if err != nil {
		t.Fatalf("Failed move on board boundary: %v", err)
	}
}

func TestGenericHandleMove_UnsupportedPiece(t *testing.T) {
	game.Board = "8/8/8/8/8/8/4X3/8"

	message := &utils.Message{
		Data: []byte(`{"from":"e2","to":"e3"}`),
	}

	err := utils.GenericHandleMove(game, message)

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

	message := &utils.Message{
		Data: []byte(`{"from":"e2","to":"e3"}`),
	}

	err := utils.GenericHandleMove(game, message)
	if err != nil {
		t.Fatal(err)
	}

	if message.Board == initialFen {
		t.Fatal("FEN string was not updated in the message")
	}
}

func TestGenericHandleMove_RookBlocked(t *testing.T) {
	game.Board = "8/8/8/8/8/8/4P3/4R3"

	message := &utils.Message{
		Data: []byte(`{"from":"e1","to":"e3"}`),
	}

	err := utils.GenericHandleMove(game, message)

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

		message := &utils.Message{
			Data: []byte(`{
					"from":"` + testCase.from + `",
					"to":"` + testCase.to + `"
				}`),
		}

		err := utils.GenericHandleMove(newGame, message)

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

	for i, testCase := range testCases {
		newGame := game
		newGame.Board = testCase.board

		message := &utils.Message{
			Data: []byte(`{
                    "from":"` + testCase.from + `",
                    "to":"` + testCase.to + `"
                }`),
		}

		err := utils.GenericHandleMove(newGame, message)

		if err != nil {
			t.Fatalf("case %d: unexpected castling error: %v", i, err)
		}

		if message.Board != testCase.expectedBoard {
			t.Errorf("case %d: castling board state mismatch\nexpected: %s\ngot:      %s",
				i, testCase.expectedBoard, message.Board)
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

	for i, testCase := range testCases {
		newGame := game
		newGame.Board = testCase.board

		message := &utils.Message{
			Data: []byte(`{
                    "from":"` + testCase.from + `",
                    "to":"` + testCase.to + `"
                }`),
		}

		err := utils.GenericHandleMove(newGame, message)

		if err != nil {
			t.Fatalf("case %d: unexpected castling error: %v", i, err)
		}

		if message.Board != testCase.expectedBoard {
			t.Errorf("case %d: black castling board state mismatch\nexpected: %s\ngot:      %s",
				i, testCase.expectedBoard, message.Board)
		}
	}
}

func TestGenericHandleMove_InvalidCastling(t *testing.T) {
	testCases := []struct {
		board string
		from  string
		to    string
	}{
		{
			// white cannot castle while in check,
			board: "4r1k1/8/8/8/8/8/8/4K2R",
			from:  "e1",
			to:    "g1",
		},
		{
			// white cannot castle Through check,
			board: "5rk1/8/8/8/8/8/8/4K2R",
			from:  "e1",
			to:    "g1",
		},
		{
			// black cannot castle while in check,
			board: "4k2r/8/8/8/8/8/8/4R1K1",
			from:  "e8",
			to:    "g8",
		},
		{
			// black cannot castle Through check,
			board: "4k2r/8/8/8/8/8/8/5RK1",
			from:  "e8",
			to:    "g8",
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

	for _, testCase := range testCases {
		newGame := game
		newGame.Board = testCase.board

		message := &utils.Message{
			Data: []byte(`{
					"from":"` + testCase.from + `",
					"to":"` + testCase.to + `"
				}`),
		}

		err := utils.GenericHandleMove(newGame, message)

		if err == nil {
			t.Fatalf("expected castling move to fail")
		}
	}
}

func TestGenericHandleMove_EnPassant(t *testing.T) {
	testCases := []struct {
		board         string
		from          string
		to            string
		expectedBoard string
	}{
		{
			// white en passant capture to the left
			board:         "8/8/8/3pP3/8/8/8/8",
			from:          "e5",
			to:            "d6",
			expectedBoard: "8/8/3P4/8/8/8/8/8",
		},
		{
			// white en passant capture to the right
			board:         "8/8/8/4Pp2/8/8/8/8",
			from:          "e5",
			to:            "f6",
			expectedBoard: "8/8/5P2/8/8/8/8/8",
		},
		{
			// black en passant capture to the left
			board:         "8/8/8/8/2Pp4/8/8/8",
			from:          "d4",
			to:            "c3",
			expectedBoard: "8/8/8/8/8/2p5/8/8",
		},
		{
			// black en passant capture to the right
			board:         "8/8/8/8/3pP3/8/8/8",
			from:          "d4",
			to:            "e3",
			expectedBoard: "8/8/8/8/8/4p3/8/8",
		},
	}

	for _, testCase := range testCases {
		newGame := game
		newGame.Board = testCase.board

		message := &utils.Message{
			Data: []byte(`{
                        "from":"` + testCase.from + `",
                        "to":"` + testCase.to + `"
                    }`),
		}

		err := utils.GenericHandleMove(newGame, message)

		if err != nil {
			t.Fatalf("unexpected en passant error: %v", err)
		}

		if message.Board != testCase.expectedBoard {
			t.Errorf("en passant board state mismatch\nexpected: %s\ngot:      %s",
				testCase.expectedBoard, message.Board)
		}
	}
}
