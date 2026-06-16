package controller

import (
	"context"

	"github.com/bujic-movie/bujic-movie/internal/service"
	"github.com/bujic-movie/bujic-movie/pkg/response"
	"github.com/gin-gonic/gin"
)

type ScrapeController struct {
	scrapeService service.ScrapeService
}

type ScrapeRequest struct {
	Path      string `json:"path" binding:"required"`
	Overwrite bool   `json:"overwrite"`
	MediaType string `json:"media_type"` // "", "movie", "tv"
}

func NewScrapeController(scrapeService service.ScrapeService) *ScrapeController {
	return &ScrapeController{scrapeService: scrapeService}
}

// Scrape triggers metadata scraping for a path asynchronously or synchronously
func (ctrl *ScrapeController) Scrape(c *gin.Context) {
	var req ScrapeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Path is required")
		return
	}

	// Run scrape task in background goroutine to prevent HTTP timeout
	go func() {
		_ = ctrl.scrapeService.ScrapePathWithType(context.Background(), req.Path, req.Overwrite, req.MediaType)
	}()

	response.Success(c, gin.H{
		"message": "Scrape task started in background",
	})
}
