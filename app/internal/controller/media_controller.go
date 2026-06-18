package controller

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/bujic-movie/bujic-movie/internal/repository"
	"github.com/bujic-movie/bujic-movie/pkg/parser"
	"github.com/bujic-movie/bujic-movie/pkg/response"
	"github.com/gin-gonic/gin"
)

type MediaController struct {
	mediaRepo repository.MediaRepository
}

func NewMediaController(mediaRepo repository.MediaRepository) *MediaController {
	return &MediaController{mediaRepo: mediaRepo}
}

// List returns a paginated list of media files in the database
func (ctrl *MediaController) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "1000"))
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 1000
	}
	offset := (page - 1) * limit

	rawMedias, err := ctrl.mediaRepo.ListAll()
	if err != nil {
		response.InternalServerError(c, err.Error())
		return
	}

	grouped := ctrl.groupMedias(rawMedias)

	total := len(grouped)
	start := offset
	if start > total {
		start = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	paginated := grouped[start:end]

	response.Success(c, paginated)
}

// Search searches for media files by title query
func (ctrl *MediaController) Search(c *gin.Context) {
	query := c.Query("q")
	if query == "" {
		response.BadRequest(c, "Query parameter 'q' is required")
		return
	}

	rawMedias, err := ctrl.mediaRepo.Search(query)
	if err != nil {
		response.InternalServerError(c, err.Error())
		return
	}

	grouped := ctrl.groupMedias(rawMedias)
	response.Success(c, grouped)
}

// Delete removes a media file entry from database (and season episodes if it is TV)
func (ctrl *MediaController) Delete(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid media ID")
		return
	}

	media, err := ctrl.mediaRepo.GetByID(uint(id))
	if err != nil {
		response.InternalServerError(c, err.Error())
		return
	}

	if media.Type == "tv" {
		err = ctrl.mediaRepo.DeleteSeason(media.TMDBID, media.Season)
	} else {
		err = ctrl.mediaRepo.Delete(uint(id))
	}

	if err != nil {
		response.InternalServerError(c, err.Error())
		return
	}

	response.Success(c, gin.H{
		"message": "Media record deleted successfully",
	})
}

func (ctrl *MediaController) groupMedias(rawMedias []entity.Media) []entity.Media {
	var grouped []entity.Media
	seen := make(map[string]int)

	for _, m := range rawMedias {
		if m.Type == "tv" {
			// Heal/update season in DB if it is 0
			if m.Season == 0 {
				meta := parser.ParseFilename(m.Path)
				if meta.Season > 0 {
					m.Season = meta.Season
				} else {
					m.Season = 1
				}
				_ = ctrl.mediaRepo.Update(&m)
			}

			var key string
			if m.TMDBID > 0 {
				key = fmt.Sprintf("tv-%d-%d", m.TMDBID, m.Season)
			} else {
				key = fmt.Sprintf("tv-unmatched-%d", m.ID)
			}

			if _, ok := seen[key]; ok {
				continue
			}
			m.Title = fmt.Sprintf("%s (第 %d 季)", m.Title, m.Season)
			m.Path = filepath.Dir(m.Path)
			grouped = append(grouped, m)
			seen[key] = len(grouped) - 1
		} else {
			var key string
			if m.TMDBID > 0 {
				key = fmt.Sprintf("movie-%d", m.TMDBID)
			} else {
				key = fmt.Sprintf("movie-unmatched-%d", m.ID)
			}

			if _, ok := seen[key]; ok {
				continue
			}
			grouped = append(grouped, m)
			seen[key] = len(grouped) - 1
		}
	}
	return grouped
}
