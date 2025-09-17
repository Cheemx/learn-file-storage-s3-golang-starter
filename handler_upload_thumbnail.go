package main

import (
	"database/sql"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

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

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	// TODO: implement the upload here
	const maxMemory = 10 << 20 // 10 * 1024 * 1024 bytes (10 MB)
	err = r.ParseMultipartForm(maxMemory)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error parsing request", err)
		return
	}

	// get image data from form
	file, fileHeader, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error parsing file", err)
		return
	}

	// read all the image data in byte slice
	mediaType := fileHeader.Header.Get("Content-Type")

	// get video metadata
	vid, err := cfg.db.GetVideo(videoID)
	if err != nil {
		if err == sql.ErrNoRows {
			respondWithError(w, http.StatusBadRequest, "error getting video from db", err)
			return
		}
		respondWithError(w, http.StatusInternalServerError, "error getting video from db", err)
		return
	}

	if vid.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "trying to access another's video", err)
		return
	}

	mType, _, err := mime.ParseMediaType(mediaType)
	if (mType != "image/jpeg" && mType != "image/png") || err != nil {
		respondWithError(w, http.StatusBadRequest, "malformed file extension", err)
		return
	}

	fileExt := strings.Split(mediaType, "/")
	if len(fileExt) < 2 {
		respondWithError(w, http.StatusBadRequest, "malformed file format", err)
		return
	}
	datafile := fmt.Sprintf("%s.%s", videoID, fileExt[1])
	filename := filepath.Join(cfg.assetsRoot, datafile)
	imgFile, err := os.Create(filename)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error updating thumbnail", err)
		return
	}
	defer imgFile.Close()

	_, err = io.Copy(imgFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error copying file", err)
		return
	}

	thumbnailURL := fmt.Sprintf("http://localhost:%s/assets/%s", cfg.port, datafile)
	vid.ThumbnailURL = &thumbnailURL

	err = cfg.db.UpdateVideo(vid)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error updating video data", err)
		return
	}

	respondWithJSON(w, http.StatusOK, vid)
}
