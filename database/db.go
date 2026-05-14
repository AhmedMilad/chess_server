package db

import (
	"fmt"
	"log"

	"chess_server/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os"
	"time"
)

var DB *gorm.DB

func Init() {

	host := os.Getenv("DB_HOST")
	user := os.Getenv("DB_USER")
	dbname := os.Getenv("DB_NAME")
	password := os.Getenv("DB_PASSWORD")
	port := os.Getenv("DB_PORT")
	sslmode := os.Getenv("DB_SSLMODE")

	dsn := fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=%s",
		host, user, password, dbname, port, sslmode,
	)
	var err error
	DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	sqlDB, err := DB.DB()
	if err != nil {
		log.Fatalf("failed to get database instance: %v", err)
	}

	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(0)

	DB.AutoMigrate(
		&models.User{},
		&models.Setting{},
		&models.GameType{},
		&models.UserGameRating{},
		&models.Game{},
		&models.GameMove{},
		&models.GameState{},
	)

	SeedGameTypes(DB)
	fmt.Println("Database connection established")
}

func SeedGameTypes(db *gorm.DB) {
	gameTypes := []models.GameType{
		{Name: "Bullet", Duration: 1},
		{Name: "Blitz", Duration: 5},
		{Name: "Rapid", Duration: 10},
		{Name: "Classical", Duration: 30},
	}

	for _, gt := range gameTypes {
		var existing models.GameType
		if err := db.Where("name = ?", gt.Name).First(&existing).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				gt.CreatedAt = time.Now()
				gt.UpdatedAt = time.Now()
				if err := db.Create(&gt).Error; err != nil {
					log.Printf("Failed to create game type %s: %v", gt.Name, err)
				}
			}
		}
	}
}
