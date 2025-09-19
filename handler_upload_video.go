package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	// Limiting the request file size
	r.Body = http.MaxBytesReader(w, r.Body, 1<<30)

	// parsing the video ID from request
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	// authenticate the user
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	// Check if video owner and user are same ?
	vid, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't get video from database", err)
		return
	}

	if vid.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "You don't own this video", err)
		return
	}

	// parse the uploaded video from form data
	vidFile, fileHeader, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't parse file", err)
		return
	}
	defer vidFile.Close()

	fileType := fileHeader.Header.Get("Content-Type")
	mType, _, err := mime.ParseMediaType(fileType)

	if mType != "video/mp4" || err != nil {
		respondWithError(w, http.StatusBadRequest, "malformed file extension", err)
		return
	}

	// Save uploaded file temporarily
	tmpFile, err := os.CreateTemp("", "tubely_upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error creating file", err)
		return
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	io.Copy(tmpFile, vidFile)

	tmpFile.Seek(0, io.SeekStart)

	// getting file extension
	fileExt := strings.Split(fileType, "/")
	if len(fileExt) < 2 {
		respondWithError(w, http.StatusBadRequest, "malformed file format", err)
		return
	}

	// Creating a filename key to store in aws
	b := make([]byte, 32)
	_, err = rand.Read(b)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error creating filename key for video", err)
		return
	}
	key := base64.RawURLEncoding.EncodeToString(b)
	fileKey := fmt.Sprintf("%s.%s", key, fileExt[1])

	// put the video in S3
	cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &fileKey,
		Body:        tmpFile,
		ContentType: &mType,
	})

	// Updating the videoURL in database
	vidURL := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, fileKey)
	vid.VideoURL = &vidURL

	err = cfg.db.UpdateVideo(vid)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error updating video", err)
		return
	}

	updatedVid, err := cfg.db.GetVideo(videoID)
	if err != nil {
		if err == sql.ErrNoRows {
			respondWithError(w, http.StatusBadRequest, "error getting video from db", err)
			return
		}
		respondWithError(w, http.StatusInternalServerError, "error getting video from db", err)
		return
	}

	respondWithJSON(w, http.StatusOK, updatedVid)

}
