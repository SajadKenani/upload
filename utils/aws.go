package utils

import (
	"context"
	"fmt"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

var (
	S3Client  *s3.Client
	uploader  *manager.Uploader
	FileName  = uuid.New().String() + ".mp4"
	bucket    string
)

func InitS3() error {
    if uploader != nil {
        log.Println("S3 uploader already initialized.")
        return nil
    }

	bucket = os.Getenv("AWS_BUCKET_NAME")
	if bucket == "" {
		return fmt.Errorf("AWS_BUCKET_NAME environment variable is not set")
	}
    cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion("eu-central-1"))
    if err != nil {
        log.Printf("unable to load SDK config: %v", err)
        return err
    }

	S3Client = s3.NewFromConfig(cfg)
	uploader = manager.NewUploader(S3Client)
    if uploader == nil {
        return fmt.Errorf("failed to initialize S3 uploader")
    }

    log.Println("S3 uploader initialized successfully")
    return nil
}


func UploadToS3(file multipart.File, fileName string) error {
    if file == nil {
        return fmt.Errorf("file is nil")
    }

    if uploader == nil {
        return fmt.Errorf("S3 uploader not initialized")
    }

    log.Printf("Uploading file: %s to bucket: %s", fileName, bucket)

    result, err := uploader.Upload(context.TODO(), &s3.PutObjectInput{
        Bucket:      aws.String(bucket),
        Key:         aws.String(fileName),
        Body:        file,
        ContentType: aws.String("video/mp4"),
    })

    if err != nil {
        log.Printf("Failed to upload file to S3: %v", err)
        return fmt.Errorf("failed to upload file to S3: %w", err)
    }

    log.Printf("Successfully uploaded file: %s to S3. ETag: %s", fileName, *result.ETag)
    return nil
}


func GeneratePreSignedURL(nfileName string) (string, error) {
	// Create a presigned URL for the file to be accessible temporarily
	presigner := s3.NewPresignClient(S3Client)

	presignedURL, err := presigner.PresignGetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: aws.String(os.Getenv("AWS_BUCKET_NAME")),
		Key:    aws.String(nfileName),
	}, s3.WithPresignExpires(1*time.Hour))

	if err != nil {
		return "", fmt.Errorf("failed to sign request: %w", err)
	}
	return presignedURL.URL, nil
}

func UploadCompressedVideo(ctx *gin.Context, inputPath, outputPath string) (string, error) {
	// Compress the video using ffmpeg
	cmd := exec.Command("ffmpeg", "-i", inputPath, "-s", "640x360", "-vcodec", "libx264", "-crf", "28", outputPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("compression failed: %w", err)
	}

	// Open the compressed file
	file, err := os.Open(outputPath)
	if err != nil {
		return "", fmt.Errorf("can't open compressed file: %w", err)
	}
	defer file.Close()

	// Generate a unique key for the compressed video and upload it to S3
	s3Key := uuid.New().String() + ".mp4"
	if err := UploadToS3(file, s3Key); err != nil {
		log.Printf("Error uploading compressed video to S3: %v", err)
		return "", fmt.Errorf("upload to S3 failed: %w", err)
	}

	return s3Key, nil
}

func UploadHDCompressedVideo(ctx *gin.Context, inputPath, outputPath string) (string, error) {
	cmd := exec.Command("ffmpeg", "-i", inputPath, "-vf", "scale=720:-2", "-c:v", "libx264", "-crf", "28", outputPath)
	cmd.Stdout = os.Stdout
	cmd.Stdout = os.Stderr

	err := cmd.Run();
	if err != nil {
		return "", fmt.Errorf("compression failed!: %w", err)
	}

	file, err := os.Open(outputPath)
	if err != nil {
		return "", fmt.Errorf("can't open compressed file: %w", err)
	}

	// Generate a unique key for the compressed video and upload it to S3
	s3Key := uuid.New().String() + ".mp4"
	if err := UploadToS3(file, s3Key); err != nil {
		log.Printf("Error uploading compressed video to S3: %v", err)
		return "", fmt.Errorf("upload to S3 failed: %w", err)
	}

	return s3Key, nil

}

func HandleThumbnailGeneration(ctx *gin.Context) (string, error) {
	// Handle thumbnail file upload
	thumbnail, err := ctx.FormFile("thumbnail")
	if err != nil {
		HandleError(ctx, err, "Failed to get the thumbnail", http.StatusInternalServerError)
		return "", err
	}

	// Validate the thumbnail content type
	if !strings.HasPrefix(thumbnail.Header.Get("Content-Type"), "image/") {
		HandleError(ctx, nil, "Only image files are allowed", http.StatusBadRequest)
		return "", fmt.Errorf("only image files are allowed")
	}

	// Ensure the uploads directory exists
	if err := os.MkdirAll("./uploads", os.ModePerm); err != nil {
		HandleError(ctx, err, "Failed to create uploads directory", http.StatusInternalServerError)
		return "", err
	}

	// Save the uploaded thumbnail to the file system
	thumbnailPath := "./uploads/" + thumbnail.Filename
	if err := ctx.SaveUploadedFile(thumbnail, thumbnailPath); err != nil {
		HandleError(ctx, err, "Failed to save the thumbnail", http.StatusBadRequest)
		return "", err
	}

	log.Printf("Thumbnail saved at: %s", thumbnailPath)
	return thumbnailPath, nil
}

func HandleVideoRemovingFromAWS(ctx *gin.Context, originalKey, compressedKey string) error {
	// Ensure the S3 client is initialized
	if S3Client == nil {
		return fmt.Errorf("S3 client not initialized")
	}

	// Delete both original and compressed videos from S3
	for _, key := range []string{originalKey, compressedKey} {
		_, err := S3Client.DeleteObject(context.TODO(), &s3.DeleteObjectInput{
			Bucket: aws.String(os.Getenv("AWS_BUCKET_NAME")),
			Key:    aws.String(key),
		})
		if err != nil {
			log.Printf("failed to delete object %s: %v", key, err)
			return err
		}
	}

	return nil
}