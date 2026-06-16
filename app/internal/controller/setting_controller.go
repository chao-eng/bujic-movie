package controller

import (
	"github.com/bujic-movie/bujic-movie/internal/config"
	"github.com/bujic-movie/bujic-movie/pkg/response"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

type SettingController struct{}

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

func NewSettingController() *SettingController {
	return &SettingController{}
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

// Update updates configuration parameters and persists them to yaml config file
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
		viper.Set("tmdb.api_key", req.TMDBAPIKey)
	}
	if req.TMDBLanguage != "" {
		cfg.TMDB.Language = req.TMDBLanguage
		viper.Set("tmdb.language", req.TMDBLanguage)
	}
	if req.MoviePath != "" {
		cfg.Media.MoviePath = req.MoviePath
		viper.Set("media.movie_path", req.MoviePath)
	}
	if req.TVPath != "" {
		cfg.Media.TVPath = req.TVPath
		viper.Set("media.tv_path", req.TVPath)
	}
	if req.DownloadPath != "" {
		cfg.Media.DownloadPath = req.DownloadPath
		viper.Set("media.download_path", req.DownloadPath)
	}
	if req.TransferMode != "" {
		cfg.Transfer.Mode = req.TransferMode
		viper.Set("transfer.mode", req.TransferMode)
	}
	if req.Overwrite != "" {
		cfg.Transfer.OverwriteMode = req.Overwrite
		viper.Set("transfer.overwrite_mode", req.Overwrite)
	}
	if req.AutoScrape != nil {
		cfg.Transfer.AutoScrape = *req.AutoScrape
		viper.Set("transfer.auto_scrape", *req.AutoScrape)
	}

	// Persist back to configurations file
	if err := viper.WriteConfig(); err != nil {
		// If config file is not yet created, we write to default path
		_ = viper.SafeWriteConfig()
	}

	response.Success(c, gin.H{
		"message": "Configuration updated successfully",
	})
}
