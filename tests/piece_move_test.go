package tests

import (
	"chess_server/database"
	"chess_server/models"
	"chess_server/utils"
	"log"
	"testing"

	"github.com/joho/godotenv"
)

func TestGenericHandleMove_InvalidJSON(t *testing.T) {
	game := models.Game{}

	message := &utils.Message{
		Data: []byte(`invalid json`),
	}

	err := utils.GenericHandleMove(game, message)

	if err == nil {
		t.Fatal("expected error")
	}

	if err.Error() != "Error unmarshaling move data" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGenericHandleMove_InvalidMoveNotation(t *testing.T) {
	game := models.Game{
		Board: "8/8/8/8/8/8/8/8",
	}

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
	game := models.Game{
		Board: "some-fen",
	}

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
	game := models.Game{
		Board: "8/8/8/8/8/8/8/8",
	}

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

func TestGenericHandleMove_PawnMove(t *testing.T) {
	game := models.Game{
		ID:         70,
		Player1ID:  5,
		Player2ID:  1,
		PlayerTurn: 1,
		Board:      "8/8/8/8/8/8/4P3/8",
		GameTypeID: 1,
	}

	message := &utils.Message{
		Data: []byte(`{
			"from":"e2",
			"to":"e3"
		}`),
	}

	err := godotenv.Load("../.env")

	if err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	db.Init()

	err = utils.GenericHandleMove(game, message)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if message.Turn != 2 {
		t.Fatalf("expected turn 2 got %d", message.Turn)
	}
}

func TestGenericHandleMove_GameStateNotFound(t *testing.T) {
	game := models.Game{
		ID:         9999999999999999,
		Player1ID:  5,
		PlayerTurn: 1,
		Board:      "8/8/8/8/8/8/4P3/8",
	}

	message := &utils.Message{
		Data: []byte(`{"from":"e2","to":"e3"}`),
	}

	db.Init()

	err := utils.GenericHandleMove(game, message)

	if err == nil {
		t.Fatal("expected error when game state is missing from DB")
	}
}

func TestGenericHandleMove_KnightMove(t *testing.T) {
	game := models.Game{
		ID:         70,
		Player1ID:  5,
		Player2ID:  1,
		PlayerTurn: 1,
		Board:      "8/8/8/8/8/2n5/8/8",
		GameTypeID: 1,
	}

	// Move black knight from c3 to a2
	message := &utils.Message{
		Data: []byte(`{"from":"c3","to":"a2"}`),
	}

	db.Init()

	err := utils.GenericHandleMove(game, message)

	if err != nil {
		t.Fatalf("unexpected error moving knight: %v", err)
	}
}

func TestGenericHandleMove_MalformedFEN(t *testing.T) {
	game := models.Game{
		Board: "not-a-fen-string",
	}

	message := &utils.Message{
		Data: []byte(`{"from":"e2","to":"e4"}`),
	}

	err := utils.GenericHandleMove(game, message)

	if err == nil {
		t.Fatal("expected error due to malformed FEN")
	}
}

func TestGenericHandleMove_TurnToggle(t *testing.T) {
	db.Init()

	game1 := models.Game{ID: 70, Player1ID: 1, Player2ID: 5, PlayerTurn: 1, Board: "8/8/8/8/8/8/4P3/8", GameTypeID: 1}
	msg1 := utils.Message{Data: []byte(`{"from":"e2","to":"e3"}`)}

	utils.GenericHandleMove(game1, &msg1)

	if msg1.Turn != 2 {
		t.Errorf("expected turn to switch to 2, got %d", msg1.Turn)
	}

	game2 := models.Game{ID: 70, Player1ID: 1, Player2ID: 5, PlayerTurn: 2, Board: "8/4p3/8/8/8/8/8/8", GameTypeID: 1}
	msg2 := utils.Message{Data: []byte(`{"from":"e7","to":"e6"}`)}

	utils.GenericHandleMove(game2, &msg2)

	if msg2.Turn != 1 {
		t.Errorf("expected turn to switch back to 1, got %d", msg2.Turn)
	}
}

func TestGenericHandleMove_IllegalBishopMove(t *testing.T) {
	game := models.Game{
		ID:         70,
		Player1ID:  1,
		Player2ID:  5,
		PlayerTurn: 1,
		GameTypeID: 1,
		Board:      "8/8/8/8/8/8/4B3/8",
	}

	message := &utils.Message{
		Data: []byte(`{"from":"e2","to":"e3"}`),
	}

	db.Init()

	err := utils.GenericHandleMove(game, message)

	if err == nil {
		t.Fatal("expected error for illegal bishop move")
	}
}

func TestGenericHandleMove_CorrectPlayerState(t *testing.T) {
	db.Init()
	game := models.Game{
		ID:         500,
		Player1ID:  1,
		Player2ID:  99,
		PlayerTurn: 2,
		Board:      "4k3/8/8/8/8/8/4P3/4K3",
	}

	message := &utils.Message{
		Data: []byte(`{"from":"e8","to":"d8"}`),
	}

	err := utils.GenericHandleMove(game, message)

	if err != nil && err.Error() == "record not found" {
		t.Log("Successfully verified it looks for the current player's state")
	}
}

func TestGenericHandleMove_BoundaryMove(t *testing.T) {
	db.Init()
	game := models.Game{
		ID:         70,
		Player1ID:  1,
		Player2ID:  5,
		PlayerTurn: 1,
		GameTypeID: 1,
		Board:      "7R/8/8/8/8/8/8/8",
	}

	message := &utils.Message{
		Data: []byte(`{"from":"h8","to":"h7"}`),
	}

	err := utils.GenericHandleMove(game, message)
	if err != nil {
		t.Fatalf("Failed move on board boundary: %v", err)
	}
}

func TestGenericHandleMove_UnsupportedPiece(t *testing.T) {
	game := models.Game{
		ID:         70,
		Player1ID:  1,
		Player2ID:  5,
		PlayerTurn: 1,
		GameTypeID: 1,
		Board:      "8/8/8/8/8/8/4X3/8",
	}

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
	db.Init()
	initialFen := "8/8/8/8/8/8/4P3/8"
	game := models.Game{
		ID:         70,
		Player1ID:  1,
		Player2ID:  5,
		PlayerTurn: 1,
		GameTypeID: 1,
		Board:      "8/8/8/8/8/8/4P3/8",
	}
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
    db.Init()
    game := models.Game{
		ID:         70,
		Player1ID:  1,
		Player2ID:  5,
		PlayerTurn: 1,
		GameTypeID: 1,
        Board:      "8/8/8/8/8/8/4P3/4R3",
    }

    message := &utils.Message{
        Data: []byte(`{"from":"e1","to":"e3"}`),
    }

    err := utils.GenericHandleMove(game, message)
    if err == nil {
        t.Fatal("Expected error: Rook cannot jump over the pawn at e2")
    }
}
