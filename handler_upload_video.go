package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

type Video struct {
	Streams []Streams `json:"streams"`
}
type Disposition struct {
	Default         int `json:"default"`
	Dub             int `json:"dub"`
	Original        int `json:"original"`
	Comment         int `json:"comment"`
	Lyrics          int `json:"lyrics"`
	Karaoke         int `json:"karaoke"`
	Forced          int `json:"forced"`
	HearingImpaired int `json:"hearing_impaired"`
	VisualImpaired  int `json:"visual_impaired"`
	CleanEffects    int `json:"clean_effects"`
	AttachedPic     int `json:"attached_pic"`
	TimedThumbnails int `json:"timed_thumbnails"`
	NonDiegetic     int `json:"non_diegetic"`
	Captions        int `json:"captions"`
	Descriptions    int `json:"descriptions"`
	Metadata        int `json:"metadata"`
	Dependent       int `json:"dependent"`
	StillImage      int `json:"still_image"`
}
type Tags struct {
	Language    string `json:"language"`
	HandlerName string `json:"handler_name"`
	VendorID    string `json:"vendor_id"`
	Encoder     string `json:"encoder"`
	Timecode    string `json:"timecode"`
}
type Tags0 struct {
	Language    string `json:"language"`
	HandlerName string `json:"handler_name"`
	VendorID    string `json:"vendor_id"`
}
type Tags1 struct {
	Language    string `json:"language"`
	HandlerName string `json:"handler_name"`
	Timecode    string `json:"timecode"`
}
type Streams struct {
	Index              int         `json:"index"`
	CodecName          string      `json:"codec_name,omitempty"`
	CodecLongName      string      `json:"codec_long_name,omitempty"`
	Profile            string      `json:"profile,omitempty"`
	CodecType          string      `json:"codec_type"`
	CodecTagString     string      `json:"codec_tag_string"`
	CodecTag           string      `json:"codec_tag"`
	Width              int         `json:"width,omitempty"`
	Height             int         `json:"height,omitempty"`
	CodedWidth         int         `json:"coded_width,omitempty"`
	CodedHeight        int         `json:"coded_height,omitempty"`
	ClosedCaptions     int         `json:"closed_captions,omitempty"`
	FilmGrain          int         `json:"film_grain,omitempty"`
	HasBFrames         int         `json:"has_b_frames,omitempty"`
	SampleAspectRatio  string      `json:"sample_aspect_ratio,omitempty"`
	DisplayAspectRatio string      `json:"display_aspect_ratio,omitempty"`
	PixFmt             string      `json:"pix_fmt,omitempty"`
	Level              int         `json:"level,omitempty"`
	ColorRange         string      `json:"color_range,omitempty"`
	ColorSpace         string      `json:"color_space,omitempty"`
	ColorTransfer      string      `json:"color_transfer,omitempty"`
	ColorPrimaries     string      `json:"color_primaries,omitempty"`
	ChromaLocation     string      `json:"chroma_location,omitempty"`
	FieldOrder         string      `json:"field_order,omitempty"`
	Refs               int         `json:"refs,omitempty"`
	IsAvc              string      `json:"is_avc,omitempty"`
	NalLengthSize      string      `json:"nal_length_size,omitempty"`
	ID                 string      `json:"id"`
	RFrameRate         string      `json:"r_frame_rate"`
	AvgFrameRate       string      `json:"avg_frame_rate"`
	TimeBase           string      `json:"time_base"`
	StartPts           int         `json:"start_pts"`
	StartTime          string      `json:"start_time"`
	DurationTs         int         `json:"duration_ts"`
	Duration           string      `json:"duration"`
	BitRate            string      `json:"bit_rate,omitempty"`
	BitsPerRawSample   string      `json:"bits_per_raw_sample,omitempty"`
	NbFrames           string      `json:"nb_frames"`
	ExtradataSize      int         `json:"extradata_size"`
	Disposition        Disposition `json:"disposition"`
	Tags               Tags        `json:"tags0,omitempty"`
	SampleFmt          string      `json:"sample_fmt,omitempty"`
	SampleRate         string      `json:"sample_rate,omitempty"`
	Channels           int         `json:"channels,omitempty"`
	ChannelLayout      string      `json:"channel_layout,omitempty"`
	BitsPerSample      int         `json:"bits_per_sample,omitempty"`
	InitialPadding     int         `json:"initial_padding,omitempty"`
	Tags0              Tags0       `json:"tags1,omitempty"`
	Tags1              Tags1       `json:"tags2,omitempty"`
}

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

	// fast-start processing on temp path
	processedFilePath, err := processVideoForFastStart(filepath.Join("", tmpFile.Name()))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error processing file for fast start", err)
		return
	}
	ok, err := hasFastStartMoov(processedFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error verifying moov atom", err)
		return
	}
	if !ok {
		respondWithError(w, http.StatusInternalServerError, "moov atom not at start of file", nil)
		return
	}

	// get and set aspect ratio
	aspectRatio, err := getVideoAspectRatio(processedFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error getting aspect ratio of file", err)
		return
	}

	// get file for processed name
	f, err := os.Open(processedFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error opening processed file", err)
		return
	}
	defer f.Close()
	defer os.Remove(processedFilePath)

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		respondWithError(w, http.StatusInternalServerError, "error reading seeking to start in processed file", err)
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
	fileKey := fmt.Sprintf("%s/%s.%s", aspectRatio, key, fileExt[1])

	// put the video in S3
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	log.Printf("Uploading to bucket: %s", cfg.s3Bucket)
	// use ctx in PutObject
	if _, err := cfg.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &fileKey,
		Body:        f,
		ContentType: &mType,
	}); err != nil {
		respondWithError(w, http.StatusInternalServerError, "s3 upload failed", err)
		return
	}

	// Updating the videoURL in database
	vidURL := fmt.Sprintf("%s/%s", cfg.s3CfDistribution, fileKey)
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

