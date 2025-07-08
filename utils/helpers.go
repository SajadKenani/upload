package utils

import (
	"database/sql"
	"fmt"

	"time"

	"errors"

	"reflect"
	"slices"
	"strconv"

	"math/rand"

	"Backend/db"

	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-sql-driver/mysql"

	"github.com/lib/pq"

	"golang.org/x/crypto/bcrypt"
)

func SettingData(slice interface{}, mappings map[string]string) {
	sliceVal := reflect.ValueOf(slice)

	if sliceVal.Kind() != reflect.Ptr || sliceVal.Elem().Kind() != reflect.Slice {
		panic("Expected a pointer to a slice")
	}

	sliceElem := sliceVal.Elem()

	for i := 0; i < sliceElem.Len(); i++ {
		item := sliceElem.Index(i)

		for targetField, sourceField := range mappings {
			target := item.FieldByName(targetField)
			source := item.FieldByName(sourceField)

			// Check if fields exist and are settable
			if !target.IsValid() || !source.IsValid() || !target.CanSet() {
				continue
			}

			// Ensure types match before setting the value
			if target.Type() == source.Type() {
				target.Set(source)
			}
		}
	}
}
func HandleError(ctx *gin.Context, err error, message string, statusCode ...int) {
	// Default status code is 500 (Internal Server Error)
	code := http.StatusInternalServerError
	if len(statusCode) > 0 {
		code = statusCode[0] // Use custom status code if provided
	}

	// Handle specific MySQL errors (if any)
	if sqlErr, ok := err.(*mysql.MySQLError); ok {
		code = http.StatusInternalServerError
		switch sqlErr.Number {
		case 1062: // Duplicate entry
			code = http.StatusConflict
		case 1048: // Column cannot be null
			code = http.StatusBadRequest
		}
	}

	// Log the error details and message
	log.SetFlags(log.LstdFlags | log.Lshortfile) // Add this line to configure log output
	log.Printf("ERROR: %s | %v", message, err)

	// Return a JSON response with error information
	ctx.AbortWithStatusJSON(code, gin.H{
		"error":   message,
		"details": err.Error(),
		"status":  false,
	})
}
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	return string(bytes), err
}
func VerifyPassword(password, hash string) bool {
	if hash == "" {
		log.Println("Error: Empty password hash provided.")
		return false
	}

	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if err != nil {
		log.Println("Password verification failed:", err) // Log the error for debugging
		return false
	}

	return true
}
func SanitizeDatabaseName(input string) string {
	// Allow only alphanumeric characters and underscores
	validName := ""
	for _, ch := range input {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' {
			validName += string(ch)
		}
	}
	if validName == "" {
		validName = "default_table" // Prevent empty table names
	}
	return validName
}
func ParsePostgresArray(arrayStr string) ([]int, error) {
	arrayStr = strings.Trim(arrayStr, "{}")     // Remove `{}` brackets
	stringItems := strings.Split(arrayStr, ",") // Split by comma

	var result []int
	for _, s := range stringItems {
		num, err := strconv.Atoi(s)
		if err != nil {
			return nil, err
		}
		result = append(result, num)
	}

	return result, nil
}
func RemoveIndexFromAnArray(slice []int, i int) []int {
	if i < 0 || i >= len(slice) {
		return slice // Return original if index is out of range
	}
	return slices.Delete(slice, i, i+1) // Merge before and after index
}
func HandlePhoneValidation(ctx *gin.Context, phone string) (status int) {
	// Query the database for the phone number
	var phoneExists string
	err := db.DB.QueryRow("SELECT phone_number FROM admin_account WHERE phone_number = $1 LIMIT 1", phone).Scan(&phoneExists)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return http.StatusAccepted
		} else {
			HandleError(ctx, err, "Database query failed", http.StatusInternalServerError)
			return http.StatusInternalServerError
		}
	}

	return http.StatusNotAcceptable

}
func GenerateSalt() ([]byte, error) {
	salt := make([]byte, 16) // 16 bytes is recommended
	_, err := rand.Read(salt)
	if err != nil {
		return nil, err
	}
	return salt, nil
}
func ParseSubjectIDs(subjectIDsStr string) (pq.Int64Array, error) {
	// Remove curly braces and split
	subjectIDsStr = strings.Trim(subjectIDsStr, "{}")
	if subjectIDsStr == "" {
		return pq.Int64Array{}, nil
	}

	// Split and convert to int64 array
	idStrings := strings.Split(subjectIDsStr, ",")
	subjectIDs := make(pq.Int64Array, 0, len(idStrings))

	for _, idStr := range idStrings {
		id, err := strconv.ParseInt(strings.TrimSpace(idStr), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse subject ID: %v", err)
		}
		subjectIDs = append(subjectIDs, id)
	}

	return subjectIDs, nil
}

type StoredAccount struct {
	ID          string
	PhoneNumber string
	Password    string
	Name        string
	Salt        string
	Limits      int
	Status      string
	Action_date string
	Is_active  bool
	Email    string
}

func HandleCooldown(ctx *gin.Context, storedAccount StoredAccount, dbName string) {
	const layout = time.RFC3339

	actionDate, err := time.Parse(layout, storedAccount.Action_date)
	if err != nil {
		HandleError(ctx, err, "Invalid action_date format", http.StatusBadRequest)
	}

	var cooldown time.Duration
	switch storedAccount.Status {
	case "suspect":
		cooldown = -2 * time.Hour
	case "danger":
		cooldown = 2 * time.Hour
	default:
		cooldown = 0
	}

	if !storedAccount.Is_active {
		timePassed := time.Since(actionDate)
		if timePassed < cooldown {
			timeLeft := cooldown - timePassed
			log.Printf("Cooldown still active. Time left: %s", timeLeft.Round(time.Second))
		} else {
			_, err := db.DB.Exec("UPDATE " + dbName + " SET is_active = $1 WHERE id = $2", true, storedAccount.ID)
			if err != nil {
				HandleError(ctx, err, "Failed to reactivate account", http.StatusInternalServerError)	
			}
		}
	}
}

func HandleLimitAndStatus(ctx *gin.Context, storedAccount StoredAccount, dbName string) (int) {
	const layout = time.RFC3339

	status := storedAccount.Status
	updateActionDate := storedAccount.Action_date
	isActive := storedAccount.Is_active
	limits := storedAccount.Limits

	if limits > 0 {
		limits--
	}

	if limits <= 0 {
		isActive = false
		updateActionDate = time.Now().Format(layout)

		switch status {
		case "active":
			status = "suspect"
		case "suspect":
			status = "danger"
		case "danger":
			status = "inactive"
		}

		limits = 22
	}

	_, err := db.DB.Exec(`
		UPDATE ` + dbName + ` SET limits = $1, status = $2, is_active = $3, action_date = $4 WHERE id = $5
	`, limits, status, isActive, updateActionDate, storedAccount.ID)

	if err != nil {
		HandleError(ctx, err, "Failed to update limits and status", http.StatusInternalServerError)
		
	}
	
	return limits
}

func GenerateSixDigitNumber() string {
	rand.Seed(time.Now().UnixNano())
	number := rand.Intn(900000) + 100000
	return fmt.Sprintf("%06d", number)
}