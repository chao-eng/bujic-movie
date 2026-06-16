package controller

import (
	"strconv"

	"github.com/bujic-movie/bujic-movie/internal/repository"
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
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit

	medias, err := ctrl.mediaRepo.List(offset, limit)
	if err != nil {
		response.InternalServerError(c, err.Error())
		return
	}

	response.Success(c, medias)
}

// Search searches for media files by title query
func (ctrl *MediaController) Search(c *gin.Context) {
	query := c.Query("q")
	if query == "" {
		response.BadRequest(c, "Query parameter 'q' is required")
		return
	}

	medias, err := ctrl.mediaRepo.Search(query)
	if err != nil {
		response.InternalServerError(c, err.Error())
		return
	}

	response.Success(c, medias)
}

// Delete removes a media file entry from database
func (ctrl *MediaController) Delete(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid media ID")
		return
	}

	err = ctrl.mediaRepo.Delete(uint(id))
	if err != nil {
		response.InternalServerError(c, err.Error())
		return
	}

	response.Success(c, gin.H{
		"message": "Media record deleted successfully",
	})
}