func getVideoAspectRatio(filePath string) (string, error) {
	ffProbecmd := exec.Command("ffprobe", []string{"-v", "error", "-print_format", "json", "-show_streams", filePath}...)
	var bys bytes.Buffer
	ffProbecmd.Stdout = &bys
	err := ffProbecmd.Run()
	if err != nil {
		return "", err
	}
	var v Video
	err = json.Unmarshal(bys.Bytes(), &v)
	if err != nil {
		return "", err
	}

	if len(v.Streams) == 0 {
		return "", fmt.Errorf("no streams found in video")
	}

	w, h := v.Streams[0].Width, v.Streams[0].Height
	if w == 0 || h == 0 {
		return "", fmt.Errorf("invalid dimensions")
	}

	g := gcd(w, h)
	wRatio, hRatio := w/g, h/g

	if wRatio > hRatio {
		return "landscape", nil
	} else if hRatio > wRatio {
		return "portrait", nil
	}
	return "other", nil
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func processVideoForFastStart(inPath string) (string, error) {
	// ensure the output ends with .mp4
	outPath := inPath + ".processing.mp4"

	cmd := exec.Command(
		"ffmpeg",
		"-i", inPath,
		"-c", "copy",
		"-movflags", "faststart",
		"-f", "mp4",
		outPath,
	)
	// capture stderr to see ffmpeg errors
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ffmpeg failed: %v: %s", err, stderr.String())
	}
	return outPath, nil
}

func hasFastStartMoov(filePath string) (bool, error) {
	cmd := exec.Command("ffprobe",
		"-v", "trace",
		"-i", filePath,
	)

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out // ffprobe writes trace to stderr

	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("ffprobe failed: %w", err)
	}

	lines := strings.Split(out.String(), "\n")
	for i, line := range lines {
		if strings.Contains(line, "type:'moov'") {
			// check if moov came before mdat
			for j := i + 1; j < len(lines); j++ {
				if strings.Contains(lines[j], "type:'mdat'") {
					// found mdat after moov → good
					return true, nil
				}
			}
			// found moov but no mdat after
			return false, nil
		}
		if strings.Contains(line, "type:'mdat'") {
			// mdat came before moov → bad
			return false, nil
		}
	}
	return false, fmt.Errorf("moov atom not found in %s", filePath)
}
