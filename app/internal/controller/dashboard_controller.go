package controller

import (
	"github.com/bujic-movie/bujic-movie/internal/repository"
	"github.com/bujic-movie/bujic-movie/pkg/fileutil"
	"github.com/bujic-movie/bujic-movie/pkg/response"
	"github.com/gin-gonic/gin"
)

type DashboardController struct {
	mediaRepo     repository.MediaRepository
	historyRepo   repository.TransferHistoryRepository
	mediaCardRepo repository.MediaCardRepository
}

func NewDashboardController(
	mediaRepo repository.MediaRepository,
	historyRepo repository.TransferHistoryRepository,
	mediaCardRepo repository.MediaCardRepository,
) *DashboardController {
	return &DashboardController{
		mediaRepo:     mediaRepo,
		historyRepo:   historyRepo,
		mediaCardRepo: mediaCardRepo,
	}
}

func (ctrl *DashboardController) GetStats(c *gin.Context) {
	movieCount, _ := ctrl.mediaRepo.Count("movie")
	tvCount, _ := ctrl.mediaRepo.Count("tv")

	// Count pending files in DownloadPaths of all media cards
	var pendingCount int64
	if ctrl.mediaCardRepo != nil {
		cards, err := ctrl.mediaCardRepo.List()
		if err == nil {
			uniqueFiles := make(map[string]bool)
			for _, card := range cards {
				if card.DownloadPath == "" {
					continue
				}
				files, err := fileutil.FindFiles(card.DownloadPath, fileutil.IsVideo)
				if err == nil {
					for _, f := range files {
						uniqueFiles[f] = true
					}
				}
			}
			pendingCount = int64(len(uniqueFiles))
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
