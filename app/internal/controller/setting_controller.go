package controller

import (
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
		"min_file_size_mb":  cfg.Transfer.MinFileSizeMB,
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

	response.Success(c, gin.H{
		"message": "Configuration updated successfully",
	})
}
