package controller

import (
	"strconv"

	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/bujic-movie/bujic-movie/internal/service"
	"github.com/bujic-movie/bujic-movie/pkg/response"
	"github.com/gin-gonic/gin"
)

type MediaCardController struct {
	mediaCardService service.MediaCardService
}

func NewMediaCardController(mediaCardService service.MediaCardService) *MediaCardController {
	return &MediaCardController{mediaCardService: mediaCardService}
}

func (ctrl *MediaCardController) List(c *gin.Context) {
	cards, err := ctrl.mediaCardService.List()
	if err != nil {
		response.InternalServerError(c, err.Error())
		return
	}
	response.Success(c, cards)
}

func (ctrl *MediaCardController) GetByID(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid ID")
		return
	}
	card, err := ctrl.mediaCardService.GetByID(uint(id))
	if err != nil {
		response.NotFound(c, "Card not found")
		return
	}
	response.Success(c, card)
}

func (ctrl *MediaCardController) Create(c *gin.Context) {
	var card entity.MediaCard
	if err := c.ShouldBindJSON(&card); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if card.Name == "" || card.DownloadPath == "" || card.ArchivePath == "" {
		response.BadRequest(c, "Name, download_path and archive_path are required")
		return
	}
	if card.MediaType != "movie" && card.MediaType != "tv" {
		response.BadRequest(c, "media_type must be 'movie' or 'tv'")
		return
	}
	if err := ctrl.mediaCardService.Create(&card); err != nil {
		response.InternalServerError(c, err.Error())
		return
	}
	response.Success(c, card)
}

func (ctrl *MediaCardController) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid ID")
		return
	}
	var card entity.MediaCard
	if err := c.ShouldBindJSON(&card); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	card.ID = uint(id)
	if card.MediaType != "" && card.MediaType != "movie" && card.MediaType != "tv" {
		response.BadRequest(c, "media_type must be 'movie' or 'tv'")
		return
	}
	if err := ctrl.mediaCardService.Update(&card); err != nil {
		response.InternalServerError(c, err.Error())
		return
	}
	response.Success(c, card)
}

func (ctrl *MediaCardController) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid ID")
		return
	}
	if err := ctrl.mediaCardService.Delete(uint(id)); err != nil {
		response.InternalServerError(c, err.Error())
		return
	}
	response.Success(c, gin.H{"message": "Card deleted successfully"})
}

func (ctrl *MediaCardController) SetDefault(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid ID")
		return
	}
	if err := ctrl.mediaCardService.SetDefault(uint(id)); err != nil {
		response.InternalServerError(c, err.Error())
		return
	}
	response.Success(c, gin.H{"message": "Default card set successfully"})
}

func (ctrl *MediaCardController) GetDefault(c *gin.Context) {
	card, err := ctrl.mediaCardService.GetDefault()
	if err != nil {
		response.NotFound(c, "No default card found")
		return
	}
	response.Success(c, card)
}
