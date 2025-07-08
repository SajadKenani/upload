package main

import (
	"Backend/db"
	"Backend/utils"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"fmt"
	"log"
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/google/uuid"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func RecoveryWithLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("🔥 Panic recovered: %v", r)
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error":   "Internal server error",
					"details": fmt.Sprintf("%v", r),
					"status":  false,
				})
			}
		}()
		c.Next()
	}
}

func main() {
	// Load .env file
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("Error loading .env file")
	}

	utils.InitS3()
	// Initialize the product and department database connections
	db.InitDB()
	r := gin.Default()
	r.Use(gin.Logger(), gin.Recovery(), RecoveryWithLogger())
	r.Use(func(c *gin.Context) {
		log.Printf("→ [%s] %s", c.Request.Method, c.Request.URL.Path)
		c.Next()
		log.Printf("← %d %s", c.Writer.Status(), c.FullPath())
	})
	
	r.Static("/uploads", "./uploads")

	// Enable CORS with specific configurations
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"}, // it should accept everything
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE"},
		AllowHeaders:     []string{"Authorization", "Content-Type"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))

	r.POST("/api/upload-video", UploadVideo)

	fmt.Println("Server running at http://localhost:8086")
	if err := http.ListenAndServe(":8086", r); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

type Video struct {
	ID            int    `json:"id,omitempty" db:"id"`
	AuthorID      int    `json:"author_id" db:"author_id"`
	URL           string `json:"url" db:"url"`
	CompressedURL string `json:"compressed_url" db:"compressed_url"`
	GenID         string `json:"gen_id" db:"gen_id"`
	ThumbNail     string `json:"thumbnail" db:"thumbnail"`
	Title         string `json:"title" db:"title"`
	Description   string `json:"description" db:"description"`
	LectureID     int    `json:"lecture_id" db:"lecture_id"`
	CreateAt      string `json:"create_at" db:"create_at"`
}

func UploadVideo(ctx *gin.Context) {
	if !strings.HasPrefix(ctx.ContentType(), "multipart/form-data") {
		utils.HandleError(ctx, nil, "Invalid content type", http.StatusBadRequest)
		return
	}

	// === Get video file ===
	videoFile, videoHeader, err := ctx.Request.FormFile("video")
	if err != nil {
		utils.HandleError(ctx, nil, "Unable to retrieve video file", http.StatusInternalServerError)
		return
	}
	defer videoFile.Close()

	log.Printf("Received video file: %v", videoHeader.Filename)

	// === Ensure uploads directory exists ===
	if err := os.MkdirAll("./uploads", os.ModePerm); err != nil {
		utils.HandleError(ctx, nil, "Failed to create upload directory", http.StatusInternalServerError)
		return
	}

	// === Save uploaded video locally ===
	tempID := uuid.New().String()
	localPath := "./uploads/" + tempID + "_" + videoHeader.Filename
	out, err := os.Create(localPath)
	if err != nil {
		utils.HandleError(ctx, nil, "Failed to save uploaded video", http.StatusInternalServerError)
		return
	}
	defer out.Close()

	log.Printf("Received path: %v", localPath)

	if _, err = io.Copy(out, videoFile); err != nil {
		utils.HandleError(ctx, nil, "Failed to write video file", http.StatusInternalServerError)
		return
	}

	// === Upload original to S3 ===
	// originalS3Key := tempID + ".mp4"
	// originalFile, err := os.Open(localPath)
	// if err != nil {
	// 	utils.HandleError(ctx, nil, "Failed to open saved video", http.StatusInternalServerError)
	// 	return
	// }
	// defer originalFile.Close()

	// log.Printf("Received path: %v", localPath)

	// if err = utils.UploadToS3(originalFile, originalS3Key); err != nil {
	// 	utils.HandleError(ctx, nil, "Failed to upload original video", http.StatusInternalServerError)
	// 	return
	// }

	// === Compress to 360p and upload compressed version ===
	compressedPath := localPath + "_compressed.mp4"
	compressedS3Key, err := utils.UploadCompressedVideo(ctx, localPath, compressedPath)
	if err != nil {
		utils.HandleError(ctx, nil, "Failed to compress or upload video", http.StatusInternalServerError)
		return
	}

	log.Printf("Received compressedS3Key: %v", compressedS3Key)

	// === Compress to 720p and upload compressed version ===
	compressedHDPath := localPath + "_hdcompressed.mp4"
	compressedHDS3Key, err := utils.UploadHDCompressedVideo(ctx, localPath, compressedHDPath)
	if err != nil {
		utils.HandleError(ctx, nil, "Failed to compress or upload the HD video", http.StatusInternalServerError)
		return 
	}

	// === Get form fields ===
	var video Video
	video.AuthorID, err = strconv.Atoi(ctx.PostForm("author_id"))
	if err != nil {
		utils.HandleError(ctx, nil, "Invalid author_id format", http.StatusBadRequest)
		return
	}

	video.LectureID, err = strconv.Atoi(ctx.PostForm("lecture_id"))
	if err != nil {
		utils.HandleError(ctx, nil, "Invalid lecture_id format", http.StatusBadRequest)
		return
	}

	video.Title = ctx.PostForm("title")
	video.Description = ctx.PostForm("desc")
	video.GenID = tempID
	video.CreateAt = time.Now().Format("2006-01-02 15:04:05")
	// video.URL = originalS3Key

	// === Thumbnail generation ===
	thumbnailPath, err := utils.HandleThumbnailGeneration(ctx)
	if err != nil {
		log.Printf("Error generating thumbnail: %v", err)
		utils.HandleError(ctx, nil, "Error generating thumbnail", http.StatusInternalServerError)
		return
	}
	video.ThumbNail = thumbnailPath

	// === Insert into DB ===
	_, err = db.DB.Exec(`
		INSERT INTO videos 
		(url, author_id, gen_id, thumbnail, title, description, lecture_id, create_at, compressed_url)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		compressedHDS3Key, video.AuthorID, video.GenID, video.ThumbNail,
		video.Title, video.Description, video.LectureID, video.CreateAt, compressedS3Key,
	)
	if err != nil {
		log.Printf("Failed to insert into the database: %v", err)
		utils.HandleError(ctx, nil, "Failed to insert into the database", http.StatusInternalServerError)
		return
	}

	// Success response
	ctx.JSON(http.StatusOK, gin.H{"message": "Video was uploaded and compressed successfully"})
}