package controller

import (
	"github.com/bujic-movie/bujic-movie/internal/config"
	"github.com/bujic-movie/bujic-movie/internal/repository"
	"github.com/bujic-movie/bujic-movie/pkg/fileutil"
	"github.com/bujic-movie/bujic-movie/pkg/response"
	"github.com/gin-gonic/gin"
)

type DashboardController struct {
	mediaRepo   repository.MediaRepository
	historyRepo repository.TransferHistoryRepository
}

func NewDashboardController(mediaRepo repository.MediaRepository, historyRepo repository.TransferHistoryRepository) *DashboardController {
	return &DashboardController{
		mediaRepo:   mediaRepo,
		historyRepo: historyRepo,
	}
}

func (ctrl *DashboardController) GetStats(c *gin.Context) {
	movieCount, _ := ctrl.mediaRepo.Count("movie")
	tvCount, _ := ctrl.mediaRepo.Count("tv")

	// Count pending files in DownloadPath
	var pendingCount int64
	cfg := config.GlobalConfig
	if cfg != nil && cfg.Media.DownloadPath != "" {
		files, err := fileutil.FindFiles(cfg.Media.DownloadPath, fileutil.IsVideo)
		if err == nil {
			pendingCount = int64(len(files))
		}
	}

	// Calculate success rate from transfer history
	successCount, _ := ctrl.historyRepo.Count("success")
	totalCount, _ := ctrl.historyRepo.CountAll()

	var successRate float64 = 100.0
	if totalCount > 0 {
		successRate = (float64(successCount) / float64(totalCount)) * 100.0
	}

	response.Success(c, gin.H{
		"movie_count":   movieCount,
		"tv_count":      tvCount,
		"pending_count": pendingCount,
		"success_rate":  successRate,
	})
}
