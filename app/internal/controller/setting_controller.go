package controller

import (
	"fmt"

	"github.com/bujic-movie/bujic-movie/internal/config"
	"github.com/bujic-movie/bujic-movie/internal/db"
	"github.com/bujic-movie/bujic-movie/pkg/response"
	"github.com/bujic-movie/bujic-movie/pkg/tmdb"
	"github.com/gin-gonic/gin"
)

type SettingController struct {
	tmdbClient *tmdb.Client
}

type SettingUpdateRequest struct {
	TMDBAPIKey   string `json:"tmdb_api_key"`
	TMDBLanguage string `json:"tmdb_language"`
	MoviePath    string `json:"movie_path"`
	TVPath       string `json:"tv_path"`
	DownloadPath string `json:"download_path"`
	TransferMode string `json:"transfer_mode"`
	Overwrite    string `json:"overwrite_mode"`
	AutoScrape   *bool  `json:"auto_scrape"`
	ScrapePerson *bool  `json:"scrape_person"`
	LockNFO      *bool  `json:"lock_nfo"`
}

func NewSettingController(tmdbClient *tmdb.Client) *SettingController {
	return &SettingController{tmdbClient: tmdbClient}
}

// Get returns the current active configuration
func (ctrl *SettingController) Get(c *gin.Context) {
	cfg := config.GlobalConfig
	response.Success(c, gin.H{
		"tmdb_api_key":      cfg.TMDB.APIKey,
		"tmdb_language":     cfg.TMDB.Language,
		"movie_path":        cfg.Media.MoviePath,
		"tv_path":           cfg.Media.TVPath,
		"download_path":     cfg.Media.DownloadPath,
		"transfer_mode":     cfg.Transfer.Mode,
		"overwrite_mode":    cfg.Transfer.OverwriteMode,
		"auto_scrape":       cfg.Transfer.AutoScrape,
		"scrape_person":     cfg.Transfer.ScrapePerson,
		"min_file_size_mb":  cfg.Transfer.MinFileSizeMB,
		"lock_nfo":          cfg.Media.LockNFO,
	})
}

// Update updates configuration parameters and persists them to SQLite database
func (ctrl *SettingController) Update(c *gin.Context) {
	var req SettingUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	cfg := config.GlobalConfig
	
	// Update fields if provided
	if req.TMDBAPIKey != "" {
		cfg.TMDB.APIKey = req.TMDBAPIKey
		_ = db.SaveSetting("tmdb.api_key", req.TMDBAPIKey)
		ctrl.tmdbClient.SetAPIKey(req.TMDBAPIKey)
	}
	if req.TMDBLanguage != "" {
		cfg.TMDB.Language = req.TMDBLanguage
		_ = db.SaveSetting("tmdb.language", req.TMDBLanguage)
	}
	if req.MoviePath != "" {
		cfg.Media.MoviePath = req.MoviePath
		_ = db.SaveSetting("media.movie_path", req.MoviePath)
	}
	if req.TVPath != "" {
		cfg.Media.TVPath = req.TVPath
		_ = db.SaveSetting("media.tv_path", req.TVPath)
	}
	if req.DownloadPath != "" {
		cfg.Media.DownloadPath = req.DownloadPath
		_ = db.SaveSetting("media.download_path", req.DownloadPath)
	}
	if req.TransferMode != "" {
		cfg.Transfer.Mode = req.TransferMode
		_ = db.SaveSetting("transfer.mode", req.TransferMode)
	}
	if req.Overwrite != "" {
		cfg.Transfer.OverwriteMode = req.Overwrite
		_ = db.SaveSetting("transfer.overwrite_mode", req.Overwrite)
	}
	if req.AutoScrape != nil {
		cfg.Transfer.AutoScrape = *req.AutoScrape
		val := "false"
		if *req.AutoScrape {
			val = "true"
		}
		_ = db.SaveSetting("transfer.auto_scrape", val)
	}
	if req.ScrapePerson != nil {
		cfg.Transfer.ScrapePerson = *req.ScrapePerson
		val := "false"
		if *req.ScrapePerson {
			val = "true"
		}
		_ = db.SaveSetting("transfer.scrape_person", val)
	}
	if req.LockNFO != nil {
		cfg.Media.LockNFO = *req.LockNFO
		val := "false"
		if *req.LockNFO {
			val = "true"
		}
		_ = db.SaveSetting("media.lock_nfo", val)
	}

	response.Success(c, gin.H{
		"message": "Configuration updated successfully",
	})
}

type PasswordUpdateRequest struct {
	OldPassword          string `json:"old_password"`
	NewPassword          string `json:"new_password"`
	EncryptedOldPassword string `json:"encrypted_old_password"`
	EncryptedNewPassword string `json:"encrypted_new_password"`
	KeyID                string `json:"key_id"`
	OldIV                string `json:"old_iv"`
	NewIV                string `json:"new_iv"`
}

// UpdatePassword verifies old password and updates to new password
func (ctrl *SettingController) UpdatePassword(c *gin.Context) {
	var req PasswordUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	oldPassword := req.OldPassword
	newPassword := req.NewPassword

	// If challenge key is provided, decrypt the fields
	if req.KeyID != "" {
		key, ok := getChallenge(req.KeyID)
		if !ok {
			response.Unauthorized(c, "Invalid or expired encryption challenge key")
			return
		}
		defer deleteChallenge(req.KeyID)

		if req.EncryptedOldPassword != "" && req.OldIV != "" {
			decrypted, err := decryptWithKey(key, req.EncryptedOldPassword, req.OldIV)
			if err != nil {
				response.Unauthorized(c, fmt.Sprintf("Failed to decrypt old password: %v", err))
				return
			}
			oldPassword = decrypted
		}

		if req.EncryptedNewPassword != "" && req.NewIV != "" {
			decrypted, err := decryptWithKey(key, req.EncryptedNewPassword, req.NewIV)
			if err != nil {
				response.Unauthorized(c, fmt.Sprintf("Failed to decrypt new password: %v", err))
				return
			}
			newPassword = decrypted
		}
	}

	if oldPassword == "" || newPassword == "" {
		response.BadRequest(c, "Both old and new passwords are required")
		return
	}

	cfg := config.GlobalConfig
	if oldPassword != cfg.Server.Password {
		response.Unauthorized(c, "Current password verification failed")
		return
	}

	// Persist to database
	if err := db.SaveSetting("server.password", newPassword); err != nil {
		response.InternalServerError(c, fmt.Sprintf("Failed to persist new password: %v", err))
		return
	}

	// Update memory configuration
	cfg.Server.Password = newPassword

	response.Success(c, gin.H{
		"message": "Password updated successfully",
	})
}
