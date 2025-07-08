package db

import (
	"log"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver
	"github.com/jmoiron/sqlx"
)

var DB *sqlx.DB

// InitDB initializes the database connection and creates tables if necessary
func InitDB() {
	var err error
	// PostgreSQL connection DSN
	dsn := os.Getenv("DATABASE_SECRET_KEY")

	// Open a connection to the PostgreSQL database
	DB, err = sqlx.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("Error openinvarg PostgreSQL database: %v", err)
		return
	}

	_, err = DB.Exec("SET CLIENT_ENCODING TO 'UTF8';")
	if err != nil {
		log.Fatal("Error setting encoding:", err)
	}

	// Verify the connection by pinging the database
	if err = DB.Ping(); err != nil {
		log.Fatalf("Error pinging PostgreSQL database: %v", err)
		return
	}
	log.Println("Successfully connected to the PostgreSQL database")
}
